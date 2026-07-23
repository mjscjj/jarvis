package chat

import (
	"context"
	"strings"
	"testing"
)

// fakeSharedMemoryReader 是共享记忆读取打桩：text 为要注入的文本，err 非空模拟读表失败。
type fakeSharedMemoryReader struct {
	text string
	err  error
}

func (f fakeSharedMemoryReader) Text(context.Context) (string, error) {
	return f.text, f.err
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWithSharedMemory(t, fakeSharedMemoryReader{})
}

func newTestServiceWithSharedMemory(t *testing.T, reader fakeSharedMemoryReader) *Service {
	t.Helper()
	svc, err := NewService(Options{
		Bin:             "codex",
		Model:           "gpt-5.5",
		Sandbox:         "danger-full-access",
		ReasoningEffort: "medium",
		Timeout:         600 * 1e9,
		DSN:             "root:secret@tcp(127.0.0.1:3306)/jarvis",
		SharedMemory:    reader,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func TestBuildPromptInjectsDSNAndContext(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt, err := svc.buildPrompt(context.Background(), Request{
		Message: "现在有几个待办？",
		PageContext: &PageContext{
			ActiveKey: "todos",
			Selection: &PageSelection{Kind: "todo", ID: 12, Label: "修复登录超时"},
		},
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{
		"root:secret@tcp(127.0.0.1:3306)/jarvis", // DSN 明文注入
		"todos",                                  // active_key
		"修复登录超时",                                 // selection.label
		"现在有几个待办？",                               // 用户消息
		"安全约束",                                   // 防注入提示
		"BEGIN_AVAILABLE_TOOLS",                  // 工具说明由工具层独立注入
		"jarvis-tools",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block\n%s", prompt)
	}
}

// 首轮 prompt 注入非空共享记忆：包含 BEGIN_SHARED_MEMORY 标记、内容与「可信」字样。
func TestBuildPromptInjectsSharedMemory(t *testing.T) {
	t.Parallel()
	svc := newTestServiceWithSharedMemory(t, fakeSharedMemoryReader{text: "线上库密码是 hunter2"})
	prompt, err := svc.buildPrompt(context.Background(), Request{Message: "帮我查一下"})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "线上库密码是 hunter2", "可信"} {
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
