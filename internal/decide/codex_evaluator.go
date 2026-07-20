package decide

import (
	"context"
	"fmt"

	"jarvis/internal/domain"
)

// Disposition is Codex's own verdict on how to handle an extracted Todo, returned
// verbatim in the decision schema (no longer re-inferred by us):
//   - ready: enough context, plan is clear, safe to auto-run → route auto.
//   - need_review: plan is clear but a human should look → route need_decision.
//   - need_info: Codex tried tools and still lacks a key fact → route need_info.
//   - drop: not worth doing → route dropped.
const (
	DispositionReady      = "ready"
	DispositionNeedReview = "need_review"
	DispositionNeedInfo   = "need_info"
	DispositionDrop       = "drop"
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

	disposition := result.Decision.Disposition
	confidence := aggregateFactors(result.Decision.ConfidenceFactors)
	risk := aggregateFactors(result.Decision.RiskFactors)
	route, err := routeForDisposition(disposition)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: %w", todo.ID, err)
	}

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
		Clarifications:         result.Decision.Clarifications,
	}
	return input, nil
}

// routeForDisposition maps Codex's own disposition to the stored route:
//   - ready       → auto          (system auto-creates Task, M5 executes)
//   - need_review → need_decision (human confirms on the page)
//   - need_info   → need_info     (human supplies the missing fact)
//   - drop        → dropped       (terminal, no Task)
func routeForDisposition(disposition string) (string, error) {
	switch disposition {
	case DispositionReady:
		return RouteAuto, nil
	case DispositionNeedReview:
		return RouteNeedDecision, nil
	case DispositionNeedInfo:
		return RouteNeedInfo, nil
	case DispositionDrop:
		return RouteDropped, nil
	default:
		return "", fmt.Errorf("unknown codex disposition %q", disposition)
	}
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
