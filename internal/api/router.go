// Package api 注册 Hertz 路由。管理后台 REST 与各模块内部接口都挂在这里。
package api

import (
	"fmt"

	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/chat"
	"jarvis/internal/decide"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/insight"
	"jarvis/internal/knowledge"
	"jarvis/internal/progress"
	"jarvis/internal/scheduledtask"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/textstore"
	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/app/server"
	"gorm.io/gorm"
)

// Dependencies are process-level dependencies shared by API handlers.
type Dependencies struct {
	DB                  *gorm.DB
	Todos               extract.TodoReader
	Confirmations       decide.ConfirmationService
	ConfirmationDetails decide.ConfirmationDetailReader
	Tasks               execute.TaskService
	Executor            *execute.AgentExecutor
	Projects            *background.ProjectService
	Persons             *background.PersonService
	Groups              *background.GroupBackgroundService
	Resolve             *background.ResolveService
	Profile             *background.ProfileService
	Resources           *background.ResourceService
	SharedMemory        *sharedmem.SharedMemoryService
	WorkRules           *workrule.Service
	TextStorage         *textstore.Service
	ScheduledTasks      *scheduledtask.Service
	Skills              *skill.Service
	RelationFacts       knowledge.FactService
	Progress            progress.EventService
	Overview            *insight.OverviewService
	Digests             *insight.DigestService
	DailyDigests        DailyDigestService      // 每日进度总结（个人 codex + 关键群 qwen）；nil 则不注册 /api/daily-digests 路由
	Worklog             *insight.WorklogService // 进度页「今天的文档」「项目代码」两个 Tab
	DigestSummarizer    *insight.Summarizer     // 可选：codex 未启用时为 nil，总结接口返回 503
	Debug               *insight.DebugService
	Logs                *insight.LogReader
	Chat                *chat.Service    // 可选：chat 未启用时为 nil，此时不注册 /api/chat 路由
	Capture             *capture.Service // 调试面板手动采集触发；nil 则不注册 /api/debug/capture/* 路由
}

