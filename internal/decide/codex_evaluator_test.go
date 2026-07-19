package decide

import (
	"context"
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

func TestDispositionFromDecision(t *testing.T) {
	cases := []struct {
		name     string
		decision CodexDecision
		want     string
	}{
		{
			name:     "unclear plan -> need_info",
			decision: CodexDecision{PlanIsClear: false, ProposedPlan: nil},
			want:     DispositionNeedInfo,
		},
		{
			name:     "clear flag but nil plan -> need_info",
			decision: CodexDecision{PlanIsClear: true, ProposedPlan: nil},
			want:     DispositionNeedInfo,
		},
		{
			name:     "clear plan with review flag -> need_review",
			decision: CodexDecision{PlanIsClear: true, ProposedPlan: clearPlan(), RecommendedReview: true},
			want:     DispositionNeedReview,
		},
		{
			name:     "clear plan with uncertainty -> need_review",
			decision: CodexDecision{PlanIsClear: true, ProposedPlan: clearPlan(), UncertaintyFactors: []string{"unclear owner"}},
			want:     DispositionNeedReview,
		},
		{
			name:     "clear plan no flags -> auto_execute",
			decision: CodexDecision{PlanIsClear: true, ProposedPlan: clearPlan()},
			want:     DispositionAutoExecute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dispositionFromDecision(tc.decision); got != tc.want {
				t.Fatalf("disposition = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRouteForDisposition(t *testing.T) {
	if got := routeForDisposition(DispositionNeedInfo); got != RouteNeedInfo {
		t.Fatalf("need_info route = %q, want %q", got, RouteNeedInfo)
	}
	if got := routeForDisposition(DispositionNeedReview); got != RouteNeedDecision {
		t.Fatalf("need_review route = %q, want %q", got, RouteNeedDecision)
	}
	if got := routeForDisposition(DispositionAutoExecute); got != RouteNeedDecision {
		t.Fatalf("auto_execute route = %q, want %q", got, RouteNeedDecision)
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
		Slots:           datatypes.JSON([]byte(`{"repo":"jarvis"}`)),
		ContextSnapshot: testContextSnapshot(t),
	}
	runner := &fakeCodexDecisionRunner{result: &CodexResult{
		SessionID: "sess-1",
		Decision: CodexDecision{
			ConfidenceFactors: clearFactors(), RiskFactors: riskyFactors(),
			ConfidenceBasis: "explicit", PlanIsClear: true, ProposedPlan: clearPlan(),
			RecommendedReview: true,
		},
	}}
	evaluator, err := NewCodexEvaluator(runner)
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
	if input.Confidence != 0.9 || input.Risk != 0.4 {
		t.Fatalf("aggregated scores conf=%v risk=%v", input.Confidence, input.Risk)
	}
	if runner.calls != 1 {
		t.Fatalf("codex called %d times, want 1", runner.calls)
	}
}
