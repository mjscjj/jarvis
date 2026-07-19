package provider

// TodoExtractionJSONSchema is the provider-facing strict schema. Optional
// values are nullable, but every property is required so omitted fields fail at
// the model boundary instead of being guessed by Go code.
func TodoExtractionJSONSchema() map[string]any {
	stringOrNull := func() map[string]any { return map[string]any{"type": []string{"string", "null"}} }
	stringArrayOrNull := func() map[string]any {
		return map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": "string"}}
	}
	slotProperties := map[string]any{
		"repo_ref": stringOrNull(), "change_summary": stringOrNull(), "based_on": stringOrNull(),
		"scope": stringOrNull(), "acceptance": stringOrNull(), "source_ref": stringOrNull(),
		"target_chat_id": stringOrNull(), "summary_scope": stringOrNull(), "assignees": stringArrayOrNull(),
		"question": stringOrNull(), "lookup_sources": stringArrayOrNull(), "deliverable": stringOrNull(),
		"meeting_title": stringOrNull(), "attendees": stringArrayOrNull(), "proposed_time": stringOrNull(),
		"duration_minutes": map[string]any{"type": []string{"integer", "null"}},
		"agenda":           stringOrNull(), "meeting_room": stringOrNull(), "message_body": stringOrNull(),
		"doc_title": stringOrNull(), "followup_action": stringOrNull(),
	}
	slotNames := []string{
		"repo_ref", "change_summary", "based_on", "scope", "acceptance", "source_ref",
		"target_chat_id", "summary_scope", "assignees", "question", "lookup_sources",
		"deliverable", "meeting_title", "attendees", "proposed_time", "duration_minutes",
		"agenda", "meeting_room", "message_body", "doc_title", "followup_action",
	}
	candidate := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action_type": map[string]any{
				"type": "string",
				"enum": []string{"code_change", "summary_post", "investigate", "schedule_meeting", "reply_message", "doc_write", "manual_followup"},
			},
			"title":               map[string]any{"type": "string"},
			"description":         map[string]any{"type": "string"},
			"commitment_strength": map[string]any{"type": "string", "enum": []string{"firm", "tentative", "mentioned"}},
			"assigner_open_id":    stringOrNull(),
			"project_hint":        stringOrNull(),
			"due_date":            stringOrNull(),
			"source_message_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"source_quote":        map[string]any{"type": "string"},
			"slots":               map[string]any{"type": "object", "additionalProperties": false, "properties": slotProperties, "required": slotNames},
			"info_sufficient":     map[string]any{"type": "boolean"},
			"missing_info":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{
			"action_type", "title", "description", "commitment_strength", "assigner_open_id",
			"project_hint", "due_date", "source_message_ids", "source_quote", "slots",
			"info_sufficient", "missing_info",
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"candidates": map[string]any{"type": "array", "items": candidate},
		},
		"required": []string{"candidates"},
	}
}
