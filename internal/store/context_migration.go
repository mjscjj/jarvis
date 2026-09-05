package store

import (
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"jarvis/internal/contextpack"
	"jarvis/internal/datatypes"
)

// migrateContextContent consolidates old frozen carriers once, before workers
// start. It never reads current entity/message rows to recreate past evidence.
// Each row is independently restartable; old columns are removed only after all
// rows have been copied. Unknown legacy fields are preserved as raw material.
func migrateContextContent(db *gorm.DB) error {
	if err := migrateMaterialPackets(db); err != nil {
		return err
	}
	for _, table := range []string{"todo", "task"} {
		legacy := "context_snapshot"
		if table == "task" {
			legacy = "background"
		}
		if !db.Migrator().HasColumn(table, legacy) {
			continue
		}
		var after uint64
		for {
			var rows []struct {
				ID          uint64
				Title       string
				Description string
				Capture     datatypes.JSON
				Source      datatypes.JSON
				Content     datatypes.JSON
			}
			selection := "id,title,description,context_snapshot AS capture,extraction_result AS source,content"
			if table == "task" {
				selection = "id,title,'' AS description,background AS capture,source_payload AS source"
			}
			if err := db.Table(table).Select(selection).Where("id > ?", after).Order("id").Limit(100).Find(&rows).Error; err != nil {
				return fmt.Errorf("read legacy %s: %w", table, err)
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				after = row.ID
				if table == "todo" && len(row.Content) > 0 && string(row.Content) != "null" {
					if err := contextpack.Validate(row.Content); err != nil {
						return err
					}
					continue
				}
				if table == "task" && contextpack.Validate(row.Source) == nil {
					continue
				}
				capture := json.RawMessage(row.Capture)
				if len(capture) == 0 || string(capture) == "null" {
					capture = json.RawMessage(`{}`)
				}
				source := json.RawMessage(row.Source)
				if len(source) == 0 {
					source = json.RawMessage(`null`)
				}
				// Include retired flat semantic fields too; these may contain pre-payload
				// historical observations and are not safe to discard during migration.
				if table == "todo" {
					var old map[string]any
					if err := db.Table(table).Where("id = ?", row.ID).Take(&old).Error; err != nil {
						return err
					}
					legacyFields := map[string]any{}
					for _, key := range []string{"description", "context", "open_questions", "commitment_strength"} {
						legacyFields[key] = old[key]
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(capture, &fields); err != nil {
						return err
					}
					fields["legacy_clue_fields"], _ = json.Marshal(legacyFields)
					capture, _ = json.Marshal(fields)
				}
				brief := row.Description
				if brief == "" {
					brief = row.Title
					var original struct {
						Payload string `json:"payload"`
					}
					if json.Unmarshal(source, &original) == nil && original.Payload != "" {
						brief = original.Payload
					}
				}
				capture, err := flattenLegacyCapture(capture)
				if err != nil {
					return err
				}
				packet, err := contextpack.Freeze(source, capture, brief, nil)
				if err != nil {
					return fmt.Errorf("migrate %s id=%d: %w", table, row.ID, err)
				}
				column := "content"
				if table == "task" {
					column = "source_payload"
				}
				if err := db.Table(table).Where("id = ?", row.ID).UpdateColumn(column, string(packet)).Error; err != nil {
					return err
				}
			}
		}
		columns := []string{legacy}
		for _, column := range columns {
			if db.Migrator().HasColumn(table, column) {
				if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error; err != nil {
					return fmt.Errorf("remove %s.%s: %w", table, column, err)
				}
			}
		}
	}
	for _, column := range []string{"extraction_result", "context", "open_questions", "commitment_strength"} {
		if db.Migrator().HasColumn("todo", column) {
			if err := db.Exec("ALTER TABLE todo DROP COLUMN " + column).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
