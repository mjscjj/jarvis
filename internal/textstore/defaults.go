package textstore

const (
	SystemPromptM3Key = "m3_system_prompt"
	// SystemPromptDecisionKey drives M5's decision step (is this clue worth
	// executing?), which used to be a separate M4 stage.
	SystemPromptDecisionKey = "m5_decision_system_prompt"
	SystemPromptM5Key       = "m5_system_prompt"
	ApprovalPolicyKey       = "m5_approval_policy"
)

type definition struct {
	key      string
	name     string
	filename string
}

func definitions() []definition {
	return []definition{
		{key: SystemPromptM3Key, name: "M3 系统提示词", filename: "m3-system-prompt.md"},
		{key: SystemPromptDecisionKey, name: "M5 判断系统提示词", filename: "m5-decision-system-prompt.md"},
		{key: SystemPromptM5Key, name: "M5 执行系统提示词", filename: "m5-system-prompt.md"},
		{key: ApprovalPolicyKey, name: "M5 审批策略", filename: "m5-approval-policy.md"},
	}
}
