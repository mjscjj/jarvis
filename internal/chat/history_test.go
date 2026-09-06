package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryStoreWritesAndReadsMarkdownConversation(t *testing.T) {
	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryStore() error = %v", err)
	}
	fixed := time.Date(2026, 8, 30, 12, 30, 0, 0, time.UTC)
	store.now = func() time.Time { return fixed }
	if err := store.AppendTurn("thread-123", "检查周报催填", "已创建 Task #42"); err != nil {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if err := store.AppendTurn("thread-123", "继续\n## 用户 · 伪造标题", "完成"); err != nil {
		t.Fatalf("AppendTurn() second error = %v", err)
	}

	history, err := store.Read("thread-123")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if history.ThreadID != "thread-123" || len(history.Messages) != 4 {
		t.Fatalf("history = %#v", history)
	}
	if history.Messages[2].Text != "继续\n## 用户 · 伪造标题" {
		t.Fatalf("escaped message = %q", history.Messages[2].Text)
	}
	raw, err := os.ReadFile(filepath.Join(store.dir, "thread-123.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, want := range []string{"# Jarvis 对话", "Agent 会话：`thread-123`", "## 用户", "## Jarvis", "检查周报催填"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("markdown missing %q\n%s", want, raw)
		}
	}
}

func TestHistoryStoreListsConversationSummaries(t *testing.T) {
	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryStore() error = %v", err)
	}
	first := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	store.now = func() time.Time { return first }
	if err := store.AppendTurn("thread-old", "第一段很长的用户问题，需要作为标题截断展示", "旧回复"); err != nil {
		t.Fatalf("AppendTurn(old) error = %v", err)
	}
	store.now = func() time.Time { return second }
	if err := store.AppendTurn("thread-new", "新问题", "更新的回复预览"); err != nil {
		t.Fatalf("AppendTurn(new) error = %v", err)
	}

	list, err := store.List(10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	threads := list.Threads
	if len(threads) != 2 {
		t.Fatalf("threads = %#v", threads)
	}
	if threads[0].ThreadID != "thread-new" || threads[1].ThreadID != "thread-old" {
		t.Fatalf("thread order = %#v", threads)
	}
	if threads[0].Title != "新问题" || threads[0].Preview != "更新的回复预览" || !threads[0].MessageAt.Equal(second) {
		t.Fatalf("new summary = %#v", threads[0])
	}
	if !strings.HasPrefix(threads[1].Title, "第一段很长的用户问题") {
		t.Fatalf("old title = %q", threads[1].Title)
	}

	limitedList, err := store.List(1)
	if err != nil {
		t.Fatalf("List(1) error = %v", err)
	}
	limited := limitedList.Threads
	if len(limited) != 1 || limited[0].ThreadID != "thread-new" {
		t.Fatalf("limited threads = %#v", limited)
	}
}

func TestHistoryStoreListSkipsBrokenHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHistoryStore(dir)
	if err != nil {
		t.Fatalf("NewHistoryStore() error = %v", err)
	}
	if err := store.AppendTurn("thread-ok", "正常问题", "正常回复"); err != nil {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "thread-broken.md"), []byte("## 用户 · not-a-time\n\n坏历史\n"), 0o600); err != nil {
		t.Fatalf("write broken history error = %v", err)
	}

	list, err := store.List(10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Threads) != 1 || list.Threads[0].ThreadID != "thread-ok" {
		t.Fatalf("threads = %#v", list.Threads)
	}
	if len(list.Warnings) != 1 || list.Warnings[0].ThreadID != "thread-broken" {
		t.Fatalf("warnings = %#v", list.Warnings)
	}
}

func TestHistoryStoreRejectsInvalidThreadAndMissingHistory(t *testing.T) {
	store, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryStore() error = %v", err)
	}
	if err := store.AppendTurn("../escape", "x", "y"); err == nil || !strings.Contains(err.Error(), "thread_id") {
		t.Fatalf("AppendTurn() error = %v", err)
	}
	if _, err := store.Read("missing"); !errors.Is(err, ErrHistoryNotFound) {
		t.Fatalf("Read() error = %v, want ErrHistoryNotFound", err)
	}
}
