// Package moduleconfig owns every machine-enforced setting of the optional
// OKR module. Jarvis's global config deliberately does not know these fields.
package moduleconfig

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"jarvis/internal/fileconfig"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DatabasePath  string         `yaml:"database_path"`
	UploadDir     string         `yaml:"upload_dir"`
	MaxImageBytes int64          `yaml:"max_image_bytes"`
	Identity      IdentityConfig `yaml:"identity"`
}

type IdentityConfig struct {
	Enabled          bool   `yaml:"enabled"`
	AppID            string `yaml:"app_id"`
	AppSecretEnv     string `yaml:"app_secret_env"`
	SessionTTLHours  int    `yaml:"session_ttl_hours"`
	CookieSecure     bool   `yaml:"cookie_secure"`
	FeishuBaseURL    string `yaml:"feishu_base_url"`
	FeishuAccountURL string `yaml:"feishu_account_url"`
}

func (c IdentityConfig) AppSecret() string {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(c.AppSecretEnv)))
}

func Load(path string) (Config, error) {
	raw, err := fileconfig.Read(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode OKR module config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate OKR module config %s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabasePath) == "" {
		return fmt.Errorf("database_path is required")
	}
	if strings.TrimSpace(c.UploadDir) == "" {
		return fmt.Errorf("upload_dir is required")
	}
	if c.MaxImageBytes <= 0 {
		return fmt.Errorf("max_image_bytes must be positive")
	}
	if !c.Identity.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Identity.AppID) == "" || strings.TrimSpace(c.Identity.AppSecretEnv) == "" {
		return fmt.Errorf("identity app_id and app_secret_env are required when enabled")
	}
	if c.Identity.AppSecret() == "" {
		return fmt.Errorf("identity secret environment variable %s is empty", c.Identity.AppSecretEnv)
	}
	if c.Identity.SessionTTLHours <= 0 {
		return fmt.Errorf("identity.session_ttl_hours must be positive")
	}
	if strings.TrimSpace(c.Identity.FeishuBaseURL) == "" || strings.TrimSpace(c.Identity.FeishuAccountURL) == "" {
		return fmt.Errorf("identity Feishu URLs are required when enabled")
	}
	return nil
}
