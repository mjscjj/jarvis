package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"jarvis/internal/contextpack"
)

// Upgrade the retired material representation once at startup. Readers only
// understand the current format; no live world data is used to rebuild history.
func migrateMaterialPackets(db *gorm.DB) error {
	for _, target := range []struct{ table, column, path string }{
		{"todo", "content", "$"}, {"task", "source_payload", "$"}, {"todo_event", "snapshot", "$.content"},
	} {
		if !db.Migrator().HasTable(target.table) || !db.Migrator().HasColumn(target.table, target.column) {
			continue
		}
		var after uint64
		for {
			var rows []struct {
				ID   uint64
				Body string
			}
			predicate := "json_type(" + target.column + ", '" + target.path + ".materials') = 'object'"
			if err := db.Table(target.table).Select("id,"+target.column+" AS body").Where("id > ?", after).Where(predicate).Order("id").Limit(100).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				raw := []byte(row.Body)
				var envelope map[string]json.RawMessage
				if target.path != "$" {
					if err := json.Unmarshal(raw, &envelope); err != nil {
						return err
					}
					raw = envelope["content"]
				}
				flat, err := flattenMaterialPacket(raw)
				if err != nil {
					return fmt.Errorf("flatten %s %d: %w", target.table, row.ID, err)
				}
				if envelope != nil {
					envelope["content"] = flat
					flat, err = json.Marshal(envelope)
					if err != nil {
						return err
					}
				}
				updates := map[string]any{target.column: string(flat)}
				if target.table == "todo" && db.Migrator().HasColumn("todo", "source_message_ids") {
					ids := contextpack.SourceMessageIDs(flat)
					if len(ids) == 0 {
						ids = json.RawMessage(`[]`)
					}
					updates["source_message_ids"] = string(ids)
				}
				if err := db.Table(target.table).Where("id = ?", row.ID).UpdateColumns(updates).Error; err != nil {
					return err
				}
				after = row.ID
			}
		}
	}
	return nil
}

func flattenMaterialPacket(raw []byte) ([]byte, error) {
	var packet, materials map[string]json.RawMessage
	if err := json.Unmarshal(raw, &packet); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(packet["materials"], &materials); err != nil {
		return nil, err
	}
	source := materials["source"]
	capture := map[string]json.RawMessage{}
	for key, body := range materials {
		if key != "source" && key != "messages" && key != "conversation" && !strings.HasPrefix(key, "message:") {
			capture[key] = body
		}
	}
	var refs []string
	for _, key := range []string{"conversation", "messages"} {
		var ids []string
		if len(materials[key]) > 0 && string(materials[key]) != "null" {
			if err := json.Unmarshal(materials[key], &ids); err != nil {
				return nil, err
			}
		}
		refs = append(refs, ids...)
	}
	var remaining []string
	for key := range materials {
		if strings.HasPrefix(key, "message:") {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	refs = append(refs, remaining...)
	var rows []json.RawMessage
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		body, ok := materials[ref]
		if !ok {
			return nil, fmt.Errorf("missing old material %q", ref)
		}
		rows = append(rows, body)
	}
	capture["messages"], _ = json.Marshal(rows)
	notes := map[string]json.RawMessage{}
	for key, body := range packet {
		if key == "materials" {
			continue
		}
		if key == "trigger" || key == "scene" || key == "background" {
			var layer map[string]json.RawMessage
			if err := json.Unmarshal(body, &layer); err != nil {
				return nil, err
			}
			delete(layer, "refs")
			if len(layer) == 0 {
				continue
			}
			if len(layer) == 1 && layer["summary"] != nil {
				body = layer["summary"]
			} else {
				body, _ = json.Marshal(layer)
			}
		}
		notes[key] = body
	}
	captureRaw, _ := json.Marshal(capture)
	annotation, _ := json.Marshal(notes)
	// Some historical sources cite messages whose bodies were never captured.
	// Preserve that absence. Full archival reads remain available; creating a new
	// packet or attempting to execute it still requires every primary original.
	flat, err := json.Marshal(map[string]json.RawMessage{"source": source, "capture": captureRaw, "annotation": annotation})
	if err != nil {
		return nil, err
	}
	if err := contextpack.Validate(flat); err != nil {
		return nil, err
	}
	return flat, nil
}

// Older snapshots had both cited messages and a surrounding conversation.
// Consolidate them only during migration; current producers emit messages once.
func flattenLegacyCapture(raw []byte) ([]byte, error) {
	var capture map[string]json.RawMessage
	if err := json.Unmarshal(raw, &capture); err != nil {
		return nil, err
	}
	rows := []json.RawMessage{}
	seen := map[string]json.RawMessage{}
	for _, key := range []string{"conversation", "messages"} {
		var old []json.RawMessage
		if len(capture[key]) > 0 {
			if err := json.Unmarshal(capture[key], &old); err != nil {
				return nil, err
			}
		}
		for _, body := range old {
			var msg struct {
				ID string `json:"message_id"`
			}
			if err := json.Unmarshal(body, &msg); err != nil {
				return nil, err
			}
			if previous, ok := seen[msg.ID]; ok {
				// Compare JSON without decoding numbers into float64.
				var compactA, compactB json.RawMessage
				compactA, _ = json.Marshal(previous)
				compactB, _ = json.Marshal(body)
				if string(compactA) != string(compactB) {
					return nil, fmt.Errorf("conflicting legacy message %q", msg.ID)
				}
				continue
			}
			seen[msg.ID] = body
			rows = append(rows, body)
		}
	}
	delete(capture, "conversation")
	capture["messages"], _ = json.Marshal(rows)
	return json.Marshal(capture)
}
