package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

// 升级后 webview 若继续用缓存里的旧 index.html，会去加载已被清理的旧 chunk 而
// 白屏。入口文档必须明确 no-store，带哈希的静态资源不受影响。
func TestWebEntryNoStoreAppliesToEntryDocumentOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "index-abc123.js"), []byte("export {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := server.New()
	h.Use(WebEntryNoStore())
	h.StaticFS("/", &app.FS{Root: root, IndexNames: []string{"index.html"}})

	for _, testCase := range []struct {
		path string
		want string
	}{
		{"/", "no-store"},
		{"/index.html", "no-store"},
		{"/assets/index-abc123.js", ""},
	} {
		response := ut.PerformRequest(h.Engine, "GET", testCase.path, nil).Result()
		if response.StatusCode() != 200 {
			t.Fatalf("GET %s status = %d", testCase.path, response.StatusCode())
		}
		if got := string(response.Header.Peek("Cache-Control")); got != testCase.want {
			t.Fatalf("GET %s Cache-Control = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

func TestWebEntryNoStoreSkipsAPIAndUpdateFiles(t *testing.T) {
	for _, path := range []string{"/api/resources", UpdateFilePrefix + "/latest.json"} {
		if isWebEntryDocument(path) {
			t.Fatalf("%s 不该被当作前端入口文档", path)
		}
	}
}
