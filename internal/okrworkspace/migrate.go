package okrworkspace

import (
	"fmt"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	// Existing progress rows predate the explicit week lifecycle. Materialize
	// their scopes once during migration; runtime reads use the week table only.
	type historicalWeek struct {
		Quarter  string
		Week     string
		OpenedAt time.Time
	}
	var historical []historicalWeek
	if err := db.Table("okr_workspace_progress AS progress").
		Select("objective.quarter, progress.week, MIN(progress.created_at) AS opened_at").
		Joins("JOIN okr_workspace_point AS point ON point.id = progress.point_id").
		Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
		Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
		Where("objective.quarter <> '' AND progress.week <> ''").
		Group("objective.quarter, progress.week").Scan(&historical).Error; err != nil {
		return fmt.Errorf("list historical weekly report scopes: %w", err)
	}
	for _, item := range historical {
		openedAt := item.OpenedAt
		if openedAt.IsZero() {
			openedAt = time.Now().UTC()
		}
		row := domain.WeeklyReportWeek{Quarter: item.Quarter, Week: item.Week, OpenedBy: "migration", OpenedAt: openedAt.UTC()}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("backfill weekly report scope %s/%s: %w", item.Quarter, item.Week, err)
		}
	}
	return nil
}
