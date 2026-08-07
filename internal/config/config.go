// Package config 负责加载 Jarvis 的本地配置。
//
// 本地可信环境：配置文件明文存储密钥，不加密。加载遵循 fail-fast——
// 文件缺失或解析失败直接返回 error，绝不静默使用零值默认跑起来。
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// Config 是全局配置的根。各子结构对应总纲 §1 技术栈里的外部依赖。
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	SQLite        SQLiteConfig        `yaml:"sqlite"`
	Model         ModelConfig         `yaml:"model"`
	FactEngine    FactEngineConfig    `yaml:"factengine"`
	Proactive     ProactiveConfig     `yaml:"proactive"`
	MeetingSweep  MeetingSweepConfig  `yaml:"meeting_sweep"`
	MorningBrief  MorningBriefConfig  `yaml:"morning_brief"`
	Extract       ExtractConfig       `yaml:"extract"`
	LarkCLI       LarkCLIConfig       `yaml:"lark_cli"`
	Capture       CaptureConfig       `yaml:"capture"`
	CardApproval  CardApprovalConfig  `yaml:"card_approval"`
	Codex         CodexConfig         `yaml:"codex"`
	Execute       ExecuteConfig       `yaml:"execute"`
	Chat          ChatConfig          `yaml:"chat"`
	Skills        SkillsConfig        `yaml:"skills"`
	DailyDigest   DailyDigestConfig   `yaml:"dailydigest"`
	ScheduledTask ScheduledTaskConfig `yaml:"scheduled_task"`
}

// ServerConfig Hertz 监听配置。
type ServerConfig struct {
	Addr     string   `yaml:"addr"`      // 形如 0.0.0.0:18800
	WebRoot  string   `yaml:"web_root"`  // React production build directory
	LogFiles []string `yaml:"log_files"` // 运行日志文件（供调试面板尾读并归并）；默认 server 的 stdout+stderr 两个文件。cron 日志走 stderr，必须都读。
}

// SQLiteConfig is the single local business source of truth.
type SQLiteConfig struct {
	Path string `yaml:"path"`
}

// ModelConfig controls the Ark chat model used for semantic adjudication and
// the optional model_api extraction engine. Endpoint identity is code-owned.
type ModelConfig struct {
	Model            string `yaml:"model"`
	IsReasoningModel bool   `yaml:"is_reasoning_model"`
	TimeoutSec       int    `yaml:"timeout_sec"`
}

// FactEngineConfig controls the offline world-maintenance Agent. One session
// reads a coarsely bounded batch of new material and writes current entities,
// relations and Facts directly through tools. It stays off the M2→M3→M5 path.
type FactEngineConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Schedule string `yaml:"schedule"`
	// RollupSchedule is the daily compression cron. It runs independently of
	// Schedule (which drives detail extraction) and writes source_kind=rollup
	// facts for the previous local day.
	RollupSchedule string `yaml:"rollup_schedule"`

	Bin         string `yaml:"bin"`
	Model       string `yaml:"model"`
	RollupModel string `yaml:"rollup_model"`
	Sandbox     string `yaml:"sandbox"`
	TimeoutSec  int    `yaml:"timeout_sec"`

	// BatchLimit supplies the initial rows-per-source candidate. The worker halves
	// it until the complete rendered material fits MaxMaterialChars; it never
	// truncates an individual material item.
	BatchLimit        int `yaml:"batch_limit"`
	MaxMaterialChars  int `yaml:"max_material_chars"`
	WindowGapMinutes  int `yaml:"window_gap_minutes"`
	WindowMaxMessages int `yaml:"window_max_messages"`
}

