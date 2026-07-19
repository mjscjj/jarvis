// Package config 负责加载 Jarvis 的本地配置。
//
// 本地可信环境：配置文件明文存储密钥/DSN，不加密。加载遵循 fail-fast——
// 文件缺失或解析失败直接返回 error，绝不静默使用零值默认跑起来。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是全局配置的根。各子结构对应总纲 §1 技术栈里的外部依赖。
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	MySQL   MySQLConfig   `yaml:"mysql"`
	Mem0    Mem0Config    `yaml:"mem0"`
	Model   ModelConfig   `yaml:"model"`
	Extract ExtractConfig `yaml:"extract"`
	LarkCLI LarkCLIConfig `yaml:"lark_cli"`
	Capture CaptureConfig `yaml:"capture"`
	Decide  DecideConfig  `yaml:"decide"`
	Codex   CodexConfig   `yaml:"codex"`
	Execute ExecuteConfig `yaml:"execute"`
	Chat    ChatConfig    `yaml:"chat"`
}

// ServerConfig Hertz 监听配置。
type ServerConfig struct {
	Addr    string   `yaml:"addr"`      // 形如 127.0.0.1:18800
	WebRoot string   `yaml:"web_root"`  // React production build directory
	LogFiles []string `yaml:"log_files"` // 运行日志文件（供调试面板尾读并归并）；默认 server 的 stdout+stderr 两个文件。cron 日志走 stderr，必须都读。
}

// MySQLConfig 结构化存储（source of truth）。
type MySQLConfig struct {
	DSN             string `yaml:"dsn"`               // user:pass@tcp(127.0.0.1:3306)/jarvis?charset=utf8mb4&parseTime=true&loc=Local
	MaxOpenConns    int    `yaml:"max_open_conns"`    // 连接池上限
	MaxIdleConns    int    `yaml:"max_idle_conns"`    // 空闲连接
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // 秒
}

// Mem0Config Python sidecar（总纲 §5）。
type Mem0Config struct {
	BaseURL           string `yaml:"base_url"` // http://127.0.0.1:18900
	OwnerID           string `yaml:"owner_id"` // 单用户系统统一 user_id，默认 owner
	TimeoutSec        int    `yaml:"timeout_sec"`
	BatchLimit        int    `yaml:"batch_limit"`
	WindowGapMinutes  int    `yaml:"window_gap_minutes"`
	WindowMaxMessages int    `yaml:"window_max_messages"`
	Schedule          string `yaml:"schedule"`
	QdrantHost        string `yaml:"qdrant_host"`
	QdrantPort        int    `yaml:"qdrant_port"`      // Python client 使用的 HTTP 端口
	QdrantGRPCPort    int    `yaml:"qdrant_grpc_port"` // Go official client 使用的 gRPC 端口
	Collection        string `yaml:"collection"`
	StateDir          string `yaml:"state_dir"`
	EmbeddingModel    string `yaml:"embedding_model"`
	EmbeddingDims     int    `yaml:"embedding_dims"`
}

// ModelConfig 高频抽取用的 OpenAI 兼容端点（M2/M3，总纲 §6）。
type ModelConfig struct {
	BaseURL          string `yaml:"base_url"`
	APIKey           string `yaml:"api_key"` // 本地明文
	Model            string `yaml:"model"`
	IsReasoningModel bool   `yaml:"is_reasoning_model"`
	TimeoutSec       int    `yaml:"timeout_sec"`
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
	CodexSandbox         string `yaml:"codex_sandbox"`
	CodexNetwork         bool   `yaml:"codex_network"`
	CodexReasoningEffort string `yaml:"codex_reasoning_effort"`
	BatchMessages         int     `yaml:"batch_messages"`
	ContextMessages       int     `yaml:"context_messages"`
	ContextWindowMinutes  int     `yaml:"context_window_minutes"`
	OpenTodoLimit         int     `yaml:"open_todo_limit"`
	MemoryTopK            int     `yaml:"memory_top_k"`
	MemoryThreshold       float64 `yaml:"memory_threshold"`
	MaxPromptChars        int     `yaml:"max_prompt_chars"`
	SemanticCollection    string  `yaml:"semantic_collection"`
	SemanticThreshold     float64 `yaml:"semantic_threshold"`
	SemanticNeighborLimit int     `yaml:"semantic_neighbor_limit"`

	// M3 function-calling tool loop. MaxToolRounds hard-caps model tool calls
	// per unit (fail-fast when exceeded); ToolTimeoutSec bounds one tool call;
	// HistoryToolLimit caps rows returned by query_chat_history; ToolMemoryMaxTopK
	// caps top_k the model may request from search_memory.
	MaxToolRounds     int `yaml:"max_tool_rounds"`
	ToolTimeoutSec    int `yaml:"tool_timeout_sec"`
	HistoryToolLimit  int `yaml:"history_tool_limit"`
	ToolMemoryMaxTopK int `yaml:"tool_memory_max_top_k"`
}

