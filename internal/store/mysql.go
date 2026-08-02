// Package store owns MySQL connection lifecycle and schema migration.
package store

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenMySQL opens and verifies the configured MySQL connection. A returned DB
// is always pingable; connection errors are not deferred until the first query.
func OpenMySQL(ctx context.Context, cfg config.MySQLConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Warn),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get mysql sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return db, nil
}

// Migrate creates or updates all tables owned by implemented milestones.
// Migration errors abort startup so the process never serves a partial schema.
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate schema: db is nil")
	}
	if err := migrateScheduledTaskV2(db); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := migrateNaturalLanguageFacts(db); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := dropActionHash(db); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	models := append(domain.CoreModels(), domain.CaptureModels()...)
	models = append(models, domain.ExtractModels()...)
	models = append(models, domain.DecideModels()...)
	models = append(models, domain.ExecuteModels()...)
	models = append(models, domain.KnowledgeModels()...)
	models = append(models, domain.ProgressModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := backfillTaskRuntimeMVP(db); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := dropRetiredColumns(db); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

// backfillTaskRuntimeMVP derives the new Task source fields from the exact Todo
// relation already stored on historical rows. It does not infer missing business
// context. New non-Todo Tasks are already complete when the Factory creates them.
func backfillTaskRuntimeMVP(db *gorm.DB) error {
	var tasks []domain.Task
	if err := db.Find(&tasks).Error; err != nil {
		return fmt.Errorf("load Tasks for runtime backfill: %w", err)
	}
	for i := range tasks {
		task := &tasks[i]
		updates := map[string]any{}
		if task.TodoID != nil {
			var todo domain.Todo
			if err := db.First(&todo, *task.TodoID).Error; err != nil {
				return fmt.Errorf("load Todo id=%d for Task id=%d runtime backfill: %w", *task.TodoID, task.ID, err)
			}
			if task.Target == "" {
				task.Target = todo.Target
				updates["target"] = todo.Target
			}
			if task.SourceType == "" || task.SourceType == "todo" {
				updates["source_type"] = "todo"
			}
			if task.SourceID == nil {
				updates["source_id"] = *task.TodoID
			}
		}
		if task.ExecutionMode == "" {
			task.ExecutionMode = "standard"
			updates["execution_mode"] = "standard"
		}
		if task.Target == "" {
			return fmt.Errorf("Task id=%d has no target and no Todo target to backfill", task.ID)
		}
		if len(updates) == 0 {
			continue
		}
		if err := db.Model(&domain.Task{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("backfill Task id=%d runtime fields: %w", task.ID, err)
		}
	}
	return nil
}

// dropRetiredColumns removes columns that no longer back any behavior. The drop
// is unconditional: none of them carry information worth preserving.
//
//   - todo.route stored the same string as todo.status.
//   - todo.confidence/risk and the decision_audit scoring columns belonged to the
//     rule engine, which no longer has an implementation.
//   - todo.ttl_at was never read or written anywhere.
//   - todo.extraction_model/prompt_version were written but never read.
//   - task.autonomy_mode was derived by a function that ignored its argument and
//     always returned "copilot"; task.approval_ref never had a writer.
//   - todo.manual_gate_required belonged to the Todo-level confirmation queue,
//     which is gone: the only human gate now lives on the Task.
func dropRetiredColumns(db *gorm.DB) error {
	retired := []struct {
		model   any
		table   string
		columns []string
	}{
		{model: &domain.Todo{}, table: "todo", columns: []string{
			"route", "confidence", "risk", "ttl_at", "extraction_model", "prompt_version",
			"manual_gate_required",
		}},
		{model: &domain.Task{}, table: "task", columns: []string{"autonomy_mode", "approval_ref"}},
		{model: &domain.DecisionAudit{}, table: "decision_audit", columns: []string{
			"confidence_eff", "risk_eff", "confidence_factors", "risk_factors", "threshold_config_version",
		}},
	}
	migrator := db.Migrator()
	for _, entry := range retired {
		if !migrator.HasTable(entry.model) {
			continue
		}
		for _, column := range entry.columns {
			if !migrator.HasColumn(entry.model, column) {
				continue
			}
			if err := migrator.DropColumn(entry.model, column); err != nil {
				return fmt.Errorf("drop retired column %s.%s: %w", entry.table, column, err)
			}
		}
	}
	return nil
}

// dropActionHash removes the abandoned action-integrity hash. It only ever
// guarded against action_type/target/plan drift, which contradicts M5's right to
// revise plan / background / decision_payload while executing (AGENTS.md §4).
// task.action_hash is NOT NULL without a default, so the column must go before
// inserts stop supplying it.
// Table names and the raw ALTER are deliberate: action_hash no longer exists on
// either model, so passing a model here would make the drop depend on GORM
// resolving an unknown name as a column rather than a struct field.
func dropActionHash(db *gorm.DB) error {
	migrator := db.Migrator()
	for _, table := range []string{"task", "decision_audit"} {
		if !migrator.HasTable(table) || !migrator.HasColumn(table, "action_hash") {
			continue
		}
		if err := db.Exec("ALTER TABLE `" + table + "` DROP COLUMN `action_hash`").Error; err != nil {
			return fmt.Errorf("drop %s.action_hash: %w", table, err)
		}
	}
	return nil
}

// migrateNaturalLanguageFacts replaces the first, over-structured relation
// and project-event schemas. Those schemas were never populated in the local
// runtime. Refuse to guess when another database contains rows.
func migrateNaturalLanguageFacts(db *gorm.DB) error {
	type legacyTable struct {
		model        any
		name         string
		legacyColumn string
	}
	tables := []legacyTable{
		{model: &domain.RelationFact{}, name: "relation_fact", legacyColumn: "predicate"},
		{model: &domain.ProjectEvent{}, name: "project_event", legacyColumn: "event_type"},
	}
	migrator := db.Migrator()
	toReplace := make([]legacyTable, 0, len(tables))
	for _, table := range tables {
		if !migrator.HasTable(table.model) || !migrator.HasColumn(table.name, table.legacyColumn) {
			continue
		}
		var count int64
		if err := db.Table(table.name).Count(&count).Error; err != nil {
			return fmt.Errorf("count legacy %s rows: %w", table.name, err)
		}
		if count != 0 {
			return fmt.Errorf("%s contains %d legacy rows; natural-language migration requires an explicit data decision", table.name, count)
		}
		toReplace = append(toReplace, table)
	}
	for _, table := range toReplace {
		if err := migrator.DropTable(table.model); err != nil {
			return fmt.Errorf("replace empty legacy %s table: %w", table.name, err)
		}
	}
	return nil
}

// migrateScheduledTaskV2 replaces the short-lived one-shot schema. The feature
// had not stored production data when the contract changed, so we intentionally
// fail instead of guessing how an old scheduled_at row should recur.
func migrateScheduledTaskV2(db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasTable(&domain.ScheduledTask{}) || !migrator.HasColumn("scheduled_task", "scheduled_at") {
		return nil
	}
	var count int64
	if err := db.Table("scheduled_task").Count(&count).Error; err != nil {
		return fmt.Errorf("count one-shot scheduled tasks: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("scheduled_task contains %d one-shot rows; recurring migration requires an explicit data decision", count)
	}
	if err := migrator.DropTable(&domain.ScheduledTask{}); err != nil {
		return fmt.Errorf("replace empty one-shot scheduled_task table: %w", err)
	}
	return nil
}

// Close closes the underlying connection pool.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get mysql sql.DB for close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close mysql: %w", err)
	}
	return nil
}
