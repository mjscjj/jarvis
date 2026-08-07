package textstore

import "jarvis/internal/prompttemplate"

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
			key: SystemPromptM3Key, name: "线索发现系统提示词", filename: "m3-system-prompt.md",
			description: "定义线索发现 Agent 的角色、准入判断原则和输出要求。",
			kind:        "system_prompt", stage: prompttemplate.StageM3,
		},
		{
			key: SystemPromptM5Key, name: "任务执行系统提示词", filename: "m5-system-prompt.md",
			description: "execute、apply 和 Session 恢复共用；具体阶段、审批产物及输出 Schema 由运行时动态追加。",
			kind:        "system_prompt", stage: prompttemplate.StageM5,
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
			key: SystemPromptFactExtractKey, name: "持续世界建模提示词", filename: "fact-extract-system-prompt.md",
			description: "定义哪些认知值得沉淀成实体、关键事项、事实和关系，以及哪些不写。",
			kind:        "system_prompt", stage: "fact_extract",
		},
		{
			key: SystemPromptFactRollupKey, name: "事实日压缩提示词", filename: "fact-rollup-system-prompt.md",
			description: "定义把一个主体一天的明细事实压缩成单条日事实的口径。",
			kind:        "system_prompt", stage: "fact_rollup",
		},
	}
}
