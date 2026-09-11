package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/skill"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestSkillContentAPIReadsAndUpdatesRawMarkdown(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "product-prd-review")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nname: product-prd-review\ndescription: '{{AGENT_NAME}} reviews docs'\nmodule: product-management\n---\n\n# {{AGENT_NAME}} instructions\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: product-prd-review\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := skill.NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := skill.NewRenderingService(source, func(value string) string {
		return strings.ReplaceAll(value, "{{AGENT_NAME}}", "小贾")
	})
	if err != nil {
		t.Fatal(err)
	}

	h := server.New()
	h.GET("/api/skills/:skill_name/content", GetSkillContent(service))
	h.GET("/api/skills/:skill_name/source", GetSkillSource(service))
	h.PUT("/api/skills/:skill_name/source", UpdateSkillSource(service))
	rendered := ut.PerformRequest(h.Engine, "GET", "/api/skills/product-prd-review/content", nil).Result()
	if rendered.StatusCode() != consts.StatusOK || !strings.Contains(string(rendered.Body()), "小贾") {
		t.Fatalf("rendered GET status=%d body=%s", rendered.StatusCode(), rendered.Body())
	}
	response := ut.PerformRequest(h.Engine, "GET", "/api/skills/product-prd-review/source", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("GET status=%d body=%s", response.StatusCode(), response.Body())
	}
	var read struct {
		Data skill.ContentView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &read); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.Data.Content, "{{AGENT_NAME}}") || strings.Contains(read.Data.Content, "小贾") {
		t.Fatalf("GET returned rendered content: %q", read.Data.Content)
	}

	updatedText := strings.Replace(original, "instructions", "updated instructions", 1)
	body, err := json.Marshal(skill.ContentInput{Content: updatedText, ExpectedRevision: read.Data.Revision})
	if err != nil {
		t.Fatal(err)
	}
	updated := ut.PerformRequest(h.Engine, "PUT", "/api/skills/product-prd-review/source", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if updated.StatusCode() != consts.StatusOK {
		t.Fatalf("PUT status=%d body=%s", updated.StatusCode(), updated.Body())
	}
	stale := ut.PerformRequest(h.Engine, "PUT", "/api/skills/product-prd-review/source", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if stale.StatusCode() != consts.StatusConflict {
		t.Fatalf("stale PUT status=%d body=%s", stale.StatusCode(), stale.Body())
	}
}
