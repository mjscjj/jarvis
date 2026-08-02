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
		if item.Content != "initial "+item.Key {
			t.Errorf("List()[%d].Content = %q", i, item.Content)
		}
	}
}

func TestServiceUpdateAtomicallyReplacesContent(t *testing.T) {
	service := newTestService(t)
	updated, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: " 第一行\n第二行 "})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Content != "第一行\n第二行" {
		t.Fatalf("Update().Content = %q", updated.Content)
	}
	onDisk, err := os.ReadFile(updated.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(onDisk) != "第一行\n第二行\n" {
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
	for _, key := range []string{SystemPromptM3Key, SystemPromptM5Key} {
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
		if err := os.WriteFile(path, []byte("initial "+item.key+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}
