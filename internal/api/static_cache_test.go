package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestStaticAssetCacheHeadersSeparatesDocumentsAndHashedAssets(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app-hash.js"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := server.New()
	h.Use(StaticAssetCacheHeaders())
	h.GET("/api/ping", func(_ context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, "pong")
	})
	h.StaticFS("/", &app.FS{Root: root, IndexNames: []string{"index.html"}})

	asset := ut.PerformRequest(h.Engine, "GET", "/assets/app-hash.js", nil).Result()
	if got := string(asset.Header.Peek("Cache-Control")); got != immutableAssetCacheControl {
		t.Fatalf("asset Cache-Control = %q, want %q", got, immutableAssetCacheControl)
	}
	index := ut.PerformRequest(h.Engine, "GET", "/", nil).Result()
	if got := string(index.Header.Peek("Cache-Control")); got != webDocumentCacheControl {
		t.Fatalf("index Cache-Control = %q, want %q", got, webDocumentCacheControl)
	}
	indexFile := ut.PerformRequest(h.Engine, "GET", "/index.html", nil).Result()
	if got := string(indexFile.Header.Peek("Cache-Control")); got != webDocumentCacheControl {
		t.Fatalf("index.html Cache-Control = %q, want %q", got, webDocumentCacheControl)
	}
	api := ut.PerformRequest(h.Engine, "GET", "/api/ping", nil).Result()
	if got := string(api.Header.Peek("Cache-Control")); got != "" {
		t.Fatalf("API Cache-Control = %q, want empty", got)
	}
}
