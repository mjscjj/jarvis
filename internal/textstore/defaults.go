package textstore

import "jarvis/internal/prompttemplate"

const (
	SystemPromptM3Key   = "m3_system_prompt"
	SystemPromptM5Key   = "m5_system_prompt"
	SystemPromptChatKey = "chat_system_prompt"
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
	SystemPromptFactExtractKey      = "fact_extract_system_prompt"
	WeeklyReportReminderTemplateKey = "weekly_report_reminder_template"
	OKRAgentPrinciplesKey           = "okr_agent_principles"
	OKRAgentQuarterlyDraftKey       = "okr_agent_quarterly_draft"
	OKRAgentRegionAlignmentKey      = "okr_agent_region_alignment"
	OKRAgentMeegoAlignmentKey       = "okr_agent_meego_alignment"
	OKRAgentReportAKey              = "okr_agent_report_a"
	OKRAgentReportBKey              = "okr_agent_report_b"
	OKRAgentReportCKey              = "okr_agent_report_c"
	OKRAgentWeeklyReminderKey       = "okr_agent_weekly_reminder"
	OKRAgentProgressSyncKey         = "okr_agent_progress_sync"
	OKRAgentPlanReviewKey           = "okr_agent_plan_review"
	OKRAgentProgressReviewKey       = "okr_agent_progress_review"
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
			key: SystemPromptChatKey, name: "页面对话系统提示词", filename: "chat-system-prompt.md",
			description: "定义右下角独立 Chat 的角色、直接用户授权、页面上下文和副作用边界。",
			kind:        "system_prompt", stage: "chat",
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
			key: WeeklyReportReminderTemplateKey, name: "催填消息模板", filename: "weekly-report-reminder-template.md",
			description: "定义按负责人聚合的 Review 催填消息；四类问题及具体 KR 标题由 Agent 生成。",
			kind:        "message_template", stage: "weekly_report",
		},
		{
			key: OKRAgentPrinciplesKey, name: "Agent 实现原则", filename: "okr-agent-principles.md",
			description: "定义所有 OKR Agent 流程共用的原子工具、动态规划、证据和副作用边界。",
			kind:        "agent_policy", stage: "okr_agent",
		},
		{
			key: OKRAgentQuarterlyDraftKey, name: "季度 OKR A 草稿", filename: "okr-agent-quarterly-draft.md",
			description: "从历史遗留、区域新需求和管理要求生成季度 OKR A 候选草稿。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentRegionAlignmentKey, name: "区域对齐与 OKR C", filename: "okr-agent-region-alignment.md",
			description: "对齐平台 OKR A 与各区域 OKR B，给出证据化匹配和 P0/P1/P2 建议。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentMeegoAlignmentKey, name: "研发对齐与 Meego", filename: "okr-agent-meego-alignment.md",
			description: "对齐 OKR C 与研发 OKR D，在确认范围内提出或执行 Meego 建项。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentReportAKey, name: "Report A 周报汇总", filename: "okr-agent-report-a.md",
			description: "从 Meego、策略 Leader 周报和方向汇总中整理有来源的当周进展。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentReportBKey, name: "中台周报 Report B", filename: "okr-agent-report-b.md",
			description: "把 Report A 转换为方向、Key imperatives 和月度进展口径的精简周报。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentReportCKey, name: "双周会 Report C", filename: "okr-agent-report-c.md",
			description: "合并最近两周 Report A，形成 Focus item、on track 判断和 Key Progress。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentWeeklyReminderKey, name: "周报催填", filename: "okr-agent-weekly-reminder.md",
			description: "检查当前周未填写项，按行动对象给真实负责人发送可审计提醒。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentProgressSyncKey, name: "进展巡检", filename: "okr-agent-progress-sync.md",
			description: "只读检查 Meego 和已采集消息，把有证据的变化写回世界模型。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentPlanReviewKey, name: "OKR Plan 评审", filename: "okr-agent-plan-review.md",
			description: "评审季度 Plan 是否讲清业务结果、成功标准、优先级、执行路径和需要达成的共识。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
		{
			key: OKRAgentProgressReviewKey, name: "OKR 进度评审", filename: "okr-agent-progress-review.md",
			description: "评审 Review 周的实际结果、差距与归因、风险和下一步，给出有依据的修改建议。",
			kind:        "agent_prompt", stage: "okr_agent",
		},
	}
}
