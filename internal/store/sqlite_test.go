package store

import (
	"context"
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

func TestMigrateBackfillsActivityOnce(t *testing.T) {
	db, err := OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = Close(db) })
	if err := db.AutoMigrate(&domain.Project{}, &domain.Person{}); err != nil {
		t.Fatalf("prepare referenced tables: %v", err)
	}
	statements := []string{
		`CREATE TABLE key_matter (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, status TEXT NOT NULL DEFAULT "",
			summary TEXT, project_id INTEGER, due_at DATETIME, closed_at DATETIME,
			last_progress_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT fk_key_matter_project FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE managed_resource (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, resource_type TEXT NOT NULL DEFAULT "link",
			url TEXT, description TEXT, person_id INTEGER, project_id INTEGER,
			link_principal NUMERIC NOT NULL DEFAULT false, is_active NUMERIC NOT NULL DEFAULT true,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT fk_managed_resource_person FOREIGN KEY (person_id) REFERENCES person(id) ON DELETE SET NULL,
			CONSTRAINT fk_managed_resource_project FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL
		)`,
		`INSERT INTO key_matter (title, status, last_progress_at, created_at, updated_at)
		 VALUES ('事项', '进行中', '2026-08-06 08:00:00', '2026-08-01 08:00:00', '2026-08-07 08:00:00')`,
		`INSERT INTO managed_resource (title, resource_type, created_at, updated_at)
		 VALUES ('资料', 'doc', '2026-08-01 09:00:00', '2026-08-07 09:00:00')`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare old schema: %v", err)
		}
	}
	if err := migrateActivityColumns(db); err != nil {
		t.Fatalf("migrateActivityColumns() error = %v", err)
	}
	var matter domain.KeyMatter
	if err := db.First(&matter).Error; err != nil {
		t.Fatalf("load key matter: %v", err)
	}
	if got := matter.LastActiveAt.UTC().Format("2006-01-02 15:04:05"); got != "2026-08-06 08:00:00" {
		t.Fatalf("key matter last_active_at = %s", got)
	}
	var resource domain.ManagedResource
	if err := db.First(&resource).Error; err != nil {
		t.Fatalf("load managed resource: %v", err)
	}
	if got := resource.LastActiveAt.UTC().Format("2006-01-02 15:04:05"); got != "2026-08-07 09:00:00" {
		t.Fatalf("resource last_active_at = %s", got)
	}
	touchedAt := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	if err := db.Model(&domain.ManagedResource{}).Where("id = ?", resource.ID).UpdateColumn("last_active_at", touchedAt).Error; err != nil {
		t.Fatalf("touch resource fixture: %v", err)
	}
	if err := migrateActivityColumns(db); err != nil {
		t.Fatalf("second migrateActivityColumns() error = %v", err)
	}
	if err := db.First(&resource, resource.ID).Error; err != nil {
		t.Fatalf("reload managed resource: %v", err)
	}
	if !resource.LastActiveAt.Equal(touchedAt) {
		t.Fatalf("second migration overwrote last_active_at = %s", resource.LastActiveAt)
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
	models = append(models, domain.KnowledgeModels()...)
	models = append(models, domain.ProgressModels()...)
	models = append(models, domain.FactEngineModels()...)
	return append(models, domain.ProactiveModels()...)
}
