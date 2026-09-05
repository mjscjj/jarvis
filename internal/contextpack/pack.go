// Package contextpack freezes source facts and exposes small reading projections.
// The model supplies annotations and source IDs; it never copies captured bodies.
package contextpack

import (
	"encoding/json"
	"fmt"
	"sort"
)

type Object = map[string]json.RawMessage

func object(raw []byte) (Object, error) {
	var v Object
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return v, nil
}
func encoded(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// Freeze preserves the producer's complete source and capture. Only annotations
// are model-authored. No semantic field is lifted into the captured facts.
func Freeze(source, capture []byte, brief string, annotation []byte) (json.RawMessage, error) {
	if !json.Valid(source) {
		return nil, fmt.Errorf("invalid source JSON")
	}
	if _, err := object(capture); err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}
	notes := Object{}
	if len(annotation) > 0 && string(annotation) != "null" {
		var err error
		notes, err = object(annotation)
		if err != nil {
			return nil, fmt.Errorf("annotation: %w", err)
		}
	}
	if _, ok := notes["brief"]; !ok {
		notes["brief"] = encoded(brief)
	}
	raw := encoded(Object{"source": source, "capture": capture, "annotation": encoded(notes)})
	if _, err := SourceMessages(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func sourceIDs(source []byte) ([]string, error) {
	// A direct request may be any JSON value, including a string.
	var fields Object
	if json.Unmarshal(source, &fields) != nil {
		return nil, nil
	}
	raw, ok := fields["source_message_ids"]
	if !ok {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("source_message_ids: %w", err)
	}
	return ids, nil
}

func messages(capture Object) ([]json.RawMessage, error) {
	rows := []json.RawMessage{}
	if raw, ok := capture["messages"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("capture.messages: %w", err)
		}
	}
	return rows, nil
}
func messageID(raw []byte) string {
	var row struct {
		ID string `json:"message_id"`
	}
	_ = json.Unmarshal(raw, &row)
	return row.ID
}

// Validation covers only the carrier and machine-consumed message identities.
// Annotation content and arbitrary capture/source fields remain open JSON.
func Validate(raw []byte) error {
	packet, err := object(raw)
	if err != nil {
		return err
	}
	if !json.Valid(packet["source"]) {
		return fmt.Errorf("missing or invalid source")
	}
	capture, err := object(packet["capture"])
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if _, err := object(packet["annotation"]); err != nil {
		return fmt.Errorf("annotation: %w", err)
	}
	rows, err := messages(capture)
	if err != nil {
		return err
	}
	byID := map[string]bool{}
	for _, row := range rows {
		id := messageID(row)
		if id == "" || byID[id] {
			return fmt.Errorf("missing or duplicate captured message_id %q", id)
		}
		byID[id] = true
	}
	_, err = sourceIDs(packet["source"])
	if err != nil {
		return err
	}
	return nil
}

// Source returns the original source semantics without decoding their contents.
func Source(raw []byte) (json.RawMessage, error) {
	packet, err := object(raw)
	if err != nil {
		return nil, err
	}
	if !json.Valid(packet["source"]) {
		return nil, fmt.Errorf("missing or invalid source")
	}
	return packet["source"], nil
}
func SourceMessageIDs(raw []byte) json.RawMessage {
	source, err := Source(raw)
	if err != nil {
		return nil
	}
	ids, err := sourceIDs(source)
	if err != nil || len(ids) == 0 {
		return nil
	}
	return encoded(ids)
}

// SourceMessages uses only the source's validated IDs, never model annotations.
func SourceMessages(raw []byte) ([]json.RawMessage, error) {
	if err := Validate(raw); err != nil {
		return nil, err
	}
	packet, _ := object(raw)
	capture, _ := object(packet["capture"])
	rows, _ := messages(capture)
	ids, _ := sourceIDs(packet["source"])
	byID := map[string]json.RawMessage{}
	for _, row := range rows {
		byID[messageID(row)] = row
	}
	result := make([]json.RawMessage, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			body, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("source_message_id %q missing from frozen capture", id)
			}
			result = append(result, body)
			seen[id] = true
		}
	}
	return result, nil
}

// Read projects a frozen packet. Sections read complete capture fields directly;
// conversation is the single messages array. A message lookup uses its native ID.
func Read(raw []byte, section, id string) (json.RawMessage, error) {
	if err := Validate(raw); err != nil {
		return nil, err
	}
	if section != "" && id != "" {
		return nil, fmt.Errorf("context and message_id are mutually exclusive")
	}
	packet, _ := object(raw)
	capture, _ := object(packet["capture"])
	rows, _ := messages(capture)
	if id != "" {
		for _, row := range rows {
			if messageID(row) == id {
				return row, nil
			}
		}
		return nil, fmt.Errorf("unknown frozen message_id %q", id)
	}
	switch section {
	case "full":
		return append(json.RawMessage(nil), raw...), nil
	case "source":
		return packet["source"], nil
	case "annotation":
		return packet["annotation"], nil
	case "conversation":
		return encoded(rows), nil
	case "background":
		background := Object{}
		for key, body := range capture {
			if key != "messages" {
				background[key] = body
			}
		}
		return encoded(background), nil
	case "", "overview":
	default:
		if body, ok := capture[section]; ok {
			return body, nil
		}
		return nil, fmt.Errorf("unknown context %q", section)
	}
	notes, _ := object(packet["annotation"])
	result := Object{}
	for _, key := range []string{"brief", "scene"} {
		if body, ok := notes[key]; ok {
			result[key] = body
		}
	}
	primary, err := SourceMessages(raw)
	if err != nil {
		return nil, err
	}
	result["source_messages"] = encoded(primary)
	if len(primary) > 0 {
		result["source_message_ids"] = SourceMessageIDs(raw)
	} else {
		// Direct/manual/scheduled requests have no message IDs; their original
		// request is the atomic primary evidence and must remain visible.
		result["source"] = packet["source"]
	}
	available := []string{"source", "annotation", "conversation", "background", "full"}
	keys := []string{}
	for key, body := range capture {
		if key != "messages" && string(body) != "null" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	available = append(available, keys...)
	result["available_context"] = encoded(available)
	result["conversation_message_count"] = encoded(len(rows))
	if at, ok := capture["captured_at"]; ok {
		result["captured_at"] = at
	}
	result["read"] = encoded("get-task/get-todo --id ID --context SECTION; --message-id MESSAGE_ID (frozen original)")
	return encoded(result), nil
}
