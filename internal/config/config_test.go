package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Server: ServerConfig{Addr: "127.0.0.1:18800"},
		MySQL: MySQLConfig{
			DSN:             "user:pass@tcp(127.0.0.1:3306)/jarvis",
			MaxOpenConns:    20,
			MaxIdleConns:    5,
			ConnMaxLifetime: 3600,
		},
		Mem0: Mem0Config{
			BaseURL:           "http://127.0.0.1:18900",
			OwnerID:           "owner",
			TimeoutSec:        60,
			BatchLimit:        400,
			WindowGapMinutes:  30,
			WindowMaxMessages: 40,
			Schedule:          "@every 10m",
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
			DiscoverSchedule: "@every 1h",
			HotSchedule:      "@every 5m",
			WarmSchedule:     "@every 30m",
			ColdSchedule:     "@every 6h",
		},
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid"},
		{name: "server address", mutate: func(c *Config) { c.Server.Addr = "" }, wantErr: "server.addr"},
		{name: "mysql dsn", mutate: func(c *Config) { c.MySQL.DSN = "" }, wantErr: "mysql.dsn"},
		{name: "open connections", mutate: func(c *Config) { c.MySQL.MaxOpenConns = 0 }, wantErr: "max_open_conns"},
		{name: "negative idle connections", mutate: func(c *Config) { c.MySQL.MaxIdleConns = -1 }, wantErr: "max_idle_conns"},
		{name: "idle exceeds open", mutate: func(c *Config) { c.MySQL.MaxIdleConns = 21 }, wantErr: "不能大于"},
		{name: "connection lifetime", mutate: func(c *Config) { c.MySQL.ConnMaxLifetime = 0 }, wantErr: "conn_max_lifetime"},
		{name: "mem0 base URL", mutate: func(c *Config) { c.Mem0.BaseURL = "" }, wantErr: "mem0.base_url"},
		{name: "mem0 owner", mutate: func(c *Config) { c.Mem0.OwnerID = "" }, wantErr: "mem0.owner_id"},
		{name: "mem0 timeout", mutate: func(c *Config) { c.Mem0.TimeoutSec = 0 }, wantErr: "mem0.timeout_sec"},
		{name: "mem0 batch", mutate: func(c *Config) { c.Mem0.BatchLimit = 0 }, wantErr: "mem0.batch_limit"},
		{name: "mem0 window gap", mutate: func(c *Config) { c.Mem0.WindowGapMinutes = 0 }, wantErr: "mem0.window_gap_minutes"},
		{name: "mem0 window max", mutate: func(c *Config) { c.Mem0.WindowMaxMessages = 0 }, wantErr: "mem0.window_max_messages"},
		{name: "mem0 schedule", mutate: func(c *Config) { c.Mem0.Schedule = "" }, wantErr: "mem0.schedule"},
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
		{name: "capture schedules", mutate: func(c *Config) { c.Capture.HotSchedule = "" }, wantErr: "schedule"},
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
