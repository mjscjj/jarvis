package okrworkspace

import (
	"fmt"
	"strings"
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
	if db.Migrator().HasColumn(&domain.KR{}, "priority") {
		return fmt.Errorf("migrate OKR core module: legacy KR priority column must be migrated to tags before startup")
	}
	// The old KR row carried a duplicate owner projection. Create the owner
	// table and move that data before GORM reconciles the current KR schema.
	if err := db.AutoMigrate(&domain.KROwner{}); err != nil {
		return fmt.Errorf("migrate OKR owner schema: %w", err)
	}
	models := domain.CoreModels()
	legacyOwnerProjection := db.Migrator().HasColumn(&domain.KR{}, "owner_name")
	if legacyOwnerProjection {
		filtered := make([]any, 0, len(models)-1)
		for _, model := range models {
			if _, isKR := model.(*domain.KR); !isKR {
				filtered = append(filtered, model)
			}
		}
		models = filtered
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("migrate OKR core module schema: %w", err)
	}
	if err := migrateLegacyKROwnerProjection(db); err != nil {
		return err
	}
	return nil
}

func MigrateIdentity(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate OKR identity state: database is nil")
	}
	if err := db.AutoMigrate(domain.IdentityModels()...); err != nil {
		return fmt.Errorf("migrate OKR identity state: %w", err)
	}
	return nil
}

func migrateLegacyKROwnerProjection(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&domain.KR{}, "owner_name") {
		return nil
	}
	type legacyKR struct {
		ID          string
		OwnerName   string
		OwnerOpenID string
	}
	selectColumns := "id, owner_name, '' AS owner_open_id"
	if db.Migrator().HasColumn(&domain.KR{}, "owner_open_id") {
		selectColumns = "id, owner_name, owner_open_id"
	}
	var records []legacyKR
	if err := db.Table(domain.KR{}.TableName()).Select(selectColumns).Where("trim(owner_name) <> ''").Scan(&records).Error; err != nil {
		return fmt.Errorf("list legacy KR owner projections: %w", err)
	}
	for _, record := range records {
		var count int64
		if err := db.Model(&domain.KROwner{}).Where("kr_id = ?", record.ID).Count(&count).Error; err != nil {
			return fmt.Errorf("count migrated KR owners for %s: %w", record.ID, err)
		}
		if count > 0 {
			continue
		}
		names := strings.FieldsFunc(record.OwnerName, func(r rune) bool {
			return r == '、' || r == ',' || r == '，' || r == ';' || r == '；'
		})
		owners := make([]OwnerView, 0, len(names))
		for index, name := range names {
			owner := OwnerView{Name: strings.TrimSpace(name)}
			if index == 0 {
				owner.OpenID = strings.TrimSpace(record.OwnerOpenID)
			}
			owners = append(owners, owner)
		}
		if err := replaceKROwners(db, record.ID, normalizeOwners(owners)); err != nil {
			return fmt.Errorf("migrate owners for KR %s: %w", record.ID, err)
		}
	}
	if db.Migrator().HasColumn(&domain.KR{}, "owner_open_id") {
		if err := db.Exec("DROP INDEX IF EXISTS idx_okr_workspace_kr_owner_open_id").Error; err != nil {
			return fmt.Errorf("drop legacy KR owner_open_id index: %w", err)
		}
		if err := db.Exec("ALTER TABLE okr_workspace_kr DROP COLUMN owner_open_id").Error; err != nil {
			return fmt.Errorf("drop legacy KR owner_open_id: %w", err)
		}
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_okr_workspace_kr_owner_name").Error; err != nil {
		return fmt.Errorf("drop legacy KR owner_name index: %w", err)
	}
	if err := db.Exec("ALTER TABLE okr_workspace_kr DROP COLUMN owner_name").Error; err != nil {
		return fmt.Errorf("drop legacy KR owner_name: %w", err)
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
		Quarter string
		Week    string
	}
	var historical []historicalWeek
	if err := db.Table("okr_workspace_progress AS progress").
		Select("objective.quarter, progress.week").
		Joins("JOIN okr_workspace_point AS point ON point.id = progress.point_id").
		Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
		Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
		Where("objective.quarter <> '' AND progress.week <> ''").
		Group("objective.quarter, progress.week").Scan(&historical).Error; err != nil {
		return fmt.Errorf("list historical weekly report scopes: %w", err)
	}
	for _, item := range historical {
		// Historical progress proves the scope existed, but it does not prove when
		// somebody explicitly opened it. Record the migration time instead of
		// manufacturing that product event from a progress timestamp.
		row := domain.WeeklyReportWeek{Quarter: item.Quarter, Week: item.Week, OpenedBy: "migration", OpenedAt: time.Now().UTC()}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("backfill weekly report scope %s/%s: %w", item.Quarter, item.Week, err)
		}
	}
	return nil
}
