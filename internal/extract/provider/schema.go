package provider

// TodoExtractionJSONSchema is the provider-facing strict machine envelope.
// Model semantics stay in payload as opaque text; adding a semantic concept
// must not require changing this schema or the downstream Go pipeline.
func TodoExtractionJSONSchema() map[string]any {
	stringOrNull := func() map[string]any { return map[string]any{"type": []string{"string", "null"}} }
	candidate := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action_type": map[string]any{
				"type":        "string",
				"pattern":     "^[a-z][a-z0-9_]*$",
				"description": "线索的动作类型，小写蛇形标识符。优先用常见类型：code_change/summary_post/investigate/schedule_meeting/reply_message/doc_write/manual_followup；确实不属于任何一类时用 other 或自拟一个贴切的标识符，不要为凑类型扭曲本意。",
			},
			"status": map[string]any{
				"type": "string",
				"enum": []string{"extracted", "observing"},
				"description": "这条线索是否需要行动。extracted：需要采取动作，将机械物化为 Task 并交给 M5 调查、决策和执行。" +
					"observing：值得记住但不需要任何人动手——群里达成的结论或口径、别人陈述的现状、" +
					"别人负责并会自己推进的事、你查证时顺带发现的背景和约束，都属于这类。" +
					"拿不准时先问「不做会不会有事情落空」：不会就写 observing。" +
					"observing 的线索照样完整填写其余字段并附证据，它不会消失，只是不会有人去执行。",
			},
			"title": map[string]any{"type": "string", "description": "一句话说清这件事，用于展示。"},
			"target": map[string]any{
				"type":        "string",
				"description": "这件事作用的对象/主题，作为去重标识。例：agent-runtime 鉴权重构 / Bax 融合讨论会议 / 采集死锁问题。",
			},
			"project_hint": stringOrNull(),
			"source_message_ids": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "Evidence message IDs. At least one ID must belong to a [new] message.",
			},
			"source_quote": map[string]any{
				"type":        "string",
				"description": "Exact contiguous substring copied verbatim from one cited [new] message; never paraphrase or combine messages.",
			},
			"payload": map[string]any{
				"type":        "string",
				"description": "完整语义正文，自然语言或 JSON 文本均可，程序不解析并原样交给 M5。至少写清最终要达成的现实结果、当前状态与阻塞、已补全的上下文、仍需 principal 决定的点；交办人、期限、承诺强度、推断依据、候选路径等按实际情况自由表达，不要为了字段结构拆散语义。",
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
				"description": "这批消息里所有值得留下的线索，无论要不要动手：要动手的写 status=extracted，只值得记住的写 status=observing。",
			},
		},
		"required": []string{"candidates"},
	}
}
