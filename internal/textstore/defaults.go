package textstore

const (
	SystemPromptM3Key = "m3_system_prompt"
	SystemPromptM5Key = "m5_system_prompt"
	ApprovalPolicyKey = "m5_approval_policy"
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
		{key: ApprovalPolicyKey, name: "M5 审批策略", filename: "m5-approval-policy.md"},
		{key: SystemPromptFactExtractKey, name: "离线事实抽取提示词", filename: "fact-extract-system-prompt.md"},
		{key: SystemPromptFactRollupKey, name: "事实日压缩提示词", filename: "fact-rollup-system-prompt.md"},
	}
}
