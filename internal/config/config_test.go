package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Server: ServerConfig{Addr: "127.0.0.1:18800", WebRoot: "web/dist"},
		MySQL: MySQLConfig{
			DSN:             "user:pass@tcp(127.0.0.1:3306)/jarvis",
			MaxOpenConns:    20,
			MaxIdleConns:    5,
			ConnMaxLifetime: 3600,
		},
		Extract: ExtractConfig{
			Schedule:              "@every 10m",
			Engine:                "codex",
			CodexSandbox:          "danger-full-access",
			CodexNetwork:          true,
			CodexReasoningEffort:  "low",
			BatchMessages:         400,
			ContextMessages:       20,
			ContextWindowMinutes:  120,
			OpenTodoLimit:         50,
			FactLimit:             30,
			MaxPromptChars:        60000,
			SemanticCollection:    "todo_semantic",
			SemanticThreshold:     0.85,
			SemanticNeighborLimit: 3,
			ToolTimeoutSec:        10,
			HistoryToolLimit:      50,
			QdrantHost:            "127.0.0.1",
			QdrantGRPCPort:        6334,
		},
		LarkCLI: LarkCLIConfig{
			Bin:        "lark-cli",
			RateLimit:  5,
			Burst:      10,
			Concurrent: 2,
			TimeoutSec: 60,
		},
		Capture: CaptureConfig{
			PageSize:         50,
			ScanWorkers:      2,
			HotAgeHours:      6,
			WarmAgeHours:     168,
			Timezone:         "Asia/Shanghai",
			DiscoverSchedule: "@every 6h",
			ScanSchedule:     "@every 5m",
		},
		Decide:        validDecideConfig(),
		FactEngine:    validFactEngineConfig(),
		Skills:        SkillsConfig{Root: ".agents/skills"},
		Codex:         validCodexConfig(),
		Execute:       validExecuteConfig(),
		Chat:          validChatConfig(),
		DailyDigest:   validDailyDigestConfig(),
		ScheduledTask: validScheduledTaskConfig(),
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid"},
		{name: "server address", mutate: func(c *Config) { c.Server.Addr = "" }, wantErr: "server.addr"},
		{name: "server web root", mutate: func(c *Config) { c.Server.WebRoot = "" }, wantErr: "server.web_root"},
		{name: "mysql dsn", mutate: func(c *Config) { c.MySQL.DSN = "" }, wantErr: "mysql.dsn"},
		{name: "open connections", mutate: func(c *Config) { c.MySQL.MaxOpenConns = 0 }, wantErr: "max_open_conns"},
		{name: "negative idle connections", mutate: func(c *Config) { c.MySQL.MaxIdleConns = -1 }, wantErr: "max_idle_conns"},
		{name: "idle exceeds open", mutate: func(c *Config) { c.MySQL.MaxIdleConns = 21 }, wantErr: "不能大于"},
		{name: "connection lifetime", mutate: func(c *Config) { c.MySQL.ConnMaxLifetime = 0 }, wantErr: "conn_max_lifetime"},
		{name: "extract schedule", mutate: func(c *Config) { c.Extract.Schedule = "" }, wantErr: "extract.schedule"},
		{name: "extract batch", mutate: func(c *Config) { c.Extract.BatchMessages = 0 }, wantErr: "extract.batch_messages"},
		{name: "extract context count", mutate: func(c *Config) { c.Extract.ContextMessages = -1 }, wantErr: "extract.context_messages"},
		{name: "extract context window", mutate: func(c *Config) { c.Extract.ContextWindowMinutes = 0 }, wantErr: "extract.context_window_minutes"},
		{name: "extract todo limit", mutate: func(c *Config) { c.Extract.OpenTodoLimit = 0 }, wantErr: "extract.open_todo_limit"},
		{name: "extract fact limit", mutate: func(c *Config) { c.Extract.FactLimit = 0 }, wantErr: "extract.fact_limit"},
		{name: "extract prompt limit", mutate: func(c *Config) { c.Extract.MaxPromptChars = 0 }, wantErr: "extract.max_prompt_chars"},
		{name: "extract semantic collection", mutate: func(c *Config) { c.Extract.SemanticCollection = "" }, wantErr: "semantic_collection"},
		{name: "extract semantic threshold", mutate: func(c *Config) { c.Extract.SemanticThreshold = 0 }, wantErr: "semantic_threshold"},
		{name: "extract semantic neighbor limit", mutate: func(c *Config) { c.Extract.SemanticNeighborLimit = 0 }, wantErr: "semantic_neighbor_limit"},
		{name: "extract tool timeout", mutate: func(c *Config) { c.Extract.ToolTimeoutSec = 0 }, wantErr: "tool_timeout_sec"},
		{name: "extract history tool limit", mutate: func(c *Config) { c.Extract.HistoryToolLimit = 0 }, wantErr: "history_tool_limit"},
		{name: "extract qdrant host", mutate: func(c *Config) { c.Extract.QdrantHost = "" }, wantErr: "extract.qdrant_host"},
		{name: "extract qdrant grpc port", mutate: func(c *Config) { c.Extract.QdrantGRPCPort = 0 }, wantErr: "extract.qdrant_grpc_port"},
		{name: "extract principal", mutate: func(c *Config) { c.Extract.Enabled = true }, wantErr: "principal_open_id"},
		{name: "extract model", mutate: func(c *Config) {
			c.Extract.Enabled = true
			c.Extract.PrincipalOpenID = "ou_owner"
		}, wantErr: "model.base_url"},
		{name: "extract embedding model", mutate: func(c *Config) {
			c.Extract.Enabled = true
			c.Extract.PrincipalOpenID = "ou_owner"
			c.Model = ModelConfig{BaseURL: "http://127.0.0.1:1", APIKey: "k", Model: "m", TimeoutSec: 60}
		}, wantErr: "model.embedding_model"},
		{name: "lark binary", mutate: func(c *Config) { c.LarkCLI.Bin = "" }, wantErr: "lark_cli.bin"},
		{name: "lark rate", mutate: func(c *Config) { c.LarkCLI.RateLimit = 0 }, wantErr: "lark_cli.rate_limit"},
		{name: "lark burst", mutate: func(c *Config) { c.LarkCLI.Burst = 0 }, wantErr: "lark_cli.burst"},
		{name: "lark concurrency", mutate: func(c *Config) { c.LarkCLI.Concurrent = 0 }, wantErr: "lark_cli.concurrent"},
		{name: "lark timeout", mutate: func(c *Config) { c.LarkCLI.TimeoutSec = 0 }, wantErr: "lark_cli.timeout_sec"},
		{name: "capture page size", mutate: func(c *Config) { c.Capture.PageSize = 51 }, wantErr: "capture.page_size"},
		{name: "capture workers", mutate: func(c *Config) { c.Capture.ScanWorkers = 0 }, wantErr: "capture.scan_workers"},
		{name: "capture hot age", mutate: func(c *Config) { c.Capture.HotAgeHours = 0 }, wantErr: "capture.hot_age_hours"},
		{name: "capture warm age", mutate: func(c *Config) { c.Capture.WarmAgeHours = 6 }, wantErr: "capture.warm_age_hours"},
		{name: "capture timezone", mutate: func(c *Config) { c.Capture.Timezone = "" }, wantErr: "capture.timezone"},
		{name: "capture schedules", mutate: func(c *Config) { c.Capture.ScanSchedule = "" }, wantErr: "schedule"},
		{name: "M5 judgment requires execution", mutate: func(c *Config) { c.Execute.Enabled = false }, wantErr: "M5 判断与执行"},
		{name: "M5 execution requires judgment", mutate: func(c *Config) { c.Decide.Enabled = false }, wantErr: "M5 判断与执行"},
		{name: "decide schedule", mutate: func(c *Config) { c.Decide.Schedule = "" }, wantErr: "decide.schedule"},
		{name: "decide batch", mutate: func(c *Config) { c.Decide.BatchLimit = 0 }, wantErr: "decide.batch_limit"},
		{name: "decide sandbox", mutate: func(c *Config) { c.Decide.CodexSandbox = "yolo" }, wantErr: "decide"},
		{name: "decide reasoning effort", mutate: func(c *Config) { c.Decide.CodexReasoningEffort = "" }, wantErr: "decide"},
		{name: "codex binary", mutate: func(c *Config) { c.Codex.Bin = "" }, wantErr: "codex.bin"},
		{name: "codex model", mutate: func(c *Config) { c.Codex.Model = "" }, wantErr: "codex.model"},
		{name: "codex timeout", mutate: func(c *Config) { c.Codex.TimeoutSeconds = 0 }, wantErr: "codex.timeout_seconds"},
		{name: "execute concurrency", mutate: func(c *Config) {
			c.Execute.Enabled = true
			c.Execute.Concurrency = 0
		}, wantErr: "execute.concurrency"},
		{name: "execute stale minute", mutate: func(c *Config) { c.Execute.StaleExecutingMinute = 0 }, wantErr: "execute.stale_executing_minute"},
		{name: "execute bin", mutate: func(c *Config) { c.Execute.Bin = "" }, wantErr: "execute.bin"},
		{name: "execute model", mutate: func(c *Config) { c.Execute.Model = "" }, wantErr: "execute.model"},
		{name: "execute reasoning", mutate: func(c *Config) { c.Execute.ReasoningEffort = "ultra" }, wantErr: "execute.codex_reasoning_effort"},
		{name: "execute stale vs timeout", mutate: func(c *Config) {
			c.Execute.TimeoutSecond = 600
			c.Execute.StaleExecutingMinute = 5
		}, wantErr: "execute.stale_executing_minute"},
		{name: "chat sandbox", mutate: func(c *Config) { c.Chat.Sandbox = "sandbox-x" }, wantErr: "chat.sandbox"},
		{name: "chat reasoning effort", mutate: func(c *Config) { c.Chat.ReasoningEffort = "ultra" }, wantErr: "chat.codex_reasoning_effort"},
		{name: "chat timeout", mutate: func(c *Config) { c.Chat.TimeoutSeconds = 0 }, wantErr: "chat.timeout_seconds"},
		{name: "chat model when enabled", mutate: func(c *Config) {
			c.Chat.Enabled = true
			c.Chat.Model = ""
		}, wantErr: "chat.model"},
		{name: "factengine schedule", mutate: func(c *Config) { c.FactEngine.Schedule = "" }, wantErr: "factengine.schedule"},
		{name: "factengine bin", mutate: func(c *Config) { c.FactEngine.Bin = "" }, wantErr: "factengine.bin"},
		{name: "factengine model", mutate: func(c *Config) { c.FactEngine.Model = "" }, wantErr: "factengine.model"},
		{name: "factengine sandbox", mutate: func(c *Config) { c.FactEngine.Sandbox = "yolo" }, wantErr: "factengine.sandbox"},
		{name: "factengine timeout", mutate: func(c *Config) { c.FactEngine.TimeoutSec = 0 }, wantErr: "factengine.timeout_sec"},
		{name: "factengine batch", mutate: func(c *Config) { c.FactEngine.BatchLimit = 0 }, wantErr: "factengine.batch_limit"},
		{name: "factengine window gap", mutate: func(c *Config) { c.FactEngine.WindowGapMinutes = 0 }, wantErr: "factengine.window_gap_minutes"},
		{name: "factengine window max", mutate: func(c *Config) { c.FactEngine.WindowMaxMessages = 0 }, wantErr: "factengine.window_max_messages"},
		{name: "dailydigest schedule", mutate: func(c *Config) { c.DailyDigest.Schedule = "" }, wantErr: "dailydigest.schedule"},
		{name: "dailydigest timeout", mutate: func(c *Config) { c.DailyDigest.TimeoutSeconds = 299 }, wantErr: "dailydigest.timeout_seconds"},
		{name: "dailydigest group message limit", mutate: func(c *Config) { c.DailyDigest.GroupMessageLimit = 0 }, wantErr: "dailydigest.group_message_limit"},
		{name: "dailydigest group concurrency", mutate: func(c *Config) { c.DailyDigest.GroupConcurrency = 0 }, wantErr: "dailydigest.group_concurrency"},
		{name: "scheduled task schedule", mutate: func(c *Config) { c.ScheduledTask.Schedule = "" }, wantErr: "scheduled_task.schedule"},
		{name: "scheduled task batch", mutate: func(c *Config) { c.ScheduledTask.BatchLimit = 0 }, wantErr: "scheduled_task.batch_limit"},
		{name: "scheduled task requires execute", mutate: func(c *Config) {
			c.ScheduledTask.Enabled = true
			c.Execute.Enabled = false
		}, wantErr: "execute.enabled"},
		{name: "invalid runtime schedule", mutate: func(c *Config) {
			c.Capture.ScanSchedule = "not-a-schedule"
		}, wantErr: "capture.scan_schedule"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}
			err := cfg.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateExtractEnabled(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{Addr: "127.0.0.1:18800", WebRoot: "web/dist"},
		MySQL: MySQLConfig{
			DSN: "user:pass@tcp(127.0.0.1:3306)/jarvis", MaxOpenConns: 20,
			MaxIdleConns: 5, ConnMaxLifetime: 3600,
		},
		Model: ModelConfig{
			BaseURL: "https://model.test/v1", APIKey: "plain-key", Model: "model", TimeoutSec: 60,
			EmbeddingModel: "embed-model", EmbeddingDims: 1024,
		},
		Extract: ExtractConfig{
			Enabled: true, PrincipalOpenID: "ou_owner", Schedule: "@every 10m",
			Engine: "codex", CodexSandbox: "danger-full-access", CodexNetwork: true, CodexReasoningEffort: "low",
			BatchMessages: 400, ContextMessages: 20, ContextWindowMinutes: 120,
			OpenTodoLimit: 50, FactLimit: 30, MaxPromptChars: 60000,
			SemanticCollection: "todo_semantic", SemanticThreshold: 0.85, SemanticNeighborLimit: 3,
			ToolTimeoutSec: 10, HistoryToolLimit: 50, QdrantHost: "127.0.0.1", QdrantGRPCPort: 6334,
		},
		LarkCLI: LarkCLIConfig{Bin: "lark-cli", RateLimit: 5, Burst: 10, Concurrent: 2, TimeoutSec: 60},
		Capture: CaptureConfig{
			PageSize: 50, ScanWorkers: 2, HotAgeHours: 6, WarmAgeHours: 168,
			Timezone: "Asia/Shanghai", DiscoverSchedule: "@every 6h", ScanSchedule: "@every 5m",
		},
		Decide:        validDecideConfig(),
		FactEngine:    validFactEngineConfig(),
		Skills:        SkillsConfig{Root: ".agents/skills"},
		Codex:         validCodexConfig(),
		Execute:       validExecuteConfig(),
		Chat:          validChatConfig(),
		DailyDigest:   validDailyDigestConfig(),
		ScheduledTask: validScheduledTaskConfig(),
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	cfg.Model.TimeoutSec = 0
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "model.timeout_sec") {
		t.Fatalf("validate() error = %v", err)
	}
}

