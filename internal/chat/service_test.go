package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/textstore"
)

// fakeSharedMemoryReader 是共享记忆读取打桩：text 为要注入的文本，err 非空模拟读表失败。
type fakeSharedMemoryReader struct {
	text string
	err  error
}

type fakeSystemPromptReader struct {
	err error
}

func (f fakeSystemPromptReader) Content(_ context.Context, key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	switch key {
	case textstore.SystemPromptChatKey:
		return "你是 小贾 的对话助手。安全约束：先直接回答，不要复述 Skill、工具、权限或流程；简单问题用一到四句话，不为结构化硬凑分点，结论说清后立即停止。", nil
	case textstore.OKRAgentPrinciplesKey:
		return "OKR 原子工具与权限原则", nil
	default:
		return "", textstore.ErrNotFound
	}
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
	return newTestServiceWithIdentities(t, reader, assembler, nil)
}

func newTestServiceWithIdentities(t *testing.T, reader fakeSharedMemoryReader, assembler ContextAssembler, identities FeishuIdentityResolver) *Service {
	t.Helper()
	svc, err := NewService(Options{
		AgentName:        "小贾",
		Bin:              "codex",
		Model:            "gpt-5.5",
		Sandbox:          "danger-full-access",
		ReasoningEffort:  "medium",
		Timeout:          600 * 1e9,
		HistoryDir:       t.TempDir(),
		SharedMemory:     reader,
		ContextAssembler: assembler,
		SystemPrompts:    fakeSystemPromptReader{},
		FeishuIdentities: identities,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

type fakeFeishuIdentities struct {
	identity FeishuIdentity
	err      error
	openIDs  []string
}

func (f *fakeFeishuIdentities) Resolve(_ context.Context, openID string) (FeishuIdentity, error) {
	f.openIDs = append(f.openIDs, openID)
	if f.err != nil {
		return FeishuIdentity{}, f.err
	}
	return f.identity, nil
}

func TestPromptsCarryTheSignedInUsersFeishuCredentials(t *testing.T) {
	t.Parallel()
	identities := &fakeFeishuIdentities{identity: FeishuIdentity{
		OpenID:    "ou_alice",
		Name:      "Alice",
		AppID:     "cli_test",
		TokenPath: "data/okr/feishu-tokens/ou_alice.json",
	}}
	svc := newTestServiceWithIdentities(t, fakeSharedMemoryReader{}, &fakeContextAssembler{}, identities)
	req := Request{Message: "帮我读这篇文档", UserOpenID: "ou_alice"}

	first, err := svc.buildPrompt(context.Background(), req)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	// The follow-up prompt must repeat it: the token is refreshed every turn
	// and a resumed conversation may have scrolled the first turn out.
	followup, err := svc.buildFollowupPrompt(context.Background(), req)
	if err != nil {
		t.Fatalf("buildFollowupPrompt() error = %v", err)
	}
	for _, prompt := range []string{first, followup} {
		for _, want := range []string{
			"Alice",
			"ou_alice",
			"data/okr/feishu-tokens/ou_alice.json",
			"LARKSUITE_CLI_USER_ACCESS_TOKEN",
			"cli_test",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt is missing %q:\n%s", want, prompt)
			}
		}
	}
	if len(identities.openIDs) != 2 {
		t.Fatalf("resolved open ids = %v, want one per turn", identities.openIDs)
	}
}

func TestPromptReportsUnusableFeishuCredentialsWithoutFailingTheTurn(t *testing.T) {
	t.Parallel()
	identities := &fakeFeishuIdentities{err: errors.New("token expired, sign in again")}
	svc := newTestServiceWithIdentities(t, fakeSharedMemoryReader{}, &fakeContextAssembler{}, identities)

	prompt, err := svc.buildPrompt(context.Background(), Request{Message: "在吗", UserOpenID: "ou_alice"})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{"ou_alice", "token expired, sign in again", "重新在 OKR 模块登录"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, prompt)
		}
	}
	// The tool catalog explains the env var in general; what must be absent is
	// the concrete command pointing at a token file that does not work.
	if strings.Contains(prompt, "jq -r .access_token") {
		t.Fatalf("prompt offered a token it does not have:\n%s", prompt)
	}
}