// ProactiveConfig controls the cheap periodic agent that reviews Jarvis's
// current world model. It may update internal world-model records directly,
// but hands every external action to the strong M5 by creating a normal Task.
type ProactiveConfig struct {
	Enabled             bool   `yaml:"enabled"`
	Schedule            string `yaml:"schedule"`
	StartupDelaySeconds int    `yaml:"startup_delay_seconds"`
	Bin                 string `yaml:"bin"`
	Model               string `yaml:"model"`
	Sandbox             string `yaml:"sandbox"`
	ReasoningEffort     string `yaml:"reasoning_effort"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
}

// MeetingSweepConfig controls the cheap periodic meeting collector. It searches
// recently ended Feishu meetings and delivers each as a clue for M3; it runs no
// analysis and creates no Task itself, so it mirrors ProactiveConfig's runtime
// shape without any world-model write access.
type MeetingSweepConfig struct {
	Enabled             bool   `yaml:"enabled"`
	Schedule            string `yaml:"schedule"`
	StartupDelaySeconds int    `yaml:"startup_delay_seconds"`
	Bin                 string `yaml:"bin"`
	Model               string `yaml:"model"`
	Sandbox             string `yaml:"sandbox"`
	ReasoningEffort     string `yaml:"reasoning_effort"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
}

