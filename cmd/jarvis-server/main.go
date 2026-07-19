// Command jarvis-server 是 Jarvis 的单体主进程（总纲 §1.1）。
//
// 当前启动链路：加载配置 → 连接 MySQL → 迁移核心表 → 起 Hertz。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"jarvis/internal/api"
	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/config"
	"jarvis/internal/decide"
	"jarvis/internal/domain"
	"jarvis/internal/embedding"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/extract/codexengine"
	"jarvis/internal/extract/provider"
	"jarvis/internal/larkcli"
	"jarvis/internal/memory"
	"jarvis/internal/semantic"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库迁移，成功后退出")
	discoverOnce := flag.Bool("discover-once", false, "执行一次飞书会话发现，成功后退出")
	scanChat := flag.String("scan-chat", "", "增量扫描指定飞书 chat_id，成功后退出")
	setRelatedGroups := flag.String("set-related-groups", "", "用逗号分隔的 chat_id 原子替换 related_group，成功后退出")
	memorizeOnce := flag.Bool("memorize-once", false, "执行一次消息记忆化，成功后退出")
	extractOnce := flag.Bool("extract-once", false, "执行一次 Todo 提取，成功后退出")
	decideOnce := flag.Bool("decide-once", false, "执行一次 MVP 人工确认分流，成功后退出")
	seedOnce := flag.Bool("seed", false, "一次性幂等写入初始 项目/任务/群关联 背景种子，成功后退出")
	seedPersons := flag.Bool("seed-persons", false, "从关键群真实成员导入 Person（幂等，按 open_id 跳过已存在），成功后退出")
	flag.Parse()
	actionCount := 0
	for _, selected := range []bool{*migrateOnly, *discoverOnce, *scanChat != "", *setRelatedGroups != "", *memorizeOnce, *extractOnce, *decideOnce, *seedOnce, *seedPersons} {
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
		var evaluator interface {
			Evaluate(context.Context, *domain.Todo) (*decide.EvaluationInput, error)
		}
		switch cfg.Decide.Mode {
		case decide.ManualMVPMode:
			evaluator = decide.ManualGateEvaluator{}
		case "codex":
			// M4 codex mode: judge each Todo read-only with codex gpt-5.5 and
			// route by disposition. Background is built from MySQL only (no mem0)
			// to keep the decision path deterministic and offline-safe.
			decider, err := decide.NewCodexDecider(decide.CodexOptions{
				Bin:     cfg.Codex.Bin,
				Model:   cfg.Codex.Model,
				Timeout: time.Duration(cfg.Codex.TimeoutSeconds) * time.Second,
			})
			if err != nil {
				hlog.Fatalf("initialize codex decider failed: %v", err)
			}
			decisionSnapshotter, err := decide.NewMVPBackgroundSnapshotter(db)
			if err != nil {
				hlog.Fatalf("initialize codex decision background snapshotter failed: %v", err)
			}
			codexEvaluator, err := decide.NewCodexEvaluator(decider, decisionSnapshotter)
			if err != nil {
				hlog.Fatalf("initialize codex evaluator failed: %v", err)
			}
			evaluator = codexEvaluator
		default:
			hlog.Fatalf("decide.mode 必须是 %s 或 codex", decide.ManualMVPMode)
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
			"decision completed: loaded=%d evaluated=%d need_decision=%d need_info=%d",
			stats.Loaded, stats.Evaluated, stats.NeedDecision, stats.NeedInfo,
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
		PageSize:    cfg.Capture.PageSize,
		ScanWorkers: cfg.Capture.ScanWorkers,
		HotAge:      time.Duration(cfg.Capture.HotAgeHours) * time.Hour,
		WarmAge:     time.Duration(cfg.Capture.WarmAgeHours) * time.Hour,
		Location:    location,
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
	backgroundSnapshotter, err := decide.NewMVPBackgroundSnapshotter(db)
	if err != nil {
		hlog.Fatalf("initialize confirmation background snapshotter failed: %v", err)
	}
	confirmationService, err := decide.NewService(db, backgroundSnapshotter)
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
		cfg.Codex.Bin, cfg.Codex.Model, time.Duration(cfg.Execute.TimeoutSecond)*time.Second,
	)
	if err != nil {
		hlog.Fatalf("initialize codex execution runner failed: %v", err)
	}
	agentExecutor, err := execute.NewAgentExecutor(db, taskService, codexRunner, cfg.Execute.RepoRoot, cfg.Execute.RunsDir)
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
		extractWorker, err = extract.NewWorker(pipelineStore, extractionEngine, memoryClient, deduplicator, toolBoxBuilder, extract.WorkerOptions{
			Load: extract.LoadOptions{
				BatchMessages: cfg.Extract.BatchMessages, ContextMessages: cfg.Extract.ContextMessages,
				ContextWindow: time.Duration(cfg.Extract.ContextWindowMinutes) * time.Minute,
				OpenTodoLimit: cfg.Extract.OpenTodoLimit,
			},
			PrincipalOpenID: cfg.Extract.PrincipalOpenID, ModelName: extractionModelName,
			MemoryTopK: cfg.Extract.MemoryTopK, MemoryThreshold: cfg.Extract.MemoryThreshold,
			MaxPromptChars: cfg.Extract.MaxPromptChars, MaxToolRounds: cfg.Extract.MaxToolRounds, Location: location,
			PromptToolGuidance: promptToolGuidance,
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
	captureCtx, cancelCapture := context.WithCancel(context.Background())
	defer cancelCapture()
	scheduler, err := capture.StartScheduler(captureCtx, captureService, capture.ScheduleConfig{
		Discover: cfg.Capture.DiscoverSchedule,
		Scan:     cfg.Capture.ScanSchedule,
	}, log.New(os.Stderr, "capture-cron ", log.LstdFlags|log.Lmicroseconds))
	if err != nil {
		hlog.Fatalf("start capture scheduler failed: %v", err)
	}
	memoryScheduler, err := memory.StartScheduler(
		captureCtx,
		memoryWorker,
		cfg.Mem0.Schedule,
		log.New(os.Stderr, "memory-cron ", log.LstdFlags|log.Lmicroseconds),
	)
	if err != nil {
		cancelCapture()
		<-scheduler.Stop().Done()
		hlog.Fatalf("start memory scheduler failed: %v", err)
	}
	stopExtractScheduler := func() {}
	if cfg.Extract.Enabled {
		extractScheduler, err := extract.StartScheduler(
			captureCtx,
			extractWorker,
			cfg.Extract.Schedule,
			log.New(os.Stderr, "extract-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			cancelCapture()
			<-scheduler.Stop().Done()
			<-memoryScheduler.Stop().Done()
			hlog.Fatalf("start extraction scheduler failed: %v", err)
		}
		stopExtractScheduler = func() { <-extractScheduler.Stop().Done() }
	}
	stopDecisionScheduler := func() {}
	if cfg.Decide.Enabled {
		decisionScheduler, err := decide.StartWorkerScheduler(
			captureCtx,
			decisionWorker,
			cfg.Decide.Schedule,
			log.New(os.Stderr, "decide-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			cancelCapture()
			<-scheduler.Stop().Done()
			<-memoryScheduler.Stop().Done()
			stopExtractScheduler()
			hlog.Fatalf("start MVP decision scheduler failed: %v", err)
		}
		stopDecisionScheduler = func() { <-decisionScheduler.Stop().Done() }
	}
	stopExecuteScheduler := func() {}
	if cfg.Execute.Enabled {
		executeScheduler, err := execute.StartScheduler(
			captureCtx,
			agentExecutor,
			cfg.Execute.Schedule,
			cfg.Execute.BatchLimit,
			log.New(os.Stderr, "execute-cron ", log.LstdFlags|log.Lmicroseconds),
		)
		if err != nil {
			cancelCapture()
			<-scheduler.Stop().Done()
			<-memoryScheduler.Stop().Done()
			stopExtractScheduler()
			stopDecisionScheduler()
			hlog.Fatalf("start execution scheduler failed: %v", err)
		}
		stopExecuteScheduler = func() { <-executeScheduler.Stop().Done() }
	}
	defer func() {
		cancelCapture()
		<-scheduler.Stop().Done()
		<-memoryScheduler.Stop().Done()
		stopExtractScheduler()
		stopDecisionScheduler()
		stopExecuteScheduler()
	}()

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr),
	)
	if err := api.Register(h, api.Dependencies{
		DB: db, Todos: todoStore, Confirmations: confirmationService, ConfirmationDetails: confirmationDetails,
		Tasks: taskService, Executor: agentExecutor,
		Projects: projectService, Persons: personService, Groups: groupService,
		Resolve: resolveService, Profile: profileService, Resources: resourceService,
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
