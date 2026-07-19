package decide

import (
	"context"
	"fmt"

	"jarvis/internal/domain"
)

// Disposition is Codex's suggested handling for an extracted Todo. It is the M4
// output the human observes: auto_execute means "safe to run", need_review means
// "plan is clear but a human should confirm", need_info means "not enough
// information to act". The disposition is recorded in the audit; the actual
// local-auto vs external-confirm split happens later at execution time (M5).
const (
	DispositionAutoExecute = "auto_execute"
	DispositionNeedReview  = "need_review"
	DispositionNeedInfo    = "need_info"
)

// neutralRuleScore is the seed confidence/risk handed to Codex. We deliberately
// do not implement a rule scorer (complexity redline): Codex re-scores from the
// evidence and returns real confidence_factors/risk_factors, which we aggregate.
var neutralRuleScore = RuleScore{Confidence: 0.5, Risk: 0.5}

// CodexEvaluator implements todoEvaluator by asking Codex (read-only) to judge
// each extracted Todo. It reuses the existing CodexDecider/BuildCodexPrompt/
// schema unchanged and maps Codex's signals to a disposition, then to the
// stored route (need_info or need_decision). auto_execute and need_review both
// currently land on need_decision so every Todo still passes through the
// confirmation page; the disposition rides along in the audit for observation.
type CodexEvaluator struct {
	codex codexDecisionRunner
}

func NewCodexEvaluator(codex codexDecisionRunner) (*CodexEvaluator, error) {
	if codex == nil {
		return nil, fmt.Errorf("codex evaluator decider is nil")
	}
	return &CodexEvaluator{codex: codex}, nil
}

func (e *CodexEvaluator) Evaluate(ctx context.Context, todo *domain.Todo) (*EvaluationInput, error) {
	if todo == nil || todo.ID == 0 {
		return nil, fmt.Errorf("codex evaluator Todo is invalid")
	}
	if todo.Status != "extracted" {
		return nil, fmt.Errorf("codex evaluator Todo id=%d status=%s, want extracted", todo.ID, todo.Status)
	}

	// M4 reuses the M3-frozen context_snapshot verbatim (no re-snapshot). It must
	// be present — fail-fast if empty (docs/design-context-pipeline.md §2.3).
	background, err := requireContextSnapshot(todo)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: %w", todo.ID, err)
	}
	prompt, err := BuildCodexPrompt(CodexPromptInput{Todo: todo, RuleScore: neutralRuleScore, Background: background})
	if err != nil {
		return nil, fmt.Errorf("build codex decision prompt todo_id=%d: %w", todo.ID, err)
	}

	result, err := e.codex.Decide(ctx, CodexInput{Prompt: prompt.Text})
	if err != nil {
		return nil, fmt.Errorf("codex decision failed todo_id=%d: %w", todo.ID, err)
	}
	if result == nil {
		return nil, fmt.Errorf("codex decision returned nil result todo_id=%d", todo.ID)
	}

	disposition := dispositionFromDecision(result.Decision)
	confidence := aggregateFactors(result.Decision.ConfidenceFactors)
	risk := aggregateFactors(result.Decision.RiskFactors)
	route := routeForDisposition(disposition)

	sessionID := result.SessionID
	input := &EvaluationInput{
		TodoID:                 todo.ID,
		ExpectedVersion:        todo.Version,
		Confidence:             confidence,
		Risk:                   risk,
		Route:                  route,
		RouteReason:            "codex_" + disposition,
		ConfidenceFactors:      result.Decision.ConfidenceFactors,
		RiskFactors:            result.Decision.RiskFactors,
		MatchedRules:           []string{"codex_disposition:" + disposition},
		DecisionEngine:         DecisionEngineCodex,
		CodexSessionID:         &sessionID,
		PromptVersion:          prompt.Version,
		ThresholdConfigVersion: "codex-v1",
		ProposedPlan:           result.Decision.ProposedPlan,
	}
	return input, nil
}

// dispositionFromDecision derives Codex's suggested handling from the existing
// decision schema signals (no schema change): an unclear plan means we need more
// info; a clear plan that Codex flags for review (or that carries uncertainty)
// needs human review; a clear plan with no review flag is safe to auto-execute.
func dispositionFromDecision(decision CodexDecision) string {
	if !decision.PlanIsClear || decision.ProposedPlan == nil {
		return DispositionNeedInfo
	}
	if decision.RecommendedReview || len(decision.UncertaintyFactors) > 0 {
		return DispositionNeedReview
	}
	return DispositionAutoExecute
}

// routeForDisposition maps a disposition to a stored route. need_info maps
// straight through; auto_execute and need_review both currently route to
// need_decision so nothing bypasses the confirmation page during the
// observation phase. The disposition itself is preserved in RouteReason/
// MatchedRules for later analysis of how well Codex judges.
func routeForDisposition(disposition string) string {
	if disposition == DispositionNeedInfo {
		return RouteNeedInfo
	}
	return RouteNeedDecision
}

// aggregateFactors reduces Codex's factor list to a single [0,1] score by
// averaging. Codex already validated each factor score is in range; an empty
// list is impossible here because the schema requires minItems=1, but we guard
// anyway and return the neutral 0.5 rather than divide by zero.
func aggregateFactors(factors []DecisionFactor) float64 {
	if len(factors) == 0 {
		return 0.5
	}
	var sum float64
	for _, factor := range factors {
		sum += factor.Score
	}
	return sum / float64(len(factors))
}
