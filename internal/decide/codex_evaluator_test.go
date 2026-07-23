package decide

import (
	"context"
	"os"
	"path/filepath"
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
	content, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "m4-system-prompt.md"))
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

func clearFactors() []DecisionFactor {
	return []DecisionFactor{{Name: "clarity", Score: 0.9, Basis: "explicit ask"}}
}

func riskyFactors() []DecisionFactor {
	return []DecisionFactor{{Name: "reach", Score: 0.4, Basis: "internal only"}}
}

func clearPlan() *PlanDraft {
	return &PlanDraft{Summary: "do it", Steps: []string{"step"}, Basis: []string{"quote"}}
}

func TestRouteForDisposition(t *testing.T) {
	cases := []struct {
		disposition string
		wantRoute   string
	}{
		{DispositionReady, RouteAuto},
		{DispositionNeedReview, RouteNeedDecision},
		{DispositionNeedInfo, RouteNeedInfo},
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

func TestAggregateFactors(t *testing.T) {
	factors := []DecisionFactor{{Name: "a", Score: 0.2}, {Name: "b", Score: 0.8}}
	if got := aggregateFactors(factors); got != 0.5 {
		t.Fatalf("aggregate = %v, want 0.5", got)
	}
	if got := aggregateFactors(nil); got != 0.5 {
		t.Fatalf("empty aggregate = %v, want neutral 0.5", got)
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
			Disposition:       DispositionNeedReview,
			ConfidenceFactors: clearFactors(), RiskFactors: riskyFactors(),
			ConfidenceBasis: "explicit", PlanIsClear: true, ProposedPlan: clearPlan(),
			RecommendedReview: true,
			EvidenceGathered:  []Evidence{{Label: "repo", Detail: "jarvis local"}},
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
	if input.Route != RouteNeedDecision {
		t.Fatalf("route = %q, want need_decision (need_review disposition)", input.Route)
	}
	if input.RouteReason != "codex_"+DispositionNeedReview {
		t.Fatalf("route reason = %q", input.RouteReason)
	}
	if input.DecisionEngine != DecisionEngineCodex {
		t.Fatalf("engine = %q, want codex", input.DecisionEngine)
	}
	if input.CodexSessionID == nil || *input.CodexSessionID != "sess-1" {
		t.Fatalf("session id = %v", input.CodexSessionID)
	}
	if input.ProposedPlan == nil {
		t.Fatalf("proposed plan must ride along for confirmation")
	}
	if len(input.EvidenceGathered) != 1 || input.EvidenceGathered[0].Label != "repo" {
		t.Fatalf("evidence_gathered = %#v", input.EvidenceGathered)
	}
	if input.Confidence != 0.9 || input.Risk != 0.4 {
		t.Fatalf("aggregated scores conf=%v risk=%v", input.Confidence, input.Risk)
	}
	if runner.calls != 1 {
		t.Fatalf("codex called %d times, want 1", runner.calls)
	}
}

func TestCodexEvaluatorPreservesManualGateAfterSupplement(t *testing.T) {
	todo := &domain.Todo{
		ID: 42, Version: 4, Status: "extracted", ManualGateRequired: true,
		Title: "Fix deadlock", Description: "supplemented details", ActionType: "code_change",
		Target: "采集死锁问题", Context: "repo jarvis", OpenQuestions: datatypes.JSON([]byte(`[]`)),
		ExtractionResult: datatypes.JSON([]byte(`{"action_type":"code_change","title":"Fix deadlock","target":"采集死锁问题","description":"supplemented details","source_quote":"修一下采集死锁"}`)),
		ContextSnapshot:  testContextSnapshot(t),
	}
	runner := &fakeCodexDecisionRunner{result: &CodexResult{
		SessionID: "sess-ready",
		Decision: CodexDecision{
			Disposition:       DispositionReady,
			ConfidenceFactors: clearFactors(), RiskFactors: riskyFactors(),
			ConfidenceBasis: "now complete", PlanIsClear: true, ProposedPlan: clearPlan(),
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
	if input.Route != RouteNeedDecision || input.RouteReason != "codex_ready_manual_gate_preserved" {
		t.Fatalf("route=%q reason=%q", input.Route, input.RouteReason)
	}
	found := false
	for _, rule := range input.MatchedRules {
		found = found || rule == "manual_gate_required"
	}
	if !found {
		t.Fatalf("matched rules = %#v", input.MatchedRules)
	}
}