func validExecuteConfig() ExecuteConfig {
	return ExecuteConfig{
		Enabled: true, Schedule: "@every 5m", BatchLimit: 5, Concurrency: 3,
		RepoRoot: "/tmp/repos", RunsDir: "/tmp/runs",
		Bin: "codex", Model: "fixture-exec-model", ReasoningEffort: "medium", TimeoutSecond: 600,
		StaleExecutingMinute: 30,
	}
}

func validDecideConfig() DecideConfig {
	return DecideConfig{
		Enabled: true, Schedule: "@every 10m", BatchLimit: 50,
		CodexSandbox: "danger-full-access", CodexNetwork: true, CodexReasoningEffort: "medium",
	}
}

func validCodexConfig() CodexConfig {
	return CodexConfig{
		Bin: "codex", Model: "fixture-model", TimeoutSeconds: 120,
	}
}

func validChatConfig() ChatConfig {
	return ChatConfig{
		Enabled: true, Model: "fixture-model", TimeoutSeconds: 600,
		Sandbox: "danger-full-access", ReasoningEffort: "medium",
	}
}

func validDailyDigestConfig() DailyDigestConfig {
	return DailyDigestConfig{
		Enabled: true, Schedule: "0 19 * * *", TimeoutSeconds: 600,
		GroupMessageLimit: 200, GroupConcurrency: 2,
	}
}

func validScheduledTaskConfig() ScheduledTaskConfig {
	return ScheduledTaskConfig{Enabled: false, Schedule: "@every 1m", BatchLimit: 20}
}

func validFactEngineConfig() FactEngineConfig {
	return FactEngineConfig{
		Enabled: true, Schedule: "@every 15m",
		Bin: "traex", Model: "fixture-fact-model", Sandbox: "danger-full-access", TimeoutSec: 300,
		BatchLimit: 200, WindowGapMinutes: 30, WindowMaxMessages: 40,
	}
}
