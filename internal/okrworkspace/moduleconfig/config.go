// Package moduleconfig owns every machine-enforced setting of the optional
// OKR module. Jarvis's global config deliberately does not know these fields.
package moduleconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/fileconfig"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DatabasePath  string              `yaml:"database_path"`
	UploadDir     string              `yaml:"upload_dir"`
	MaxImageBytes int64               `yaml:"max_image_bytes"`
	Identity      IdentityConfig      `yaml:"identity"`
	PreviewReview PreviewReviewConfig `yaml:"preview_review"`
	Chat          ChatConfig          `yaml:"chat"`
}

// ChatConfig belongs only to the isolated OKR conversation surface.
type ChatConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Image           string   `yaml:"image"`
	Model           string   `yaml:"model"`
	ReasoningEffort string   `yaml:"reasoning_effort"`
	TimeoutSeconds  int      `yaml:"timeout_seconds"`
	AuthFile        string   `yaml:"auth_file"`
	ModelHosts      []string `yaml:"model_hosts"`
}

func (c ChatConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Image == "" || c.Model == "" || c.ReasoningEffort == "" || c.TimeoutSeconds <= 0 || c.AuthFile == "" || len(c.ModelHosts) == 0 {
		return fmt.Errorf("enabled OKR chat requires image, model, reasoning_effort, timeout_seconds, auth_file and model_hosts")
	}
	return nil
}

// PreviewReviewConfig drives the one-shot OKR Preview review agent. The review
// is advisory and read-only, but it reaches the module's own data through
// okr-module-tools / biz-okr-tools, which call the local API — hence a
// sandbox that permits network access rather than read-only.
type PreviewReviewConfig struct {
	Bin             string `yaml:"bin"`
	Model           string `yaml:"model"`
	ReasoningEffort string `yaml:"reasoning_effort"`
	Sandbox         string `yaml:"sandbox"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
}

func (c PreviewReviewConfig) Timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}

type IdentityConfig struct {
	Enabled          bool   `yaml:"enabled"`
	AppID            string `yaml:"app_id"`
	AppSecretEnv     string `yaml:"app_secret_env"`
	SessionTTLHours  int    `yaml:"session_ttl_hours"`
	CookieSecure     bool   `yaml:"cookie_secure"`
	FeishuBaseURL    string `yaml:"feishu_base_url"`
	FeishuAccountURL string `yaml:"feishu_account_url"`
	// TokenDir holds one JSON file per signed-in open_id with that person's
	// Feishu access and refresh tokens.
	TokenDir string `yaml:"token_dir"`
	// ScopesFile lists the user scopes the device login asks for, resolved
	// relative to this config file.
	ScopesFile string `yaml:"scopes_file"`
	// Scopes is read from ScopesFile while loading, never from YAML.
	Scopes []string `yaml:"-"`
}

func (c PreviewReviewConfig) validate() error {
	for name, value := range map[string]string{
		"preview_review.bin":              c.Bin,
		"preview_review.model":            c.Model,
		"preview_review.reasoning_effort": c.ReasoningEffort,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	switch c.Sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return fmt.Errorf("preview_review.sandbox must be read-only, workspace-write or danger-full-access, got %q", c.Sandbox)
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("preview_review.timeout_seconds must be positive")
	}
	return nil
}

// ScopeParam renders the scopes the way Feishu's OAuth endpoints expect them.
func (c IdentityConfig) ScopeParam() string { return strings.Join(c.Scopes, " ") }

func (c IdentityConfig) AppSecret() string {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(c.AppSecretEnv)))
}

// LoadCore reads only the reusable OKR storage settings. Biz-only identity
// and review settings remain inert when the biz-okr module is disabled.
func LoadCore(path string) (Config, error) {
	return load(path, false)
}

// Load reads and validates the complete OKR plus Biz OKR configuration.
func Load(path string) (Config, error) {
	return load(path, true)
}

func load(path string, includeBiz bool) (Config, error) {
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
	if err := cfg.validateCore(); err != nil {
		return Config{}, fmt.Errorf("validate OKR module config %s: %w", path, err)
	}
	if !includeBiz {
		return cfg, nil
	}
	if err := cfg.validateBiz(); err != nil {
		return Config{}, fmt.Errorf("validate Biz OKR module config %s: %w", path, err)
	}
	if cfg.Identity.Enabled {
		scopesPath := filepath.Join(filepath.Dir(path), cfg.Identity.ScopesFile)
		scopes, err := loadScopes(scopesPath)
		if err != nil {
			return Config{}, err
		}
		cfg.Identity.Scopes = scopes
	}
	return cfg, nil
}

// loadScopes reads the scope list, one per line, ignoring blanks and `#`
// comments. fail-fast: a missing or effectively empty file is an error, because
// logging in with no scopes yields a token that cannot call anything.
func loadScopes(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OKR identity scopes %s: %w", path, err)
	}
	var scopes []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		scope := strings.TrimSpace(line)
		if scope == "" || strings.HasPrefix(scope, "#") {
			continue
		}
		if strings.ContainsAny(scope, " \t") {
			return nil, fmt.Errorf("OKR identity scopes %s: %q must be one scope per line", path, scope)
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("OKR identity scopes %s has no scope", path)
	}
	return scopes, nil
}

func (c Config) Validate() error {
	if err := c.validateCore(); err != nil {
		return err
	}
	return c.validateBiz()
}

func (c Config) validateCore() error {
	if strings.TrimSpace(c.DatabasePath) == "" {
		return fmt.Errorf("database_path is required")
	}
	if strings.TrimSpace(c.UploadDir) == "" {
		return fmt.Errorf("upload_dir is required")
	}
	if c.MaxImageBytes <= 0 {
		return fmt.Errorf("max_image_bytes must be positive")
	}
	return nil
}

func (c Config) validateBiz() error {
	if err := c.Chat.Validate(); err != nil {
		return err
	}
	if c.Chat.Enabled && !c.Identity.Enabled {
		return fmt.Errorf("enabled OKR chat requires Biz OKR visitor identity")
	}
	if err := c.PreviewReview.validate(); err != nil {
		return err
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
	if strings.TrimSpace(c.Identity.TokenDir) == "" {
		return fmt.Errorf("identity.token_dir is required when enabled")
	}
	if strings.TrimSpace(c.Identity.ScopesFile) == "" {
		return fmt.Errorf("identity.scopes_file is required when enabled")
	}
	return nil
}
