// Command jarvis-server 是 Jarvis 的单体主进程（总纲 §1.1）。
//
// 当前启动链路：加载配置 → 连接 SQLite → 迁移核心表 → 起 Hertz。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/agentconfig"
	"jarvis/internal/api"
	"jarvis/internal/ark"
	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/cardapproval"
	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/contextsnap"
	"jarvis/internal/dailydigest"
	"jarvis/internal/effectops"
	"jarvis/internal/embedding"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/extract/codexengine"
	"jarvis/internal/extract/provider"
	"jarvis/internal/factengine"
	"jarvis/internal/insight"
	"jarvis/internal/knowledge"
	"jarvis/internal/larkcli"
	"jarvis/internal/meetingsweep"
	"jarvis/internal/morningbrief"
	"jarvis/internal/observability"
	"jarvis/internal/pipeline"
	"jarvis/internal/proactive"
	"jarvis/internal/progress"
	"jarvis/internal/scheduledtask"
	"jarvis/internal/semantic"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/store"
	"jarvis/internal/taskcreate"
	"jarvis/internal/textstore"
	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库迁移，成功后退出")
	backfillProgressEvents := flag.Bool("backfill-progress-events", false, "为无事件历史的存量 Task 写入一次当前状态快照，成功后退出")
	discoverOnce := flag.Bool("discover-once", false, "执行一次飞书会话发现，成功后退出")
	scanChat := flag.String("scan-chat", "", "增量扫描指定飞书 chat_id，成功后退出")
	setRelatedGroups := flag.String("set-related-groups", "", "用逗号分隔的 chat_id 原子替换 related_group，成功后退出")
	extractFactsOnce := flag.Bool("extract-facts-once", false, "执行一次持续世界建模，成功后退出")
	extractOnce := flag.Bool("extract-once", false, "执行一次 Todo 提取，成功后退出")
	proactiveOnce := flag.Bool("proactive-once", false, "立即执行一次主动巡视，成功后退出；写操作通过当前运行中的 Jarvis API 完成")
	meetingSweepOnce := flag.Bool("meeting-sweep-once", false, "立即执行一次会议巡扫，成功后退出；线索通过当前运行中的 Jarvis API 投递")
	morningBriefOnce := flag.Bool("morning-brief-once", false, "立即执行一次晨间作战简报，成功后退出；手动默认只写本地 Markdown，不投递飞书")
	morningBriefDeliver := flag.Bool("morning-brief-deliver", false, "立即执行一次晨间作战简报并按定时语义投递给 Principal（当天已投递过则只更新文件），成功后退出")
	openP2P := flag.Bool("open-p2p", false, "把存量内部私聊(p2p)一次性纳入监听(related_group=1)，成功后退出")
	flag.Parse()
	// Hertz 默认到 Debug，会把每条路由注册和每次客户端断连都写进日志，压过 cron
	// 运行结果。运行日志要给人和调试面板看，保持 Info。
	hlog.SetLevel(hlog.LevelInfo)
	startupCtx := observability.EnsureLogID(context.Background())
	// hlog.CtxFatalf already exits, but keep the explicit call so a logger swap
	// can never leave main running past a failed dependency and dying later on a
	// nil pointer, which would hide the real reason behind a SIGSEGV.
	fatalf := func(format string, args ...any) {
		hlog.CtxFatalf(startupCtx, format, args...)
		os.Exit(1)
	}
	errorf := func(format string, args ...any) {
		hlog.CtxErrorf(startupCtx, format, args...)
	}
	infof := func(format string, args ...any) {
		hlog.CtxInfof(startupCtx, format, args...)
	}
	actionCount := 0
	for _, selected := range []bool{*migrateOnly, *backfillProgressEvents, *discoverOnce, *scanChat != "", *setRelatedGroups != "", *extractFactsOnce, *extractOnce, *proactiveOnce, *openP2P} {
		if selected {
			actionCount++
		}
	}
	if actionCount > 1 {
		fatalf("one-shot action flags are mutually exclusive")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		// fail-fast：配置错误启动即暴露，不带缺陷跑起来
		fatalf("load config failed: %v", err)
	}
	configPathAbsolute, err := filepath.Abs(*configPath)
	if err != nil {
		fatalf("resolve config path failed: %v", err)
	}
	textFileService, err := textstore.NewService(filepath.Join(filepath.Dir(configPathAbsolute), "prompts"))
	if err != nil {
		fatalf("initialize text file service failed: %v", err)
	}
	sharedMemoryPath, err := sharedmem.PathForConfig(configPathAbsolute)
	if err != nil {
		fatalf("resolve shared memory path failed: %v", err)
	}
	sharedMemoryService, err := sharedmem.NewSharedMemoryService(sharedMemoryPath)
	if err != nil {
		fatalf("initialize shared memory service failed: %v", err)
	}
	workRuleService, err := workrule.NewService(filepath.Join(filepath.Dir(configPathAbsolute), "rules"))
	if err != nil {
		fatalf("initialize work rule service failed: %v", err)
	}
	agentConfigService, err := agentconfig.NewService(textFileService, workRuleService)
	if err != nil {
		fatalf("initialize agent config service failed: %v", err)
	}
	skillService, err := skill.NewService(cfg.Skills.Root, filepath.Join(filepath.Dir(configPathAbsolute), "skills.yaml"))
	if err != nil {
		fatalf("initialize agent skill service failed: %v", err)
	}

	connectCtx, cancel := context.WithTimeout(startupCtx, 10*time.Second)
	defer cancel()
	db, err := store.OpenSQLite(connectCtx, cfg.SQLite)
	if err != nil {
		fatalf("connect sqlite failed: %v", err)
	}
	defer func() {
		if err := store.Close(db); err != nil {
			errorf("close sqlite failed: %v", err)
		}
	}()

	if err := store.Migrate(db); err != nil {
		fatalf("migrate sqlite failed: %v", err)
	}
	if *migrateOnly {
		infof("sqlite schema migration completed")
		return
	}
	if *backfillProgressEvents {
		stats, err := progress.BackfillTaskSnapshots(startupCtx, db, time.Now().UTC())
		if err != nil {
			fatalf("backfill task progress snapshots failed: %v", err)
		}
		infof("progress event backfill completed: tasks_scanned=%d events_created=%d", stats.TasksScanned, stats.EventsCreated)
		return
	}
	relationFactService, err := knowledge.NewService(db)
	if err != nil {
		fatalf("initialize relation fact service failed: %v", err)
	}
	progressService, err := progress.NewService(db)
	if err != nil {
		fatalf("initialize progress service failed: %v", err)
	}
	if _, err := skillService.Scan(startupCtx); err != nil {
		fatalf("scan agent skills failed: %v", err)
	}

	materializer, err := execute.NewMaterializer(db)
	if err != nil {
		fatalf("initialize Todo materializer failed: %v", err)
	}

	larkClient, err := larkcli.New(larkcli.Options{
		Bin:         cfg.LarkCLI.Bin,
		Profile:     cfg.LarkCLI.Profile,
		RateLimit:   cfg.LarkCLI.RateLimit,
		Burst:       cfg.LarkCLI.Burst,
		Concurrency: cfg.LarkCLI.Concurrent,
		Timeout:     time.Duration(cfg.LarkCLI.TimeoutSec) * time.Second,
	})
	if err != nil {
		fatalf("initialize lark-cli failed: %v", err)
	}
	location, err := time.LoadLocation(cfg.Capture.Timezone)
	if err != nil {
		fatalf("load capture timezone failed: %v", err)
	}
	captureService, err := capture.NewService(db, larkClient, capture.Options{
		PageSize:           cfg.Capture.PageSize,
		ScanWorkers:        cfg.Capture.ScanWorkers,
		HotAge:             time.Duration(cfg.Capture.HotAgeHours) * time.Hour,
		WarmAge:            time.Duration(cfg.Capture.WarmAgeHours) * time.Hour,
		Location:           location,
		PrincipalOpenID:    cfg.Extract.PrincipalOpenID,
		SearchOverlap:      10 * time.Minute,
		ActivationContext:  time.Duration(cfg.Extract.ContextWindowMinutes) * time.Minute,
		AutoRelatedP2PTopN: cfg.Capture.AutoRelatedP2PTopN,
	})
	if err != nil {
		fatalf("initialize capture service failed: %v", err)
	}
	factEngineStore, err := factengine.NewGORMStore(db)
	if err != nil {
		fatalf("initialize fact engine store failed: %v", err)
	}
	factExtractor, err := factengine.NewExtractor(factengine.ExtractorOptions{
		Bin:             cfg.FactEngine.Bin,
		Model:           cfg.FactEngine.Model,
		ReasoningEffort: cfg.FactEngine.ReasoningEffort,
		Sandbox:         cfg.FactEngine.Sandbox,
		WorkspaceRoot:   filepath.Dir(filepath.Dir(configPathAbsolute)),
		Timeout:         time.Duration(cfg.FactEngine.TimeoutSec) * time.Second,
	})
	if err != nil {
		fatalf("initialize fact engine extractor failed: %v", err)
	}
	factSources := []factengine.MaterialSource{
		{Name: factengine.SourceMessage, StartAtPresent: true, MaxID: factEngineStore.MaxMessageID, Units: factEngineStore.MessageUnits},
		{Name: factengine.SourceTodo, MaxID: factEngineStore.MaxTodoEventID, Units: factEngineStore.TodoUnits},
		{Name: factengine.SourceTask, MaxID: factEngineStore.MaxTaskEventID, Units: factEngineStore.TaskUnits},
	}
	factEngineWorker, err := factengine.NewWorker(factEngineStore, factSources, factExtractor, factengine.WorkerOptions{
		BatchLimit:       cfg.FactEngine.BatchLimit,
		MaxMaterialChars: cfg.FactEngine.MaxMaterialChars,
		Window: factengine.WindowOptions{
			Gap:         time.Duration(cfg.FactEngine.WindowGapMinutes) * time.Minute,
			MaxMessages: cfg.FactEngine.WindowMaxMessages,
			Location:    location,
		},
		Prompts: textFileService,
	})
	if err != nil {
		fatalf("initialize fact engine worker failed: %v", err)
	}
	factRollupExtractor, err := factengine.NewExtractor(factengine.ExtractorOptions{
		Bin:           cfg.FactEngine.Bin,
		Model:         cfg.FactEngine.RollupModel,
		Sandbox:       "read-only",
		WorkspaceRoot: filepath.Dir(filepath.Dir(configPathAbsolute)),
		Timeout:       time.Duration(cfg.FactEngine.TimeoutSec) * time.Second,
	})
	if err != nil {
		fatalf("initialize fact rollup extractor failed: %v", err)
	}
	todoStore, err := extract.NewTodoStore(db)
	if err != nil {
		fatalf("initialize todo store failed: %v", err)
	}
	taskService, err := execute.NewStore(db)
	if err != nil {
		fatalf("initialize MVP Task service failed: %v", err)
	}
	contextAssembler, err := contextsnap.NewAssembler(db, cfg.Extract.PrincipalOpenID)
	if err != nil {
		fatalf("initialize common context snapshot assembler failed: %v", err)
	}
	factRollupWorker, err := factengine.NewRollupWorker(db, factRollupExtractor, progressService, textFileService, contextAssembler, location)
	if err != nil {
		fatalf("initialize fact rollup worker failed: %v", err)
	}
	taskFactory, err := taskcreate.NewFactory(db, contextAssembler)
	if err != nil {
		fatalf("initialize Task factory failed: %v", err)
	}
	taskSubmitter, err := taskcreate.NewSubmitter(taskFactory)
	if err != nil {
		fatalf("initialize Task submitter failed: %v", err)
	}
	messageRecaller, err := effectops.NewMessageRecaller(db, larkClient)
	if err != nil {
		fatalf("initialize feishu message recaller failed: %v", err)
	}
	codexRunner, err := execute.NewCodexRunner(
		cfg.Execute.Bin, cfg.Execute.Model, cfg.Execute.ReasoningEffort,
		time.Duration(cfg.Execute.TimeoutSecond)*time.Second,
	)
	if err != nil {
		fatalf("initialize execute runner failed: %v", err)
	}
	dailyDigestRunner, err := execute.NewCodexRunner(
		cfg.Execute.Bin, cfg.Execute.Model, cfg.Execute.ReasoningEffort,
		time.Duration(cfg.DailyDigest.TimeoutSeconds)*time.Second,
	)
	if err != nil {
		fatalf("initialize daily digest runner failed: %v", err)
	}
	proactiveRunner, err := execute.NewCodexRunner(
		cfg.Proactive.Bin, cfg.Proactive.Model, cfg.Proactive.ReasoningEffort,
		time.Duration(cfg.Proactive.TimeoutSeconds)*time.Second,
	)
	if err != nil {
		fatalf("initialize proactive runner failed: %v", err)
	}
	proactiveStore, err := proactive.NewStore(db)
	if err != nil {
		fatalf("initialize proactive run store failed: %v", err)
	}
	proactiveWorker, err := proactive.NewWorker(proactive.Options{
		Runner:        proactiveRunner,
		Recorder:      proactiveStore,
		Prompts:       textFileService,
		SharedMemory:  sharedMemoryService,
		Sandbox:       cfg.Proactive.Sandbox,
		WorkspaceRoot: filepath.Dir(filepath.Dir(configPathAbsolute)),
		Location:      location,
		Engine:        cfg.Proactive.Bin,
		Model:         cfg.Proactive.Model,
	})
	if err != nil {
		fatalf("initialize proactive worker failed: %v", err)
	}
	meetingSweepRunner, err := execute.NewCodexRunner(
		cfg.MeetingSweep.Bin, cfg.MeetingSweep.Model, cfg.MeetingSweep.ReasoningEffort,
		time.Duration(cfg.MeetingSweep.TimeoutSeconds)*time.Second,
	)
	if err != nil {
		fatalf("initialize meeting sweep runner failed: %v", err)
	}
	meetingSweepWorker, err := meetingsweep.NewWorker(meetingsweep.Options{
		Runner:        meetingSweepRunner,
		Prompts:       textFileService,
		Sandbox:       cfg.MeetingSweep.Sandbox,
		WorkspaceRoot: filepath.Dir(filepath.Dir(configPathAbsolute)),
		Location:      location,
		Engine:        cfg.MeetingSweep.Bin,
		Model:         cfg.MeetingSweep.Model,
	})
	if err != nil {
		fatalf("initialize meeting sweep worker failed: %v", err)
	}
	morningBriefRunner, err := execute.NewCodexRunner(
		cfg.MorningBrief.Bin, cfg.MorningBrief.Model, cfg.MorningBrief.ReasoningEffort,
		time.Duration(cfg.MorningBrief.TimeoutSeconds)*time.Second,
	)
	if err != nil {
		fatalf("initialize morning brief runner failed: %v", err)
	}
	morningBriefWorker, err := morningbrief.NewWorker(morningbrief.Options{
		Runner:        morningBriefRunner,
		Prompts:       textFileService,
		Sandbox:       cfg.MorningBrief.Sandbox,
		WorkspaceRoot: filepath.Dir(filepath.Dir(configPathAbsolute)),
		Location:      location,
	})
	if err != nil {
		fatalf("initialize morning brief worker failed: %v", err)
	}
	morningBriefReader, err := morningbrief.NewReader(filepath.Dir(filepath.Dir(configPathAbsolute)), location)
	if err != nil {
		fatalf("initialize morning brief reader failed: %v", err)
	}
	var approvalNotifier execute.ApprovalNotifier
	if cfg.CardApproval.Enabled {
		approvalClient, err := larkcli.New(larkcli.Options{
			Bin:         cfg.LarkCLI.Bin,
			Profile:     cfg.CardApproval.Profile,
			RateLimit:   cfg.LarkCLI.RateLimit,
			Burst:       cfg.LarkCLI.Burst,
			Concurrency: cfg.LarkCLI.Concurrent,
			Timeout:     time.Duration(cfg.LarkCLI.TimeoutSec) * time.Second,
		})
		if err != nil {
			fatalf("initialize approval lark-cli failed: %v", err)
		}
		approvalNotifier, err = cardapproval.NewNotifier(approvalClient, cfg.CardApproval.PrincipalOpenID, cfg.Server.Addr)
		if err != nil {
			fatalf("initialize approval notifier failed: %v", err)
		}
	}
	agentExecutor, err := execute.NewAgentExecutor(
		taskService, codexRunner, sharedMemoryService, workRuleService, textFileService, skillService, approvalNotifier, cfg.Execute.RepoRoot, cfg.Execute.RunsDir,
	)
	if err != nil {
		fatalf("initialize agent executor failed: %v", err)
	}
	scheduledTaskService, err := scheduledtask.NewService(
		db, taskSubmitter, agentExecutor, cfg.ScheduledTask.BatchLimit,
	)
	if err != nil {
		fatalf("initialize scheduled task service failed: %v", err)
	}
	projectService, err := background.NewProjectService(db)
	if err != nil {
		fatalf("initialize project service failed: %v", err)
	}
	keyMatterService, err := background.NewKeyMatterService(db)
	if err != nil {
		fatalf("initialize key matter service failed: %v", err)
	}
	personService, err := background.NewPersonService(db)
	if err != nil {
		fatalf("initialize person service failed: %v", err)
	}
	groupService, err := background.NewGroupBackgroundService(db, captureService)
	if err != nil {
		fatalf("initialize group background service failed: %v", err)
	}
	resolveService, err := background.NewResolveService(larkClient)
	if err != nil {
		fatalf("initialize person resolve service failed: %v", err)
	}
	profileService, err := background.NewProfileService(db, cfg.Extract.PrincipalOpenID)
	if err != nil {
		fatalf("initialize principal profile service failed: %v", err)
	}
	resourceService, err := background.NewResourceService(db)
	if err != nil {
		fatalf("initialize resource service failed: %v", err)
	}
	overviewService, err := insight.NewOverviewService(db)
	if err != nil {
		fatalf("initialize overview service failed: %v", err)
	}
	digestService, err := insight.NewDigestService(db, location)
	if err != nil {
		fatalf("initialize digest service failed: %v", err)
	}
	meetingReviewService, err := insight.NewMeetingReviewService(db, location)
	if err != nil {
		fatalf("initialize meeting review service failed: %v", err)
	}
	worklogService, err := insight.NewWorklogService(db, location)
	if err != nil {
		fatalf("initialize worklog service failed: %v", err)
	}
	// 进度总结按需复用 M5 的 codex runner（read-only 出纯文本）；codex 不可用时留空，接口返回 503。
	digestSummarizer, err := insight.NewSummarizer(codexRunner)
	if err != nil {
		fatalf("initialize digest summarizer failed: %v", err)
	}
	// 每日进度总结：个人与关键群统一复用 execute 段的官方 codex runner，
	// danger-full-access + 联网自跑 lark-cli/bytedcli/git，并分别注入对应 Skill。
	dailyDigestService, err := dailydigest.NewService(dailydigest.Options{
		DB:              db,
		Location:        location,
		Runner:          dailyDigestRunner,
		PrincipalOpenID: cfg.Extract.PrincipalOpenID,
		GitAuthor:       cfg.DailyDigest.GitAuthor,
		RepoRoot:        cfg.Execute.RepoRoot,
		WorkspaceRoot:   filepath.Dir(filepath.Dir(configPathAbsolute)),
		PersonSkillDir:  filepath.Join(cfg.Skills.Root, "summarize-person-day"),
		GroupSkillDir:   filepath.Join(cfg.Skills.Root, "feishu-group-daily-summary"),
		SummarySandbox:  "danger-full-access",
		GroupMsgLimit:   cfg.DailyDigest.GroupMessageLimit,
		GroupConcur:     cfg.DailyDigest.GroupConcurrency,
	})
	if err != nil {
		fatalf("initialize daily digest service failed: %v", err)
	}
	logReader, err := insight.NewLogReader(cfg.Server.LogFiles)
	if err != nil {
		fatalf("initialize log reader failed: %v", err)
	}
	debugService, err := insight.NewDebugService(db, logReader)
	if err != nil {
		fatalf("initialize debug service failed: %v", err)
	}
	var extractWorker *extract.Worker
	var semanticIndex *semantic.Index
	if cfg.Extract.Enabled || *extractOnce {
		modelClient, err := provider.NewClient(
			ark.BaseURL,
			ark.APIKey,
			cfg.Model.Model,
			time.Duration(cfg.Model.TimeoutSec)*time.Second,
		)
		if err != nil {
			fatalf("initialize extraction model client failed: %v", err)
		}
		embeddingClient, err := embedding.NewClient(time.Duration(cfg.Model.TimeoutSec) * time.Second)
		if err != nil {
			fatalf("initialize Todo embedding client failed: %v", err)
		}
		semanticIndex, err = semantic.NewIndex(semantic.Options{
			Host: cfg.Extract.QdrantHost, Port: cfg.Extract.QdrantGRPCPort,
			Collection: cfg.Extract.SemanticCollection, EmbeddingModel: ark.EmbeddingModel,
			Dimensions:     ark.EmbeddingDimensions,
			ScoreThreshold: cfg.Extract.SemanticThreshold, NeighborLimit: cfg.Extract.SemanticNeighborLimit,
			ActiveStatuses: extract.ActiveTodoStatuses(),
		})
		if err != nil {
			fatalf("initialize Todo semantic index failed: %v", err)
		}
		ensureCtx, cancelEnsure := context.WithTimeout(startupCtx, 10*time.Second)
		if err := semanticIndex.Ensure(ensureCtx); err != nil {
			cancelEnsure()
			fatalf("ensure Todo semantic index failed: %v", err)
		}
		cancelEnsure()
		pipelineStore, err := extract.NewPipelineStore(db, location, semanticIndex, cfg.Extract.PrincipalOpenID)
		if err != nil {
			fatalf("initialize extraction pipeline store failed: %v", err)
		}
		deduplicator, err := extract.NewDeduplicator(embeddingClient, semanticIndex, pipelineStore, modelClient)
		if err != nil {
			fatalf("initialize Todo semantic deduplicator failed: %v", err)
		}
		toolBoxBuilder, err := extract.NewRegistryToolBoxBuilder(db, extract.ToolBoxConfig{
			ToolTimeout:     time.Duration(cfg.Extract.ToolTimeoutSec) * time.Second,
			HistoryMaxLimit: cfg.Extract.HistoryToolLimit,
			Location:        location,
		})
		if err != nil {
			fatalf("initialize extraction tool box builder failed: %v", err)
		}
		// Engine selection: codex self-runs CLIs only to collect the decisive facts
		// needed for Task admission (danger-full-access + network + low reasoning);
		// the model API is the alternate function-calling engine. The deduplicator
		// always uses modelClient for SameAction adjudication regardless.
		var extractionEngine extract.ToolExtractor = modelClient
		extractionModelName := cfg.Model.Model
		agentToolCatalog := false
		if cfg.Extract.Engine == "codex" {
			codexExtractor, err := codexengine.New(codexengine.Options{
				Bin: cfg.Codex.Bin, Model: cfg.Codex.Model,
				Sandbox: cfg.Extract.CodexSandbox, Network: cfg.Extract.CodexNetwork,
				ReasoningEffort: cfg.Extract.CodexReasoningEffort,
				Timeout:         time.Duration(cfg.Codex.TimeoutSeconds) * time.Second,
			})
			if err != nil {
				fatalf("initialize codex extraction engine failed: %v", err)
			}
			extractionEngine = codexExtractor
			extractionModelName = cfg.Codex.Model
			agentToolCatalog = true
		}
		extractWorker, err = extract.NewWorker(pipelineStore, extractionEngine, progressService, deduplicator, toolBoxBuilder, extract.WorkerOptions{
			Load: extract.LoadOptions{
				BatchMessages: cfg.Extract.BatchMessages, ContextMessages: cfg.Extract.ContextMessages,
				ContextWindow: time.Duration(cfg.Extract.ContextWindowMinutes) * time.Minute,
				OpenTodoLimit: cfg.Extract.OpenTodoLimit, RecentTaskLimit: cfg.Extract.RecentTaskLimit,
			},
			PrincipalOpenID: cfg.Extract.PrincipalOpenID, ModelName: extractionModelName,
			FactLimit: cfg.Extract.FactLimit, KeyPersonLimit: cfg.Extract.KeyPersonLimit,
			MaxPromptChars: cfg.Extract.MaxPromptChars, Location: location,
			EvidenceRetryMax: cfg.Extract.EvidenceRetryMax,
			AgentToolCatalog: agentToolCatalog,
			WorkRules:        workRuleService,
			Skills:           skillService,
			SystemPrompts:    textFileService,
		})
		if err != nil {
			fatalf("initialize extraction worker failed: %v", err)
		}
	}
	if semanticIndex != nil {
		defer func() {
			if err := semanticIndex.Close(); err != nil {
				errorf("close Todo semantic index failed: %v", err)
			}
		}()
	}
	if *discoverOnce {
		if err := captureService.DiscoverChats(startupCtx); err != nil {
			fatalf("discover chats failed: %v", err)
		}
		infof("chat discovery completed")
		return
	}
	if *scanChat != "" {
		if err := captureService.ScanChat(startupCtx, *scanChat); err != nil {
			fatalf("scan chat failed: %v", err)
		}
		infof("chat scan completed: %s", *scanChat)
		return
	}
	if *setRelatedGroups != "" {
		if err := captureService.ReplaceRelatedGroups(strings.Split(*setRelatedGroups, ",")); err != nil {
			fatalf("set related groups failed: %v", err)
		}
		infof("related groups replaced")
		return
	}
	if *openP2P {
		opened, err := captureService.OpenInternalP2P()
		if err != nil {
			fatalf("open internal p2p chats failed: %v", err)
		}
		infof("internal p2p chats opened for monitoring: %d", opened)
		return
	}
	if *extractFactsOnce {
		stats, err := factEngineWorker.ExtractOnce(startupCtx)
		if err != nil {
			fatalf("extract facts failed: %v", err)
		}
		infof(
			"world maintenance completed: calls=%d units=%d material_chars=%d sources=%+v result=%q",
			stats.Calls, stats.Units, stats.MaterialChars, stats.Sources, stats.Result,
		)
		return
	}
	if *extractOnce {
		chatIDs, err := extractWorker.PendingChatIDs(startupCtx)
		if err != nil {
			fatalf("list pending extraction chats failed: %v", err)
		}
		for _, chatID := range chatIDs {
			stats, _, err := extractWorker.ExtractChat(startupCtx, chatID)
			if err != nil {
				fatalf("extract todos chat_id=%s failed: %v", chatID, err)
			}
			infof(
				"todo extraction chat_id=%s: units=%d candidates=%d created=%d updated=%d",
				chatID, stats.Units, stats.Candidates, stats.Created, stats.Updated,
			)
		}
		infof("todo extraction completed: chats=%d", len(chatIDs))
		return
	}
	if *proactiveOnce {
		result, err := proactiveWorker.RunOnce(startupCtx)
		if err != nil {
			fatalf("proactive review failed: %v", err)
		}
		infof("proactive review completed: %s", result)
		return
	}
	if *meetingSweepOnce {
		result, err := meetingSweepWorker.RunOnce(startupCtx)
		if err != nil {
			fatalf("meeting sweep failed: %v", err)
		}
		infof("meeting sweep completed: %s", result)
		return
	}
	if *morningBriefOnce || *morningBriefDeliver {
		trigger := morningbrief.TriggerManual
		if *morningBriefDeliver {
			trigger = morningbrief.TriggerSchedule
		}
		result, err := morningBriefWorker.Run(startupCtx, trigger)
		if err != nil {
			fatalf("morning brief failed: %v", err)
		}
		infof("morning brief completed trigger=%s: %s", trigger, result)
		return
	}
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()

	stopPipelineScheduler := func() {}
	waitPipeline := func() {}
	if cfg.Extract.Enabled || cfg.Execute.Enabled {
		var (
			executionTaskStore *execute.Store
			executionAgent     *execute.AgentExecutor
		)
		if cfg.Execute.Enabled {
			executionTaskStore = taskService
			executionAgent = agentExecutor
		}
		coordinator, err := pipeline.NewCoordinator(
			extractWorker,
			materializer,
			executionTaskStore,
			executionAgent,
			pipeline.Options{
				ExtractConcurrency:   cfg.Extract.Concurrency,
				ExecutionBatchLimit:  cfg.Execute.BatchLimit,
				ExecutionConcurrency: cfg.Execute.Concurrency,
				StaleExecuting:       time.Duration(cfg.Execute.StaleExecutingMinute) * time.Minute,
				Logger:               log.New(os.Stderr, "pipeline ", log.LstdFlags|log.Lmicroseconds),
			},
		)
		if err != nil {
			fatalf("initialize real-time pipeline failed: %v", err)
		}
		if err := coordinator.Start(runtimeCtx); err != nil {
			fatalf("start real-time pipeline failed: %v", err)
		}
		waitPipeline = coordinator.Wait
		if cfg.Extract.Enabled {
			if err := captureService.SetScanObserver(coordinator); err != nil {
				cancelRuntime()
				waitPipeline()
				fatalf("wire capture to real-time pipeline failed: %v", err)
			}
		}
		if cfg.Execute.Enabled {
			if err := taskSubmitter.SetNotifier(coordinator); err != nil {
				cancelRuntime()
				waitPipeline()
				fatalf("wire Task submitter to pipeline failed: %v", err)
			}
		}
		pipelineScheduler, err := pipeline.StartScheduler(
			runtimeCtx,
			coordinator,
			pipeline.ScheduleConfig{
				Extract: cfg.Extract.Schedule,
				Execute: cfg.Execute.Schedule,
			},
			log.New(os.Stderr, "pipeline-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			cancelRuntime()
			waitPipeline()
			fatalf("start pipeline compensation scheduler failed: %v", err)
		}
		stopPipelineScheduler = func() { <-pipelineScheduler.Stop().Done() }
		if err := coordinator.ReconcileAll(runtimeCtx); err != nil {
			cancelRuntime()
			stopPipelineScheduler()
			waitPipeline()
			fatalf("queue startup pipeline reconciliation failed: %v", err)
		}
	}

	scheduler, err := capture.StartScheduler(runtimeCtx, captureService, capture.ScheduleConfig{
		Discover: cfg.Capture.DiscoverSchedule,
		Scan:     cfg.Capture.ScanSchedule,
	}, log.New(os.Stderr, "capture-cron ", log.LstdFlags|log.Lmicroseconds))
	if err != nil {
		cancelRuntime()
		stopPipelineScheduler()
		waitPipeline()
		fatalf("start capture scheduler failed: %v", err)
	}
	// 进程重启后，旧进程留下的生成任务不可能继续，启动时显式标失败。
	recoveredDailyDigests, err := dailyDigestService.RecoverInterrupted(runtimeCtx)
	if err != nil {
		cancelRuntime()
		<-scheduler.Stop().Done()
		stopPipelineScheduler()
		waitPipeline()
		fatalf("recover interrupted daily digests failed: %v", err)
	}
	if recoveredDailyDigests > 0 {
		infof("daily digests recovered after restart: %d", recoveredDailyDigests)
	}
	// 个人总结 cron：enabled 时起，disabled 时手动生成接口仍可用。
	stopDailyDigest := func() {}
	if cfg.DailyDigest.Enabled {
		dailyDigestScheduler, err := dailydigest.StartScheduler(
			runtimeCtx,
			dailyDigestService,
			cfg.DailyDigest.Schedule,
			log.New(os.Stderr, "daily-digest-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			cancelRuntime()
			<-scheduler.Stop().Done()
			stopPipelineScheduler()
			waitPipeline()
			fatalf("start daily digest scheduler failed: %v", err)
		}
		stopDailyDigest = func() { <-dailyDigestScheduler.Stop().Done() }
	}
	stopScheduledTasks := func() {}
	recovered, err := scheduledTaskService.RecoverRunning(runtimeCtx)
	if err != nil {
		fatalf("recover scheduled tasks failed: %v", err)
	}
	if recovered > 0 {
		infof("scheduled tasks recovered after restart: %d", recovered)
	}
	if cfg.ScheduledTask.Enabled {
		scheduledTaskScheduler, err := scheduledtask.StartScheduler(
			runtimeCtx, scheduledTaskService, cfg.ScheduledTask.Schedule,
			log.New(os.Stderr, "scheduledtask-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			fatalf("start scheduled task scheduler failed: %v", err)
		}
		stopScheduledTasks = func() { <-scheduledTaskScheduler.Stop().Done() }
	}
	// 持续世界建模 cron：跑在关键路径之外，disabled 时 -extract-facts-once 仍可手动跑一轮。
	stopFactEngine := func() {}
	stopFactRollup := func() {}
	if cfg.FactEngine.Enabled {
		factEngineScheduler, err := factengine.StartScheduler(
			runtimeCtx, factEngineWorker, cfg.FactEngine.Schedule,
			log.New(os.Stderr, "factengine-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			fatalf("start fact engine scheduler failed: %v", err)
		}
		stopFactEngine = func() { <-factEngineScheduler.Stop().Done() }
		factRollupScheduler, err := factengine.StartRollupScheduler(
			runtimeCtx, factRollupWorker, cfg.FactEngine.RollupSchedule,
			log.New(os.Stderr, "factrollup-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			fatalf("start fact rollup scheduler failed: %v", err)
		}
		stopFactRollup = func() { <-factRollupScheduler.Stop().Done() }
	}
	var cardApprovalProcessor api.CardApprovalProcessor
	if cfg.CardApproval.Enabled {
		cardLogger := log.New(os.Stderr, "card-approval ", log.LstdFlags|log.Lmicroseconds)
		cardApprovalProcessor, err = cardapproval.NewRelayHandler(
			agentExecutor, cfg.CardApproval.PrincipalOpenID, cardLogger,
		)
		if err != nil {
			fatalf("build CC Connect card approval handler failed: %v", err)
		}
	}
	stopProactive := func() {}
	if cfg.Proactive.Enabled {
		proactiveScheduler, err := proactive.StartScheduler(
			runtimeCtx,
			proactiveWorker,
			cfg.Proactive.Schedule,
			time.Duration(cfg.Proactive.StartupDelaySeconds)*time.Second,
			log.New(os.Stderr, "proactive-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			fatalf("start proactive scheduler failed: %v", err)
		}
		stopProactive = proactiveScheduler.Stop
	}
	stopMeetingSweep := func() {}
	if cfg.MeetingSweep.Enabled {
		meetingSweepScheduler, err := meetingsweep.StartScheduler(
			runtimeCtx,
			meetingSweepWorker,
			cfg.MeetingSweep.Schedule,
			time.Duration(cfg.MeetingSweep.StartupDelaySeconds)*time.Second,
			log.New(os.Stderr, "meeting-sweep-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			fatalf("start meeting sweep scheduler failed: %v", err)
		}
		stopMeetingSweep = meetingSweepScheduler.Stop
	}
	stopMorningBrief := func() {}
	if cfg.MorningBrief.Enabled {
		morningBriefScheduler, err := morningbrief.StartScheduler(
			runtimeCtx,
			morningBriefWorker,
			cfg.MorningBrief.Schedule,
			time.Duration(cfg.MorningBrief.StartupDelaySeconds)*time.Second,
			location,
			log.New(os.Stderr, "morning-brief-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			fatalf("start morning brief scheduler failed: %v", err)
		}
		stopMorningBrief = morningBriefScheduler.Stop
	}
	defer func() {
		cancelRuntime()
		<-scheduler.Stop().Done()
		stopDailyDigest()
		stopScheduledTasks()
		stopFactEngine()
		stopFactRollup()
		stopProactive()
		stopMeetingSweep()
		stopMorningBrief()
		stopPipelineScheduler()
		waitPipeline()
	}()

	// 流式对话服务：enabled 时实例化并注入 Dependencies.Chat；disabled 时留 nil，
	// router 据此不注册 /api/chat 路由（与 execute 的 Executor 一致）。
	// CLI 与 M5 执行共用 execute.bin，模型/思考级别走 chat 段（配置上与 execute 对齐）。
	var chatService *chat.Service
	if cfg.Chat.Enabled {
		chatService, err = chat.NewService(chat.Options{
			Bin:              cfg.Execute.Bin,
			Model:            cfg.Chat.Model,
			Sandbox:          cfg.Chat.Sandbox,
			ReasoningEffort:  cfg.Chat.ReasoningEffort,
			Timeout:          time.Duration(cfg.Chat.TimeoutSeconds) * time.Second,
			SharedMemory:     sharedMemoryService,
			ContextAssembler: contextAssembler,
		})
		if err != nil {
			fatalf("initialize chat service failed: %v", err)
		}
	}

	h := server.Default(
		server.WithHostPorts(cfg.Server.Addr),
	)
	h.Use(observability.Middleware())
	runtimeSettingsService, err := config.NewRuntimeSettingsService(*configPath, cfg)
	if err != nil {
		fatalf("initialize runtime settings service failed: %v", err)
	}
	readinessTargets := api.ReadinessTargets{
		LarkCLIBin:  cfg.LarkCLI.Bin,
		AgentCLIBin: cfg.Execute.Bin,
	}
	// 语义去重关闭时 semanticIndex 是 nil 指针；直接赋进接口字段会得到一个非 nil
	// 接口，探针就分不清「主动关掉」和「连不上 Qdrant」。
	if semanticIndex != nil {
		readinessTargets.VectorIndex = semanticIndex
	}
	if err := api.Register(h, api.Dependencies{
		DB: db, Todos: todoStore, TodoStatus: todoStore,
		Tasks: taskService, TaskSubmitter: taskSubmitter, Executor: agentExecutor,
		MessageRecaller: messageRecaller,
		Projects:        projectService, KeyMatters: keyMatterService,
		Persons: personService, Groups: groupService,
		Resolve: resolveService, Profile: profileService, Resources: resourceService,
		SharedMemory:   sharedMemoryService,
		WorkRules:      workRuleService,
		TextFiles:      textFileService,
		AgentConfig:    agentConfigService,
		ScheduledTasks: scheduledTaskService,
		Skills:         skillService,
		RelationFacts:  relationFactService,
		Progress:       progressService,
		FactQueries:    progressService,
		Overview:       overviewService, Digests: digestService, DigestSummarizer: digestSummarizer,
		MeetingReviews: meetingReviewService,
		DailyDigests:   dailyDigestService,
		MorningBriefs:  morningBriefReader,
		Worklog:        worklogService,
		FactRollups:    factRollupWorker,
		FactRollupLoc:  location,
		Debug:          debugService, Logs: logReader, Chat: chatService, Capture: captureService,
		RuntimeSettings:    runtimeSettingsService,
		ContextAssembler:   contextAssembler,
		CardApprovals:      cardApprovalProcessor,
		CardApprovalSecret: cfg.CardApproval.RelaySecret,
		Readiness:          readinessTargets,
	}); err != nil {
		fatalf("register API routes failed: %v", err)
	}
	webInfo, err := os.Stat(cfg.Server.WebRoot)
	if err != nil {
		fatalf("load web build root failed: %v", err)
	}
	if !webInfo.IsDir() {
		fatalf("web build root is not a directory: %s", cfg.Server.WebRoot)
	}
	h.StaticFS("/", &app.FS{Root: cfg.Server.WebRoot, IndexNames: []string{"index.html"}})

	infof("jarvis-server listening on %s", cfg.Server.Addr)
	// Spin 阻塞运行并处理优雅退出（SIGINT/SIGTERM/SIGHUP）。
	h.Spin()
}
