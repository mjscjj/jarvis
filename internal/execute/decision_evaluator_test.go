package execute

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"

	"gorm.io/datatypes"
)

type fakeCodexDecisionRunner struct {
	calls  int
	result *CodexResult
	err    error
}

func (f *fakeCodexDecisionRunner) Decide(_ context.Context, _ CodexInput) (*CodexResult, error) {
	f.calls++
	return f.result, f.err
}

// fakeSharedMemoryReader 是共享记忆读取打桩：text 为要注入的文本，err 非空模拟读表失败。
type fakeSharedMemoryReader struct {
	text string
	err  error
}

func (f fakeSharedMemoryReader) Text(context.Context) (string, error) {
	return f.text, f.err
}

type fakeWorkRuleReader struct{}

func (fakeWorkRuleReader) Block(context.Context, string) (string, error) { return "", nil }

type fakeSkillReader struct{}

func (fakeSkillReader) Catalog(context.Context, string) (string, error) { return "", nil }

type fakeSystemPromptReader struct{}

func (fakeSystemPromptReader) Content(context.Context, string) (string, error) {
	content, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "m5-decision-system-prompt.md"))
	return string(content), err
}

// testContextSnapshot builds a minimal valid frozen snapshot for a Todo so
// requireContextSnapshot (fail-fast) is satisfied in unit tests.
func testContextSnapshot(t *testing.T) datatypes.JSON {
	t.Helper()
	raw, err := contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      "2026-07-19T00:00:00Z",
		Group:           &contextsnap.Group{ID: 1, ChatID: "oc_x"},
		Messages:        []contextsnap.Message{{MessageID: "m1", ChatID: "oc_x", Content: "leader asked to fix"}},
	}.Encode()
	if err != nil {
		t.Fatalf("build test snapshot: %v", err)
	}
	return datatypes.JSON(raw)
}

func clearPlan() json.RawMessage {
	return json.RawMessage(`{"summary":"do it","steps":["step"],"future":{"free":true}}`)
}

func TestRouteForDisposition(t *testing.T) {
	cases := []struct {
		disposition string
		wantRoute   string
	}{
		{DispositionReady, RouteAuto},
		{DispositionObserve, RouteObserving},
		{DispositionDrop, RouteDropped},
	}
	for _, tc := range cases {
		t.Run(tc.disposition, func(t *testing.T) {
			got, err := routeForDisposition(tc.disposition)
			if err != nil {
				t.Fatalf("routeForDisposition(%q) error = %v", tc.disposition, err)
			}
			if got != tc.wantRoute {
				t.Fatalf("route = %q, want %q", got, tc.wantRoute)
			}
		})
	}
	if _, err := routeForDisposition("bogus"); err == nil {
		t.Fatalf("unknown disposition must error")
	}
}

// The Todo-level confirmation queue is gone: a clue that needs the principal is
// still ready, and the question rides along to M5 on the Task.
func TestRouteForDispositionRejectsRetiredHumanGates(t *testing.T) {
	for _, disposition := range []string{"need_review", "need_info"} {
		if _, err := routeForDisposition(disposition); err == nil {
			t.Fatalf("disposition %q must no longer be routable", disposition)
		}
	}
}

func TestCodexEvaluatorMapsDecisionToEvaluationInput(t *testing.T) {
	todo := &domain.Todo{
		ID: 42, Version: 3, Status: "extracted",
		Title: "Fix deadlock", Description: "leader asked to fix the scan deadlock", ActionType: "code_change",
		Target: "采集死锁问题", Context: "repo jarvis", OpenQuestions: datatypes.JSON([]byte(`[]`)),
		ExtractionResult: datatypes.JSON([]byte(`{"action_type":"code_change","title":"Fix deadlock","target":"采集死锁问题","description":"leader asked to fix the scan deadlock","source_quote":"修一下采集死锁"}`)),
		ContextSnapshot:  testContextSnapshot(t),
	}
	runner := &fakeCodexDecisionRunner{result: &CodexResult{
		SessionID: "sess-1",
		Decision: CodexDecision{
			Disposition: DispositionReady,
			Plan:        clearPlan(),
			Payload:     json.RawMessage(`{"summary":"review","blocks":[{"kind":"evidence","label":"repo","content":"jarvis local"}]}`),
		},
	}}
	evaluator, err := NewCodexEvaluator(nil, runner, fakeSharedMemoryReader{}, fakeWorkRuleReader{}, fakeSkillReader{}, fakeSystemPromptReader{})
	if err != nil {
		t.Fatalf("NewCodexEvaluator() error = %v", err)
	}

	input, err := evaluator.Evaluate(context.Background(), todo)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if input.TodoID != 42 || input.ExpectedVersion != 3 {
		t.Fatalf("identity drift: %#v", input)
	}
	if input.Route != RouteAuto {
		t.Fatalf("route = %q, want auto (ready disposition)", input.Route)
	}
	if input.RouteReason != "codex_"+DispositionReady {
		t.Fatalf("route reason = %q", input.RouteReason)
	}
	if input.DecisionEngine != DecisionEngineCodex {
		t.Fatalf("engine = %q, want codex", input.DecisionEngine)
	}
	if input.CodexSessionID == nil || *input.CodexSessionID != "sess-1" {
		t.Fatalf("session id = %v", input.CodexSessionID)
	}
	if len(input.Plan) == 0 {
		t.Fatalf("plan must ride along to the Task")
	}
	if got := string(input.DecisionPayload); !strings.Contains(got, `"jarvis local"`) {
		t.Fatalf("decision payload = %s", got)
	}
	if runner.calls != 1 {
		t.Fatalf("codex called %d times, want 1", runner.calls)
	}
}

// A dropped clue is terminal and carries no plan, so nothing reaches M5.
func TestCodexEvaluatorRoutesDropWithoutPlan(t *testing.T) {
	todo := &domain.Todo{
		ID: 43, Version: 1, Status: "extracted",
		Title: "闲聊", Description: "没有实际行动", ActionType: "agent_task",
		Target: "闲聊", Context: "无", OpenQuestions: datatypes.JSON([]byte(`[]`)),
		ExtractionResult: datatypes.JSON([]byte(`{"action_type":"agent_task","title":"闲聊","target":"闲聊","description":"没有实际行动","source_quote":"随便说说"}`)),
		ContextSnapshot:  testContextSnapshot(t),
	}
	runner := &fakeCodexDecisionRunner{result: &CodexResult{
		SessionID: "sess-drop",
		Decision: CodexDecision{
			Disposition: DispositionDrop,
			Payload:     json.RawMessage(`{"summary":"没有可执行的事"}`),
		},
	}}
	evaluator, err := NewCodexEvaluator(nil, runner, fakeSharedMemoryReader{}, fakeWorkRuleReader{}, fakeSkillReader{}, fakeSystemPromptReader{})
	if err != nil {
		t.Fatalf("NewCodexEvaluator() error = %v", err)
	}

	input, err := evaluator.Evaluate(context.Background(), todo)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if input.Route != RouteDropped {
		t.Fatalf("route = %q, want dropped", input.Route)
	}
	if len(input.Plan) != 0 {
		t.Fatalf("dropped clue must not carry a plan: %s", input.Plan)
	}
}
