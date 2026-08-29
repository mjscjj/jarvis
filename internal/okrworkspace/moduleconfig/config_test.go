package moduleconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOwnsStrictOKRModuleConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "okr.yaml")
	raw := `database_path: data/okr/okr.db
upload_dir: data/okr/assets
max_image_bytes: 1024
identity:
  enabled: false
  app_secret_env: TEST_OKR_SECRET
  session_ttl_hours: 24
  feishu_base_url: https://open.feishu.cn
  feishu_account_url: https://accounts.feishu.cn
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
	if err := os.WriteFile(path, []byte(raw+"unknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("Load() unknown field error = %v", err)
	}
}
