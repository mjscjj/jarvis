// Package api 注册 Hertz 路由。管理后台 REST 与各模块内部接口都挂在这里。
package api

import (
	"fmt"
	"strings"
	"time"

	"jarvis/internal/agentconfig"
	"jarvis/internal/appmodule"
	"jarvis/internal/authn"
	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/contextsnap"
	"jarvis/internal/delegation"
	"jarvis/internal/effectops"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/insight"
	"jarvis/internal/notice"
	"jarvis/internal/onboarding"
	"jarvis/internal/plugin"
	"jarvis/internal/progress"
	"jarvis/internal/scheduledtask"
	"jarvis/internal/security"
	"jarvis/internal/sharedmem"
	"jarvis/internal/taskcreate"
	"jarvis/internal/textstore"
	"jarvis/internal/toolquery"
	"jarvis/internal/workrule"
	"jarvis/internal/worldprogress"

	"github.com/cloudwego/hertz/pkg/app/server"
	"gorm.io/gorm"
)

// Dependencies are process-level dependencies shared by API handlers.
type Dependencies struct {
	AgentDisplayName   string
	Auth               *authn.Service
	DB                 *gorm.DB
	Todos              extract.TodoReader
	TodoStatus         extract.TodoStatusWriter
	Tasks              execute.TaskService
	TaskSubmitter      *taskcreate.Submitter
	Executor           *execute.AgentExecutor
	MessageRecaller    *effectops.MessageRecaller // 撤回任务已发出的飞书消息
	PrincipalNotices   *notice.Service
	Projects           *background.ProjectService
	ProjectPortability *background.ProjectPortabilityService
	KeyMatters         *background.KeyMatterService
	Persons            *background.PersonService
	Groups             *background.GroupBackgroundService
	Resolve            *background.ResolveService
	Profile            *background.ProfileService
	Resources          *background.ResourceService
	Pages              *background.PageService
	Relations          *background.RelationService
	WorldPurge         *background.WorldPurgeService
	SharedMemory       *sharedmem.SharedMemoryService
	WorkRules          *workrule.Service
	TextFiles          *textstore.Service
	AgentConfig        *agentconfig.Service
	AppModules         *appmodule.Service
	OKRModule          *OKRModuleDependencies
	BizOKRModule       *BizOKRModuleDependencies
	ScheduledTasks     *scheduledtask.Service
	Plugins            *plugin.Service
	Skills             SkillService
	Progress           progress.EventService
	FactQueries        progress.FactQueryService
	WorldProgress      worldprogress.AssessmentService
	Overview           *insight.OverviewService
	Digests            *insight.DigestService
	DailyDigests       DailyDigestService      // 每日进度总结（个人/关键群均用 codex）；nil 则不注册 /api/daily-digests 路由
	MorningBriefs      MorningBriefService     // 晨报 Markdown 归档，只读
	Worklog            *insight.WorklogService // 进度页「今天的文档」「项目代码」两个 Tab
	MeetingReviews     *insight.MeetingReviewService
	DigestSummarizer   *insight.Summarizer // 可选：codex 未启用时为 nil，总结接口返回 503
	FactTimelineLoc    *time.Location      // 事实时间线按自然日分组的时区
	Debug              *insight.DebugService
	Logs               *insight.LogReader
	Chat               *chat.Service
	PublicBaseURL      string           // 这台部署对外可打开的根地址；分享链接用它替换浏览器地址栏里的 IP
	Capture            *capture.Service // 调试面板手动采集触发；nil 则不注册 /api/debug/capture/* 路由
	RuntimeSettings    *config.RuntimeSettingsService
	SecurityAudit      *security.AuditService
	ContextAssembler   *contextsnap.Assembler
	CardAsks           CardAskProcessor
	CardApprovalSecret string
	MeetingSweep       MeetingSweepWaker // 会议事件转发唤醒巡扫；巡扫未启用时为 nil，此时不注册 /internal/meeting-sweep/wake 路由
	Readiness          ReadinessTargets  // /readyz 探测的外部依赖；缺失只降级，不影响 /healthz
	SystemControl      SystemShutdowner
	Onboarding         *onboarding.Service
	UpdateRoot         string // 可选：DEV2 发布目录；为空时不注册 /jarvis-updates
}

