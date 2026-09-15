package textstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceListsAndReadsEveryDefinition(t *testing.T) {
	service := newTestService(t)
	items, err := service.List(t.Context())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != len(definitions()) {
		t.Fatalf("List() length = %d, want %d", len(items), len(definitions()))
	}
	for i, item := range items {
		if item.Key != definitions()[i].key {
			t.Errorf("List()[%d].Key = %q, want %q", i, item.Key, definitions()[i].key)
		}
		if item.Content != testContent(definitions()[i]) {
			t.Errorf("List()[%d].Content = %q", i, item.Content)
		}
		if item.Kind == "" || item.Stage == "" {
			t.Errorf("List()[%d] missing presentation metadata: %+v", i, item)
		}
	}
}

// The admin editor renders List verbatim, so a file registered without a name
// or description would reach the UI as a blank tab.
func TestEveryDefinitionCarriesEditorLabels(t *testing.T) {
	for _, item := range definitions() {
		if strings.TrimSpace(item.name) == "" || strings.TrimSpace(item.description) == "" {
			t.Errorf("definition %q needs both a name and a description, got %+v", item.key, item)
		}
	}
}

func TestServiceUpdateAtomicallyReplacesContent(t *testing.T) {
	service := newTestService(t)
	updated, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: " 第一行\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}\n第二行 "})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Content != "第一行\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}\n第二行" {
		t.Fatalf("Update().Content = %q", updated.Content)
	}
	onDisk, err := os.ReadFile(updated.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(onDisk) != "第一行\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}\n第二行\n" {
		t.Fatalf("on-disk content = %q", onDisk)
	}
}

func TestServiceRejectsUnknownKeyAndEmptyContent(t *testing.T) {
	service := newTestService(t)
	if _, err := service.Get(t.Context(), "../secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update(empty) error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceRejectsInvalidSystemPromptTemplateWithoutReplacingFile(t *testing.T) {
	service := newTestService(t)
	before, err := service.Content(t.Context(), SystemPromptM5Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: "missing placeholders"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update(invalid template) error = %v, want ErrInvalidInput", err)
	}
	after, err := service.Content(t.Context(), SystemPromptM5Key)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("invalid update replaced file: before=%q after=%q", before, after)
	}
}

func TestNewServiceFailsWhenRequiredFileIsMissingOrEmpty(t *testing.T) {
	directory := t.TempDir()
	writeDefinitions(t, directory)
	if err := os.Remove(filepath.Join(directory, definitions()[0].filename)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(directory); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NewService(missing) error = %v, want ErrNotFound", err)
	}

	writeDefinitions(t, directory)
	if err := os.WriteFile(filepath.Join(directory, definitions()[1].filename), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(directory); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewService(empty) error = %v, want ErrInvalidInput", err)
	}
}

func TestRepositoryPromptsDoNotEmbedToolManuals(t *testing.T) {
	service, err := NewService(filepath.Join("..", "..", "conf", "prompts"))
	if err != nil {
		t.Fatalf("NewService(repository prompts) error = %v", err)
	}
	for _, key := range []string{SystemPromptM3Key, SystemPromptM5Key, SystemPromptProactiveKey} {
		content, err := service.Content(t.Context(), key)
		if err != nil {
			t.Fatalf("Content(%q) error = %v", key, err)
		}
		for _, toolName := range []string{"jarvis-tools", "lark-cli", "bytedcli"} {
			if strings.Contains(content, toolName) {
				t.Fatalf("%s must not embed tool manual %q", key, toolName)
			}
		}
	}
}

func TestRepositoryM5PromptKeepsTemporaryArtifactsOutOfWorkspaces(t *testing.T) {
	service, err := NewService(filepath.Join("..", "..", "conf", "prompts"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := service.Content(t.Context(), SystemPromptM5Key)
	if err != nil {
		t.Fatal(err)
	}
	const required = "不需要持久化的临时文件，建议放在 `tmp/` 目录下。"
	if !strings.Contains(content, required) {
		t.Fatalf("M5 prompt missing temporary artifact rule %q", required)
	}
}

func TestRepositoryCCPromptOwnsTheForegroundHandoffContract(t *testing.T) {
	service, err := NewService(filepath.Join("..", "..", "conf", "prompts"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := service.Content(t.Context(), SystemPromptCCKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), SystemPromptCCKey, Input{Content: content}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CC installation template must not pretend to support live edits: %v", err)
	}
	for _, required := range []string{
		"{{REPO_ROOT}}", "get-context --chat-id", "get-shared-memory",
		"create-task", "source_type=manual", "delivery_required", "reply_target",
		"supplement-task", "resume-task",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("CC prompt missing handoff contract %q", required)
		}
	}
	if strings.Contains(content, "m5-system-prompt.md") {
		t.Fatal("CC prompt must not depend on the M5 prompt file")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	writeDefinitions(t, directory)
	service, err := NewService(directory)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func writeDefinitions(t *testing.T, directory string) {
	t.Helper()
	for _, item := range definitions() {
		path := filepath.Join(directory, item.filename)
		if err := os.WriteFile(path, []byte(testContent(item)+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func testContent(item definition) string {
	switch item.key {
	case InitiativeLevelKey:
		return "normal"
	case SystemPromptM3Key:
		return "initial " + item.key + "\n{{WORK_RULES}}"
	case SystemPromptM5Key:
		return "initial " + item.key + "\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}"
	default:
		return "initial " + item.key
	}
}

func TestInitiativeReadWriteAndInvalidFiles(t *testing.T) {
	s := newTestService(t)
	var path string
	for _, level := range []string{"quiet", "active", "normal"} {
		updated, err := s.Update(t.Context(), InitiativeLevelKey, Input{Content: level})
		if err != nil {
			t.Fatal(err)
		}
		path = updated.Path
		if got, err := s.Content(t.Context(), InitiativeLevelKey); err != nil || got != level {
			t.Fatalf("read after save = %q, %v", got, err)
		}
	}
	for _, invalid := range []string{"", "unknown", "normal\nactive"} {
		if _, err := s.Update(t.Context(), InitiativeLevelKey, Input{Content: invalid}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid update %q: %v", invalid, err)
		}
		if got, _ := s.Content(t.Context(), InitiativeLevelKey); got != "normal" {
			t.Fatalf("invalid update replaced level with %q", got)
		}
	}
	for _, invalid := range []string{"", "typo"} {
		if err := os.WriteFile(path, []byte(invalid), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content(t.Context(), InitiativeLevelKey); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid local file %q: %v", invalid, err)
		}
		if _, err := NewService(filepath.Dir(path)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("startup accepted invalid local file: %v", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Content(t.Context(), InitiativeLevelKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing level: %v", err)
	}
}
