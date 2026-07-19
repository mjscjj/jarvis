package decide

import (
	"context"
	"fmt"

	"jarvis/internal/domain"
)

const (
	ManualMVPMode          = "manual_mvp"
	manualMVPRouteReason   = "mvp_manual_gate"
	manualMVPConfigVersion = "mvp-manual-v1"
	manualMVPMatchedRule   = "mvp.manual_gate"
)

// ManualGateEvaluator is the MVP decision policy: every extracted Todo is
// presented to the user before a Task can be created. It deliberately does not
// invent confidence/risk scores or invoke a model.
type ManualGateEvaluator struct{}

func (ManualGateEvaluator) Evaluate(_ context.Context, todo *domain.Todo) (*EvaluationInput, error) {
	if todo == nil || todo.ID == 0 {
		return nil, fmt.Errorf("manual MVP evaluator Todo is invalid")
	}
	if todo.Status != "extracted" {
		return nil, fmt.Errorf("manual MVP evaluator Todo id=%d status=%s, want extracted", todo.ID, todo.Status)
	}
	return &EvaluationInput{
		TodoID: todo.ID, ExpectedVersion: todo.Version,
		Route: RouteNeedDecision, RouteReason: manualMVPRouteReason,
		MatchedRules: []string{manualMVPMatchedRule}, DecisionEngine: DecisionEngineManual,
		ThresholdConfigVersion: manualMVPConfigVersion, ManualGate: true,
	}, nil
}
