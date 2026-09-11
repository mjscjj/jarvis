package workrule

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceRendersOnlyCurrentStageRules(t *testing.T) {
	service := newTestService(t)
	block, err := service.Block(t.Context(), StageExecute)
	if err != nil {
		t.Fatalf("Block() error = %v", err)
	}
	for _, want := range []string{"BEGIN_WORK_RULES", "execute rule", "当前阶段：execute"} {
		if !strings.Contains(block, want) {
			t.Fatalf("Block() missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "decide rule") {
		t.Fatalf("Block() contains another stage:\n%s", block)
	}
	for _, stage := range []string{"all", "proactive", "unknown"} {
		if _, err := service.Block(t.Context(), stage); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Block(%q) error = %v, want ErrInvalidInput", stage, err)
		}
	}
}

func TestServiceUpdatesOnlyAllowlistedFile(t *testing.T) {
	service := newTestService(t)
	updated, err := service.Update(t.Context(), StageExecute, Input{Content: "new execute rule"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Content != "new execute rule" {
		t.Fatalf("updated content = %q", updated.Content)
	}
	if _, err := service.Update(t.Context(), "../secret", Input{Content: "x"}); err == nil {
		t.Fatal("unknown key must fail")
	}
	if _, err := service.Update(t.Context(), "all", Input{Content: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy all-stage key error = %v, want ErrNotFound", err)
	}
}

func TestServiceRejectsEmptyRuleFile(t *testing.T) {
	service := newTestService(t)
	if _, err := service.Update(t.Context(), StageExtract, Input{Content: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty update error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(service.directory, "m3.md"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Block(t.Context(), StageExtract); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty rules error = %v", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	contents := map[string]string{
		"m3.md": "extract rule", "m5.md": "execute rule",
	}
	for name, content := range contents {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(directory)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}
