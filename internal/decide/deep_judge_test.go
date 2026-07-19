package decide

import (
	"context"
	"errors"
	"testing"
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

func TestDeepJudgeOnlyCallsCodexInsideGrayZone(t *testing.T) {
	runner := &fakeCodexDecisionRunner{result: &CodexResult{SessionID: "fixture"}}
	judge := newFixtureDeepJudge(t, runner, BudgetRouteNeedDecision)

	outside, err := judge.Judge(context.Background(), RuleScore{Confidence: 0.9, Risk: 0.2}, CodexInput{Prompt: "unused"})
	if err != nil {
		t.Fatalf("outside Judge() error = %v", err)
	}
	if outside.Engine != DecisionEngineRule || outside.Reason != DeepJudgeReasonOutsideGray || runner.calls != 0 {
		t.Fatalf("outside result=%#v calls=%d", outside, runner.calls)
	}

	inside, err := judge.Judge(context.Background(), RuleScore{Confidence: 0.6, Risk: 0.6}, CodexInput{Prompt: "fixture"})
	if err != nil {
		t.Fatalf("inside Judge() error = %v", err)
	}
	if inside.Engine != DecisionEngineCodex || inside.Codex == nil || runner.calls != 1 {
		t.Fatalf("inside result=%#v calls=%d", inside, runner.calls)
	}
}

func TestDeepJudgeBudgetBehavior(t *testing.T) {
	for _, test := range []struct {
		name         string
		behavior     string
		wantOverride string
		wantEngine   string
	}{
		{name: "route to person", behavior: BudgetRouteNeedDecision, wantOverride: DecisionRouteNeedDecision, wantEngine: DecisionEngineCodex},
		{name: "explicit rule degradation", behavior: BudgetDegradeToRule, wantEngine: DecisionEngineRule},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeCodexDecisionRunner{err: ErrCodexBudgetExceeded}
			judge := newFixtureDeepJudge(t, runner, test.behavior)
			result, err := judge.Judge(context.Background(), RuleScore{Confidence: 0.7, Risk: 0.4}, CodexInput{Prompt: "fixture"})
			if err != nil {
				t.Fatalf("Judge() error = %v", err)
			}
			if result.Engine != test.wantEngine || result.RouteOverride != test.wantOverride || result.Reason != DeepJudgeReasonBudget {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestDeepJudgeFailuresRouteToPerson(t *testing.T) {
	for _, test := range []struct {
		name   string
		result *CodexResult
		err    error
		reason string
	}{
		{name: "timeout", err: context.DeadlineExceeded, reason: DeepJudgeReasonTimeout},
		{name: "invalid output", err: errors.New("invalid structured output"), reason: DeepJudgeReasonCodexFailure},
		{name: "nil result", reason: DeepJudgeReasonCodexFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			judge := newFixtureDeepJudge(t, &fakeCodexDecisionRunner{result: test.result, err: test.err}, BudgetRouteNeedDecision)
			result, err := judge.Judge(context.Background(), RuleScore{Confidence: 0.7, Risk: 0.4}, CodexInput{Prompt: "fixture"})
			if err != nil {
				t.Fatalf("Judge() error = %v", err)
			}
			if result.RouteOverride != DecisionRouteNeedDecision || result.Reason != test.reason || result.FailureDetail == "" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestDeepJudgePropagatesCallerCancellation(t *testing.T) {
	judge := newFixtureDeepJudge(t, &fakeCodexDecisionRunner{err: context.Canceled}, BudgetRouteNeedDecision)
	_, err := judge.Judge(context.Background(), RuleScore{Confidence: 0.7, Risk: 0.4}, CodexInput{Prompt: "fixture"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Judge() error = %v, want context.Canceled", err)
	}
}

func TestDeepJudgeRejectsInvalidScore(t *testing.T) {
	judge := newFixtureDeepJudge(t, &fakeCodexDecisionRunner{}, BudgetRouteNeedDecision)
	if _, err := judge.Judge(context.Background(), RuleScore{Confidence: 1.1, Risk: 0.4}, CodexInput{}); err == nil {
		t.Fatal("Judge() accepted invalid score")
	}
}

func newFixtureDeepJudge(t *testing.T, runner codexDecisionRunner, behavior string) *DeepJudge {
	t.Helper()
	judge, err := NewDeepJudge(runner, GrayZone{
		ConfidenceLow: 0.6, ConfidenceHigh: 0.85, RiskLow: 0.25, RiskHigh: 0.6,
	}, behavior)
	if err != nil {
		t.Fatalf("NewDeepJudge() error = %v", err)
	}
	return judge
}
