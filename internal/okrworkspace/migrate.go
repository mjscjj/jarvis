package okrworkspace

import (
	"fmt"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// Migrate owns the optional module's schema lifecycle. Jarvis core invokes it
// only while the module is enabled and never imports the individual models.
// Disabling the module intentionally keeps existing rows intact.
func Migrate(db *gorm.DB) error {
	if err := MigrateCore(db); err != nil {
		return err
	}
	return MigrateWeeklyReport(db)
}

func MigrateCore(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate OKR core module: database is nil")
	}
	if err := db.AutoMigrate(domain.CoreModels()...); err != nil {
		return fmt.Errorf("migrate OKR core module schema: %w", err)
	}
	return nil
}

func MigrateWeeklyReport(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate weekly report module: database is nil")
	}
	if err := db.AutoMigrate(domain.WeeklyReportModels()...); err != nil {
		return fmt.Errorf("migrate weekly report module schema: %w", err)
	}
	return nil
}
