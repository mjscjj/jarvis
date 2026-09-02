package moduleconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
}

func TestLoadFailsFastOnUnusableScopesFile(t *testing.T) {
	t.Setenv("TEST_OKR_SECRET", "test-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "okr.yaml")
	raw := `database_path: data/okr/okr.db
upload_dir: data/okr/assets
max_image_bytes: 1024
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
