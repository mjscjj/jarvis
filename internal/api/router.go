// Package api 注册 Hertz 路由。管理后台 REST 与各模块内部接口都挂在这里。
package api

import (
	"fmt"
	"time"

	"jarvis/internal/agentconfig"
	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/contextsnap"
	"jarvis/internal/effectops"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/insight"
	"jarvis/internal/knowledge"
	"jarvis/internal/progress"
	"jarvis/internal/scheduledtask"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/taskcreate"
	"jarvis/internal/textstore"
	"jarvis/internal/toolquery"
	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/app/server"
	"gorm.io/gorm"
)

// Dependencies are process-level dependencies shared by API handlers.
type Dependencies struct {
	DB                 *gorm.DB
	Todos              extract.TodoReader
	TodoStatus         extract.TodoStatusWriter
	Tasks              execute.TaskService
	TaskSubmitter      *taskcreate.Submitter
	Executor           *execute.AgentExecutor
	MessageRecaller    *effectops.MessageRecaller // 撤回任务已发出的飞书消息
	Projects           *background.ProjectService
	KeyMatters         *background.KeyMatterService
	Persons            *background.PersonService
	Groups             *background.GroupBackgroundService
	Resolve            *background.ResolveService
	Profile            *background.ProfileService
	Resources          *background.ResourceService
	SharedMemory       *sharedmem.SharedMemoryService
	WorkRules          *workrule.Service
	TextFiles          *textstore.Service
	AgentConfig        *agentconfig.Service
	ScheduledTasks     *scheduledtask.Service
	Skills             *skill.Service
	RelationFacts      knowledge.FactService
	Progress           progress.EventService
	FactQueries        progress.FactQueryService
	Overview           *insight.OverviewService
	Digests            *insight.DigestService
	DailyDigests       DailyDigestService      // 每日进度总结（个人/关键群均用 codex）；nil 则不注册 /api/daily-digests 路由
	MorningBriefs      MorningBriefService     // 晨报 Markdown 归档，只读
	Worklog            *insight.WorklogService // 进度页「今天的文档」「项目代码」两个 Tab
	MeetingReviews     *insight.MeetingReviewService
	DigestSummarizer   *insight.Summarizer // 可选：codex 未启用时为 nil，总结接口返回 503
	FactRollups        FactRollupGenerator // 事实日压缩手动触发；nil 则接口返回 503
	FactRollupLoc      *time.Location      // 手动触发时解析 YYYY-MM-DD 的时区
	Debug              *insight.DebugService
	Logs               *insight.LogReader
	Chat               *chat.Service    // 可选：chat 未启用时为 nil，此时不注册 /api/chat 路由
	Capture            *capture.Service // 调试面板手动采集触发；nil 则不注册 /api/debug/capture/* 路由
	RuntimeSettings    *config.RuntimeSettingsService
	ContextAssembler   *contextsnap.Assembler
	CardApprovals      CardApprovalProcessor
	CardApprovalSecret string
	Readiness          ReadinessTargets // /readyz 探测的外部依赖；缺失只降级，不影响 /healthz
}

