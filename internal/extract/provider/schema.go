package provider

// TodoExtractionJSONSchema is the provider-facing strict schema. Optional
// values are nullable, but every property is required so omitted fields fail at
// the model boundary instead of being guessed by Go code.
//
// The clue is described by general fields (target/desired_outcome/context/
// open_questions) instead of a per-action_type slot vocabulary, plus a free-form
// semantics pocket for model reasoning that has no dedicated field; see
// extract.Candidate.
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
				"description": "这条线索要不要进决策。extracted：需要 principal 采取动作，交给决策环节判断怎么做。" +
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
			"desired_outcome": map[string]any{
				"type":        "string",
				"description": "这条线索最终要让现实变成什么样才算完成，自然语言一句话。写最终结果，不要写中间步骤：证据是阻塞时（无权限、缺信息、等他人），desired_outcome 仍然写解除阻塞之后真正要拿到的结果，把阻塞本身写进 description/semantics。例：产出这场会的结论并生成落到我身上的待办（当前卡在妙记无 view 权限）。",
			},
			"description": map[string]any{"type": "string", "description": "要做什么、当前进展到哪、有什么已知阻塞，自然语言、可执行导向。"},
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
			"semantics": map[string]any{
				"type":        "string",
				"description": "自由表达区：以上字段装不下、但下游判断需要的内容都写在这里，自然语言或 JSON 文本都可以。例如当前阻塞和解除条件、你的推断链和依据、候选路径与取舍、建议的下一步、你查到但不确定是否相关的线索。程序不解析这段内容，会原样带给 M5 判断环节和 M5 执行。没有要补充的写空字符串。",
			},
		},
		"required": []string{
			"action_type", "status", "title", "target", "desired_outcome", "description", "context", "open_questions",
			"commitment_strength", "assigner_open_id", "project_hint", "due_date",
			"source_message_ids", "source_quote", "semantics",
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
