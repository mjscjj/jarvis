package provider

// TodoExtractionJSONSchema is the provider-facing strict schema. Optional
// values are nullable, but every property is required so omitted fields fail at
// the model boundary instead of being guessed by Go code.
//
// The clue is described by three general fields (target/context/open_questions)
// instead of a per-action_type slot vocabulary; see extract.Candidate.
func TodoExtractionJSONSchema() map[string]any {
	stringOrNull := func() map[string]any { return map[string]any{"type": []string{"string", "null"}} }
	candidate := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action_type": map[string]any{
				"type":        "string",
				"pattern":     "^[a-z][a-z0-9_]*$",
				"description": "线索的动作类型，小写蛇形标识符。优先用常见类型：code_change/summary_post/investigate/schedule_meeting/reply_message/doc_write/notify_principal/manual_followup；确实不属于任何一类时用 other 或自拟一个贴切的标识符，不要为凑类型扭曲本意。纯粹值得我知道、无需动作的信息用 notify_principal。",
			},
			"title": map[string]any{"type": "string", "description": "一句话说清这件事，用于展示。"},
			"target": map[string]any{
				"type":        "string",
				"description": "这件事作用的对象/主题，作为去重标识。例：agent-runtime 鉴权重构 / Bax 融合讨论会议 / 采集死锁问题。",
			},
			"description": map[string]any{"type": "string", "description": "要做什么、要达成什么，自然语言、可执行导向。"},
			"context": map[string]any{
				"type":        "string",
				"description": "你主动补全的背景：归属项目/仓库、相关代码/commit/文档/会议链接、涉及的人和系统、相关历史。自然语言，把关键事实和链接直接写出。没有可留空字符串。",
			},
			"open_questions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "只有你查遍也确定不了、必须由 principal 本人决定或提供的点。能自己查到的不要写在这里。没有就空数组。",
			},
			"commitment_strength": map[string]any{"type": "string", "enum": []string{"firm", "tentative", "mentioned"}},
			"assigner_open_id":    stringOrNull(),
			"project_hint":        stringOrNull(),
			"due_date":            stringOrNull(),
			"source_message_ids": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "Evidence message IDs. At least one ID must belong to a [new] message.",
			},
			"source_quote": map[string]any{
				"type":        "string",
				"description": "Exact contiguous substring copied verbatim from one cited [new] message; never paraphrase or combine messages.",
			},
		},
		"required": []string{
			"action_type", "title", "target", "description", "context", "open_questions",
			"commitment_strength", "assigner_open_id", "project_hint", "due_date",
			"source_message_ids", "source_quote",
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
