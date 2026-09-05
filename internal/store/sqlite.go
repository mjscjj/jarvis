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
	return openSQLite(ctx, cfg, "WAL", "NORMAL")
}

// OpenTrackedSQLite opens a SQLite database whose main file is committed with
// the repository. DELETE journaling makes every successful write visible in
// that file instead of leaving the latest state in an untracked WAL sidecar.
func OpenTrackedSQLite(ctx context.Context, cfg config.SQLiteConfig) (*gorm.DB, error) {
	return openSQLite(ctx, cfg, "DELETE", "FULL")
}

// OpenReadOnlySQLite gives the chat sidecar live context without making it a
// second business-data writer. Mutations continue to go through the main API.
func OpenReadOnlySQLite(ctx context.Context, cfg config.SQLiteConfig) (*gorm.DB, error) {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, fmt.Errorf("open read-only sqlite: path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path %q: %w", path, err)
	}
	dsn := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(absolute),
		RawQuery: url.Values{
			"mode":          []string{"ro"},
			"_busy_timeout": []string{"5000"},
			"_foreign_keys": []string{"on"},
		}.Encode(),
	}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Warn),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only sqlite %q: %w", absolute, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get read-only sqlite sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping read-only sqlite %q: %w", absolute, err)
	}
	return db, nil
}

func openSQLite(ctx context.Context, cfg config.SQLiteConfig, journalMode, synchronous string) (*gorm.DB, error) {
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
			"_journal_mode": []string{journalMode},
			"_synchronous":  []string{synchronous},
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

// Migrate creates or updates the current schema.
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate schema: db is nil")
	}
	models := append(domain.CoreModels(), domain.CaptureModels()...)
	models = append(models, domain.ExtractModels()...)
	models = append(models, domain.ExecuteModels()...)
	models = append(models, domain.ProgressModels()...)
	models = append(models, domain.FactEngineModels()...)
	models = append(models, domain.ProactiveModels()...)
	models = append(models, domain.PluginModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
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