// MorningBriefConfig controls the weekday morning planning brief. The agent is
// Skill-driven and write-mostly to local Markdown; its only pre-authorized
// external side effect is one Feishu DM to the principal. Runtime shape matches
// MeetingSweepConfig so -morning-brief-once remains available when cron is off.
type MorningBriefConfig struct {
	Enabled             bool   `yaml:"enabled"`
	Schedule            string `yaml:"schedule"`
	StartupDelaySeconds int    `yaml:"startup_delay_seconds"`
	Bin                 string `yaml:"bin"`
	Model               string `yaml:"model"`
	Sandbox             string `yaml:"sandbox"`
	ReasoningEffort     string `yaml:"reasoning_effort"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
}

// ExtractConfig controls the M3 extraction worker. Disabled is an explicit
// deployment state; once enabled every required dependency is validated.
type ExtractConfig struct {
	Enabled         bool   `yaml:"enabled"`
	PrincipalOpenID string `yaml:"principal_open_id"`
	Schedule        string `yaml:"schedule"`

	// Engine selects the M3 extraction engine: "codex" (default, an agent that
	// can self-run lark-cli/bytedcli/git/jarvis-tools to infer project/repos) or
	// "model_api" (the legacy kimi function-calling loop, kept as fallback).
	Engine string `yaml:"engine"`
	// CodexSandbox / CodexNetwork / CodexReasoningEffort configure the codex
	// engine. In the local trusted environment the sandbox is danger-full-access
	// with network enabled so codex can query Feishu-side info; reasoning_effort
	// is forced low to override the user's global xhigh and cap per-call latency.
	CodexSandbox          string  `yaml:"codex_sandbox"`
	CodexNetwork          bool    `yaml:"codex_network"`
	CodexReasoningEffort  string  `yaml:"codex_reasoning_effort"`
	Concurrency           int     `yaml:"concurrency"`
	BatchMessages         int     `yaml:"batch_messages"`
	ContextMessages       int     `yaml:"context_messages"`
	ContextWindowMinutes  int     `yaml:"context_window_minutes"`
	OpenTodoLimit         int     `yaml:"open_todo_limit"`
	MaxPromptChars        int     `yaml:"max_prompt_chars"`
	SemanticCollection    string  `yaml:"semantic_collection"`
	SemanticThreshold     float64 `yaml:"semantic_threshold"`
	SemanticNeighborLimit int     `yaml:"semantic_neighbor_limit"`

	// FactLimit caps how many of a subject's *today* detail facts (excluding
	// rollups) are injected into one extraction prompt. Each subject also gets
	// at most one previous-day rollup on top of this.
	FactLimit int `yaml:"fact_limit"`
	// KeyPersonLimit caps how many person subjects (assigner ∪ leaders ∪
	// speakers) contribute facts to one extraction prompt.
	KeyPersonLimit int `yaml:"key_person_limit"`
	// RecentTaskLimit caps how many recently progressed tasks are injected.
	RecentTaskLimit int `yaml:"recent_task_limit"`

	// QdrantHost/QdrantGRPCPort locate the vector store backing SemanticCollection.
	QdrantHost     string `yaml:"qdrant_host"`
	QdrantGRPCPort int    `yaml:"qdrant_grpc_port"`

	// M3 function-calling tool loop. ToolTimeoutSec bounds one tool call;
	// HistoryToolLimit caps rows returned by query_chat_history.
	ToolTimeoutSec   int `yaml:"tool_timeout_sec"`
	HistoryToolLimit int `yaml:"history_tool_limit"`

	// EvidenceRetryMax caps how many extra extraction attempts are made per unit
	// when a candidate's source_quote is not a verbatim substring of the cited
	// [new] messages. On such a failure the model is fed the mismatch details plus
	// the cited 原文 and asked to re-extract without paraphrasing/splicing. 0
	// disables retry (extract once). Must be >= 0.
	EvidenceRetryMax int `yaml:"evidence_retry_max"`
}

// LarkCLIConfig lark-cli 子进程封装（总纲 §4）。
type LarkCLIConfig struct {
	Bin        string  `yaml:"bin"`         // lark-cli 绝对路径
	Profile    string  `yaml:"profile"`     // 可选；为空时使用 lark-cli 当前默认 profile
	RateLimit  float64 `yaml:"rate_limit"`  // 令牌桶补充速率 tokens/s
	Burst      int     `yaml:"burst"`       // 令牌桶容量
	Concurrent int     `yaml:"concurrent"`  // 并发子进程上限
	TimeoutSec int     `yaml:"timeout_sec"` // 单次调用超时
}

// CaptureConfig controls M2 pagination, time parsing and chat tier thresholds.
// HotAgeHours/WarmAgeHours drive only the display-only tier label; related
// chats are scanned at one uniform ScanSchedule cadence regardless of tier.
type CaptureConfig struct {
	PageSize         int    `yaml:"page_size"`
	ScanWorkers      int    `yaml:"scan_workers"`
	HotAgeHours      int    `yaml:"hot_age_hours"`
	WarmAgeHours     int    `yaml:"warm_age_hours"`
	Timezone         string `yaml:"timezone"`
	DiscoverSchedule string `yaml:"discover_schedule"`
	ScanSchedule     string `yaml:"scan_schedule"`
	// AutoRelatedP2PTopN：discover 时按 active_time 自动纳入监听的内部真人私聊
	// 上限。只开最活跃的前 N 个，僵尸老私聊与服务号私聊不开。
	AutoRelatedP2PTopN int `yaml:"auto_related_p2p_top_n"`
}

// CardApprovalConfig accepts authenticated localhost callbacks forwarded by
// CC Connect, which remains the sole owner of the Jarvis Bot Feishu connection.
type CardApprovalConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Profile         string `yaml:"profile"`
	PrincipalOpenID string `yaml:"principal_open_id"`
	RelaySecret     string `yaml:"relay_secret"`
}

// CodexConfig controls the agent CLI used by M3 extraction.
type CodexConfig struct {
	Bin            string `yaml:"bin"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// ExecuteConfig controls M5 agent-driven execution. Enabled turns on real-time
// auto-execution plus its compensation cron; manual execution via the API is
// always available regardless. RepoRoot is the base directory a Task's
// repo_ref slot is joined under for code changes. Bin/Model 可独立于
// codex 段（例如抽取用 traex，真正执行用官方 codex + 更强模型）。
type ExecuteConfig struct {
	Enabled              bool   `yaml:"enabled"`                // 是否开实时自动执行（本地动作）
	Schedule             string `yaml:"schedule"`               // 补偿 cron 表达式
	BatchLimit           int    `yaml:"batch_limit"`            // 单次 sweep 最多执行的 Task 数
	Concurrency          int    `yaml:"concurrency"`            // 单次 sweep 内并行执行的 Task 数（>=1）
	RepoRoot             string `yaml:"repo_root"`              // code_change repo_ref 的基目录
	RunsDir              string `yaml:"runs_dir"`               // diff/产物落盘目录
	Bin                  string `yaml:"bin"`                    // M5 执行用的 agent CLI（codex / traex）
	Model                string `yaml:"model"`                  // M5 执行模型
	ReasoningEffort      string `yaml:"reasoning_effort"`       // M5 思考级别：minimal/low/medium/high/xhigh
	TimeoutSecond        int    `yaml:"timeout_second"`         // 单次执行超时
	StaleExecutingMinute int    `yaml:"stale_executing_minute"` // executing 超过此时长仍未结束 → 标 failed（防重启僵尸）
}

// ChatConfig 控制「基于 agent CLI 的流式对话服务」（/api/chat，SSE）。
// Enabled=false 时不注册路由。CLI 二进制复用 execute.bin（与 M5 同引擎）；
// 沙箱固定为对话场景的 danger-full-access + 联网（本地可信环境），reasoning_effort 可调。
type ChatConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Model           string `yaml:"model"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
	Sandbox         string `yaml:"sandbox"`
	ReasoningEffort string `yaml:"reasoning_effort"`
}

// SkillsConfig controls the repository skill root scanned into the stage-aware
// Skill catalog. Relative paths are resolved from the server working directory.
type SkillsConfig struct {
	Root string `yaml:"root"`
}

// DailyDigestConfig 控制「每日进度总结」：19:00 cron 自动生成 + 页面手动异步触发。
// Enabled=false 时不起 scheduler（手动生成接口仍可用）。个人全景使用一轮并行
// 外部取证，独立超时只负责终止失控运行。
type DailyDigestConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Schedule          string `yaml:"schedule"`            // cron 表达式，默认 "0 19 * * *"（每晚 19:00）
	TimeoutSeconds    int    `yaml:"timeout_seconds"`     // 单次 Codex 总编排硬上限，默认 600
	GitAuthor         string `yaml:"git_author"`          // 个人日报查询提交时使用的 git log --author 模式，由初始化写入本机配置
	GroupMessageLimit int    `yaml:"group_message_limit"` // 每群每天喂进 prompt 的消息上限，默认 200
	GroupConcurrency  int    `yaml:"group_concurrency"`   // 一轮批量里群总结的并发上限，默认 2，>=1
}

// ScheduledTaskConfig controls the recurring Codex task scanner.
// The runner itself reuses execute.bin/model/reasoning/timeout.
type ScheduledTaskConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Schedule   string `yaml:"schedule"`
	BatchLimit int    `yaml:"batch_limit"`
}

// Load 从指定路径读取并解析 YAML 配置。fail-fast：任何错误直接返回。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := decodeKnownYAML(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	overridePath := RuntimeOverridePath(path)
	overrideRaw, err := os.ReadFile(overridePath)
	if err == nil {
		if err := decodeKnownYAML(overrideRaw, &cfg); err != nil {
			return nil, fmt.Errorf("parse runtime config override %q: %w", overridePath, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read runtime config override %q: %w", overridePath, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	if err := cfg.resolvePaths(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

// resolvePaths 把配置里的相对路径按进程工作目录展开成绝对路径，让基线配置
// 不必写死某台机器的用户名和安装位置。
//
// 只处理会离开本进程的两个路径：repo_root 会写进交给模型的 prompt，runs_dir
// 下的产物路径同样要在其它工作目录里可用；而 codex 子进程的工作目录是被改的
// 仓库或临时目录，相对值到那里就解析错了。sqlite.path、server.web_root、
// skills.root 只在本进程内打开，保持相对即可。
func (c *Config) resolvePaths() error {
	for _, item := range []struct {
		name  string
		value *string
	}{
		{"execute.repo_root", &c.Execute.RepoRoot},
		{"execute.runs_dir", &c.Execute.RunsDir},
	} {
		absolute, err := filepath.Abs(*item.value)
		if err != nil {
			return fmt.Errorf("展开 %s %q 失败: %w", item.name, *item.value, err)
		}
		*item.value = absolute
	}
	return nil
}

func decodeKnownYAML(raw []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

// validate 校验当前已启用模块的全部启动条件。model/codex 会在各自
// 里程碑启用时加入对应校验。
func (c *Config) validate() error {
	if c.Server.Addr == "" {
		return fmt.Errorf("server.addr 不能为空")
	}
	if c.Server.WebRoot == "" {
		return fmt.Errorf("server.web_root 不能为空")
	}
	if len(c.Server.LogFiles) == 0 {
		// stdout（路由/启动）与 stderr（各 cron 运行结果、报错）默认都读，否则 cron 日志漏看。
		c.Server.LogFiles = []string{"var/log/jarvis-server.log", "var/log/jarvis-server.error.log"}
	}
	if c.SQLite.Path == "" {
		return fmt.Errorf("sqlite.path 不能为空")
	}
	if err := c.validateFactEngine(); err != nil {
		return err
	}
	if err := c.validateProactive(); err != nil {
		return err
	}
	if err := c.validateMeetingSweep(); err != nil {
		return err
	}
	if err := c.validateMorningBrief(); err != nil {
		return err
	}
	if c.Extract.Schedule == "" {
		return fmt.Errorf("extract.schedule 不能为空")
	}
	if c.Extract.Concurrency <= 0 {
		return fmt.Errorf("extract.concurrency 必须大于 0")
	}
	if c.Extract.BatchMessages <= 0 {
		return fmt.Errorf("extract.batch_messages 必须大于 0")
	}
	if c.Extract.ContextMessages < 0 {
		return fmt.Errorf("extract.context_messages 不能小于 0")
	}
	if c.Extract.ContextWindowMinutes <= 0 {
		return fmt.Errorf("extract.context_window_minutes 必须大于 0")
	}
	if c.Extract.OpenTodoLimit <= 0 {
		return fmt.Errorf("extract.open_todo_limit 必须大于 0")
	}
	if c.Extract.FactLimit <= 0 {
		return fmt.Errorf("extract.fact_limit 必须大于 0")
	}
	if c.Extract.KeyPersonLimit <= 0 {
		return fmt.Errorf("extract.key_person_limit 必须大于 0")
	}
	if c.Extract.RecentTaskLimit <= 0 {
		return fmt.Errorf("extract.recent_task_limit 必须大于 0")
	}
	if c.Extract.MaxPromptChars <= 0 {
		return fmt.Errorf("extract.max_prompt_chars 必须大于 0")
	}
	if c.Extract.SemanticCollection == "" {
		return fmt.Errorf("extract.semantic_collection 不能为空")
	}
	if c.Extract.QdrantHost == "" {
		return fmt.Errorf("extract.qdrant_host 不能为空")
	}
	if c.Extract.QdrantGRPCPort <= 0 || c.Extract.QdrantGRPCPort > 65535 {
		return fmt.Errorf("extract.qdrant_grpc_port 必须在 1 到 65535 之间")
	}
	if c.Extract.SemanticThreshold <= 0 || c.Extract.SemanticThreshold > 1 {
		return fmt.Errorf("extract.semantic_threshold 必须在 0（不含）到 1 之间")
	}
	if c.Extract.SemanticNeighborLimit <= 0 {
		return fmt.Errorf("extract.semantic_neighbor_limit 必须大于 0")
	}
	if c.Extract.ToolTimeoutSec <= 0 {
		return fmt.Errorf("extract.tool_timeout_sec 必须大于 0")
	}
	if c.Extract.HistoryToolLimit <= 0 {
		return fmt.Errorf("extract.history_tool_limit 必须大于 0")
	}
	if c.Extract.EvidenceRetryMax < 0 {
		return fmt.Errorf("extract.evidence_retry_max 不能为负数")
	}
	if c.Extract.Engine != "codex" && c.Extract.Engine != "model_api" {
		return fmt.Errorf("extract.engine 必须是 codex 或 model_api")
	}
	if err := validateCodexSandbox("extract.codex_sandbox", c.Extract.CodexSandbox); err != nil {
		return err
	}
	if err := validateReasoningEffort("extract", c.Extract.CodexReasoningEffort); err != nil {
		return err
	}
	if c.Extract.Enabled {
		if c.Extract.PrincipalOpenID == "" {
			return fmt.Errorf("extract.principal_open_id 不能为空")
		}
		if c.Model.Model == "" {
			return fmt.Errorf("extract 启用时 model.model 不能为空")
		}
		if c.Model.TimeoutSec <= 0 {
			return fmt.Errorf("extract 启用时 model.timeout_sec 必须大于 0")
		}
	}
	if c.LarkCLI.Bin == "" {
		return fmt.Errorf("lark_cli.bin 不能为空")
	}
	if c.LarkCLI.RateLimit <= 0 {
		return fmt.Errorf("lark_cli.rate_limit 必须大于 0")
	}
	if c.LarkCLI.Burst <= 0 {
		return fmt.Errorf("lark_cli.burst 必须大于 0")
	}
	if c.LarkCLI.Concurrent <= 0 {
		return fmt.Errorf("lark_cli.concurrent 必须大于 0")
	}
	if c.LarkCLI.TimeoutSec <= 0 {
		return fmt.Errorf("lark_cli.timeout_sec 必须大于 0")
	}
	if c.Capture.PageSize < 1 || c.Capture.PageSize > 50 {
		return fmt.Errorf("capture.page_size 必须在 1 到 50 之间")
	}
	if c.Capture.ScanWorkers <= 0 {
		return fmt.Errorf("capture.scan_workers 必须大于 0")
	}
	if c.Capture.HotAgeHours <= 0 {
		return fmt.Errorf("capture.hot_age_hours 必须大于 0")
	}
	if c.Capture.WarmAgeHours <= c.Capture.HotAgeHours {
		return fmt.Errorf("capture.warm_age_hours 必须大于 capture.hot_age_hours")
	}
	if c.Capture.Timezone == "" {
		return fmt.Errorf("capture.timezone 不能为空")
	}
	if c.Capture.DiscoverSchedule == "" || c.Capture.ScanSchedule == "" {
		return fmt.Errorf("capture 的 discover/scan schedule 均不能为空")
	}
	if c.CardApproval.Enabled {
		if c.CardApproval.Profile == "" {
			return fmt.Errorf("card_approval.enabled=true 时 profile 不能为空")
		}
		if c.CardApproval.PrincipalOpenID == "" {
			return fmt.Errorf("card_approval.enabled=true 时 principal_open_id 不能为空")
		}
		if strings.TrimSpace(c.CardApproval.RelaySecret) == "" {
			return fmt.Errorf("card_approval.enabled=true 时 relay_secret 不能为空")
		}
	}
	if c.Capture.AutoRelatedP2PTopN < 0 {
		return fmt.Errorf("capture.auto_related_p2p_top_n 不能为负数")
	}
	if c.Codex.Bin == "" {
		return fmt.Errorf("codex.bin 不能为空")
	}
	if c.Codex.Model == "" {
		return fmt.Errorf("codex.model 不能为空")
	}
	if c.Codex.TimeoutSeconds <= 0 {
		return fmt.Errorf("codex.timeout_seconds 必须大于 0")
	}
	if c.Execute.RepoRoot == "" {
		return fmt.Errorf("execute.repo_root 不能为空")
	}
	if c.Execute.RunsDir == "" {
		return fmt.Errorf("execute.runs_dir 不能为空")
	}
	if c.Execute.Bin == "" {
		return fmt.Errorf("execute.bin 不能为空")
	}
	if c.Execute.Model == "" {
		return fmt.Errorf("execute.model 不能为空")
	}
	if err := validateReasoningEffort("execute", c.Execute.ReasoningEffort); err != nil {
		return err
	}
	if c.Execute.TimeoutSecond <= 0 {
		return fmt.Errorf("execute.timeout_second 必须大于 0")
	}
	if c.Execute.StaleExecutingMinute <= 0 {
		return fmt.Errorf("execute.stale_executing_minute 必须大于 0")
	}
	if c.Execute.StaleExecutingMinute*60 <= c.Execute.TimeoutSecond {
		return fmt.Errorf("execute.stale_executing_minute（%dm）必须大于 timeout_second（%ds），否则会误杀仍在跑的任务",
			c.Execute.StaleExecutingMinute, c.Execute.TimeoutSecond)
	}
	if c.Execute.Enabled {
		if c.Execute.Schedule == "" {
			return fmt.Errorf("execute.schedule 不能为空")
		}
		if c.Execute.BatchLimit <= 0 {
			return fmt.Errorf("execute.batch_limit 必须大于 0")
		}
		if c.Execute.Concurrency <= 0 {
			return fmt.Errorf("execute.concurrency 必须大于 0")
		}
	}
	if err := validateCodexSandbox("chat.sandbox", c.Chat.Sandbox); err != nil {
		return err
	}
	if err := validateReasoningEffort("chat", c.Chat.ReasoningEffort); err != nil {
		return err
	}
	if c.Chat.TimeoutSeconds <= 0 {
		return fmt.Errorf("chat.timeout_seconds 必须大于 0")
	}
	if c.Chat.Enabled && c.Chat.Model == "" {
		return fmt.Errorf("chat 启用时 chat.model 不能为空")
	}
	if c.Skills.Root == "" {
		return fmt.Errorf("skills.root 不能为空")
	}
	if c.DailyDigest.Schedule == "" {
		return fmt.Errorf("dailydigest.schedule 不能为空")
	}
	if c.DailyDigest.TimeoutSeconds < 300 {
		return fmt.Errorf("dailydigest.timeout_seconds 必须大于等于 300")
	}
	if strings.TrimSpace(c.DailyDigest.GitAuthor) == "" {
		return fmt.Errorf("dailydigest.git_author 不能为空")
	}
	if c.DailyDigest.GroupMessageLimit <= 0 {
		return fmt.Errorf("dailydigest.group_message_limit 必须大于 0")
	}
	if c.DailyDigest.GroupConcurrency < 1 {
		return fmt.Errorf("dailydigest.group_concurrency 必须大于等于 1")
	}
	if c.ScheduledTask.Schedule == "" {
		return fmt.Errorf("scheduled_task.schedule 不能为空")
	}
	if c.ScheduledTask.BatchLimit <= 0 {
		return fmt.Errorf("scheduled_task.batch_limit 必须大于 0")
	}
	if c.ScheduledTask.Enabled && !c.Execute.Enabled {
		return fmt.Errorf("scheduled_task 启用时 execute.enabled 必须为 true")
	}
	schedules := []struct {
		name string
		spec string
	}{
		{name: "factengine.schedule", spec: c.FactEngine.Schedule},
		{name: "factengine.rollup_schedule", spec: c.FactEngine.RollupSchedule},
		{name: "proactive.schedule", spec: c.Proactive.Schedule},
		{name: "meeting_sweep.schedule", spec: c.MeetingSweep.Schedule},
		{name: "morning_brief.schedule", spec: c.MorningBrief.Schedule},
		{name: "extract.schedule", spec: c.Extract.Schedule},
		{name: "capture.discover_schedule", spec: c.Capture.DiscoverSchedule},
		{name: "capture.scan_schedule", spec: c.Capture.ScanSchedule},
		{name: "execute.schedule", spec: c.Execute.Schedule},
		{name: "dailydigest.schedule", spec: c.DailyDigest.Schedule},
		{name: "scheduled_task.schedule", spec: c.ScheduledTask.Schedule},
	}
	for _, schedule := range schedules {
		if _, err := cron.ParseStandard(schedule.spec); err != nil {
			return fmt.Errorf("%s=%q 不是有效 cron/@every 表达式: %w", schedule.name, schedule.spec, err)
		}
	}
	return nil
}

// validateProactive validates every field even when the cron is disabled,
// because -proactive-once remains available as an explicit one-shot action.
func (c *Config) validateProactive() error {
	if c.Proactive.Schedule == "" {
		return fmt.Errorf("proactive.schedule 不能为空")
	}
	if c.Proactive.StartupDelaySeconds <= 0 {
		return fmt.Errorf("proactive.startup_delay_seconds 必须大于 0")
	}
	if c.Proactive.Bin == "" {
		return fmt.Errorf("proactive.bin 不能为空")
	}
	if c.Proactive.Model == "" {
		return fmt.Errorf("proactive.model 不能为空")
	}
	if err := validateCodexSandbox("proactive.sandbox", c.Proactive.Sandbox); err != nil {
		return err
	}
	if err := validateReasoningEffort("proactive", c.Proactive.ReasoningEffort); err != nil {
		return err
	}
	if c.Proactive.TimeoutSeconds <= 0 {
		return fmt.Errorf("proactive.timeout_seconds 必须大于 0")
	}
	return nil
}

// validateMeetingSweep validates every field even when the cron is disabled,
// because -meeting-sweep-once remains available as an explicit one-shot action.
func (c *Config) validateMeetingSweep() error {
	if c.MeetingSweep.Schedule == "" {
		return fmt.Errorf("meeting_sweep.schedule 不能为空")
	}
	if c.MeetingSweep.StartupDelaySeconds <= 0 {
		return fmt.Errorf("meeting_sweep.startup_delay_seconds 必须大于 0")
	}
	if c.MeetingSweep.Bin == "" {
		return fmt.Errorf("meeting_sweep.bin 不能为空")
	}
	if c.MeetingSweep.Model == "" {
		return fmt.Errorf("meeting_sweep.model 不能为空")
	}
	if err := validateCodexSandbox("meeting_sweep.sandbox", c.MeetingSweep.Sandbox); err != nil {
		return err
	}
	if err := validateReasoningEffort("meeting_sweep", c.MeetingSweep.ReasoningEffort); err != nil {
		return err
	}
	if c.MeetingSweep.TimeoutSeconds <= 0 {
		return fmt.Errorf("meeting_sweep.timeout_seconds 必须大于 0")
	}
	return nil
}

// validateMorningBrief validates every field even when the cron is disabled,
// because -morning-brief-once remains available as an explicit one-shot action.
func (c *Config) validateMorningBrief() error {
	if c.MorningBrief.Schedule == "" {
		return fmt.Errorf("morning_brief.schedule 不能为空")
	}
	if c.MorningBrief.StartupDelaySeconds <= 0 {
		return fmt.Errorf("morning_brief.startup_delay_seconds 必须大于 0")
	}
	if c.MorningBrief.Bin == "" {
		return fmt.Errorf("morning_brief.bin 不能为空")
	}
	if c.MorningBrief.Model == "" {
		return fmt.Errorf("morning_brief.model 不能为空")
	}
	if err := validateCodexSandbox("morning_brief.sandbox", c.MorningBrief.Sandbox); err != nil {
		return err
	}
	if err := validateReasoningEffort("morning_brief", c.MorningBrief.ReasoningEffort); err != nil {
		return err
	}
	if c.MorningBrief.TimeoutSeconds <= 0 {
		return fmt.Errorf("morning_brief.timeout_seconds 必须大于 0")
	}
	return nil
}

// validateFactEngine validates the offline fact engine. Everything is required
// regardless of Enabled: the one-shot CLI action runs the engine with a disabled
// cron, so half-configured values must fail at load rather than at first use.
func (c *Config) validateFactEngine() error {
	if c.FactEngine.Schedule == "" {
		return fmt.Errorf("factengine.schedule 不能为空")
	}
	if c.FactEngine.RollupSchedule == "" {
		return fmt.Errorf("factengine.rollup_schedule 不能为空")
	}
	if c.FactEngine.Bin == "" {
		return fmt.Errorf("factengine.bin 不能为空")
	}
	if c.FactEngine.Model == "" {
		return fmt.Errorf("factengine.model 不能为空")
	}
	if c.FactEngine.RollupModel == "" {
		return fmt.Errorf("factengine.rollup_model 不能为空")
	}
	if err := validateCodexSandbox("factengine.sandbox", c.FactEngine.Sandbox); err != nil {
		return err
	}
	if c.FactEngine.TimeoutSec <= 0 {
		return fmt.Errorf("factengine.timeout_sec 必须大于 0")
	}
	if c.FactEngine.BatchLimit <= 0 {
		return fmt.Errorf("factengine.batch_limit 必须大于 0")
	}
	if c.FactEngine.MaxMaterialChars <= 0 {
		return fmt.Errorf("factengine.max_material_chars 必须大于 0")
	}
	if c.FactEngine.WindowGapMinutes <= 0 {
		return fmt.Errorf("factengine.window_gap_minutes 必须大于 0")
	}
	if c.FactEngine.WindowMaxMessages <= 0 {
		return fmt.Errorf("factengine.window_max_messages 必须大于 0")
	}
	return nil
}

// validateCodexSandbox enforces the codex sandbox mode is one of the values
// codex CLI accepts. danger-full-access is intentionally allowed: it is the
// explicit local-trusted-environment posture per docs/design-context-pipeline.md.
// key is the full config key so the error points at the line to edit — sections
// do not agree on the field name (codex_sandbox vs sandbox).
func validateCodexSandbox(key, value string) error {
	switch value {
	case "read-only", "workspace-write", "danger-full-access":
		return nil
	default:
		return fmt.Errorf("%s 必须是 read-only / workspace-write / danger-full-access", key)
	}
}

// validateReasoningEffort enforces the reasoning effort is one codex accepts.
// Jarvis always sets this explicitly to override the user's global xhigh.
func validateReasoningEffort(section, value string) error {
	switch value {
	case "minimal", "low", "medium", "high", "xhigh":
		return nil
	default:
		return fmt.Errorf("%s.codex_reasoning_effort 必须是 minimal / low / medium / high / xhigh", section)
	}
}
