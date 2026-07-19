package decide

import (
	"errors"
	"math"
	"testing"
)

func TestValidateEvaluationInput(t *testing.T) {
	valid := fixtureEvaluationInput()
	if err := validateEvaluationInput(valid); err != nil {
		t.Fatalf("validateEvaluationInput() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*EvaluationInput)
	}{
		{name: "zero id", mutate: func(input *EvaluationInput) { input.TodoID = 0 }},
		{name: "unsupported route", mutate: func(input *EvaluationInput) { input.Route = "auto" }},
		{name: "nan score", mutate: func(input *EvaluationInput) { input.Confidence = math.NaN() }},
		{name: "no factors", mutate: func(input *EvaluationInput) { input.ConfidenceFactors = nil }},
		{name: "no rules", mutate: func(input *EvaluationInput) { input.MatchedRules = nil }},
		{name: "unknown engine", mutate: func(input *EvaluationInput) { input.DecisionEngine = "model" }},
		{name: "no config version", mutate: func(input *EvaluationInput) { input.ThresholdConfigVersion = "" }},
		{name: "Codex without trace", mutate: func(input *EvaluationInput) {
			input.DecisionEngine = DecisionEngineCodex
			input.PromptVersion = CodexPromptVersion
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := validateEvaluationInput(input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func fixtureEvaluationInput() EvaluationInput {
	return EvaluationInput{
		TodoID: 1, ExpectedVersion: 0, Confidence: 0.7, Risk: 0.4,
		Route: RouteNeedDecision, RouteReason: "default_safe_route",
		ConfidenceFactors: []DecisionFactor{{Name: "slots", Score: 0.8, Basis: "complete"}},
		RiskFactors:       []DecisionFactor{{Name: "irreversible", Score: 0.2, Basis: "read only"}},
		MatchedRules:      []string{"default.need_decision"}, DecisionEngine: DecisionEngineRule,
		ThresholdConfigVersion: "fixture-v1",
	}
}
