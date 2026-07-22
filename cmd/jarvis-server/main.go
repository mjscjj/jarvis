// Command jarvis-server 是 Jarvis 的单体主进程（总纲 §1.1）。
//
// 当前启动链路：加载配置 → 连接 MySQL → 迁移核心表 → 起 Hertz。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"jarvis/internal/api"
	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/dailydigest"
	"jarvis/internal/decide"
	"jarvis/internal/domain"
	"jarvis/internal/embedding"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/extract/codexengine"
	"jarvis/internal/extract/provider"
	"jarvis/internal/insight"
	"jarvis/internal/knowledge"
	"jarvis/internal/larkcli"
	"jarvis/internal/memory"
	"jarvis/internal/pipeline"
	"jarvis/internal/progress"
	"jarvis/internal/semantic"
	"jarvis/internal/sharedmem"
	"jarvis/internal/store"
	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
)

// dailyDigestGitAuthor 是个人每日总结 prompt 里引导 codex 跑 git log --author 的
// 作者名（即「我」）。单用户本地系统，固定值即可。
const dailyDigestGitAuthor = "chujiejie.1"

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库迁移，成功后退出")
	backfillProgressEvents := flag.Bool("backfill-progress-events", false, "为无事件历史的存量 Task 写入一次当前状态快照，成功后退出")
	discoverOnce := flag.Bool("discover-once", false, "执行一次飞书会话发现，成功后退出")
	scanChat := flag.String("scan-chat", "", "增量扫描指定飞书 chat_id，成功后退出")
	setRelatedGroups := flag.String("set-related-groups", "", "用逗号分隔的 chat_id 原子替换 related_group，成功后退出")
	memorizeOnce := flag.Bool("memorize-once", false, "执行一次消息记忆化，成功后退出")
	extractOnce := flag.Bool("extract-once", false, "执行一次 Todo 提取，成功后退出")
	decideOnce := flag.Bool("decide-once", false, "执行一次 MVP 人工确认分流，成功后退出")
	seedOnce := flag.Bool("seed", false, "一次性幂等写入初始 项目/任务/群关联 背景种子，成功后退出")
	seedPersons := flag.Bool("seed-persons", false, "从关键群真实成员导入 Person（幂等，按 open_id 跳过已存在），成功后退出")
	openP2P := flag.Bool("open-p2p", false, "把存量内部私聊(p2p)一次性纳入监听(related_group=1)，成功后退出")
	flag.Parse()
	actionCount := 0
	for _, selected := range []bool{*migrateOnly, *backfillProgressEvents, *discoverOnce, *scanChat != "", *setRelatedGroups != "", *memorizeOnce, *extractOnce, *decideOnce, *seedOnce, *seedPersons, *openP2P} {
		if selected {
			actionCount++
		}
	}
	if actionCount > 1 {
		hlog.Fatalf("one-shot action flags are mutually exclusive")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		// fail-fast：配置错误启动即暴露，不带缺陷跑起来
		hlog.Fatalf("load config failed: %v", err)
	}

	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := store.OpenMySQL(connectCtx, cfg.MySQL)
	if err != nil {
		hlog.Fatalf("connect mysql failed: %v", err)
	}
	defer func() {
		if err := store.Close(db); err != nil {
			hlog.Errorf("close mysql failed: %v", err)
		}
	}()

	if err := store.Migrate(db); err != nil {
		hlog.Fatalf("migrate mysql failed: %v", err)
	}
	if *migrateOnly {
		hlog.Infof("mysql schema migration completed")
		return
	}
	if *backfillProgressEvents {
		stats, err := progress.BackfillTaskSnapshots(context.Background(), db, time.Now().UTC())
		if err != nil {
			hlog.Fatalf("backfill task progress snapshots failed: %v", err)
		}
		hlog.Infof("progress event backfill completed: tasks_scanned=%d events_created=%d", stats.TasksScanned, stats.EventsCreated)
		return
	}
	if *seedOnce {
		stats, err := background.Seed(context.Background(), db)
		if err != nil {
			hlog.Fatalf("seed backgrounds failed: %v", err)
		}
		hlog.Infof(
			"seed completed: projects_created=%d projects_skipped=%d tasks_created=%d tasks_skipped=%d groups_linked=%d",
			stats.ProjectsCreated, stats.ProjectsSkipped, stats.TasksCreated, stats.TasksSkipped, stats.GroupsLinked,
		)
		return
	}

	// 共享记忆（可信自由文本）装载服务：分发给 M3/M4/M5/chat 四条链路，各注入点组装
	// prompt 时实时读表。构造失败 fail-fast。
	sharedMemoryService, err := sharedmem.NewSharedMemoryService(db)
	if err != nil {
		hlog.Fatalf("initialize shared memory service failed: %v", err)
	}
	workRuleService, err := workrule.NewService(db)
	if err != nil {
		hlog.Fatalf("initialize work rule service failed: %v", err)
	}
	relationFactService, err := knowledge.NewService(db)
	if err != nil {
		hlog.Fatalf("initialize relation fact service failed: %v", err)
	}
	progressService, err := progress.NewService(db)
	if err != nil {
		hlog.Fatalf("initialize progress service failed: %v", err)
	}

	var decisionWorker *decide.DecisionWorker
	if cfg.Decide.Enabled || *decideOnce {
		if cfg.Decide.BatchLimit <= 0 {
			hlog.Fatalf("decision requires positive decide.batch_limit")
		}
		decisionSource, err := decide.NewEvaluationSource(db)
		if err != nil {
			hlog.Fatalf("initialize decision source failed: %v", err)
		}
		decisionStore, err := decide.NewEvaluationStore(db)
		if err != nil {
			hlog.Fatalf("initialize decision store failed: %v", err)
		}
		evaluator, err := buildDecisionEvaluator(cfg, db, sharedMemoryService, workRuleService)
		if err != nil {
			hlog.Fatalf("initialize decision evaluator failed: %v", err)
		}
		decisionWorker, err = decide.NewDecisionWorker(
			decisionSource,
			evaluator,
			decisionStore,
			decide.WorkerOptions{BatchLimit: cfg.Decide.BatchLimit},
		)
		if err != nil {
			hlog.Fatalf("initialize decision worker failed: %v", err)
		}
	}
	if *decideOnce {
		stats, err := decisionWorker.EvaluateOnce(context.Background())
		if err != nil {
			hlog.Fatalf("route Todos to manual confirmation failed: %v", err)
		}
		hlog.Infof(
			"decision completed: loaded=%d evaluated=%d auto=%d need_decision=%d need_info=%d dropped=%d",
			stats.Loaded, stats.Evaluated, stats.Auto, stats.NeedDecision, stats.NeedInfo, stats.Dropped,
		)
		return
	}

	larkClient, err := larkcli.New(larkcli.Options{
		Bin:         cfg.LarkCLI.Bin,
		RateLimit:   cfg.LarkCLI.RateLimit,
		Burst:       cfg.LarkCLI.Burst,
		Concurrency: cfg.LarkCLI.Concurrent,
		Timeout:     time.Duration(cfg.LarkCLI.TimeoutSec) * time.Second,
	})
	if err != nil {
		hlog.Fatalf("initialize lark-cli failed: %v", err)
	}
	if *seedPersons {
		stats, err := background.SeedPersonsFromKeyGroups(context.Background(), db, larkClient)
		if err != nil {
			hlog.Fatalf("seed persons from key groups failed: %v", err)
		}
		hlog.Infof(
			"seed persons completed: groups_scanned=%d persons_seen=%d persons_added=%d persons_skipped=%d",
			stats.GroupsScanned, stats.PersonsSeen, stats.PersonsAdded, stats.PersonsSkip,
		)
		return
	}
	location, err := time.LoadLocation(cfg.Capture.Timezone)
	if err != nil {
		hlog.Fatalf("load capture timezone failed: %v", err)
	}
	captureService, err := capture.NewService(db, larkClient, capture.Options{
		PageSize:           cfg.Capture.PageSize,
		ScanWorkers:        cfg.Capture.ScanWorkers,
		HotAge:             time.Duration(cfg.Capture.HotAgeHours) * time.Hour,
		WarmAge:            time.Duration(cfg.Capture.WarmAgeHours) * time.Hour,
		Location:           location,
		AutoRelatedP2PTopN: cfg.Capture.AutoRelatedP2PTopN,
	})
	if err != nil {
		hlog.Fatalf("initialize capture service failed: %v", err)
	}
	memoryClient, err := memory.NewClient(cfg.Mem0.BaseURL, time.Duration(cfg.Mem0.TimeoutSec)*time.Second)
	if err != nil {
		hlog.Fatalf("initialize memory client failed: %v", err)
	}
	memoryStore, err := memory.NewGORMStore(db)
	if err != nil {
		hlog.Fatalf("initialize memory store failed: %v", err)
	}
	memoryWorker, err := memory.NewWorker(memoryStore, memoryClient, memory.WorkerOptions{
		BatchLimit:        cfg.Mem0.BatchLimit,
		WindowGap:         time.Duration(cfg.Mem0.WindowGapMinutes) * time.Minute,
		WindowMaxMessages: cfg.Mem0.WindowMaxMessages,
		Location:          location,
	})
	if err != nil {
		hlog.Fatalf("initialize memory worker failed: %v", err)
	}
	todoStore, err := extract.NewTodoStore(db)
	if err != nil {
		hlog.Fatalf("initialize todo store failed: %v", err)
	}
	// Build an evaluator + store so the confirmation service can re-run M4
	// asynchronously after a need_info supplement, independent of the decision
	// cron being enabled. Mirrors the worker's evaluator selection.
	supplementEvaluator, err := buildDecisionEvaluator(cfg, db, sharedMemoryService, workRuleService)
	if err != nil {
		hlog.Fatalf("initialize supplement evaluator failed: %v", err)
	}
	supplementStore, err := decide.NewEvaluationStore(db)
	if err != nil {
		hlog.Fatalf("initialize supplement evaluation store failed: %v", err)
	}
	confirmationService, err := decide.NewService(db, supplementEvaluator, supplementStore)
	if err != nil {
		hlog.Fatalf("initialize confirmation service failed: %v", err)
	}
	confirmationDetails, err := decide.NewConfirmationDetailStore(db, todoStore)
	if err != nil {
		hlog.Fatalf("initialize confirmation detail store failed: %v", err)
	}
	taskService, err := execute.NewStore(db)
	if err != nil {
		hlog.Fatalf("initialize MVP Task service failed: %v", err)
	}
	codexRunner, err := execute.NewCodexRunner(
		cfg.Execute.Bin, cfg.Execute.Model, cfg.Execute.ReasoningEffort,
		time.Duration(cfg.Execute.TimeoutSecond)*time.Second,
	)
	if err != nil {
		hlog.Fatalf("initialize execute runner failed: %v", err)
	}
	agentExecutor, err := execute.NewAgentExecutor(
		db, taskService, codexRunner, sharedMemoryService, workRuleService, cfg.Execute.RepoRoot, cfg.Execute.RunsDir,
	)
	if err != nil {
		hlog.Fatalf("initialize agent executor failed: %v", err)
	}
	projectService, err := background.NewProjectService(db)
	if err != nil {
		hlog.Fatalf("initialize project service failed: %v", err)
	}
	personService, err := background.NewPersonService(db)
	if err != nil {
		hlog.Fatalf("initialize person service failed: %v", err)
	}
	groupService, err := background.NewGroupBackgroundService(db, captureService)
	if err != nil {
		hlog.Fatalf("initialize group background service failed: %v", err)
	}
	resolveService, err := background.NewResolveService(larkClient)
	if err != nil {
		hlog.Fatalf("initialize person resolve service failed: %v", err)
	}
	profileService, err := background.NewProfileService(db, cfg.Extract.PrincipalOpenID)
	if err != nil {
		hlog.Fatalf("initialize principal profile service failed: %v", err)
	}
	resourceService, err := background.NewResourceService(db)
	if err != nil {
		hlog.Fatalf("initialize resource service failed: %v", err)
	}
	overviewService, err := insight.NewOverviewService(db)
	if err != nil {
		hlog.Fatalf("initialize overview service failed: %v", err)
	}
	digestService, err := insight.NewDigestService(db, location)
	if err != nil {
		hlog.Fatalf("initialize digest service failed: %v", err)
	}
	worklogService, err := insight.NewWorklogService(db, location)
	if err != nil {
		hlog.Fatalf("initialize worklog service failed: %v", err)
	}
	// 进度总结按需复用 M5 的 codex runner（read-only 出纯文本）；codex 不可用时留空，接口返回 503。
	digestSummarizer, err := insight.NewSummarizer(codexRunner)
	if err != nil {
		hlog.Fatalf("initialize digest summarizer failed: %v", err)
	}
	// 每日进度总结：个人用 execute 段 codex（danger-full-access + 联网自跑工具），
	// 群用 model 段 qwen 单次调用。qwen client 独立于 M3 抽取（后者仅 extract.enabled 时建）。
	dailyDigestQwen, err := provider.NewClient(
		cfg.Model.BaseURL, cfg.Model.APIKey, cfg.Model.Model,
		time.Duration(cfg.Model.TimeoutSec)*time.Second,
	)
	if err != nil {
		hlog.Fatalf("initialize daily digest qwen client failed: %v", err)
	}
	dailyDigestService, err := dailydigest.NewService(dailydigest.Options{
		DB:              db,
		Location:        location,
		PersonRunner:    codexRunner,
		GroupRunner:     dailyDigestQwen,
		PrincipalOpenID: cfg.Extract.PrincipalOpenID,
		GitAuthor:       dailyDigestGitAuthor,
		PersonSandbox:   "danger-full-access",
		GroupMsgLimit:   cfg.DailyDigest.GroupMessageLimit,
		GroupConcur:     cfg.DailyDigest.GroupConcurrency,
	})
	if err != nil {
		hlog.Fatalf("initialize daily digest service failed: %v", err)
	}
	logReader, err := insight.NewLogReader(cfg.Server.LogFiles)
	if err != nil {
		hlog.Fatalf("initialize log reader failed: %v", err)
	}
	debugService, err := insight.NewDebugService(db, cfg.Mem0.BaseURL, cfg.Mem0.QdrantHost, 0, logReader)
	if err != nil {
		hlog.Fatalf("initialize debug service failed: %v", err)
	}
	var extractWorker *extract.Worker
	var semanticIndex *semantic.Index
	if cfg.Extract.Enabled || *extractOnce {
		modelClient, err := provider.NewClient(
			cfg.Model.BaseURL,
			cfg.Model.APIKey,
			cfg.Model.Model,
			time.Duration(cfg.Model.TimeoutSec)*time.Second,
		)
		if err != nil {
			hlog.Fatalf("initialize extraction model client failed: %v", err)
		}
		embeddingClient, err := embedding.NewClient(
			cfg.Model.BaseURL,
			cfg.Model.APIKey,
			cfg.Mem0.EmbeddingModel,
			cfg.Mem0.EmbeddingDims,
			time.Duration(cfg.Model.TimeoutSec)*time.Second,
		)
		if err != nil {
			hlog.Fatalf("initialize Todo embedding client failed: %v", err)
		}
		semanticIndex, err = semantic.NewIndex(semantic.Options{
			Host: cfg.Mem0.QdrantHost, Port: cfg.Mem0.QdrantGRPCPort,
			Collection: cfg.Extract.SemanticCollection, EmbeddingModel: cfg.Mem0.EmbeddingModel,
			Dimensions:     cfg.Mem0.EmbeddingDims,
			ScoreThreshold: cfg.Extract.SemanticThreshold, NeighborLimit: cfg.Extract.SemanticNeighborLimit,
			ActiveStatuses: extract.ActiveTodoStatuses(),
		})
		if err != nil {
			hlog.Fatalf("initialize Todo semantic index failed: %v", err)
		}
		ensureCtx, cancelEnsure := context.WithTimeout(context.Background(), 10*time.Second)
		if err := semanticIndex.Ensure(ensureCtx); err != nil {
			cancelEnsure()
			hlog.Fatalf("ensure Todo semantic index failed: %v", err)
		}
		cancelEnsure()
		pipelineStore, err := extract.NewPipelineStore(db, location, semanticIndex, cfg.Extract.PrincipalOpenID)
		if err != nil {
			hlog.Fatalf("initialize extraction pipeline store failed: %v", err)
		}
		deduplicator, err := extract.NewDeduplicator(embeddingClient, semanticIndex, pipelineStore, modelClient)
		if err != nil {
			hlog.Fatalf("initialize Todo semantic deduplicator failed: %v", err)
		}
		toolBoxBuilder, err := extract.NewRegistryToolBoxBuilder(db, memoryClient, extract.ToolBoxConfig{
			ToolTimeout:     time.Duration(cfg.Extract.ToolTimeoutSec) * time.Second,
			HistoryMaxLimit: cfg.Extract.HistoryToolLimit,
			MemoryDefaultK:  cfg.Extract.MemoryTopK,
			MemoryMaxK:      cfg.Extract.ToolMemoryMaxTopK,
			MemoryThreshold: cfg.Extract.MemoryThreshold,
			Location:        location,
		})
		if err != nil {
			hlog.Fatalf("initialize extraction tool box builder failed: %v", err)
		}
		// Engine selection: codex is a full agent that self-runs CLIs to infer
		// project/repo (danger-full-access + network + low reasoning); kimi is the
		// legacy function-calling loop kept as fallback. The deduplicator always
		// uses modelClient (kimi) for its SameAction adjudication regardless.
		var extractionEngine extract.ToolExtractor = modelClient
		extractionModelName := cfg.Model.Model
		promptToolGuidance := ""
		if cfg.Extract.Engine == "codex" {
			codexExtractor, err := codexengine.New(codexengine.Options{
				Bin: cfg.Codex.Bin, Model: cfg.Codex.Model,
				Sandbox: cfg.Extract.CodexSandbox, Network: cfg.Extract.CodexNetwork,
				ReasoningEffort: cfg.Extract.CodexReasoningEffort,
				Timeout:         time.Duration(cfg.Codex.TimeoutSeconds) * time.Second,
			})
			if err != nil {
				hlog.Fatalf("initialize codex extraction engine failed: %v", err)
			}
			extractionEngine = codexExtractor
			extractionModelName = cfg.Codex.Model
			promptToolGuidance = extract.CodexToolGuidance
		}
		extractWorker, err = extract.NewWorker(pipelineStore, extractionEngine, memoryClient, deduplicator, toolBoxBuilder, sharedMemoryService, extract.WorkerOptions{
			Load: extract.LoadOptions{
				BatchMessages: cfg.Extract.BatchMessages, ContextMessages: cfg.Extract.ContextMessages,
				ContextWindow: time.Duration(cfg.Extract.ContextWindowMinutes) * time.Minute,
				OpenTodoLimit: cfg.Extract.OpenTodoLimit,
			},
			PrincipalOpenID: cfg.Extract.PrincipalOpenID, ModelName: extractionModelName,
			MemoryTopK: cfg.Extract.MemoryTopK, MemoryThreshold: cfg.Extract.MemoryThreshold,
			MaxPromptChars: cfg.Extract.MaxPromptChars, MaxToolRounds: cfg.Extract.MaxToolRounds, Location: location,
			EvidenceRetryMax:   cfg.Extract.EvidenceRetryMax,
			PromptToolGuidance: promptToolGuidance,
			WorkRules:          workRuleService,
		})
		if err != nil {
			hlog.Fatalf("initialize extraction worker failed: %v", err)
		}
	}
	if semanticIndex != nil {
		defer func() {
			if err := semanticIndex.Close(); err != nil {
				hlog.Errorf("close Todo semantic index failed: %v", err)
			}
		}()
	}
	if *discoverOnce {
		if err := captureService.DiscoverChats(context.Background()); err != nil {
			hlog.Fatalf("discover chats failed: %v", err)
		}
		hlog.Infof("chat discovery completed")
		return
	}
	if *scanChat != "" {
		if err := captureService.ScanChat(context.Background(), *scanChat); err != nil {
			hlog.Fatalf("scan chat failed: %v", err)
		}
		hlog.Infof("chat scan completed: %s", *scanChat)
		return
	}
	if *setRelatedGroups != "" {
		if err := captureService.ReplaceRelatedGroups(strings.Split(*setRelatedGroups, ",")); err != nil {
			hlog.Fatalf("set related groups failed: %v", err)
		}
		hlog.Infof("related groups replaced")
		return
	}
	if *openP2P {
		opened, err := captureService.OpenInternalP2P()
		if err != nil {
			hlog.Fatalf("open internal p2p chats failed: %v", err)
		}
		hlog.Infof("internal p2p chats opened for monitoring: %d", opened)
		return
	}
	if *memorizeOnce {
		stats, err := memoryWorker.MemorizeOnce(context.Background())
		if err != nil {
			hlog.Fatalf("memorize messages failed: %v", err)
		}
		hlog.Infof(
			"message memory completed: loaded=%d processed=%d memorized=%d skipped=%d windows=%d",
			stats.Loaded, stats.Processed, stats.MemorizedMessages, stats.SkippedMessages, stats.Windows,
		)
		return
	}
	if *extractOnce {
		stats, err := extractWorker.ExtractOnce(context.Background())
		if err != nil {
			hlog.Fatalf("extract todos failed: %v", err)
		}
		hlog.Infof(
			"todo extraction completed: chats_loaded=%d chats_processed=%d units=%d candidates=%d created=%d updated=%d",
			stats.ChatsLoaded, stats.ChatsProcessed, stats.Units, stats.Candidates, stats.Created, stats.Updated,
		)
		return
	}
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()

	stopPipelineScheduler := func() {}
	waitPipeline := func() {}
	if cfg.Extract.Enabled || cfg.Decide.Enabled || cfg.Execute.Enabled {
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
			decisionWorker,
			executionTaskStore,
			executionAgent,
			pipeline.Options{
				ExecutionBatchLimit:  cfg.Execute.BatchLimit,
				ExecutionConcurrency: cfg.Execute.Concurrency,
				StaleExecuting:       time.Duration(cfg.Execute.StaleExecutingMinute) * time.Minute,
				Logger:               log.New(os.Stderr, "pipeline ", log.LstdFlags|log.Lmicroseconds),
			},
		)
		if err != nil {
			hlog.Fatalf("initialize real-time pipeline failed: %v", err)
		}
		if err := coordinator.Start(runtimeCtx); err != nil {
			hlog.Fatalf("start real-time pipeline failed: %v", err)
		}
		waitPipeline = coordinator.Wait
		if cfg.Extract.Enabled {
			if err := captureService.SetScanObserver(coordinator); err != nil {
				cancelRuntime()
				waitPipeline()
				hlog.Fatalf("wire capture to real-time pipeline failed: %v", err)
			}
		}
		if cfg.Decide.Enabled || cfg.Execute.Enabled {
			if err := confirmationService.SetLifecycleNotifier(coordinator); err != nil {
				cancelRuntime()
				waitPipeline()
				hlog.Fatalf("wire confirmation to real-time pipeline failed: %v", err)
			}
		}
		pipelineScheduler, err := pipeline.StartScheduler(
			runtimeCtx,
			coordinator,
			pipeline.ScheduleConfig{
				Extract: cfg.Extract.Schedule,
				Decide:  cfg.Decide.Schedule,
				Execute: cfg.Execute.Schedule,
			},
			log.New(os.Stderr, "pipeline-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			cancelRuntime()
			waitPipeline()
			hlog.Fatalf("start pipeline compensation scheduler failed: %v", err)
		}
		stopPipelineScheduler = func() { <-pipelineScheduler.Stop().Done() }
		if err := coordinator.ReconcileAll(runtimeCtx); err != nil {
			cancelRuntime()
			stopPipelineScheduler()
			waitPipeline()
			hlog.Fatalf("queue startup pipeline reconciliation failed: %v", err)
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
		hlog.Fatalf("start capture scheduler failed: %v", err)
	}
	memoryScheduler, err := memory.StartScheduler(
		runtimeCtx,
		memoryWorker,
		cfg.Mem0.Schedule,
		log.New(os.Stderr, "memory-cron ", log.LstdFlags|log.Lmicroseconds),
	)
	if err != nil {
		cancelRuntime()
		<-scheduler.Stop().Done()
		stopPipelineScheduler()
		waitPipeline()
		hlog.Fatalf("start memory scheduler failed: %v", err)
	}
	// 每日进度总结 19:00 cron：enabled 时起，disabled 时手动生成接口仍可用。
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
			<-memoryScheduler.Stop().Done()
			stopPipelineScheduler()
			waitPipeline()
			hlog.Fatalf("start daily digest scheduler failed: %v", err)
		}
		stopDailyDigest = func() { <-dailyDigestScheduler.Stop().Done() }
	}
	defer func() {
		cancelRuntime()
		<-scheduler.Stop().Done()
		<-memoryScheduler.Stop().Done()
		stopDailyDigest()
		stopPipelineScheduler()
		waitPipeline()
	}()

	// 流式对话服务：enabled 时实例化并注入 Dependencies.Chat；disabled 时留 nil，
	// router 据此不注册 /api/chat 路由（与 execute 的 Executor 一致）。
	// CLI 与 M5 执行共用 execute.bin，模型/思考级别走 chat 段（配置上与 execute 对齐）。
	var chatService *chat.Service
	if cfg.Chat.Enabled {
		chatService, err = chat.NewService(chat.Options{
			Bin:             cfg.Execute.Bin,
			Model:           cfg.Chat.Model,
			Sandbox:         cfg.Chat.Sandbox,
			ReasoningEffort: cfg.Chat.ReasoningEffort,
			Timeout:         time.Duration(cfg.Chat.TimeoutSeconds) * time.Second,
			DSN:             cfg.MySQL.DSN,
			SharedMemory:    sharedMemoryService,
		})
		if err != nil {
			hlog.Fatalf("initialize chat service failed: %v", err)
		}
	}

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr),
	)
	if err := api.Register(h, api.Dependencies{
		DB: db, Todos: todoStore, Confirmations: confirmationService, ConfirmationDetails: confirmationDetails,
		Tasks: taskService, Executor: agentExecutor,
		Projects: projectService, Persons: personService, Groups: groupService,
		Resolve: resolveService, Profile: profileService, Resources: resourceService,
		SharedMemory:  sharedMemoryService,
		WorkRules:     workRuleService,
		RelationFacts: relationFactService,
		Progress:      progressService,
		Overview:      overviewService, Digests: digestService, DigestSummarizer: digestSummarizer,
		DailyDigests: dailyDigestService,
		Worklog:      worklogService,
		Debug:        debugService, Logs: logReader, Chat: chatService, Capture: captureService,
	}); err != nil {
		hlog.Fatalf("register API routes failed: %v", err)
	}
	webInfo, err := os.Stat(cfg.Server.WebRoot)
	if err != nil {
		hlog.Fatalf("load web build root failed: %v", err)
	}
	if !webInfo.IsDir() {
		hlog.Fatalf("web build root is not a directory: %s", cfg.Server.WebRoot)
	}
	h.StaticFS("/", &app.FS{Root: cfg.Server.WebRoot, IndexNames: []string{"index.html"}})

	hlog.Infof("jarvis-server listening on %s", cfg.Server.Addr)
	// Spin 阻塞运行并处理优雅退出（SIGINT/SIGTERM/SIGHUP）。
	h.Spin()
}

