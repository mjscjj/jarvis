package textstore

const (
	SystemPromptM3Key = "m3_system_prompt"
	SystemPromptM5Key = "m5_system_prompt"
	// SystemPromptProactiveKey drives the low-cost heartbeat agent that curates
	// Jarvis's internal world model and creates Tasks for M5 without performing
	// external business effects itself.
	SystemPromptProactiveKey = "proactive_system_prompt"
	// SystemPromptMeetingSweepKey drives the low-cost meeting collector: it
	// searches recently ended Feishu meetings and delivers each as a clue for
	// M3, performing no analysis of its own.
	SystemPromptMeetingSweepKey = "meeting_sweep_system_prompt"
	// SystemPromptMorningBriefKey drives the daily morning planning brief: a
	// read-mostly Skill agent that writes a short Feishu start-of-day plan and
	// a local Markdown full brief. It does not create Tasks.
	SystemPromptMorningBriefKey = "morning_brief_system_prompt"
	ApprovalPolicyKey           = "m5_approval_policy"
	// SystemPromptFactExtractKey drives the offline fact engine, which distils
	// long-lived facts out of material the pipeline already produced.
	SystemPromptFactExtractKey = "fact_extract_system_prompt"
	// SystemPromptFactRollupKey drives the daily compression that turns one
	// subject's detail facts for a day into a single rollup fact.
	SystemPromptFactRollupKey = "fact_rollup_system_prompt"
)

type definition struct {
	key      string
	name     string
	filename string
}

func definitions() []definition {
	return []definition{
		{key: SystemPromptM3Key, name: "M3 系统提示词", filename: "m3-system-prompt.md"},
		{key: SystemPromptM5Key, name: "M5 执行系统提示词", filename: "m5-system-prompt.md"},
		{key: SystemPromptProactiveKey, name: "主动巡视系统提示词", filename: "proactive-system-prompt.md"},
		{key: SystemPromptMeetingSweepKey, name: "会议巡扫系统提示词", filename: "meeting-sweep-system-prompt.md"},
		{key: SystemPromptMorningBriefKey, name: "晨间作战简报系统提示词", filename: "morning-brief-system-prompt.md"},
		{key: ApprovalPolicyKey, name: "M5 审批策略", filename: "m5-approval-policy.md"},
		{key: SystemPromptFactExtractKey, name: "持续世界建模提示词", filename: "fact-extract-system-prompt.md"},
		{key: SystemPromptFactRollupKey, name: "事实日压缩提示词", filename: "fact-rollup-system-prompt.md"},
	}
}
