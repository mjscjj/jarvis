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
