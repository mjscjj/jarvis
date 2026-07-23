package textstore

const (
	SystemPromptM3Key = "m3_system_prompt"
	SystemPromptM4Key = "m4_system_prompt"
	SystemPromptM5Key = "m5_system_prompt"
	ApprovalPolicyKey = "m5_approval_policy"
)

type definition struct {
	key      string
	name     string
	filename string
}

func definitions() []definition {
	return []definition{
		{key: SystemPromptM3Key, name: "M3 系统提示词", filename: "m3-system-prompt.md"},
		{key: SystemPromptM4Key, name: "M4 系统提示词", filename: "m4-system-prompt.md"},
		{key: SystemPromptM5Key, name: "M5 系统提示词", filename: "m5-system-prompt.md"},
		{key: ApprovalPolicyKey, name: "M5 审批策略", filename: "m5-approval-policy.md"},
	}
}
