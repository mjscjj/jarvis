//go:build integration

package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// TestMigrateMySQL validates the actual MySQL DDL. It is opt-in because it
// requires a dedicated empty database:
//
// JARVIS_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/jarvis_migration_test?parseTime=true' go test ./internal/store -run TestMigrateMySQL
func TestMigrateMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("JARVIS_TEST_MYSQL_DSN is required for MySQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := OpenMySQL(ctx, config.MySQLConfig{
		DSN:             dsn,
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: 60,
	})
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Close(db); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	models := append(domain.CoreModels(), domain.CaptureModels()...)
	models = append(models, domain.ExtractModels()...)
	models = append(models, domain.ExecuteModels()...)
	models = append(models, domain.KnowledgeModels()...)
	models = append(models, domain.ProgressModels()...)
	for _, model := range models {
		if db.Migrator().HasTable(model) {
			t.Fatalf("integration database is not empty: table %T already exists", model)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, model := range models {
		if !db.Migrator().HasTable(model) {
			t.Errorf("missing migrated table %T", model)
		}
	}

	assertColumnType(t, db, "todo", "revision", "int")
	assertColumnType(t, db, "todo", "version", "int")
	assertColumnType(t, db, "task", "version", "int")
	assertColumnType(t, db, "scan_record", "fetched_count", "int")
	assertColumnType(t, db, "scan_record", "duration_ms", "int")
	assertColumnType(t, db, "feishu_group", "related_group", "tinyint(1)")
	assertColumnType(t, db, "todo_event", "detail", "json")
	assertColumnType(t, db, "relation_fact", "description", "text")
	assertColumnType(t, db, "task_event", "detail", "json")
	assertColumnType(t, db, "project_event", "description", "text")
	assertNoColumn(t, db, "relation_fact", "predicate")
	assertNoColumn(t, db, "project_event", "event_type")
	assertNoColumn(t, db, "task", "confirmed_by")
	assertNoColumn(t, db, "task", "confirmed_at")
	assertNoColumn(t, db, "task", "decision_payload")
	if db.Migrator().HasTable("decision_audit") {
		t.Error("unexpected retired decision_audit table")
	}

	for _, table := range []string{"project", "feishu_group", "person", "todo", "task", "resource"} {
		var extra string
		if err := db.Raw(`
			SELECT EXTRA
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'updated_at'
		`, table).Scan(&extra).Error; err != nil {
			t.Fatalf("query %s.updated_at metadata: %v", table, err)
		}
		if !strings.Contains(strings.ToLower(extra), "on update current_timestamp") {
			t.Errorf("%s.updated_at EXTRA = %q, want ON UPDATE CURRENT_TIMESTAMP", table, extra)
		}
	}
}

func assertNoColumn(t *testing.T, db *gorm.DB, table, column string) {
	t.Helper()
	if db.Migrator().HasColumn(table, column) {
		t.Errorf("unexpected legacy column %s.%s", table, column)
	}
}

func assertColumnType(t *testing.T, db *gorm.DB, table, column, want string) {
	t.Helper()
	var got string
	if err := db.Raw(`
		SELECT COLUMN_TYPE
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`, table, column).Scan(&got).Error; err != nil {
		t.Fatalf("query %s.%s metadata: %v", table, column, err)
	}
	if got != want {
		t.Errorf("%s.%s type = %q, want %q", table, column, got, want)
	}
}
