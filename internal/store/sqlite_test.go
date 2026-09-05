package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
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
