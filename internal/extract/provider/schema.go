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
				"description": "线索性质和展示提示，不是 M5 的执行路线。使用小写蛇形标识符；优先用常见类型：code_change/summary_post/investigate/schedule_meeting/reply_message/doc_write/manual_followup，确实不属于时用 other 或自拟贴切标识符。",
			},
			"status": map[string]any{
				"type": "string",
				"enum": []string{"extracted", "observing"},
				"description": "Task 准入结论。extracted：存在需要 Principal 或 Jarvis 介入的未闭环结果，值得启动 M5；" +
					"observing：值得记住，但当前没有需要 Principal 或 Jarvis 推进的缺口。" +
					"价值、责任或实现方式仍有不确定性不妨碍 extracted，但必须有可信的未闭环事项；只是可能有用不能准入。",
			},
			"title": map[string]any{"type": "string", "description": "一句话说清这件事，用于展示。"},
			"target": map[string]any{
				"type":        "string",
				"description": "这件事作用的对象/主题，作为去重标识。例：agent-runtime 鉴权重构 / Bax 融合讨论会议 / 采集死锁问题。",
			},
			"project_hint": map[string]any{
				"type":        "string",
				"description": "所属项目的名称或代号；无法判断时写空字符串。",
			},
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
				"description": "开放的准入简报，自然语言或 JSON 文本均可，程序不解析并原样交给 M5。写清为什么与 Principal 有关、哪里尚未闭环、当前责任人、已核验事实、status 依据和剩余不确定性；不要写执行计划、候选方案、具体副作用或伪造的最终完成标准。",
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
				"description": "这批消息里通过 Task 准入判断、值得留下的线索：需要 Principal/Jarvis 介入的写 extracted，只值得记住的写 observing；闲聊、无新增事实和完全无关内容不输出。",
			},
		},
		"required": []string{"candidates"},
	}
}
