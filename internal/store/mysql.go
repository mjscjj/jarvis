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
