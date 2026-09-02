package moduleconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOwnsStrictOKRModuleConfig(t *testing.T) {
	t.Setenv("TEST_OKR_SECRET", "test-secret")
	path := filepath.Join(t.TempDir(), "okr.yaml")
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
	if err := os.WriteFile(path, []byte(raw+"unknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("Load() unknown field error = %v", err)
	}
}
