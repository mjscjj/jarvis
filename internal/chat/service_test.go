package chat

import (
	"strings"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(Options{
		Bin:             "codex",
		Model:           "gpt-5.5",
		Sandbox:         "danger-full-access",
		ReasoningEffort: "medium",
		Timeout:         600 * 1e9,
		DSN:             "root:secret@tcp(127.0.0.1:3306)/jarvis",
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func TestBuildPromptInjectsDSNAndContext(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt := svc.buildPrompt(Request{
		Message: "现在有几个待办？",
		PageContext: &PageContext{
			ActiveKey: "todos",
			Selection: &PageSelection{Kind: "todo", ID: 12, Label: "修复登录超时"},
		},
	})
	for _, want := range []string{
		"root:secret@tcp(127.0.0.1:3306)/jarvis", // DSN 明文注入
		"todos",                                  // active_key
		"修复登录超时",                                // selection.label
		"现在有几个待办？",                              // 用户消息
		"安全约束",                                   // 防注入提示
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
}

// 多轮 followup 不再灌系统指引/DSN（resume 已带历史），只带 page_context + 消息。
func TestBuildFollowupPromptOmitsSystemGuidance(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt := svc.buildFollowupPrompt(Request{
		Message:     "那第一个呢？",
		ThreadID:    "tid",
		PageContext: &PageContext{ActiveKey: "todos"},
	})
	if strings.Contains(prompt, "root:secret") {
		t.Fatalf("followup prompt should not re-inject DSN\n%s", prompt)
	}
	if !strings.Contains(prompt, "那第一个呢？") {
		t.Fatalf("followup prompt missing user message\n%s", prompt)
	}
}

func TestPageContextBlockEmpty(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	if got := svc.pageContextBlock(nil); got != "" {
		t.Fatalf("nil page context should render empty, got %q", got)
	}
	if got := svc.pageContextBlock(&PageContext{}); got != "" {
		t.Fatalf("empty page context should render empty, got %q", got)
	}
}

func TestStreamRejectsBlankMessage(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	err := svc.Stream(t.Context(), Request{Message: "   "}, func(Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Fatalf("Stream() error = %v, want message required", err)
	}
}