// Register 把所有路由挂到 Hertz 实例上。
func Register(h *server.Hertz, deps Dependencies) error {
	if h == nil {
		return fmt.Errorf("api hertz server is nil")
	}
	if deps.DB == nil {
		return fmt.Errorf("api mysql dependency is nil")
	}
	if deps.Todos == nil {
		return fmt.Errorf("api todo reader dependency is nil")
	}
	if deps.Confirmations == nil {
		return fmt.Errorf("api confirmation service dependency is nil")
	}
	if deps.ConfirmationDetails == nil {
		return fmt.Errorf("api confirmation detail reader dependency is nil")
	}
	if deps.Tasks == nil {
		return fmt.Errorf("api Task service dependency is nil")
	}
	if deps.Projects == nil {
		return fmt.Errorf("api project service dependency is nil")
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
	if deps.TextStorage == nil {
		return fmt.Errorf("api text storage service dependency is nil")
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
	if deps.Overview == nil {
		return fmt.Errorf("api overview service dependency is nil")
	}
	if deps.Digests == nil {
		return fmt.Errorf("api digest service dependency is nil")
	}
	if deps.Debug == nil {
		return fmt.Errorf("api debug service dependency is nil")
	}
	if deps.Logs == nil {
		return fmt.Errorf("api log reader dependency is nil")
	}
	h.GET("/healthz", Health(deps.DB))
	h.GET("/api/todos", ListTodos(deps.Todos))
	h.GET("/api/todos/:todo_id", GetTodo(deps.Todos))
	h.GET("/api/confirmations", ListConfirmations(deps.Todos))
	h.GET("/api/confirmations/:todo_id", GetConfirmation(deps.ConfirmationDetails))
	h.POST("/api/confirmations/:todo_id/approve", ApproveConfirmation(deps.Confirmations))
	h.POST("/api/confirmations/:todo_id/reject", RejectConfirmation(deps.Confirmations))
	h.POST("/api/confirmations/:todo_id/supplement", SupplementConfirmation(deps.Confirmations))
	h.GET("/api/tasks", ListTasks(deps.Tasks))
	h.GET("/api/tasks/:task_id/runs", ListTaskRuns(deps.Tasks))
	h.GET("/api/tasks/:task_id/events", ListTaskEvents(deps.Progress))
	h.POST("/api/tasks/:task_id/finish", FinishTask(deps.Tasks))
	h.POST("/api/tasks/:task_id/supplement", SupplementTask(deps.Tasks))
	h.GET("/api/relation-facts", ListRelationFacts(deps.RelationFacts))
	h.POST("/api/relation-facts", CreateRelationFact(deps.RelationFacts))
	h.PUT("/api/relation-facts/:fact_id", UpdateRelationFact(deps.RelationFacts))
	h.DELETE("/api/relation-facts/:fact_id", DeleteRelationFact(deps.RelationFacts))
	if deps.Executor != nil {
		h.POST("/api/tasks/:task_id/execute", ExecuteTask(deps.Executor))
		h.POST("/api/tasks/:task_id/rerun", RerunTask(deps.Executor))
		h.POST("/api/tasks/:task_id/reapply", ReapplyTask(deps.Executor))
		h.POST("/api/tasks/:task_id/approve", ApproveTask(deps.Executor))
		h.POST("/api/tasks/:task_id/reject", RejectTask(deps.Executor))
	}
	// M1 背景管理：Project/Person 全量 CRUD；Group 只可改人工背景字段（采集字段归 M2）。
	h.GET("/api/projects", ListProjects(deps.Projects))
	h.POST("/api/projects", CreateProject(deps.Projects))
	h.GET("/api/projects/:project_id", GetProject(deps.Projects))
	h.PUT("/api/projects/:project_id", UpdateProject(deps.Projects))
	h.DELETE("/api/projects/:project_id", DeleteProject(deps.Projects))
	h.GET("/api/projects/:project_id/events", ListProjectEvents(deps.Progress))
	h.POST("/api/projects/:project_id/events", AppendProjectEvent(deps.Progress))
	h.GET("/api/persons", ListPersons(deps.Persons))
	h.POST("/api/persons/resolve", ResolvePerson(deps.Resolve))
	h.POST("/api/persons", CreatePerson(deps.Persons))
	h.GET("/api/persons/:person_id", GetPerson(deps.Persons))
	h.PUT("/api/persons/:person_id", UpdatePerson(deps.Persons))
	h.DELETE("/api/persons/:person_id", DeletePerson(deps.Persons))
	h.GET("/api/groups", ListGroups(deps.Groups))
	h.PUT("/api/groups/:group_id", UpdateGroupBackground(deps.Groups))
	// 决策主体（“我”）：单例 profile，读取 + upsert。
	h.GET("/api/profile", GetProfile(deps.Profile))
	h.PUT("/api/profile", UpdateProfile(deps.Profile))
	// 共享记忆：全局单例大文本，读取 + 整段覆盖保存。
	h.GET("/api/shared-memory", GetSharedMemory(deps.SharedMemory))
	h.PUT("/api/shared-memory", UpdateSharedMemory(deps.SharedMemory))
	// 工作规则：可信、分阶段注入 M3/M4/M5；支持全阶段或指定一个/多个阶段。
	h.GET("/api/work-rules", ListWorkRules(deps.WorkRules))
	h.POST("/api/work-rules", CreateWorkRule(deps.WorkRules))
	h.PUT("/api/work-rules/:work_rule_id", UpdateWorkRule(deps.WorkRules))
	h.DELETE("/api/work-rules/:work_rule_id", DeleteWorkRule(deps.WorkRules))
	// 通用纯文本存储：审批规则等运行时提示词由后台实时维护。
	h.GET("/api/text-storage", ListTextStorage(deps.TextStorage))
	h.POST("/api/text-storage", CreateTextStorage(deps.TextStorage))
	h.PUT("/api/text-storage/:text_storage_id", UpdateTextStorage(deps.TextStorage))
	h.DELETE("/api/text-storage/:text_storage_id", DeleteTextStorage(deps.TextStorage))
	// 周期定时任务：独立 CRUD、手动触发；自动执行由进程内每分钟 scheduler 负责。
	h.GET("/api/scheduled-tasks", ListScheduledTasks(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks", CreateScheduledTask(deps.ScheduledTasks))
	h.PUT("/api/scheduled-tasks/:scheduled_task_id", UpdateScheduledTask(deps.ScheduledTasks))
	h.DELETE("/api/scheduled-tasks/:scheduled_task_id", DeleteScheduledTask(deps.ScheduledTasks))
	h.POST("/api/scheduled-tasks/:scheduled_task_id/trigger", TriggerScheduledTask(deps.ScheduledTasks))
	// Skills：扫描仓库 SKILL.md，后台控制启用状态和 M3/M4/M5 生效范围。
	h.GET("/api/skills", ListSkills(deps.Skills))
	h.POST("/api/skills/scan", ScanSkills(deps.Skills))
	h.PUT("/api/skills/:skill_id", UpdateSkill(deps.Skills))
	h.GET("/api/skills/:skill_name/content", GetSkillContent(deps.Skills))
	// Overview 看板 + 进度：跨模块只读聚合，无表无 cron；总结按需调 codex。
	h.GET("/api/overview", GetOverview(deps.Overview))
	h.GET("/api/digests", GetDigests(deps.Digests))
	h.POST("/api/digests/summarize", SummarizeDigest(deps.Digests, deps.DigestSummarizer))
	// 每日进度总结：按日期读当天全部 scope + 异步触发单条生成/重算。
	if deps.DailyDigests != nil {
		h.GET("/api/daily-digests", GetDailyDigests(deps.DailyDigests))
		h.POST("/api/daily-digests/generate", GenerateDailyDigest(deps.DailyDigests))
	}
	// 进度页工作日志：我今天写/收到的文档、我今天在各仓库的 MR（实时调 bytedcli）。
	if deps.Worklog != nil {
		h.GET("/api/worklog/commits", GetWorklogCommits(deps.Worklog))
		h.GET("/api/worklog/documents", GetWorklogDocuments(deps.Worklog))
	}
	// 调试面板：依赖健康/表计数/积压、模块运行、采集流水、抽取水位、最近 todo/task、运行日志尾读。
	h.GET("/api/debug/status", GetDebugStatus(deps.Debug))
	h.GET("/api/debug/modules", GetDebugModules(deps.Debug))
	h.GET("/api/debug/failures", GetDebugFailures(deps.Debug))
	h.GET("/api/debug/scans", GetDebugScans(deps.Debug))
	h.GET("/api/debug/watermarks", GetDebugWatermarks(deps.Debug))
	h.GET("/api/debug/todos", GetDebugTodos(deps.Debug))
	h.GET("/api/debug/tasks", GetDebugTasks(deps.Debug))
	h.GET("/api/debug/logs", GetDebugLogs(deps.Logs))
	// 调试面板手动触发：手动跑一轮 M1 采集，无需等 cron。
	if deps.Capture != nil {
		h.POST("/api/debug/capture/discover", DiscoverChatsManually(deps.Capture))
		h.POST("/api/debug/capture/scan-related", ScanRelatedManually(deps.Capture))
		h.POST("/api/debug/capture/scan-chat", ScanChatManually(deps.Capture))
	}
	// 手动维护的资源：可关联 人/项目/我，供后台管理与 M3 工具按需查询。
	h.GET("/api/resources", ListResources(deps.Resources))
	h.POST("/api/resources", CreateResource(deps.Resources))
	h.GET("/api/resources/:resource_id", GetResource(deps.Resources))
	h.PUT("/api/resources/:resource_id", UpdateResource(deps.Resources))
	h.DELETE("/api/resources/:resource_id", DeleteResource(deps.Resources))
	// 基于 codex CLI 的流式对话（SSE）。与 execute 一致：未启用（nil）则不注册路由。
	if deps.Chat != nil {
		h.POST("/api/chat", Chat(deps.Chat))
	}
	return nil
}
