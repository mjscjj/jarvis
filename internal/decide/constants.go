package decide

import (
	"context"
	"fmt"
	"math"
)

// Decision routes stored on a Todo. auto (the old rule-engine route) was removed
// with the gray-zone logic; codex decisions land on need_info or need_decision.
const (
	RouteNeedInfo     = "need_info"
	RouteNeedDecision = "need_decision"
)

// Decision engines recorded in the audit trail.
const (
	DecisionEngineRule   = "rule"
	DecisionEngineCodex  = "codex"
	DecisionEngineManual = "manual"
)

// RuleScore is the confidence/risk pair seeded into the Codex prompt. Codex
// re-scores from the evidence and returns its own factor breakdown.
type RuleScore struct {
	Confidence float64 `json:"confidence"`
	Risk       float64 `json:"risk"`
}

// codexDecisionRunner is the read-only decision port implemented by CodexDecider
// and consumed by CodexEvaluator.
type codexDecisionRunner interface {
	Decide(context.Context, CodexInput) (*CodexResult, error)
}

func validateRuleScore(score RuleScore) error {
	if !unitScore(score.Confidence) {
		return fmt.Errorf("rule confidence=%v is outside [0,1]", score.Confidence)
	}
	if !unitScore(score.Risk) {
		return fmt.Errorf("rule risk=%v is outside [0,1]", score.Risk)
	}
	return nil
}

func unitScore(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
