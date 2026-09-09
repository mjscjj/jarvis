package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

func TestOpenSQLiteAndMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "jarvis.db")
	db, err := OpenSQLite(context.Background(), config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Close(db); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() error = %v", err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, model := range allModels() {
		if !db.Migrator().HasTable(model) {
			t.Errorf("missing migrated table %T", model)
		}
	}
}

func TestOpenReadOnlySQLiteRejectsWrites(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "readonly.db")
	writable, err := OpenSQLite(t.Context(), config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Exec("CREATE TABLE fixture (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := writable.Exec("INSERT INTO fixture(value) VALUES (?)", "visible").Error; err != nil {
		t.Fatal(err)
	}
	if err := Close(writable); err != nil {
		t.Fatal(err)
	}

	readonly, err := OpenReadOnlySQLite(t.Context(), config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer Close(readonly)
	var value string
	if err := readonly.Raw("SELECT value FROM fixture WHERE id = 1").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "visible" {
		t.Fatalf("value = %q, want visible", value)
	}
	if err := readonly.Exec("INSERT INTO fixture(value) VALUES (?)", "forbidden").Error; err == nil {
		t.Fatal("read-only database unexpectedly accepted a write")
	}
}

func TestOpenTrackedSQLiteWritesDirectlyToMainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tracked", "okr.db")
	db, err := OpenTrackedSQLite(t.Context(), config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenTrackedSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = Close(db) })

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal_mode pragma: %v", err)
	}
	if journalMode != "delete" {
		t.Fatalf("journal_mode = %q, want delete", journalMode)
	}
	if err := db.Exec("CREATE TABLE tracked_write (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create tracked table: %v", err)
	}
	if _, err := os.Stat(path + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("tracked database WAL exists or cannot be checked: %v", err)
	}
}

func TestSQLiteUpdatedAtUsesGORM(t *testing.T) {
	db := openMigratedSQLite(t)
	project := domain.Project{Name: "Jarvis", Role: "owner", Status: "active", Priority: 3}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	createdUpdatedAt := project.UpdatedAt
	time.Sleep(time.Millisecond)
	if err := db.Model(&project).Update("name", "Jarvis 2").Error; err != nil {
		t.Fatalf("update project: %v", err)
	}
	if !project.UpdatedAt.After(createdUpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want after %s", project.UpdatedAt, createdUpdatedAt)
	}
}

func TestMigrateRejectsNilDatabase(t *testing.T) {
	if err := Migrate(nil); err == nil {
		t.Fatal("Migrate(nil) error = nil")
	}
}

func TestMigrateReplacesUniqueTodoFingerprintIndex(t *testing.T) {
	db := openMigratedSQLite(t)
	if err := db.Migrator().DropIndex(&domain.Todo{}, "idx_todo_fingerprint"); err != nil {
		t.Fatalf("drop current Todo fingerprint index: %v", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX uk_todo_fingerprint ON todo(dedup_fingerprint)").Error; err != nil {
		t.Fatalf("create legacy Todo fingerprint index: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if db.Migrator().HasIndex(&domain.Todo{}, "uk_todo_fingerprint") {
		t.Fatal("legacy unique Todo fingerprint index still exists")
	}
	if !db.Migrator().HasIndex(&domain.Todo{}, "idx_todo_fingerprint") {
		t.Fatal("ordinary Todo fingerprint index was not created")
	}

	now := time.Now().UTC()
	for _, title := range []string{"first occurrence", "second occurrence"} {
		todo := domain.Todo{
			Title: title, Description: title, ActionType: "investigate", Target: "same target",
			SourceMessageIDs: datatypes.JSON(`["om_test"]`), SourceQuote: title,
			Status: "materialized", DedupFingerprint: "same-fingerprint",
			Revision: 1, FirstSeenAt: now, LastEvidenceAt: now,
		}
		if err := db.Create(&todo).Error; err != nil {
			t.Fatalf("create %q Todo after migration: %v", title, err)
		}
	}
}

func TestMigrateRejectsRealLegacyContextSchema(t *testing.T) {
	db, err := OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "legacy.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(db) })
	createRealLegacyContextSchema(t, db)

	err = Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "rebuild the database explicitly") {
		t.Fatalf("Migrate() error = %v, want explicit rebuild requirement", err)
	}
}

func createRealLegacyContextSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		"CREATE TABLE `todo` (`id` integer PRIMARY KEY AUTOINCREMENT,`title` text NOT NULL,`description` text NOT NULL,`action_type` text NOT NULL,`target` text NOT NULL,`context` text NOT NULL,`open_questions` JSON NOT NULL,`commitment_strength` text NOT NULL,`source_message_ids` JSON NOT NULL,`source_quote` text NOT NULL,`group_id` integer,`project_id` integer,`assigner_open_id` text,`is_leader_assigned` numeric NOT NULL DEFAULT false,`due_at` datetime,`status` text NOT NULL DEFAULT \"extracted\",`dedup_fingerprint` text NOT NULL,`context_snapshot` JSON,`extraction_result` JSON,`resolution` JSON,`revision` integer NOT NULL DEFAULT 1,`version` integer NOT NULL DEFAULT 0,`first_seen_at` datetime NOT NULL,`last_evidence_at` datetime NOT NULL,`created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,`updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,`content` JSON)",
		"CREATE TABLE `task` (`id` integer PRIMARY KEY AUTOINCREMENT,`todo_id` integer,`title` text NOT NULL,`action_type` text NOT NULL,`target` text NOT NULL DEFAULT \"\",`background_old` JSON NOT NULL,`source_clue` JSON,`plan` JSON,`source_type` text NOT NULL DEFAULT \"todo\",`source_id` integer,`occurrence_key` text,`execution_mode` text NOT NULL DEFAULT \"standard\",`status` text NOT NULL DEFAULT \"pending\",`execution_result` JSON,`summary` text,`last_progress_at` datetime,`execution_supplements` JSON,`project_id` integer,`repo_path` text,`version` integer NOT NULL DEFAULT 0,`created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,`updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,`source_payload` JSON,`background` JSON)",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create real legacy context schema: %v", err)
		}
	}
}

func openMigratedSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Close(db); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

func allModels() []any {
	models := append(domain.CoreModels(), domain.CaptureModels()...)
	models = append(models, domain.ExtractModels()...)
	models = append(models, domain.ExecuteModels()...)
	models = append(models, domain.ProgressModels()...)
	models = append(models, domain.FactEngineModels()...)
	return append(models, domain.ProactiveModels()...)
}