// LarkCLIConfig lark-cli 子进程封装（总纲 §4）。
type LarkCLIConfig struct {
	Bin        string  `yaml:"bin"`         // lark-cli 绝对路径
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
}

// DecideConfig controls the M4 MVP gate. The only enabled mode for now is
// manual_mvp: extracted Todos wait for explicit user approval.
type DecideConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Mode       string `yaml:"mode"`
	Schedule   string `yaml:"schedule"`
	BatchLimit int    `yaml:"batch_limit"`

	// CodexSandbox / CodexNetwork / CodexReasoningEffort configure the M4 codex
	// evaluator (mode=codex). It shares the same full-access + network + low
	// reasoning posture as M3 so it can self-query to fill gaps during decision.
	CodexSandbox         string `yaml:"codex_sandbox"`
	CodexNetwork         bool   `yaml:"codex_network"`
	CodexReasoningEffort string `yaml:"codex_reasoning_effort"`
}

// CodexConfig M4 决策用 codex CLI（总纲 §11.2，全部可配置、不硬编码）。
type CodexConfig struct {
	Bin            string `yaml:"bin"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// ExecuteConfig controls M5 agent-driven execution. Enabled turns on the
// auto-execution cron (local actions only); manual execution via the API is
// always available regardless. RepoRoot is the base directory a Task's
// repo_ref slot is joined under for code changes.
type ExecuteConfig struct {
	Enabled       bool   `yaml:"enabled"`        // 是否开自动执行 cron（本地动作）
	Schedule      string `yaml:"schedule"`       // cron 表达式
	BatchLimit    int    `yaml:"batch_limit"`    // 单次 sweep 最多执行的 Task 数
	Concurrency   int    `yaml:"concurrency"`    // 单次 sweep 内并行执行的 Task 数（>=1）
	RepoRoot      string `yaml:"repo_root"`      // code_change repo_ref 的基目录
	RunsDir       string `yaml:"runs_dir"`       // diff/产物落盘目录
	TimeoutSecond int    `yaml:"timeout_second"` // 单次 codex 执行超时
}

// ChatConfig 控制「基于 codex CLI 的流式对话服务」（/api/chat，SSE）。
// Enabled=false 时不注册路由。复用 codex.bin；沙箱固定为对话场景的
// danger-full-access + 联网（本地可信环境），reasoning_effort 可调。
type ChatConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Model           string `yaml:"model"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
	Sandbox         string `yaml:"sandbox"`
	ReasoningEffort string `yaml:"reasoning_effort"`
}

// Load 从指定路径读取并解析 YAML 配置。fail-fast：任何错误直接返回。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

