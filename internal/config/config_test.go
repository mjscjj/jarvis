package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repo_root 会写进交给模型的 prompt，runs_dir 下的产物路径也要在别的工作目录里
// 可用，所以基线配置允许写相对路径，但加载后必须已经是绝对路径。
func TestLoadExpandsRelativeExecutePaths(t *testing.T) {
	source := strings.NewReplacer(
		`repo_root: "/tmp/repos"`, `repo_root: ".."`,
		`runs_dir: "/tmp/runs"`, `runs_dir: "runs"`,
	).Replace(runtimeSettingsTestYAML)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := filepath.Dir(workingDir); cfg.Execute.RepoRoot != want {
		t.Fatalf("execute.repo_root = %q, want %q", cfg.Execute.RepoRoot, want)
	}
	if want := filepath.Join(workingDir, "runs"); cfg.Execute.RunsDir != want {
		t.Fatalf("execute.runs_dir = %q, want %q", cfg.Execute.RunsDir, want)
	}
}

func TestLoadKeepsAbsoluteExecutePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(runtimeSettingsTestYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Execute.RepoRoot != "/tmp/repos" || cfg.Execute.RunsDir != "/tmp/runs" {
		t.Fatalf("execute paths = %q / %q, want them untouched", cfg.Execute.RepoRoot, cfg.Execute.RunsDir)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Server: ServerConfig{Addr: "0.0.0.0:18800", WebRoot: "web/dist"},
		SQLite: SQLiteConfig{Path: "var/jarvis.db"},
		Extract: ExtractConfig{
			Schedule:              "@every 10m",
			Concurrency:           2,
			Engine:                "codex",
			CodexSandbox:          "danger-full-access",
			CodexNetwork:          true,
			CodexReasoningEffort:  "low",
			BatchMessages:         400,
			ContextMessages:       20,
			ContextWindowMinutes:  120,
			OpenTodoLimit:         50,
			FactLimit:             10,
			KeyPersonLimit:        5,
			RecentTaskLimit:       10,
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
		FactEngine:    validFactEngineConfig(),
		Proactive:     validProactiveConfig(),
		MeetingSweep:  validMeetingSweepConfig(),
		MorningBrief:  validMorningBriefConfig(),
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
		{name: "sqlite path", mutate: func(c *Config) { c.SQLite.Path = "" }, wantErr: "sqlite.path"},
		{name: "extract schedule", mutate: func(c *Config) { c.Extract.Schedule = "" }, wantErr: "extract.schedule"},
		{name: "extract concurrency", mutate: func(c *Config) { c.Extract.Concurrency = 0 }, wantErr: "extract.concurrency"},
		{name: "extract batch", mutate: func(c *Config) { c.Extract.BatchMessages = 0 }, wantErr: "extract.batch_messages"},
		{name: "extract context count", mutate: func(c *Config) { c.Extract.ContextMessages = -1 }, wantErr: "extract.context_messages"},
		{name: "extract context window", mutate: func(c *Config) { c.Extract.ContextWindowMinutes = 0 }, wantErr: "extract.context_window_minutes"},
		{name: "extract todo limit", mutate: func(c *Config) { c.Extract.OpenTodoLimit = 0 }, wantErr: "extract.open_todo_limit"},
		{name: "extract fact limit", mutate: func(c *Config) { c.Extract.FactLimit = 0 }, wantErr: "extract.fact_limit"},
		{name: "extract key person limit", mutate: func(c *Config) { c.Extract.KeyPersonLimit = 0 }, wantErr: "extract.key_person_limit"},
		{name: "extract recent task limit", mutate: func(c *Config) { c.Extract.RecentTaskLimit = 0 }, wantErr: "extract.recent_task_limit"},
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
		}, wantErr: "model.model"},
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
		{name: "card approval profile", mutate: func(c *Config) { c.CardApproval.Enabled = true }, wantErr: "card_approval"},
		{name: "card approval principal", mutate: func(c *Config) {
			c.CardApproval = CardApprovalConfig{Enabled: true, Profile: "cli_approval"}
		}, wantErr: "card_approval.enabled"},
		{name: "card approval relay secret", mutate: func(c *Config) {
			c.CardApproval = CardApprovalConfig{
				Enabled: true, Profile: "cli_cc_connect", PrincipalOpenID: "ou_owner",
			}
		}, wantErr: "relay_secret"},
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
		{name: "factengine rollup model", mutate: func(c *Config) { c.FactEngine.RollupModel = "" }, wantErr: "factengine.rollup_model"},
		{name: "factengine sandbox", mutate: func(c *Config) { c.FactEngine.Sandbox = "yolo" }, wantErr: "factengine.sandbox"},
		{name: "factengine timeout", mutate: func(c *Config) { c.FactEngine.TimeoutSec = 0 }, wantErr: "factengine.timeout_sec"},
		{name: "factengine batch", mutate: func(c *Config) { c.FactEngine.BatchLimit = 0 }, wantErr: "factengine.batch_limit"},
		{name: "factengine material chars", mutate: func(c *Config) { c.FactEngine.MaxMaterialChars = 0 }, wantErr: "factengine.max_material_chars"},
		{name: "factengine window gap", mutate: func(c *Config) { c.FactEngine.WindowGapMinutes = 0 }, wantErr: "factengine.window_gap_minutes"},
		{name: "factengine window max", mutate: func(c *Config) { c.FactEngine.WindowMaxMessages = 0 }, wantErr: "factengine.window_max_messages"},
		{name: "proactive schedule", mutate: func(c *Config) { c.Proactive.Schedule = "" }, wantErr: "proactive.schedule"},
		{name: "proactive startup delay", mutate: func(c *Config) { c.Proactive.StartupDelaySeconds = 0 }, wantErr: "proactive.startup_delay_seconds"},
		{name: "proactive bin", mutate: func(c *Config) { c.Proactive.Bin = "" }, wantErr: "proactive.bin"},
		{name: "proactive model", mutate: func(c *Config) { c.Proactive.Model = "" }, wantErr: "proactive.model"},
		{name: "proactive sandbox", mutate: func(c *Config) { c.Proactive.Sandbox = "yolo" }, wantErr: "proactive.sandbox"},
		{name: "proactive reasoning", mutate: func(c *Config) { c.Proactive.ReasoningEffort = "ultra" }, wantErr: "proactive.codex_reasoning_effort"},
		{name: "proactive timeout", mutate: func(c *Config) { c.Proactive.TimeoutSeconds = 0 }, wantErr: "proactive.timeout_seconds"},
		{name: "meeting sweep schedule", mutate: func(c *Config) { c.MeetingSweep.Schedule = "" }, wantErr: "meeting_sweep.schedule"},
		{name: "meeting sweep startup delay", mutate: func(c *Config) { c.MeetingSweep.StartupDelaySeconds = 0 }, wantErr: "meeting_sweep.startup_delay_seconds"},
		{name: "meeting sweep bin", mutate: func(c *Config) { c.MeetingSweep.Bin = "" }, wantErr: "meeting_sweep.bin"},
		{name: "meeting sweep model", mutate: func(c *Config) { c.MeetingSweep.Model = "" }, wantErr: "meeting_sweep.model"},
		{name: "meeting sweep sandbox", mutate: func(c *Config) { c.MeetingSweep.Sandbox = "yolo" }, wantErr: "meeting_sweep.sandbox"},
		{name: "meeting sweep reasoning", mutate: func(c *Config) { c.MeetingSweep.ReasoningEffort = "ultra" }, wantErr: "meeting_sweep.codex_reasoning_effort"},
		{name: "meeting sweep timeout", mutate: func(c *Config) { c.MeetingSweep.TimeoutSeconds = 0 }, wantErr: "meeting_sweep.timeout_seconds"},
		{name: "morning brief schedule", mutate: func(c *Config) { c.MorningBrief.Schedule = "" }, wantErr: "morning_brief.schedule"},
		{name: "morning brief startup delay", mutate: func(c *Config) { c.MorningBrief.StartupDelaySeconds = 0 }, wantErr: "morning_brief.startup_delay_seconds"},
		{name: "morning brief bin", mutate: func(c *Config) { c.MorningBrief.Bin = "" }, wantErr: "morning_brief.bin"},
		{name: "morning brief model", mutate: func(c *Config) { c.MorningBrief.Model = "" }, wantErr: "morning_brief.model"},
		{name: "morning brief sandbox", mutate: func(c *Config) { c.MorningBrief.Sandbox = "yolo" }, wantErr: "morning_brief.sandbox"},
		{name: "morning brief reasoning", mutate: func(c *Config) { c.MorningBrief.ReasoningEffort = "ultra" }, wantErr: "morning_brief.codex_reasoning_effort"},
		{name: "morning brief timeout", mutate: func(c *Config) { c.MorningBrief.TimeoutSeconds = 0 }, wantErr: "morning_brief.timeout_seconds"},
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
		Server: ServerConfig{Addr: "0.0.0.0:18800", WebRoot: "web/dist"},
		SQLite: SQLiteConfig{Path: "var/jarvis.db"},
		Model: ModelConfig{
			Model: "model", TimeoutSec: 60,
		},
		Extract: ExtractConfig{
			Enabled: true, PrincipalOpenID: "ou_owner", Schedule: "@every 10m",
			Engine: "codex", CodexSandbox: "danger-full-access", CodexNetwork: true, CodexReasoningEffort: "low",
			Concurrency: 2, BatchMessages: 400, ContextMessages: 20, ContextWindowMinutes: 120,
			OpenTodoLimit: 50, FactLimit: 10, KeyPersonLimit: 5, RecentTaskLimit: 10, MaxPromptChars: 60000,
			SemanticCollection: "todo_semantic", SemanticThreshold: 0.85, SemanticNeighborLimit: 3,
			ToolTimeoutSec: 10, HistoryToolLimit: 50, QdrantHost: "127.0.0.1", QdrantGRPCPort: 6334,
		},
		LarkCLI: LarkCLIConfig{Bin: "lark-cli", RateLimit: 5, Burst: 10, Concurrent: 2, TimeoutSec: 60},
		Capture: CaptureConfig{
			PageSize: 50, ScanWorkers: 2, HotAgeHours: 6, WarmAgeHours: 168,
			Timezone: "Asia/Shanghai", DiscoverSchedule: "@every 6h", ScanSchedule: "@every 5m",
		},
		FactEngine:    validFactEngineConfig(),
		Proactive:     validProactiveConfig(),
		MeetingSweep:  validMeetingSweepConfig(),
		MorningBrief:  validMorningBriefConfig(),
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
		GitAuthor: "owner@example.com", GroupMessageLimit: 200, GroupConcurrency: 2,
	}
}

