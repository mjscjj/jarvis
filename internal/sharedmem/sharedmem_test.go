package sharedmem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderBlockEmpty(t *testing.T) {
	for _, input := range []string{"", "   ", "\n\t "} {
		if got := RenderBlock(input); got != "" {
			t.Fatalf("RenderBlock(%q) = %q, want empty", input, got)
		}
	}
}

func TestRenderBlockNonEmpty(t *testing.T) {
	block := RenderBlock("  线上库密码是 hunter2\n别直连生产  ")
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "END_SHARED_MEMORY", "可信", "线上库密码是 hunter2", "别直连生产"} {
		if !strings.Contains(block, want) {
			t.Fatalf("RenderBlock() missing %q:\n%s", want, block)
		}
	}
}

func TestSharedMemoryFileReadWriteAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-memory.md")
	if err := os.WriteFile(path, []byte("第一条\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewSharedMemoryService(path)
	if err != nil {
		t.Fatalf("NewSharedMemoryService() error = %v", err)
	}
	view, err := service.Get(t.Context())
	if err != nil || view.Content != "第一条" || view.Path != path {
		t.Fatalf("Get() = %#v err=%v", view, err)
	}
	if _, err := service.Upsert(t.Context(), "第二版"); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	view, err = service.Append(t.Context(), " 第三条 ")
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if view.Content != "第二版\n第三条" {
		t.Fatalf("Append() content = %q", view.Content)
	}
	if _, err := service.Append(t.Context(), " "); err == nil {
		t.Fatal("blank append must fail")
	}
}

func TestNewSharedMemoryServiceFailsForMissingFile(t *testing.T) {
	if _, err := NewSharedMemoryService(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("missing shared memory file must fail")
	}
}

func TestAppendNote(t *testing.T) {
	if got := appendNote("已有记忆\n", "  新一条  "); got != "已有记忆\n新一条" {
		t.Fatalf("appendNote() = %q", got)
	}
}

func TestSharedMemoryRejectsContentOver2000Characters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-memory.md")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewSharedMemoryService(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Upsert(t.Context(), strings.Repeat("记", MaxCharacters)); err != nil {
		t.Fatalf("Upsert() rejected exact limit: %v", err)
	}
	if _, err := service.Upsert(t.Context(), strings.Repeat("记", MaxCharacters+1)); !errors.Is(err, ErrContentTooLong) {
		t.Fatalf("Upsert() error = %v, want ErrContentTooLong", err)
	}
	view, err := service.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.Content != strings.Repeat("记", MaxCharacters) {
		t.Fatal("rejected Upsert changed shared memory")
	}
}

func TestSharedMemoryRejectsAppendWhenResultExceedsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-memory.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("记", MaxCharacters-2)), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewSharedMemoryService(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Append(t.Context(), "住"); err != nil {
		t.Fatalf("Append() rejected exact limit: %v", err)
	}
	if _, err := service.Append(t.Context(), "住"); !errors.Is(err, ErrContentTooLong) {
		t.Fatalf("Append() error = %v, want ErrContentTooLong", err)
	}
}

func TestNewSharedMemoryServiceRejectsOversizedExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-memory.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("记", MaxCharacters+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSharedMemoryService(path); !errors.Is(err, ErrContentTooLong) {
		t.Fatalf("NewSharedMemoryService() error = %v, want ErrContentTooLong", err)
	}
}