// validate 校验当前已启用模块的全部启动条件。model/codex 会在各自
// 里程碑启用时加入对应校验；mem0 模型参数由独立 sidecar 启动时校验。
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
	if c.MySQL.DSN == "" {
		return fmt.Errorf("mysql.dsn 不能为空")
	}
	if c.MySQL.MaxOpenConns <= 0 {
		return fmt.Errorf("mysql.max_open_conns 必须大于 0")
	}
	if c.MySQL.MaxIdleConns < 0 {
		return fmt.Errorf("mysql.max_idle_conns 不能小于 0")
	}
	if c.MySQL.MaxIdleConns > c.MySQL.MaxOpenConns {
		return fmt.Errorf("mysql.max_idle_conns 不能大于 mysql.max_open_conns")
	}
	if c.MySQL.ConnMaxLifetime <= 0 {
		return fmt.Errorf("mysql.conn_max_lifetime 必须大于 0")
	}
	if c.Mem0.BaseURL == "" {
		return fmt.Errorf("mem0.base_url 不能为空")
	}
	if c.Mem0.OwnerID == "" {
		return fmt.Errorf("mem0.owner_id 不能为空")
	}
	if c.Mem0.TimeoutSec <= 0 {
		return fmt.Errorf("mem0.timeout_sec 必须大于 0")
	}
	if c.Mem0.BatchLimit <= 0 {
		return fmt.Errorf("mem0.batch_limit 必须大于 0")
	}
	if c.Mem0.WindowGapMinutes <= 0 {
		return fmt.Errorf("mem0.window_gap_minutes 必须大于 0")
	}
	if c.Mem0.WindowMaxMessages <= 0 {
		return fmt.Errorf("mem0.window_max_messages 必须大于 0")
	}
	if c.Mem0.Schedule == "" {
		return fmt.Errorf("mem0.schedule 不能为空")
	}
	if c.Mem0.QdrantHost == "" {
		return fmt.Errorf("mem0.qdrant_host 不能为空")
	}
	if c.Mem0.QdrantPort <= 0 || c.Mem0.QdrantPort > 65535 {
		return fmt.Errorf("mem0.qdrant_port 必须在 1 到 65535 之间")
	}
	if c.Mem0.QdrantGRPCPort <= 0 || c.Mem0.QdrantGRPCPort > 65535 {
		return fmt.Errorf("mem0.qdrant_grpc_port 必须在 1 到 65535 之间")
	}
	if c.Mem0.Collection == "" || c.Mem0.StateDir == "" {
		return fmt.Errorf("mem0.collection/state_dir 均不能为空")
	}
	if c.Mem0.EmbeddingModel == "" || c.Mem0.EmbeddingDims <= 0 {
		return fmt.Errorf("mem0.embedding_model 不能为空且 embedding_dims 必须大于 0")
	}
	if c.Extract.Schedule == "" {
		return fmt.Errorf("extract.schedule 不能为空")
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
	if c.Extract.MemoryTopK <= 0 {
		return fmt.Errorf("extract.memory_top_k 必须大于 0")
	}
	if c.Extract.MemoryThreshold < 0 || c.Extract.MemoryThreshold > 1 {
		return fmt.Errorf("extract.memory_threshold 必须在 0 到 1 之间")
	}
	if c.Extract.MaxPromptChars <= 0 {
		return fmt.Errorf("extract.max_prompt_chars 必须大于 0")
	}
	if c.Extract.SemanticCollection == "" {
		return fmt.Errorf("extract.semantic_collection 不能为空")
	}
	if c.Extract.SemanticThreshold <= 0 || c.Extract.SemanticThreshold > 1 {
		return fmt.Errorf("extract.semantic_threshold 必须在 0（不含）到 1 之间")
	}
	if c.Extract.SemanticNeighborLimit <= 0 {
		return fmt.Errorf("extract.semantic_neighbor_limit 必须大于 0")
	}
	if c.Extract.MaxToolRounds <= 0 {
		return fmt.Errorf("extract.max_tool_rounds 必须大于 0")
	}
	if c.Extract.ToolTimeoutSec <= 0 {
		return fmt.Errorf("extract.tool_timeout_sec 必须大于 0")
	}
	if c.Extract.HistoryToolLimit <= 0 {
		return fmt.Errorf("extract.history_tool_limit 必须大于 0")
	}
	if c.Extract.ToolMemoryMaxTopK < c.Extract.MemoryTopK {
		return fmt.Errorf("extract.tool_memory_max_top_k 不能小于 extract.memory_top_k")
	}
	if c.Extract.Engine != "codex" && c.Extract.Engine != "model_api" {
		return fmt.Errorf("extract.engine 必须是 codex 或 model_api")
	}
	if err := validateCodexSandbox("extract", c.Extract.CodexSandbox); err != nil {
		return err
	}
	if err := validateReasoningEffort("extract", c.Extract.CodexReasoningEffort); err != nil {
		return err
	}
	if c.Extract.Enabled {
		if c.Extract.PrincipalOpenID == "" {
			return fmt.Errorf("extract.principal_open_id 不能为空")
		}
		if c.Model.BaseURL == "" || c.Model.APIKey == "" || c.Model.Model == "" {
			return fmt.Errorf("extract 启用时 model.base_url/api_key/model 均不能为空")
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
	if c.Decide.Enabled {
		if c.Decide.Mode != "manual_mvp" && c.Decide.Mode != "codex" {
			return fmt.Errorf("decide.mode 必须是 manual_mvp 或 codex")
		}
		if c.Decide.Schedule == "" {
			return fmt.Errorf("decide.schedule 不能为空")
		}
		if c.Decide.BatchLimit <= 0 {
			return fmt.Errorf("decide.batch_limit 必须大于 0")
		}
		if c.Decide.Mode == "codex" {
			if err := validateCodexSandbox("decide", c.Decide.CodexSandbox); err != nil {
				return err
			}
			if err := validateReasoningEffort("decide", c.Decide.CodexReasoningEffort); err != nil {
				return err
			}
		}
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
	if c.Execute.TimeoutSecond <= 0 {
		return fmt.Errorf("execute.timeout_second 必须大于 0")
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
	if err := validateCodexSandbox("chat", c.Chat.Sandbox); err != nil {
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
	return nil
}

// validateCodexSandbox enforces the codex sandbox mode is one of the values
// codex CLI accepts. danger-full-access is intentionally allowed: it is the
// explicit local-trusted-environment posture per docs/design-context-pipeline.md.
func validateCodexSandbox(section, value string) error {
	switch value {
	case "read-only", "workspace-write", "danger-full-access":
		return nil
	default:
		return fmt.Errorf("%s.codex_sandbox 必须是 read-only / workspace-write / danger-full-access", section)
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
