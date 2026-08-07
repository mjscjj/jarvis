// Package store owns the local SQLite connection and schema lifecycle.
package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"jarvis/internal/config"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenSQLite opens the single local business database. One connection is
// deliberate: every write is serialized in-process, which matches Jarvis's
// single-user scale and avoids adding lock retries around SQLite's one-writer
// model. Agent access goes through jarvis-tools instead of opening this file.
func OpenSQLite(ctx context.Context, cfg config.SQLiteConfig) (*gorm.DB, error) {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, fmt.Errorf("open sqlite: path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory for %q: %w", absolute, err)
	}
	dsn := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(absolute),
		RawQuery: url.Values{
			"_busy_timeout": []string{"5000"},
			"_foreign_keys": []string{"on"},
			"_journal_mode": []string{"WAL"},
			"_synchronous":  []string{"NORMAL"},
		}.Encode(),
	}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Warn),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", absolute, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sqlite sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", absolute, err)
	}
	return db, nil
}

// Migrate creates or updates the current schema. The explicit activity-column
// steps are a one-time migration for the pre-activity local database; SQLite
// cannot add a column with CURRENT_TIMESTAMP as its default, so the column gets
// a stable non-null sentinel before existing rows are backfilled from their
// actual historical timestamps.
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate schema: db is nil")
	}
	if err := migrateActivityColumns(db); err != nil {
		return err
	}
	models := append(domain.CoreModels(), domain.CaptureModels()...)
	models = append(models, domain.ExtractModels()...)
	models = append(models, domain.ExecuteModels()...)
	models = append(models, domain.KnowledgeModels()...)
	models = append(models, domain.ProgressModels()...)
	models = append(models, domain.FactEngineModels()...)
	models = append(models, domain.ProactiveModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

func migrateActivityColumns(db *gorm.DB) error {
	hadKeyMatterActivity := db.Migrator().HasColumn(&domain.KeyMatter{}, "LastActiveAt")
	hadResourceActivity := db.Migrator().HasColumn(&domain.ManagedResource{}, "LastActiveAt")
	if db.Migrator().HasTable(&domain.KeyMatter{}) && !hadKeyMatterActivity {
		if err := db.Exec(`ALTER TABLE key_matter ADD COLUMN last_active_at datetime NOT NULL DEFAULT '1970-01-01 00:00:00'`).Error; err != nil {
			return fmt.Errorf("add key matter last_active_at: %w", err)
		}
		if err := db.Exec(`UPDATE key_matter SET last_active_at = COALESCE(last_progress_at, updated_at, created_at)`).Error; err != nil {
			return fmt.Errorf("backfill key matter last_active_at: %w", err)
		}
	}
	if db.Migrator().HasTable(&domain.ManagedResource{}) && !hadResourceActivity {
		if err := db.Exec(`ALTER TABLE managed_resource ADD COLUMN last_active_at datetime NOT NULL DEFAULT '1970-01-01 00:00:00'`).Error; err != nil {
			return fmt.Errorf("add managed resource last_active_at: %w", err)
		}
		if err := db.Exec(`UPDATE managed_resource SET last_active_at = COALESCE(updated_at, created_at)`).Error; err != nil {
			return fmt.Errorf("backfill managed resource last_active_at: %w", err)
		}
	}
	return nil
}

func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sqlite sql.DB for close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}
	return nil
}