// Register 把所有路由挂到 Hertz 实例上。
func Register(h *server.Hertz, deps Dependencies) error {
	if h == nil {
		return fmt.Errorf("api hertz server is nil")
	}
	if deps.DB == nil {
		return fmt.Errorf("api database dependency is nil")
	}
	if deps.TodoStatus == nil {
		return fmt.Errorf("api todo status writer dependency is nil")
	}
	if deps.Todos == nil {
		return fmt.Errorf("api todo reader dependency is nil")
	}
	if deps.Tasks == nil {
		return fmt.Errorf("api Task service dependency is nil")
	}
	if deps.TaskSubmitter == nil {
		return fmt.Errorf("api Task submitter dependency is nil")
	}
	if deps.MessageRecaller == nil {
		return fmt.Errorf("api message recaller dependency is nil")
	}
	if deps.Projects == nil {
		return fmt.Errorf("api project service dependency is nil")
	}
	if deps.KeyMatters == nil {
		return fmt.Errorf("api key matter service dependency is nil")
	}
	if deps.Persons == nil {
		return fmt.Errorf("api person service dependency is nil")
	}
	if deps.Groups == nil {
		return fmt.Errorf("api group service dependency is nil")
	}
	if deps.Resolve == nil {
		return fmt.Errorf("api resolve service dependency is nil")
	}
	if deps.Profile == nil {
		return fmt.Errorf("api profile service dependency is nil")
	}
	if deps.Resources == nil {
		return fmt.Errorf("api resource service dependency is nil")
	}
	if deps.SharedMemory == nil {
		return fmt.Errorf("api shared memory service dependency is nil")
	}
	if deps.WorkRules == nil {
		return fmt.Errorf("api work rule service dependency is nil")
	}
	if deps.TextFiles == nil {
		return fmt.Errorf("api text file service dependency is nil")
	}
	if deps.AgentConfig == nil {
		return fmt.Errorf("api agent config service dependency is nil")
	}
	if deps.ScheduledTasks == nil {
		return fmt.Errorf("api scheduled task service dependency is nil")
	}
	if deps.Skills == nil {
		return fmt.Errorf("api skill service dependency is nil")
	}
	if deps.RelationFacts == nil {
		return fmt.Errorf("api relation fact service dependency is nil")
	}
	if deps.Progress == nil {
		return fmt.Errorf("api progress service dependency is nil")
	}
	if deps.FactQueries == nil {
		return fmt.Errorf("api fact query service dependency is nil")
	}
	if deps.Overview == nil {
		return fmt.Errorf("api overview service dependency is nil")
	}
	if deps.Digests == nil {
		return fmt.Errorf("api digest service dependency is nil")
	}
	if deps.MeetingReviews == nil {
		return fmt.Errorf("api meeting review service dependency is nil")
	}
	if deps.MorningBriefs == nil {
		return fmt.Errorf("api morning brief service dependency is nil")
	}
	if deps.Debug == nil {
		return fmt.Errorf("api debug service dependency is nil")
	}
	if deps.Logs == nil {
		return fmt.Errorf("api log reader dependency is nil")
	}
	if deps.RuntimeSettings == nil {
		return fmt.Errorf("api runtime settings dependency is nil")
	}
	if deps.ContextAssembler == nil {
		return fmt.Errorf("api context assembler dependency is nil")
	}
	toolQueries, err := toolquery.NewService(deps.DB)
	if err != nil {
		return fmt.Errorf("create tool query service: %w", err)
	}
	h.GET("/healthz", Health(deps.DB))
	h.GET("/readyz", Readiness(deps.DB, deps.Readiness))
	h.GET("/api/messages", ListToolMessages(toolQueries))
	h.GET("/api/captured-resources", ListCapturedResources(toolQueries))
	h.GET("/api/captured-resources/:resource_id", GetCapturedResource(toolQueries))
	h.POST("/api/context", AssembleContext(deps.ContextAssembler))
	h.GET("/api/todos", ListTodos(deps.Todos))
	h.GET("/api/todos/:todo_id", GetTodo(deps.Todos))
	h.PATCH("/api/todos/:todo_id/status", SetTodoStatus(deps.TodoStatus))
	h.GET("/api/tasks", ListTasks(deps.Tasks))
	h.POST("/api/tasks", CreateTask(deps.TaskSubmitter))
	h.GET("/api/tasks/:task_id", GetTask(deps.Tasks))
	h.GET("/api/tasks/:task_id/runs", ListTaskRuns(deps.Tasks))
	h.GET("/api/tasks/:task_id/events", ListTaskEvents(deps.Progress))
	h.POST("/api/tasks/:task_id/finish", FinishTask(deps.Tasks))
	h.POST("/api/tasks/:task_id/close", CloseTask(deps.Tasks))
	h.PATCH("/api/tasks/:task_id", UpdateTask(deps.Tasks))
	h.POST("/api/tasks/:task_id/supplement", SupplementTask(deps.Tasks))
	// 撤回任务「对外产出」里的某条飞书消息（走 lark-cli，按钮点击即高危确认）。
	h.POST("/api/tasks/:task_id/effects/recall-message", RecallEffectMessage(deps.MessageRecaller, deps.Tasks))
	h.GET("/api/relation-facts", ListRelationFacts(deps.RelationFacts))
	h.POST("/api/relation-facts", CreateRelationFact(deps.RelationFacts))
	h.PUT("/api/relation-facts/:fact_id", UpdateRelationFact(deps.RelationFacts))
	h.DELETE("/api/relation-facts/:fact_id", DeleteRelationFact(deps.RelationFacts))
	if deps.Executor != nil {
		h.GET("/api/tasks/:task_id/output", GetTaskRunOutput(deps.Executor))
		h.POST("/api/tasks/:task_id/execute", ExecuteTask(deps.Executor))
		h.POST("/api/tasks/:task_id/interrupt", InterruptTask(deps.Executor))
		h.POST("/api/tasks/:task_id/rerun", RerunTask(deps.Executor))
		h.POST("/api/tasks/:task_id/reapply", ReapplyTask(deps.Executor))
		h.POST("/api/tasks/:task_id/resume", ResumeTaskAfterHuman(deps.Executor))
		h.POST("/api/tasks/:task_id/approve", ApproveTask(deps.Executor))
		h.POST("/api/tasks/:task_id/reject", RejectTask(deps.Executor))
	}
	if deps.CardApprovals != nil {
		h.POST("/internal/card-approval/callback", RelayCardApproval(deps.CardApprovals, deps.CardApprovalSecret))
	}
	// M1 背景管理：Project/Person 全量 CRUD；Group 只可改人工背景字段（采集字段归 M2）。
	h.GET("/api/projects", ListProjects(deps.Projects))
	h.POST("/api/projects", CreateProject(deps.Projects))
	h.GET("/api/projects/:project_id", GetProject(deps.Projects))
	h.PUT("/api/projects/:project_id", UpdateProject(deps.Projects))
	h.DELETE("/api/projects/:project_id", DeleteProject(deps.Projects))
	h.GET("/api/key-matters", ListKeyMatters(deps.KeyMatters))
	h.POST("/api/key-matters", CreateKeyMatter(deps.KeyMatters))
	h.GET("/api/key-matters/:key_matter_id", GetKeyMatter(deps.KeyMatters))
	h.PUT("/api/key-matters/:key_matter_id", UpdateKeyMatter(deps.KeyMatters))
	h.POST("/api/key-matters/:key_matter_id/touch", TouchKeyMatter(deps.KeyMatters))
	h.DELETE("/api/key-matters/:key_matter_id", DeleteKeyMatter(deps.KeyMatters))
	h.GET("/api/facts", ListFacts(deps.Progress))
	h.POST("/api/facts", AppendFact(deps.Progress))
	h.GET("/api/facts/timeline", FactTimeline(deps.FactQueries, deps.FactRollupLoc))
	h.GET("/api/facts/search", SearchFacts(deps.FactQueries))
	h.POST("/api/fact-rollups/generate", GenerateFactRollups(deps.FactRollups, deps.FactRollupLoc))
	h.GET("/api/persons", ListPersons(deps.Persons))
	h.POST("/api/persons/resolve", ResolvePerson(deps.Resolve))
	h.POST("/api/persons", CreatePerson(deps.Persons))
	h.GET("/api/persons/:person_id", GetPerson(deps.Persons))
	h.PUT("/api/persons/:person_id", UpdatePerson(deps.Persons))
	h.DELETE("/api/persons/:person_id", DeletePerson(deps.Persons))
	h.GET("/api/groups", ListGroups(deps.Groups))
	h.PUT("/api/groups/:group_id", UpdateGroupBackground(deps.Groups))
	// Principal（“我”）：单例 profile，读取 + upsert。
	h.GET("/api/profile", GetProfile(deps.Profile))
	h.PUT("/api/profile", UpdateProfile(deps.Profile))
	// 共享记忆：全局单例大文本，读取 + 整段覆盖保存。
	h.GET("/api/shared-memory", GetSharedMemory(deps.SharedMemory))
	h.PUT("/api/shared-memory", UpdateSharedMemory(deps.SharedMemory))
	h.POST("/api/shared-memory/append", AppendSharedMemory(deps.SharedMemory))
	// 运行配置：只开放调试常用的 Agent CLI、模型、超时、并发和模块开关。
	// 保存到本地覆盖文件，进程重启后生效。
	h.GET("/api/runtime-settings", GetRuntimeSettings(deps.RuntimeSettings))
	h.PUT("/api/runtime-settings", UpdateRuntimeSettings(deps.RuntimeSettings))
	// 工作规则：M3 与 M5 各自读取一个固定 Markdown 文件。
	h.GET("/api/work-rules", ListWorkRules(deps.WorkRules))
	h.GET("/api/work-rules/:work_rule_key", GetWorkRule(deps.WorkRules))
	h.PUT("/api/work-rules/:work_rule_key", UpdateWorkRule(deps.WorkRules))
	// 受控 Markdown 文件：系统提示词和审批策略由后台实时维护。
	h.GET("/api/text-files", ListTextFiles(deps.TextFiles))
	h.GET("/api/text-files/:text_file_key", GetTextFile(deps.TextFiles))
	h.PUT("/api/text-files/:text_file_key", UpdateTextFile(deps.TextFiles))
	// Agent 设置：按线索发现/任务执行展示与运行时同源的稳定系统指令预览。
	h.GET("/api/agent-config/stages/:agent_stage/preview", GetAgentConfigPreview(deps.AgentConfig))
	// 周期定时任务：独立 CRUD、手动触发；自动执行由进程内每分钟 scheduler 负责。
	h.GET("/api/scheduled-tasks", ListScheduledTasks(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks", CreateScheduledTask(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks/yield", YieldUntil(deps.ScheduledTasks))
	h.GET("/api/scheduled-tasks/:scheduled_task_id", GetScheduledTask(deps.ScheduledTasks))
	h.PUT("/api/scheduled-tasks/:scheduled_task_id", UpdateScheduledTask(deps.ScheduledTasks))
	h.DELETE("/api/scheduled-tasks/:scheduled_task_id", DeleteScheduledTask(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks/:scheduled_task_id/trigger", TriggerScheduledTask(deps.ScheduledTasks))
	// Skills：扫描仓库 SKILL.md，后台控制启用状态和 M3/M5 生效范围。
	h.GET("/api/skills", ListSkills(deps.Skills))
	h.POST("/api/skills/scan", ScanSkills(deps.Skills))
	h.PUT("/api/skills/:skill_name", UpdateSkill(deps.Skills))
	h.GET("/api/skills/:skill_name/content", GetSkillContent(deps.Skills))
	// Overview 看板 + 进度：跨模块只读聚合，无表无 cron；总结按需调 codex。
	h.GET("/api/overview", GetOverview(deps.Overview))
	h.GET("/api/digests", GetDigests(deps.Digests))
	h.POST("/api/digests/summarize", SummarizeDigest(deps.Digests, deps.DigestSummarizer))
	h.GET("/api/review/meetings", GetMeetingReviews(deps.MeetingReviews))
	// 每日进度总结：按日期读当天全部 scope + 异步触发单条生成/重算。
	if deps.DailyDigests != nil {
		h.GET("/api/daily-digests", GetDailyDigests(deps.DailyDigests))
		h.POST("/api/daily-digests/generate", GenerateDailyDigest(deps.DailyDigests))
	}
	// 晨间作战简报：直接读取 canonical Markdown，不复制进数据库。
	h.GET("/api/morning-briefs", ListMorningBriefs(deps.MorningBriefs))
	// 进度页工作日志：我今天写/收到的文档、我今天在各仓库的 MR（实时调 bytedcli）。
	if deps.Worklog != nil {
		h.GET("/api/worklog/commits", GetWorklogCommits(deps.Worklog))
		h.GET("/api/worklog/documents", GetWorklogDocuments(deps.Worklog))
	}
	// 调试面板：模块与采集运行、实时 Agent、抽取水位、运行日志尾读。
	h.GET("/api/debug/modules", GetDebugModules(deps.Debug))
	h.GET("/api/debug/agent-processes", GetDebugAgentProcesses(deps.Debug))
	h.GET("/api/debug/failures", GetDebugFailures(deps.Debug))
	h.GET("/api/debug/monitoring", GetDebugMonitoring(deps.Debug))
	h.GET("/api/debug/proactive-runs", GetDebugProactiveRuns(deps.Debug))
	h.GET("/api/debug/proactive-runs/:run_id", GetDebugProactiveRun(deps.Debug))
	h.GET("/api/debug/scans", GetDebugScans(deps.Debug))
	h.GET("/api/debug/watermarks", GetDebugWatermarks(deps.Debug))
	h.GET("/api/debug/logs", GetDebugLogs(deps.Logs))
	h.GET("/api/system-tasks/runs", GetSystemTaskRuns(deps.Logs))
	// 调试面板手动触发：手动跑一轮 M1 采集，无需等 cron。
	if deps.Capture != nil {
		h.POST("/api/debug/capture/discover", DiscoverChatsManually(deps.Capture))
		h.POST("/api/debug/capture/scan-related", ScanRelatedManually(deps.Capture))
		h.POST("/api/debug/capture/scan-chat", ScanChatManually(deps.Capture))
		// 通用线索投递：任何 agent 把一条观察到的事实交给 M2，M2 原样存证并唤醒 M3。
		h.POST("/api/clues", AppendClue(deps.Capture))
	}
	// 手动维护的资源：可关联 人/项目/我，供后台管理与 M3 工具按需查询。
	h.GET("/api/resources", ListResources(deps.Resources))
	h.POST("/api/resources", CreateResource(deps.Resources))
	h.GET("/api/resources/:resource_id", GetResource(deps.Resources))
	h.PUT("/api/resources/:resource_id", UpdateResource(deps.Resources))
	h.POST("/api/resources/:resource_id/touch", TouchResource(deps.Resources))
	h.DELETE("/api/resources/:resource_id", DeleteResource(deps.Resources))
	// 基于 codex CLI 的流式对话（SSE）。与 execute 一致：未启用（nil）则不注册路由。
	if deps.Chat != nil {
		h.POST("/api/chat", Chat(deps.Chat))
	}
	// 精确 API 路由优先于这个兜底。必须在进程注册根 StaticFS 之前拦住
	// 未知 /api/*，否则 Hertz 会把它当作 web/dist 下的静态文件并返回
	// 非 JSON 404，调用方拿不到 logid，服务端也会打印误导性的文件错误。
	h.Any("/api/*path", apiNotFound())
	return nil
}
