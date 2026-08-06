package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/contextsnap"
)

// fakeSharedMemoryReader 是共享记忆读取打桩：text 为要注入的文本，err 非空模拟读表失败。
type fakeSharedMemoryReader struct {
	text string
	err  error
}

func (f fakeSharedMemoryReader) Text(context.Context) (string, error) {
	return f.text, f.err
}

type fakeContextAssembler struct {
	options contextsnap.AssembleOptions
	err     error
}

func (f *fakeContextAssembler) AssembleConversation(_ context.Context, options contextsnap.AssembleOptions) (json.RawMessage, error) {
	f.options = options
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(`{"snapshot_version":"v1","principal":{"open_id":"ou_me","name":"我"},"other_projects":[],"memories":[]}`), nil
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWithSharedMemory(t, fakeSharedMemoryReader{})
}

func newTestServiceWithSharedMemory(t *testing.T, reader fakeSharedMemoryReader) *Service {
	t.Helper()
	return newTestServiceWithDependencies(t, reader, &fakeContextAssembler{})
}

func newTestServiceWithDependencies(t *testing.T, reader fakeSharedMemoryReader, assembler ContextAssembler) *Service {
	t.Helper()
	svc, err := NewService(Options{
		Bin:              "codex",
		Model:            "gpt-5.5",
		Sandbox:          "danger-full-access",
		ReasoningEffort:  "medium",
		Timeout:          600 * 1e9,
		SharedMemory:     reader,
		ContextAssembler: assembler,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func TestBuildPromptInjectsToolsAndContext(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt, err := svc.buildPrompt(context.Background(), Request{
		Message: "现在有几个待办？",
		PageContext: &PageContext{
			ActiveKey: "todos",
			Selection: &PageSelection{Kind: "todo", ID: 12, Label: "修复登录超时"},
			ViewState: json.RawMessage(`{"view":"observing","page":"2"}`),
		},
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{
		"todos",                 // active_key
		"修复登录超时",                // selection.label
		`"view":"observing"`,    // view_state
		"现在有几个待办？",              // 用户消息
		"安全约束",                  // 防注入提示
		"BEGIN_AVAILABLE_TOOLS", // 工具说明由工具层独立注入
		"jarvis-tools",
		"BEGIN_JARVIS_CONTEXT",
		`"open_id":"ou_me"`,
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

// 多轮 followup 不再灌系统指引（resume 已带历史），只带 page_context + 消息。
func TestBuildFollowupPromptOmitsSystemGuidance(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt, err := svc.buildFollowupPrompt(t.Context(), Request{
		Message:     "那第一个呢？",
		ThreadID:    "tid",
		PageContext: &PageContext{ActiveKey: "todos"},
	})
	if err != nil {
		t.Fatalf("buildFollowupPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "那第一个呢？") {
		t.Fatalf("followup prompt missing user message\n%s", prompt)
	}
	if !strings.Contains(prompt, "BEGIN_JARVIS_CONTEXT") {
		t.Fatalf("followup prompt missing refreshed Jarvis context\n%s", prompt)
	}
}

func TestBuildPromptScopesContextToSelectedProject(t *testing.T) {
	t.Parallel()
	assembler := &fakeContextAssembler{}
	svc := newTestServiceWithDependencies(t, fakeSharedMemoryReader{}, assembler)
	_, err := svc.buildPrompt(t.Context(), Request{
		Message: "看看这个项目",
		PageContext: &PageContext{Selection: &PageSelection{
			Kind: "project", ID: 42, Label: "Jarvis",
		}},
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if assembler.options.ProjectID == nil || *assembler.options.ProjectID != 42 {
		t.Fatalf("context options = %#v", assembler.options)
	}
}

func TestBuildPromptScopesContextToSelectedGroup(t *testing.T) {
	t.Parallel()
	assembler := &fakeContextAssembler{}
	svc := newTestServiceWithDependencies(t, fakeSharedMemoryReader{}, assembler)
	_, err := svc.buildPrompt(t.Context(), Request{
		Message: "看看这个会话",
		PageContext: &PageContext{Selection: &PageSelection{
			Kind: "group", ID: 7, Label: "Agent Runtime",
		}},
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if assembler.options.GroupID == nil || *assembler.options.GroupID != 7 {
		t.Fatalf("context options = %#v", assembler.options)
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
