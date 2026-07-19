package decide

import (
	"context"
	"testing"

	"jarvis/internal/domain"
)

func TestManualGateEvaluator(t *testing.T) {
	input, err := (ManualGateEvaluator{}).Evaluate(context.Background(), &domain.Todo{ID: 42, Status: "extracted", Version: 3})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if input.TodoID != 42 || input.ExpectedVersion != 3 || input.Route != RouteNeedDecision || !input.ManualGate {
		t.Fatalf("EvaluationInput = %#v", input)
	}
	if input.ConfidenceFactors != nil || input.RiskFactors != nil || input.DecisionEngine != DecisionEngineManual {
		t.Fatalf("manual MVP evaluation contains scoring data: %#v", input)
	}
	if err := validateEvaluationInput(*input); err != nil {
		t.Fatalf("validateEvaluationInput() error = %v", err)
	}
}

func TestManualGateEvaluatorRejectsInvalidTodo(t *testing.T) {
	for _, todo := range []*domain.Todo{nil, {}, {ID: 1, Status: "need_decision"}} {
		if _, err := (ManualGateEvaluator{}).Evaluate(context.Background(), todo); err == nil {
			t.Fatalf("Evaluate(%#v) succeeded", todo)
		}
	}
}