// decisionEvaluator is the M4 evaluator shape shared by the decision worker and
// the confirmation service's async re-evaluation. It matches decide's internal
// evaluator interface structurally.
type decisionEvaluator interface {
	Evaluate(context.Context, *domain.Todo) (*decide.EvaluationInput, error)
}

// buildDecisionEvaluator constructs the M4 evaluator from config. codex mode
// judges each Todo read-only with codex and reuses the M3-frozen snapshot;
// manual_mvp routes everything to human confirmation.
func buildDecisionEvaluator(cfg *config.Config, db *gorm.DB, sharedMem sharedmem.SharedMemoryReader, workRules workrule.Reader) (decisionEvaluator, error) {
	switch cfg.Decide.Mode {
	case decide.ManualMVPMode:
		return decide.ManualGateEvaluator{}, nil
	case "codex":
		decider, err := decide.NewCodexDecider(decide.CodexOptions{
			Bin:             cfg.Codex.Bin,
			Model:           cfg.Codex.Model,
			Timeout:         time.Duration(cfg.Codex.TimeoutSeconds) * time.Second,
			Sandbox:         cfg.Decide.CodexSandbox,
			Network:         cfg.Decide.CodexNetwork,
			ReasoningEffort: cfg.Decide.CodexReasoningEffort,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize codex decider: %w", err)
		}
		return decide.NewCodexEvaluator(db, decider, sharedMem, workRules)
	default:
		return nil, fmt.Errorf("decide.mode 必须是 %s 或 codex", decide.ManualMVPMode)
	}
}
