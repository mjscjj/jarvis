package execute

import "encoding/json"

// projectTaskListItem retains only the fields consumed by list presentation.
// Full evidence, questions, effects and results are read through GetTask.
func projectTaskListItem(task *TaskView) error {
	var source, result map[string]json.RawMessage
	if len(task.SourcePayload) > 0 {
		if err := json.Unmarshal(task.SourcePayload, &source); err != nil {
			return err
		}
	}
	if len(task.ExecutionResult) > 0 {
		if err := json.Unmarshal(task.ExecutionResult, &result); err != nil {
			return err
		}
	}
	capture := previewObject(source["capture"])
	names := map[string]any{}
	for _, key := range []string{"project", "group", "assigner"} {
		entity := previewObject(capture[key])
		if name := previewStrings(entity, "name"); len(name) > 0 {
			names[key] = name
		}
	}
	task.SourcePayload = nil
	if len(names) > 0 {
		task.SourcePayload, _ = json.Marshal(map[string]any{"capture": names})
	}
	preview := previewStrings(result, "stage", "summary", "error")
	question := previewObject(result["question"])
	if fields := previewStrings(question, "title"); len(fields) > 0 {
		preview["question"] = fields
	}
	waiting := previewObject(result["waiting"])
	if fields := previewStrings(waiting, "reason", "wake_at"); len(fields) > 0 {
		preview["waiting"] = fields
	}
	task.ExecutionResult = nil
	if len(preview) > 0 {
		task.ExecutionResult, _ = json.Marshal(preview)
	}
	return nil
}

// Only the list preview is shortened; stored content and detail reads are intact.
func previewStrings(object map[string]json.RawMessage, keys ...string) map[string]any {
	result := map[string]any{}
	for _, key := range keys {
		var value string
		if json.Unmarshal(object[key], &value) == nil && value != "" {
			chars := []rune(value)
			if len(chars) > 300 {
				value = string(chars[:300]) + "…"
			}
			result[key] = value
		}
	}
	return result
}

// Optional presentation fields with another shape are simply not displayed.
// Unrelated semantic values remain raw, including numbers outside float64.
func previewObject(raw json.RawMessage) map[string]json.RawMessage {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}
