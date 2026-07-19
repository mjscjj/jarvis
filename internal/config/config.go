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
	LarkCLI LarkCLIConfig `yaml:"lark_cli"`
	Capture CaptureConfig `yaml:"capture"`
	Codex   CodexConfig   `yaml:"codex"`
}

// ServerConfig Hertz 监听配置。
type ServerConfig struct {
	Addr string `yaml:"addr"` // 形如 127.0.0.1:18800
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
	BaseURL string `yaml:"base_url"` // http://127.0.0.1:18900
	OwnerID string `yaml:"owner_id"` // 单用户系统统一 user_id，默认 owner
}

// ModelConfig 高频抽取用的 OpenAI 兼容端点（M2/M3，总纲 §6）。
type ModelConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"` // 本地明文
	Model   string `yaml:"model"`
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
type CaptureConfig struct {
	PageSize         int    `yaml:"page_size"`
	ScanWorkers      int    `yaml:"scan_workers"`
	HotAgeHours      int    `yaml:"hot_age_hours"`
	WarmAgeHours     int    `yaml:"warm_age_hours"`
	Timezone         string `yaml:"timezone"`
	DiscoverSchedule string `yaml:"discover_schedule"`
	HotSchedule      string `yaml:"hot_schedule"`
	WarmSchedule     string `yaml:"warm_schedule"`
	ColdSchedule     string `yaml:"cold_schedule"`
}

// CodexConfig M4 决策用 codex CLI（总纲 §11.2，全部可配置、不硬编码）。
type CodexConfig struct {
	Bin              string        `yaml:"bin"`
	Model            string        `yaml:"model"`
	TimeoutSeconds   int           `yaml:"timeout_seconds"`
	MaxCallsPerHour  int           `yaml:"max_calls_per_hour"`
	MaxCallsPerDay   int           `yaml:"max_calls_per_day"`
	OnBudgetExceeded string        `yaml:"on_budget_exceeded"` // route_need_decision | degrade_to_rule
	OnTimeout        string        `yaml:"on_timeout"`         // 固定 route_need_decision(fail-safe)
	GrayZone         GrayZoneRange `yaml:"gray_zone"`
}

// GrayZoneRange 灰区边界：落此区间的 Todo 才触发 codex 深判（总纲 §11.2）。
type GrayZoneRange struct {
	ConfLow  float64 `yaml:"conf_low"`
	ConfHigh float64 `yaml:"conf_high"`
	RiskLow  float64 `yaml:"risk_low"`
	RiskHigh float64 `yaml:"risk_high"`
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

// validate 校验当前已启用模块的全部启动条件。尚未接入的 mem0/model/
// lark-cli/codex 会在各自里程碑启用时加入对应校验。
func (c *Config) validate() error {
	if c.Server.Addr == "" {
		return fmt.Errorf("server.addr 不能为空")
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
	if c.Capture.DiscoverSchedule == "" || c.Capture.HotSchedule == "" || c.Capture.WarmSchedule == "" || c.Capture.ColdSchedule == "" {
		return fmt.Errorf("capture 的 discover/hot/warm/cold schedule 均不能为空")
	}
	return nil
}
