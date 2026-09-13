package textstore

import "jarvis/internal/prompttemplate"

const (
	InitiativeLevelKey  = "initiative_level"
	SystemPromptM3Key   = "m3_system_prompt"
	SystemPromptM5Key   = "m5_system_prompt"
	SystemPromptChatKey = "chat_system_prompt"
	// SystemPromptCCKey drives the Feishu foreground Agent. It decides whether
	// to answer in the current turn or create a durable manual Task for M5.
	SystemPromptCCKey = "cc_system_prompt"
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
	// EntityPageGuidanceKey is the shared content contract for every entity's
	// long-term current-state page. Stage prompts own when to edit a page; this
	// file owns what a good page contains.
	EntityPageGuidanceKey = "entity_page_guidance"
	// SystemPromptFactExtractKey drives the offline fact engine, which distils
	// long-lived facts out of material the pipeline already produced.
	SystemPromptFactExtractKey = "fact_extract_system_prompt"
)

// definition also carries the editor-facing description. The admin UI renders
// whatever List returns instead of keeping its own catalog, so registering a
// file here is the only step needed to make it editable.
type definition struct {
	key         string
	name        string
	filename    string
	description string
	kind        string
	stage       string
}

func definitions() []definition {
	return []definition{
		{
			key: InitiativeLevelKey, name: "主动程度", filename: "initiative-level.md",
			description: "后台主动发现、执行扩展与通知的档位：quiet（安静）、normal（普通）、active（活跃）；后续运行实时读取。",
			kind:        "initiative_level", stage: "shared",
		},
		{
			key: SystemPromptM3Key, name: "线索发现系统提示词", filename: "m3-system-prompt.md",
			description: "定义线索发现 Agent 的角色、准入判断原则和输出要求。",
			kind:        "system_prompt", stage: prompttemplate.StageM3,
		},
		{
			key: SystemPromptM5Key, name: "任务执行系统提示词", filename: "m5-system-prompt.md",
			description: "任务执行及等待、人工回答恢复共用；主动程度、具体阶段和输出 Schema 由运行时动态追加。",
			kind:        "system_prompt", stage: prompttemplate.StageM5,
		},
		{
			key: SystemPromptChatKey, name: "对话系统提示词", filename: "chat-system-prompt.md",
			description: "定义交互对话 Agent 的角色、回答风格、工具使用与停止边界。",
			kind:        "system_prompt", stage: "chat",
		},
		{
			key: SystemPromptCCKey, name: "飞书前台系统提示词", filename: "cc-system-prompt.md",
			description: "定义 CC 前台即时处理、用户约束和正式创建 Task 交给 M5 的边界。",
			kind:        "system_prompt", stage: "cc",
		},
		{
			key: SystemPromptProactiveKey, name: "主动巡视系统提示词", filename: "proactive-system-prompt.md",
			description: "定义每小时主动巡视的时间范围、世界模型维护、Task 创建及停止边界。",
			kind:        "system_prompt", stage: "proactive",
		},
		{
			key: SystemPromptMeetingSweepKey, name: "会议巡扫系统提示词", filename: "meeting-sweep-system-prompt.md",
			description: "定义会议巡扫只采集不分析的边界：把已结束会议和未来日程原样投递为线索。",
			kind:        "system_prompt", stage: "meeting_sweep",
		},
		{
			key: SystemPromptMorningBriefKey, name: "晨间作战简报系统提示词", filename: "morning-brief-system-prompt.md",
			description: "定义工作日晨间简报的取证范围、今日结果的选择标准和唯一允许的对外投递动作。",
			kind:        "system_prompt", stage: "morning_brief",
		},
		{
			key: ApprovalPolicyKey, name: "任务执行审批策略", filename: "m5-approval-policy.md",
			description: "供任务执行 Agent 判断哪些具体动作需要先请示、哪些可以直接完成。",
			kind:        "approval_policy", stage: prompttemplate.StageM5,
		},
		{
			key: EntityPageGuidanceKey, name: "实体当前认知页指导", filename: "entity-page-guidance.md",
			description: "所有实体共用的页面内容契约：当前认知、历史演变、证据边界及 Task 运作记录的归属。",
			kind:        "guidance", stage: "shared",
		},
		{
			key: SystemPromptFactExtractKey, name: "持续世界建模提示词", filename: "fact-extract-system-prompt.md",
			description: "定义哪些认知值得沉淀成实体、关键事项、事实和关系，以及哪些不写。",
			kind:        "system_prompt", stage: "fact_extract",
		},
	}
}
