package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestUpdateFileHandlerServesManifestAndArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "latest.json"), []byte(`{"version":"0.1.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Jarvis_0.1.1_aarch64.app.tar.gz"), []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler, err := NewUpdateFileHandler(root)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.GET("/jarvis-updates/:filename", handler)
	h.HEAD("/jarvis-updates/:filename", handler)

	manifest := ut.PerformRequest(h.Engine, "GET", "/jarvis-updates/latest.json", nil).Result()
	if manifest.StatusCode() != consts.StatusOK {
		t.Fatalf("manifest status = %d body=%s", manifest.StatusCode(), manifest.Body())
	}
	if got := string(manifest.Header.Peek("Cache-Control")); got != "no-cache" {
		t.Fatalf("manifest Cache-Control = %q", got)
	}
	if got := string(manifest.Body()); got != `{"version":"0.1.1"}` {
		t.Fatalf("manifest body = %q", got)
	}

	download := ut.PerformRequest(h.Engine, "GET", "/jarvis-updates/Jarvis_0.1.1_aarch64.app.tar.gz", nil).Result()
	if download.StatusCode() != consts.StatusOK || string(download.Body()) != "artifact" {
		t.Fatalf("artifact GET status=%d body=%q", download.StatusCode(), download.Body())
	}
	missing := ut.PerformRequest(h.Engine, "GET", "/jarvis-updates/missing.tar.gz", nil).Result()
	if missing.StatusCode() != consts.StatusNotFound {
		t.Fatalf("missing artifact status = %d", missing.StatusCode())
	}

	artifact := ut.PerformRequest(h.Engine, "HEAD", "/jarvis-updates/Jarvis_0.1.1_aarch64.app.tar.gz", nil).Result()
	if artifact.StatusCode() != consts.StatusOK {
		t.Fatalf("artifact status = %d body=%s", artifact.StatusCode(), artifact.Body())
	}
	if got := string(artifact.Header.Peek("Cache-Control")); got != "public, max-age=31536000, immutable" {
		t.Fatalf("artifact Cache-Control = %q", got)
	}
	if len(artifact.Body()) != 0 {
		t.Fatalf("HEAD artifact body length = %d", len(artifact.Body()))
	}
}

// The gzip middleware reads the whole response body before compressing, so an
// update artifact must never reach it: a few hundred MB would be buffered per
// request. Regular API responses must still be compressed.
func TestCompressionExcludesUpdateFilesButNotAPI(t *testing.T) {
	root := t.TempDir()
	name := "Jarvis_0.1.1_aarch64.app.tar.gz"
	artifact := strings.Repeat("payload", 4096)
	if err := os.WriteFile(filepath.Join(root, name), []byte(artifact), 0o644); err != nil {
		t.Fatal(err)
	}
	handler, err := NewUpdateFileHandler(root)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.Use(Compression())
	h.GET(UpdateFilePrefix+"/:filename", handler)
	h.GET("/api/compressible", func(_ context.Context, c *app.RequestContext) {
		c.String(consts.StatusOK, artifact)
	})

	header := ut.Header{Key: "Accept-Encoding", Value: "gzip"}
	update := ut.PerformRequest(h.Engine, "GET", UpdateFilePrefix+"/"+name, nil, header).Result()
	if got := string(update.Header.Peek("Content-Encoding")); got != "" {
		t.Fatalf("update artifact Content-Encoding = %q, want uncompressed stream", got)
	}
	if string(update.Body()) != artifact {
		t.Fatalf("update artifact body length = %d, want %d", len(update.Body()), len(artifact))
	}

	api := ut.PerformRequest(h.Engine, "GET", "/api/compressible", nil, header).Result()
	if got := string(api.Header.Peek("Content-Encoding")); got != "gzip" {
		t.Fatalf("API Content-Encoding = %q, want gzip", got)
	}
}

func TestUpdateFileHandlerRejectsInvalidPathsAndRoots(t *testing.T) {
	if _, err := NewUpdateFileHandler(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing update root was accepted")
	}

	handler, err := NewUpdateFileHandler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.GET("/jarvis-updates/:filename", handler)
	response := ut.PerformRequest(h.Engine, "GET", "/jarvis-updates/%2e%2e", nil).Result()
	if response.StatusCode() != consts.StatusNotFound {
		t.Fatalf("invalid path status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestUpdateFileHandlerRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(private, []byte("must not serve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, filepath.Join(root, "artifact.tar.gz")); err != nil {
		t.Fatal(err)
	}
	handler, err := NewUpdateFileHandler(root)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.GET(UpdateFilePrefix+"/:filename", handler)
	response := ut.PerformRequest(h.Engine, "GET", UpdateFilePrefix+"/artifact.tar.gz", nil).Result()
	if response.StatusCode() != consts.StatusNotFound {
		t.Fatalf("symlink status = %d", response.StatusCode())
	}
}
