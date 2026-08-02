package execute

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateEvaluationInputAcceptsCodexRoutes(t *testing.T) {
	if err := validateEvaluationInput(fixtureCodexEvaluationInput()); err != nil {
		t.Fatalf("auto route rejected: %v", err)
	}
	dropped := fixtureCodexEvaluationInput()
	dropped.Route = RouteDropped
	dropped.RouteReason = "codex_" + DispositionDrop
	dropped.Plan = nil
	if err := validateEvaluationInput(dropped); err != nil {
		t.Fatalf("dropped route rejected: %v", err)
	}
}

func TestValidateEvaluationInputRejectsViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationInput)
	}{
		{"zero id", func(input *EvaluationInput) { input.TodoID = 0 }},
		{"negative version", func(input *EvaluationInput) { input.ExpectedVersion = -1 }},
		{"unknown route", func(input *EvaluationInput) { input.Route = "shipped" }},
		// The Todo-level confirmation queue is gone; its routes must be rejected.
		{"retired need_decision route", func(input *EvaluationInput) { input.Route = "need_decision" }},
		{"retired need_info route", func(input *EvaluationInput) { input.Route = "need_info" }},
		{"blank reason", func(input *EvaluationInput) { input.RouteReason = "  " }},
		{"no rules", func(input *EvaluationInput) { input.MatchedRules = nil }},
		{"blank rule", func(input *EvaluationInput) { input.MatchedRules = []string{" "} }},
		{"unknown engine", func(input *EvaluationInput) { input.DecisionEngine = "rule" }},
		{"retired manual engine", func(input *EvaluationInput) { input.DecisionEngine = "manual" }},
		{"auto without plan", func(input *EvaluationInput) { input.Plan = nil }},
		{"blank prompt version", func(input *EvaluationInput) { input.PromptVersion = "" }},
		{"no session and no failure detail", func(input *EvaluationInput) { input.CodexSessionID = nil }},
		{"empty payload", func(input *EvaluationInput) { input.DecisionPayload = nil }},
		{"null payload", func(input *EvaluationInput) { input.DecisionPayload = json.RawMessage("null") }},
		{"empty plan object", func(input *EvaluationInput) { input.Plan = json.RawMessage("{}") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := fixtureCodexEvaluationInput()
			test.mutate(&input)
			if err := validateEvaluationInput(input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// A Codex evaluation that failed to produce a session is still valid as long as
// it carries the failure detail explaining why.
func TestValidateEvaluationInputAcceptsCodexFailureWithoutSession(t *testing.T) {
	input := fixtureCodexEvaluationInput()
	input.CodexSessionID = nil
	input.FailureDetail = "codex exited before emitting thread.started"
	if err := validateEvaluationInput(input); err != nil {
		t.Fatalf("validateEvaluationInput() error = %v", err)
	}
}

func fixtureCodexEvaluationInput() EvaluationInput {
	sessionID := "thread-fixture"
	return EvaluationInput{
		TodoID: 1, ExpectedVersion: 0,
		Route: RouteAuto, RouteReason: "codex_" + DispositionReady,
		MatchedRules:    []string{"codex_disposition:" + DispositionReady},
		DecisionEngine:  DecisionEngineCodex,
		CodexSessionID:  &sessionID,
		PromptVersion:   CodexPromptVersion,
		Plan:            json.RawMessage(`{"steps":["read the agent loop code"]}`),
		DecisionPayload: json.RawMessage(`{"reasoning":"leader asked for it directly"}`),
	}
}
