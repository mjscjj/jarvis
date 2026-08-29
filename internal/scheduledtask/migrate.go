package scheduledtask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// SkillBindingMigration moves a persisted schedule when a built-in Skill is
// renamed or changes module ownership. This is metadata migration only: it
// does not enable, trigger or otherwise execute the schedule.
type SkillBindingMigration struct {
	FromSkill  string
	ToSkill    string
	FromModule string
	ToModule   string
}

func MigrateSkillBindings(ctx context.Context, db *gorm.DB, migrations []SkillBindingMigration) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("migrate scheduled task skill bindings: database is nil")
	}
	var updated int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, migration := range migrations {
			migration.FromSkill = strings.TrimSpace(migration.FromSkill)
			migration.ToSkill = strings.TrimSpace(migration.ToSkill)
			migration.FromModule = strings.TrimSpace(migration.FromModule)
			migration.ToModule = strings.TrimSpace(migration.ToModule)
			if migration.FromSkill == "" || migration.ToSkill == "" || migration.FromModule == "" || migration.ToModule == "" {
				return fmt.Errorf("migrate scheduled task skill bindings: skill and module names are required")
			}
			var records []domain.ScheduledTask
			if err := tx.Where("context_snapshot LIKE ?", "%\"skill\":\""+migration.FromSkill+"\"%").Find(&records).Error; err != nil {
				return fmt.Errorf("list schedules for skill %s: %w", migration.FromSkill, err)
			}
			for _, record := range records {
				var snapshot map[string]any
				if err := json.Unmarshal(record.ContextSnapshot, &snapshot); err != nil {
					return fmt.Errorf("decode scheduled task %d context: %w", record.ID, err)
				}
				if snapshot["skill"] != migration.FromSkill || snapshot["module"] != migration.FromModule {
					continue
				}
				snapshot["skill"] = migration.ToSkill
				snapshot["module"] = migration.ToModule
				encoded, err := json.Marshal(snapshot)
				if err != nil {
					return fmt.Errorf("encode scheduled task %d context: %w", record.ID, err)
				}
				result := tx.Model(&domain.ScheduledTask{}).Where("id = ?", record.ID).Updates(map[string]any{
					"context_snapshot": datatypes.JSON(encoded),
					"instruction":      strings.ReplaceAll(record.Instruction, migration.FromSkill, migration.ToSkill),
				})
				if result.Error != nil {
					return fmt.Errorf("update scheduled task %d skill binding: %w", record.ID, result.Error)
				}
				updated += result.RowsAffected
			}
		}
		return nil
	})
	return updated, err
}
