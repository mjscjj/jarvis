package moduleconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadOwnsStrictOKRModuleConfig(t *testing.T) {
	t.Setenv("TEST_OKR_SECRET", "test-secret")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scopes.txt"), []byte("# comment\n\noffline_access\ndocx:document:readonly\noffline_access\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "okr.yaml")
	raw := `database_path: data/okr/okr.db
upload_dir: data/okr/assets
max_image_bytes: 1024
preview_review:
  bin: traex
  model: DeepSeek-V4-Pro
  reasoning_effort: high
  sandbox: workspace-write
  timeout_seconds: 300
identity:
  enabled: true
  app_id: cli_test
  app_secret_env: TEST_OKR_SECRET
  session_ttl_hours: 24
  feishu_base_url: https://open.feishu.cn
  feishu_account_url: https://accounts.feishu.cn
  token_dir: data/okr/feishu-tokens
  scopes_file: scopes.txt
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabasePath != "data/okr/okr.db" || cfg.UploadDir != "data/okr/assets" || cfg.MaxImageBytes != 1024 {
		t.Fatalf("Load() = %+v", cfg)
	}
	if cfg.Identity.TokenDir != "data/okr/feishu-tokens" {
		t.Fatalf("Load() identity = %+v", cfg.Identity)
	}
	// Comments and blank lines are dropped, duplicates collapse, and the
	// scopes reach Feishu space-separated.
	if got := cfg.Identity.ScopeParam(); got != "offline_access docx:document:readonly" {
		t.Fatalf("ScopeParam() = %q", got)
	}
	if err := os.WriteFile(path, []byte(raw+"unknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("Load() unknown field error = %v", err)
	}
	if cfg.PreviewReview.Model != "DeepSeek-V4-Pro" || cfg.PreviewReview.Timeout() != 300*time.Second {
		t.Fatalf("Load() preview_review = %+v", cfg.PreviewReview)
	}
}

// The review agent runs a real CLI with a real sandbox, so a config that cannot
// describe one must not start the server.
func TestPreviewReviewConfigRejectsUnusableAgentSettings(t *testing.T) {
	valid := PreviewReviewConfig{
		Bin: "traex", Model: "DeepSeek-V4-Pro", ReasoningEffort: "high",
		Sandbox: "workspace-write", TimeoutSeconds: 300,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	for name, mutate := range map[string]func(*PreviewReviewConfig){
		"preview_review.bin is required":              func(c *PreviewReviewConfig) { c.Bin = " " },
		"preview_review.model is required":            func(c *PreviewReviewConfig) { c.Model = "" },
		"preview_review.reasoning_effort is required": func(c *PreviewReviewConfig) { c.ReasoningEffort = "" },
		"preview_review.sandbox must be":              func(c *PreviewReviewConfig) { c.Sandbox = "sandboxed" },
		"preview_review.timeout_seconds must be":      func(c *PreviewReviewConfig) { c.TimeoutSeconds = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			broken := valid
			mutate(&broken)
			err := broken.validate()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("validate() error = %v, want containing %q", err, name)
			}
		})
	}
}

func TestLoadFailsFastOnUnusableScopesFile(t *testing.T) {
	t.Setenv("TEST_OKR_SECRET", "test-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "okr.yaml")
	raw := `database_path: data/okr/okr.db
upload_dir: data/okr/assets
max_image_bytes: 1024
preview_review:
  bin: traex
  model: DeepSeek-V4-Pro
  reasoning_effort: high
  sandbox: workspace-write
  timeout_seconds: 300
identity:
  enabled: true
  app_id: cli_test
  app_secret_env: TEST_OKR_SECRET
  session_ttl_hours: 24
  feishu_base_url: https://open.feishu.cn
  feishu_account_url: https://accounts.feishu.cn
  token_dir: data/okr/feishu-tokens
  scopes_file: scopes.txt
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "read OKR identity scopes") {
		t.Fatalf("Load() missing scopes file error = %v", err)
	}
	scopesPath := filepath.Join(dir, "scopes.txt")
	if err := os.WriteFile(scopesPath, []byte("# only comments\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "has no scope") {
		t.Fatalf("Load() empty scopes error = %v", err)
	}
	if err := os.WriteFile(scopesPath, []byte("offline_access docx:document:readonly\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "one scope per line") {
		t.Fatalf("Load() space-separated scopes error = %v", err)
	}
}