func validScheduledTaskConfig() ScheduledTaskConfig {
	return ScheduledTaskConfig{Enabled: false, Schedule: "@every 1m", BatchLimit: 20}
}

func validFactEngineConfig() FactEngineConfig {
	return FactEngineConfig{
		Enabled: true, Schedule: "@every 15m", RollupSchedule: "0 2 * * *",
		Bin: "traex", Model: "fixture-fact-model", RollupModel: "fixture-rollup-model", Sandbox: "danger-full-access", TimeoutSec: 300,
		BatchLimit: 200, MaxMaterialChars: 100000, WindowGapMinutes: 30, WindowMaxMessages: 40,
	}
}

func validProactiveConfig() ProactiveConfig {
	return ProactiveConfig{
		Enabled: true, Schedule: "@every 1h", StartupDelaySeconds: 120,
		Bin: "traex", Model: "DeepSeek-V4-Pro", Sandbox: "danger-full-access",
		ReasoningEffort: "medium", TimeoutSeconds: 900,
	}
}

func validMeetingSweepConfig() MeetingSweepConfig {
	return MeetingSweepConfig{
		Enabled: true, Schedule: "@every 2h", StartupDelaySeconds: 150,
		Bin: "traex", Model: "DeepSeek-V4-Flash", Sandbox: "danger-full-access",
		ReasoningEffort: "low", TimeoutSeconds: 600,
	}
}

func validMorningBriefConfig() MorningBriefConfig {
	return MorningBriefConfig{
		Enabled: true, Schedule: "30 8 * * 1-5", StartupDelaySeconds: 180,
		Bin: "traex", Model: "gpt-5.6-sol", Sandbox: "danger-full-access",
		ReasoningEffort: "medium", TimeoutSeconds: 600,
	}
}
