package provider

// TodoExtractionJSONSchema is the provider-facing strict machine envelope.
// Model semantics stay in payload as opaque text; adding a semantic concept
// must not require changing this schema or the downstream Go pipeline.
func TodoExtractionJSONSchema() map[string]any {
	candidate := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action_type": map[string]any{
				"type":        "string",
				"description": "用于展示和去重的开放小写蛇形标识符；程序接受未预定义的新值。",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"extracted", "observing"},
				"description": "M3 输出的 Todo 控制状态；具体选择规则由 M3 系统提示词定义。",
			},
			"title": map[string]any{"type": "string", "description": "用于展示的简短标题。"},
			"target": map[string]any{
				"type":        "string",
				"description": "用于去重的稳定对象或主题描述。",
			},
			"project_hint": map[string]any{
				"type":        "string",
				"description": "项目名称或代号；未知时使用空字符串。",
			},
			"source_message_ids": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "真实证据消息 ID；至少一个必须对应本轮 [new] 消息。",
			},
			"source_quote": map[string]any{
				"type":        "string",
				"description": "从一条被引用的 [new] 消息中逐字连续摘录的原文。",
			},
			"payload": map[string]any{
				"type":        "string",
				"description": "原样交给下游的开放文本；程序不解析或重写。",
			},
		},
		"required": []string{
			"action_type", "status", "title", "target", "project_hint",
			"source_message_ids", "source_quote", "payload",
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"candidates": map[string]any{
				"type":        "array",
				"items":       candidate,
				"description": "M3 输出的候选线索列表；没有候选时使用空数组。",
			},
		},
		"required": []string{"candidates"},
	}
}
