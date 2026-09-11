package api

import (
	"os"
	"path/filepath"
	"testing"

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