// Register 把所有路由挂到 Hertz 实例上。
func Register(h *server.Hertz, deps Dependencies) error {
	if h == nil {
		return fmt.Errorf("api hertz server is nil")
	}
	if deps.AgentDisplayName == "" {
		return fmt.Errorf("api agent display name is empty")
	}
	if deps.Auth == nil {
		return fmt.Errorf("api auth dependency is nil")
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
	if deps.Relations == nil {
		return fmt.Errorf("api relation service dependency is nil")
	}
	if deps.WorldPurge == nil {
		return fmt.Errorf("api world purge service dependency is nil")
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
	if deps.Pages == nil {
		return fmt.Errorf("api page service dependency is nil")
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
	if deps.Plugins == nil {
		return fmt.Errorf("api plugin service dependency is nil")
	}
	if deps.Skills == nil {
		return fmt.Errorf("api skill service dependency is nil")
	}
	if deps.Progress == nil {
		return fmt.Errorf("api progress service dependency is nil")
	}
	if deps.FactQueries == nil {
		return fmt.Errorf("api fact query service dependency is nil")
	}
	if deps.WorldProgress == nil {
		return fmt.Errorf("api world progress service dependency is nil")
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
	if deps.AppModules == nil {
		return fmt.Errorf("api app module service dependency is nil")
	}
	if deps.SecurityAudit == nil {
		return fmt.Errorf("api security audit dependency is nil")
	}
	if deps.ContextAssembler == nil {
		return fmt.Errorf("api context assembler dependency is nil")
	}
	if deps.SystemControl == nil {
		return fmt.Errorf("api system control dependency is nil")
	}
	toolQueries, err := toolquery.NewService(deps.DB)
	if err != nil {
		return fmt.Errorf("create tool query service: %w", err)
	}
	h.GET("/healthz", Health(deps.DB))
	h.GET("/readyz", Readiness(deps.DB, deps.Readiness))
	if strings.TrimSpace(deps.UpdateRoot) != "" {
		updateFiles, err := NewUpdateFileHandler(deps.UpdateRoot)
		if err != nil {
			return fmt.Errorf("create update file handler: %w", err)
		}
		h.GET("/jarvis-updates/:filename", updateFiles)
		h.HEAD("/jarvis-updates/:filename", updateFiles)
	}
	h.GET("/api/auth/status", GetAuthStatus(deps.Auth))
	h.POST("/api/auth/login", LoginWithByteDance(deps.Auth))
	h.POST("/api/auth/login/complete", CompleteByteDanceLogin(deps.Auth))
	h.POST("/api/auth/logout", LogoutFromJarvis(deps.Auth))
	if deps.Onboarding != nil {
		h.GET("/api/setup/bootstrap", GetOnboardingBootstrap(deps.Onboarding))
		h.GET("/api/setup/status", GetOnboardingStatus(deps.Onboarding))
		h.POST("/api/setup/lark/connect", BeginOnboardingLarkSetup(deps.Onboarding))
		h.POST("/api/setup/lark/login", BeginOnboardingLarkLogin(deps.Onboarding))
		h.POST("/api/setup/lark/credentials", RepairOnboardingLarkCredentials(deps.Onboarding))
		h.POST("/api/setup/agent/login", BeginOnboardingAgentLogin(deps.Onboarding))
		h.GET("/api/setup/flows/:flow_id", GetOnboardingFlow(deps.Onboarding))
		h.POST("/api/setup/flows/:flow_id/cancel", CancelOnboardingFlow(deps.Onboarding))
		h.POST("/api/setup/finalize", FinalizeOnboarding(deps.Onboarding, deps.Auth))
		h.POST("/api/setup/world-model", BootstrapOnboardingWorldModel(deps.Onboarding))
	}
	h.POST("/api/system/shutdown", ShutdownSystem(deps.SystemControl))
	h.GET("/api/agent-identity", GetAgentIdentity(deps.AgentDisplayName))
	h.GET("/api/messages", ListToolMessages(toolQueries))
	h.GET("/api/messages/:message_id", GetToolMessage(toolQueries))
	h.GET("/api/todo-events/:event_id", GetToolTodoEvent(toolQueries))
	h.GET("/api/task-events/:event_id", GetToolTaskEvent(toolQueries))
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
	h.GET("/api/task-runs/:run_id", GetTaskRun(deps.Tasks))
	h.GET("/api/tasks/:task_id/events", ListTaskEvents(deps.Progress))
	h.POST("/api/tasks/:task_id/finish", FinishTask(deps.Tasks))
	h.POST("/api/tasks/:task_id/close", CloseTask(deps.Tasks))
	h.PATCH("/api/tasks/:task_id", UpdateTask(deps.Tasks))
	h.POST("/api/tasks/:task_id/supplement", SupplementTask(deps.Tasks))
	// 撤回任务「对外产出」里的某条飞书消息（走 lark-cli，按钮点击即高危确认）。
	h.POST("/api/tasks/:task_id/effects/recall-message", RecallEffectMessage(deps.MessageRecaller, deps.Tasks))
	h.POST("/api/notices/principal", NoticePrincipal(deps.PrincipalNotices))
	if deps.Executor != nil {
		h.GET("/api/tasks/:task_id/output", GetTaskRunOutput(deps.Executor))
		h.POST("/api/tasks/:task_id/execute", ExecuteTask(deps.Executor))
		h.POST("/api/tasks/:task_id/interrupt", InterruptTask(deps.Executor))
		h.POST("/api/tasks/:task_id/rerun", RerunTask(deps.Executor))
		h.POST("/api/tasks/:task_id/resume", ResumeTaskAfterHuman(deps.Executor))
	}
	if deps.CardAsks != nil {
		h.POST("/internal/card-approval/callback", RelayCardAsk(deps.CardAsks, deps.CardApprovalSecret))
	}
	if deps.Capture != nil && strings.TrimSpace(deps.CardApprovalSecret) != "" {
		h.POST("/internal/message-routing/claim", ClaimMessageRoute(deps.Capture, deps.CardApprovalSecret))
	}
	if deps.MeetingSweep != nil && strings.TrimSpace(deps.CardApprovalSecret) != "" {
		h.POST("/internal/meeting-sweep/wake", WakeMeetingSweep(deps.MeetingSweep, deps.CardApprovalSecret))
	}
	// M1 背景管理：Project/Person 全量 CRUD；Group 只可改人工背景字段（采集字段归 M2）。
	h.GET("/api/projects", ListProjects(deps.Projects))
	h.POST("/api/projects", CreateProject(deps.Projects))
	h.GET("/api/projects/:project_id", GetProject(deps.Projects))
	h.PUT("/api/projects/:project_id", UpdateProject(deps.Projects))
	h.DELETE("/api/projects/:project_id", DeleteProject(deps.Projects))
	if deps.ProjectPortability != nil {
		h.POST("/api/projects/import/preview", PreviewProjectImport(deps.ProjectPortability))
		h.POST("/api/projects/import", ImportProject(deps.ProjectPortability))
		h.GET("/api/projects/:project_id/export", ExportProject(deps.ProjectPortability))
		h.POST("/api/projects/:project_id/duplicate", DuplicateProject(deps.ProjectPortability))
		h.POST("/api/projects/:project_id/repositories/resolve", ResolveProjectRepositories(deps.ProjectPortability))
	}
	h.GET("/api/key-matters", ListKeyMatters(deps.KeyMatters))
	h.POST("/api/key-matters", CreateKeyMatter(deps.KeyMatters))
	h.GET("/api/key-matters/:key_matter_id", GetKeyMatter(deps.KeyMatters))
	h.PUT("/api/key-matters/:key_matter_id", UpdateKeyMatter(deps.KeyMatters))
	h.POST("/api/key-matters/:key_matter_id/touch", TouchKeyMatter(deps.KeyMatters))
	h.DELETE("/api/key-matters/:key_matter_id", DeleteKeyMatter(deps.KeyMatters))
	h.GET("/api/facts", ListFacts(deps.Progress))
	h.POST("/api/facts", AppendFact(deps.Progress))
	h.POST("/api/facts/batch", AppendFacts(deps.Progress))
	h.GET("/api/facts/timeline", FactTimeline(deps.FactQueries, deps.FactTimelineLoc))
	h.GET("/api/facts/search", SearchFacts(deps.FactQueries))
	h.GET("/api/world-progress", GetWorldProgressBySubjectPeriod(deps.WorldProgress))
	h.GET("/api/world-progress/period/:period_key", ListWorldProgressByPeriod(deps.WorldProgress))
	h.GET("/api/world-progress/:world_progress_id", GetWorldProgress(deps.WorldProgress))
	h.POST("/api/world-progress", CreateWorldProgress(deps.WorldProgress))
	h.PUT("/api/world-progress/:world_progress_id", UpdateWorldProgress(deps.WorldProgress))
	h.GET("/api/persons", ListPersons(deps.Persons))
	h.GET("/api/people/search", SearchFeishuPeople(deps.Resolve))
	// Deprecated browser-cache compatibility route; see ResolvePerson.
	h.POST("/api/persons/resolve", ResolvePerson(deps.Resolve))
	h.POST("/api/persons", CreatePerson(deps.Persons))
	h.GET("/api/persons/:person_id", GetPerson(deps.Persons))
	h.PUT("/api/persons/:person_id", UpdatePerson(deps.Persons))
	h.DELETE("/api/persons/:person_id", DeletePerson(deps.Persons))
	h.GET("/api/groups", ListGroups(deps.Groups))
	h.PUT("/api/groups/:group_id", UpdateGroupBackground(deps.Groups))
	h.GET("/api/pages", ListPages(deps.Pages))
	h.GET("/api/pages/:type/:id/revisions", ListPageRevisions(deps.Pages))
	h.GET("/api/pages/:type/:id", GetPage(deps.Pages))
	h.PUT("/api/pages/:type/:id", UpdatePage(deps.Pages))
	h.GET("/api/pages/:type/:id/backlinks", ListPageBacklinks(deps.Pages))
	h.GET("/api/relations", ListRelations(deps.Relations))
	h.POST("/api/relations", UpsertRelation(deps.Relations))
	h.DELETE("/api/relations/:relation_id", DeleteRelation(deps.Relations))
	h.POST("/api/world/purge", PurgeWorldEntities(deps.WorldPurge))
	h.GET("/api/world-nodes/:type/:id", ResolveWorldNode(deps.Pages, deps.OKRModule, deps.BizOKRModule))
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
	h.GET("/api/security-settings", GetSecuritySettings(deps.RuntimeSettings))
	h.PUT("/api/security-settings", UpdateSecuritySettings(deps.RuntimeSettings, deps.Capture))
	h.GET("/api/security-audit-events", ListSecurityAuditEvents(deps.SecurityAudit))
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
	// 内置功能模块：代码静态注册，conf/modules.yaml 控制下一次启动时的迁移、路由、Skill 和调度边界。
	h.GET("/api/app-modules", ListAppModules(deps.AppModules))
	h.PUT("/api/app-modules/:module_key", UpdateAppModule(deps.AppModules))
	if deps.OKRModule != nil {
		if err := RegisterOKRModuleRoutes(h, *deps.OKRModule); err != nil {
			return err
		}
	}
	if deps.BizOKRModule != nil {
		if err := RegisterBizOKRModuleRoutes(h, *deps.BizOKRModule); err != nil {
			return err
		}
	}
	// 周期定时任务：独立 CRUD、手动触发；自动执行由进程内每分钟 scheduler 负责。
	h.GET("/api/scheduled-tasks", ListScheduledTasks(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks", CreateScheduledTask(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks/yield", YieldUntil(deps.ScheduledTasks))
	h.GET("/api/scheduled-tasks/:scheduled_task_id", GetScheduledTask(deps.ScheduledTasks))
	h.PUT("/api/scheduled-tasks/:scheduled_task_id", UpdateScheduledTask(deps.ScheduledTasks))
	h.DELETE("/api/scheduled-tasks/:scheduled_task_id", DeleteScheduledTask(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks/:scheduled_task_id/trigger", TriggerScheduledTask(deps.ScheduledTasks))
	// 插件只管理外部能力的启停、授权和采集调度；采集结果仍走统一 clue 流水线。
	delegations, err := delegation.NewService(deps.DB)
	if err != nil {
		return err
	}
	h.GET("/api/delegations", ListDelegations(delegations))
	h.GET("/api/delegations/:todo_id", GetDelegation(delegations))
	h.PATCH("/api/delegations/:todo_id", UpdateDelegation(delegations))
	h.GET("/api/delegations/:todo_id/tasks", ListDelegationTasks(delegations))
	h.GET("/api/plugin-installations", ListPluginInstallations(deps.Plugins))
	h.GET("/api/plugins", ListPlugins(deps.Plugins))
	h.GET("/api/plugins/:plugin_id", GetPlugin(deps.Plugins))
	h.PATCH("/api/plugins/:plugin_id", UpdatePlugin(deps.Plugins))
	h.POST("/api/plugins/:plugin_id/authorize", AuthorizePlugin(deps.Plugins))
	h.POST("/api/plugins/:plugin_id/authorize/complete", CompletePluginAuthorization(deps.Plugins))
	h.POST("/api/plugins/:plugin_id/trigger", TriggerPlugin(deps.Plugins))
	// Skills：扫描仓库 SKILL.md，后台控制启用状态和 M3/M5 生效范围。
	h.GET("/api/skills", ListSkills(deps.Skills))
	h.POST("/api/skills/scan", ScanSkills(deps.Skills))
	h.PUT("/api/skills/:skill_name", UpdateSkill(deps.Skills))
	h.GET("/api/skills/:skill_name/content", GetSkillContent(deps.Skills))
	h.GET("/api/skills/:skill_name/source", GetSkillSource(deps.Skills))
	h.PUT("/api/skills/:skill_name/source", UpdateSkillSource(deps.Skills))
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
	// 持久多 Agent 对话（SSE）。未启用（nil）则不注册整组路由。
	if deps.Chat != nil {
		h.GET("/api/chat/agents", ListChatAgents(deps.Chat))
		h.GET("/api/chat/agents/:agent_id/models", ListChatModels(deps.Chat))
		h.GET("/api/chat/sessions", ListChatSessions(deps.Chat))
		h.POST("/api/chat/sessions", CreateChatSession(deps.Chat))
		h.GET("/api/chat/sessions/:session_id", GetChatSession(deps.Chat))
		h.PATCH("/api/chat/sessions/:session_id", UpdateChatSession(deps.Chat))
		h.DELETE("/api/chat/sessions/:session_id", DeleteChatSession(deps.Chat))
		h.POST("/api/chat/sessions/:session_id/messages", StreamChatSession(deps.Chat))
		h.POST("/api/chat/sessions/:session_id/cancel", CancelChatSession(deps.Chat))
		h.POST("/api/chat/sessions/:session_id/attachments", UploadChatAttachment(deps.Chat))
		h.DELETE("/api/chat/sessions/:session_id/attachments/:attachment_id", DeleteChatAttachment(deps.Chat))
		h.GET("/api/chat/attachments/:attachment_id/content", DownloadChatAttachment(deps.Chat))
	}
	h.GET("/api/web-config", GetWebConfig(deps.PublicBaseURL))
	// 精确 API 路由优先于这个兜底。必须在进程注册根 StaticFS 之前拦住
	// 未知 /api/*，否则 Hertz 会把它当作 web/dist 下的静态文件并返回
	// 非 JSON 404，调用方拿不到 logid，服务端也会打印误导性的文件错误。
	h.Any("/api/*path", apiNotFound())
	return nil
}
