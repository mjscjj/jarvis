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
	models := append(domain.CoreModels(), domain.CaptureModels()...)
	models = append(models, domain.ExtractModels()...)
	models = append(models, domain.DecideModels()...)
	models = append(models, domain.ExecuteModels()...)
	models = append(models, domain.KnowledgeModels()...)
	models = append(models, domain.ProgressModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
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