func TestPromptOmitsFeishuIdentityWhenNobodyIsSignedIn(t *testing.T) {
	t.Parallel()
	identities := &fakeFeishuIdentities{identity: FeishuIdentity{OpenID: "ou_alice", TokenPath: "x.json", AppID: "cli_test"}}
	svc := newTestServiceWithIdentities(t, fakeSharedMemoryReader{}, &fakeContextAssembler{}, identities)

	prompt, err := svc.buildPrompt(context.Background(), Request{Message: "在吗"})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if strings.Contains(prompt, "当前登录用户的飞书身份") {
		t.Fatalf("prompt claimed an identity without an open_id:\n%s", prompt)
	}
	if len(identities.openIDs) != 0 {
		t.Fatalf("resolver was called without an open_id: %v", identities.openIDs)
	}
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

func TestBuildPromptInjectsLatestOKRPrinciplesOnlyOnOKRPage(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt, err := svc.buildPrompt(t.Context(), Request{
		Message: "填写当前拆解",
		PageContext: &PageContext{
			ActiveKey: "okr",
			ViewState: json.RawMessage(`{"tab":"weekly-fill","quarter":"2026-Q2","week":"2026-W15","point_id":"point-1"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"BEGIN_OKR_AGENT_PRINCIPLES", "OKR 原子工具与权限原则", `"point_id":"point-1"`} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("OKR prompt missing %q\n%s", expected, prompt)
		}
	}

	nonOKR, err := svc.buildPrompt(t.Context(), Request{Message: "看看任务", PageContext: &PageContext{ActiveKey: "tasks"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nonOKR, "BEGIN_OKR_AGENT_PRINCIPLES") {
		t.Fatalf("non-OKR prompt leaked OKR principles\n%s", nonOKR)
	}
}

// TestBuildPromptSystemGuidanceKeepsAnswerFirstStyle 锁定 Chat 系统指引里的答复风格约束：
// 先直接回答、不复述 Skill/权限/流程、简单问题短答、不硬凑结构化。防止 Chat 再退化成工具说明腔。
func TestBuildPromptSystemGuidanceKeepsAnswerFirstStyle(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	prompt, err := svc.buildPrompt(context.Background(), Request{Message: "在忙吗？"})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{
		"你是 小贾 的对话助手",
		"先直接回答",
		"不要复述",
		"一到四句",
		"不为结构化硬凑分点",
		"立即停止",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system guidance missing answer-first rule %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "一定一定要用结构化表达") {
		t.Fatalf("system guidance must not force rigid structured output\n%s", prompt)
	}
	if strings.Contains(prompt, "你是 Jarvis 的对话助手") {
		t.Fatalf("system guidance still contains the fixed assistant name\n%s", prompt)
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

func TestStreamStartsNewSessionWhenResumeHasNoRollout(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = resume ]; then\n" +
		"    printf '%s\\n' 'Error: thread/resume: thread/resume failed: no rollout found for thread id old-id (code -32600)' >&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"done\n" +
		"printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"new-tid\"}'\n" +
		"printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"id\":\"item_0\",\"type\":\"agent_message\",\"text\":\"你好\"}}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Options{
		AgentName:        "小贾",
		Bin:              bin,
		Model:            "fixture-model",
		Sandbox:          "read-only",
		ReasoningEffort:  "medium",
		Timeout:          5 * time.Second,
		HistoryDir:       t.TempDir(),
		SharedMemory:     fakeSharedMemoryReader{},
		ContextAssembler: &fakeContextAssembler{},
		SystemPrompts:    fakeSystemPromptReader{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var threadID string
	var deltas []string
	if err := svc.Stream(t.Context(), Request{Message: "你好", ThreadID: "old-id"}, func(event Event) error {
		switch event.Kind {
		case EventThread:
			threadID = event.ThreadID
		case EventDelta:
			deltas = append(deltas, event.Text)
		}
		return nil
	}); err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if threadID != "new-tid" {
		t.Fatalf("thread_id = %q, want new-tid", threadID)
	}
	if len(deltas) != 1 || deltas[0] != "你好" {
		t.Fatalf("deltas = %v, want [你好]", deltas)
	}
}

func TestStreamStartsNewCursorSessionWhenSwitchingCLI(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "cursor-agent")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"cursor-session\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"已切换\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"is_error\":false}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Options{
		AgentName: "小贾",
		Bin:       bin, Model: "claude-opus-5-high", Sandbox: "danger-full-access",
		ReasoningEffort: "high", Timeout: 5 * time.Second, HistoryDir: t.TempDir(),
		SharedMemory: fakeSharedMemoryReader{}, ContextAssembler: &fakeContextAssembler{},
		SystemPrompts: fakeSystemPromptReader{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var threadID string
	var deltas []string
	err = svc.Stream(t.Context(), Request{Message: "继续", ThreadID: "old-traex-thread"}, func(event Event) error {
		switch event.Kind {
		case EventThread:
			threadID = event.ThreadID
		case EventDelta:
			deltas = append(deltas, event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "cursor_cursor-session" || !slices.Equal(deltas, []string{"已切换"}) {
		t.Fatalf("threadID=%q deltas=%#v", threadID, deltas)
	}
}

func TestStreamDoesNotRetryOtherResumeFailures(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' 'auth failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Options{
		AgentName:        "小贾",
		Bin:              bin,
		Model:            "fixture-model",
		Sandbox:          "read-only",
		ReasoningEffort:  "medium",
		Timeout:          5 * time.Second,
		HistoryDir:       t.TempDir(),
		SharedMemory:     fakeSharedMemoryReader{},
		ContextAssembler: &fakeContextAssembler{},
		SystemPrompts:    fakeSystemPromptReader{},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Stream(t.Context(), Request{Message: "你好", ThreadID: "old-id"}, func(Event) error {
		t.Fatal("failed resume must not emit a chat event")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("error = %v, want original auth failure", err)
	}
}

// 复现线上故障：上游卡住时旧进程占着 codex 的 thread-store 写入者，用户再发一条
// 就撞 "already has an active writer"，只能干等 600 秒超时或重启服务。
// 现在新一轮必须先打断旧一轮再接管，两条消息都不该卡住。
func TestSecondTurnOnSameThreadInterruptsTheStuckOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "first-turn-started")
	bin := filepath.Join(dir, "codex")
	// 第一次调用挂住不返回（模拟上游卡死），之后的调用正常应答。
	script := "#!/bin/sh\n" +
		"if [ ! -f " + marker + " ]; then\n" +
		"  : > " + marker + "\n" +
		"  while true; do sleep 0.05; done\n" +
		"fi\n" +
		"printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"tid-1\"}'\n" +
		"printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"id\":\"item_0\",\"type\":\"agent_message\",\"text\":\"第二轮\"}}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Options{
		AgentName:        "小贾",
		Bin:              bin,
		Model:            "fixture-model",
		Sandbox:          "read-only",
		ReasoningEffort:  "medium",
		Timeout:          60 * time.Second, // 必须靠打断结束，不能靠单轮超时兜底
		HistoryDir:       t.TempDir(),
		SharedMemory:     fakeSharedMemoryReader{},
		ContextAssembler: &fakeContextAssembler{},
		SystemPrompts:    fakeSystemPromptReader{},
	})
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- svc.Stream(t.Context(), Request{Message: "第一条", ThreadID: "tid-1"}, func(Event) error { return nil })
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first turn never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var deltas []string
	if err := svc.Stream(t.Context(), Request{Message: "第二条", ThreadID: "tid-1"}, func(event Event) error {
		if event.Kind == EventDelta {
			deltas = append(deltas, event.Text)
		}
		return nil
	}); err != nil {
		t.Fatalf("second turn error = %v, want it to take over the thread", err)
	}
	if len(deltas) != 1 || deltas[0] != "第二轮" {
		t.Fatalf("deltas = %v, want [第二轮]", deltas)
	}

	select {
	case err := <-firstDone:
		if err == nil {
			t.Fatal("interrupted first turn must report an error, not report success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first turn was never interrupted")
	}
}

// 首轮是新会话时，thread_id 要等 thread.started 才知道。如果只在 resume 时登记占用，
// 首轮就无法被打断——而首轮恰恰最容易被用户追发，线上就是这么一直撞
// already has an active writer 的。
func TestFollowupInterruptsAStuckFirstTurnOfANewSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "first-turn-started")
	bin := filepath.Join(dir, "codex")
	// 首轮：报出 thread_id 后挂住不返回（模拟上游卡死）。之后的调用正常应答。
	script := "#!/bin/sh\n" +
		"if [ ! -f " + marker + " ]; then\n" +
		"  : > " + marker + "\n" +
		"  printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"tid-new\"}'\n" +
		"  while true; do sleep 0.05; done\n" +
		"fi\n" +
		"printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"tid-new\"}'\n" +
		"printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"id\":\"item_0\",\"type\":\"agent_message\",\"text\":\"第二轮\"}}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Options{
		AgentName:        "小贾",
		Bin:              bin,
		Model:            "fixture-model",
		Sandbox:          "read-only",
		ReasoningEffort:  "medium",
		Timeout:          60 * time.Second,
		HistoryDir:       t.TempDir(),
		SharedMemory:     fakeSharedMemoryReader{},
		ContextAssembler: &fakeContextAssembler{},
		SystemPrompts:    fakeSystemPromptReader{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 首轮不带 thread_id，也就是新会话。
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- svc.Stream(t.Context(), Request{Message: "第一条"}, func(Event) error { return nil })
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first turn never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 等首轮把 thread_id 报上来并登记占用。
	time.Sleep(200 * time.Millisecond)

	var deltas []string
	if err := svc.Stream(t.Context(), Request{Message: "第二条", ThreadID: "tid-new"}, func(event Event) error {
		if event.Kind == EventDelta {
			deltas = append(deltas, event.Text)
		}
		return nil
	}); err != nil {
		t.Fatalf("followup error = %v, want it to take over the new session's thread", err)
	}
	if len(deltas) != 1 || deltas[0] != "第二轮" {
		t.Fatalf("deltas = %v, want [第二轮]", deltas)
	}

	select {
	case err := <-firstDone:
		if err == nil {
			t.Fatal("interrupted first turn must report an error, not report success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stuck first turn of a new session was never interrupted")
	}
}
