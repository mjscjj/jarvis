package decide

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexDeciderUsesReadOnlyStructuredContract(t *testing.T) {
	resultJSON := `{"confidence_factors":[{"name":"slots","score":0.9,"basis":"complete"}],"risk_factors":[{"name":"irreversible","score":0.2,"basis":"read only"}],"confidence_basis":"synthetic evidence","uncertainty_factors":[],"recommended_review":false,"proposed_plan":{"summary":"inspect fixture","steps":["inspect"],"parameters":[],"basis":[]},"plan_is_clear":true}`
	bin := writeCodexFixture(t, resultJSON, true)
	decider, err := NewCodexDecider(CodexOptions{Bin: bin, Model: "fixture-model", Timeout: 2 * time.Second, Budget: testCodexBudget(t)})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	result, err := decider.Decide(context.Background(), CodexInput{Prompt: "judge synthetic Todo", RepoPath: t.TempDir()})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result.SessionID != "fixture-session" || !result.Decision.PlanIsClear || result.Decision.ConfidenceFactors[0].Score != 0.9 || result.Budget.HourUsed != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Decision.ProposedPlan == nil || result.Decision.ProposedPlan.Steps[0] != "inspect" {
		t.Fatalf("proposed_plan = %#v", result.Decision.ProposedPlan)
	}
}

func TestCodexDeciderRejectsMissingSession(t *testing.T) {
	resultJSON := `{"confidence_factors":[{"name":"slots","score":0.9,"basis":"complete"}],"risk_factors":[{"name":"irreversible","score":0.2,"basis":"read only"}],"confidence_basis":"synthetic evidence","uncertainty_factors":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false}`
	decider, err := NewCodexDecider(CodexOptions{Bin: writeCodexFixture(t, resultJSON, false), Model: "fixture-model", Timeout: 2 * time.Second, Budget: testCodexBudget(t)})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	_, err = decider.Decide(context.Background(), CodexInput{Prompt: "judge synthetic Todo"})
	if err == nil || !strings.Contains(err.Error(), "thread.started") {
		t.Fatalf("Decide() error = %v, want missing thread.started", err)
	}
}

func TestCodexDeciderStopsBeforeCommandWhenBudgetExceeded(t *testing.T) {
	budget, err := NewCodexBudget(BudgetOptions{MaxCallsPerHour: 1, MaxCallsPerDay: 1})
	if err != nil {
		t.Fatalf("NewCodexBudget() error = %v", err)
	}
	if _, err := budget.Acquire(); err != nil {
		t.Fatalf("prime budget: %v", err)
	}
	decider, err := NewCodexDecider(CodexOptions{
		Bin: writeCodexFixture(t, `{}`, true), Model: "fixture-model", Timeout: 2 * time.Second, Budget: budget,
	})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	_, err = decider.Decide(context.Background(), CodexInput{Prompt: "must not execute"})
	if !errors.Is(err, ErrCodexBudgetExceeded) {
		t.Fatalf("Decide() error = %v, want ErrCodexBudgetExceeded", err)
	}
}

func testCodexBudget(t *testing.T) *CodexBudget {
	t.Helper()
	budget, err := NewCodexBudget(BudgetOptions{MaxCallsPerHour: 10, MaxCallsPerDay: 20})
	if err != nil {
		t.Fatalf("NewCodexBudget() error = %v", err)
	}
	return budget
}

func TestDecodeCodexDecisionFailsFast(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "unknown field", raw: `{"confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","uncertainty_factors":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false,"extra":1}`, want: "unknown field"},
		{name: "score out of range", raw: `{"confidence_factors":[{"name":"a","score":2,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","uncertainty_factors":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false}`, want: "outside [0,1]"},
		{name: "clear without plan", raw: `{"confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","uncertainty_factors":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":true}`, want: "requires proposed_plan"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeCodexDecision([]byte(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func writeCodexFixture(t *testing.T, resultJSON string, emitSession bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-fixture")
	sessionLine := `printf '%s\n' '{"type":"turn.completed"}'`
	if emitSession {
		sessionLine = `printf '%s\n' '{"type":"thread.started","thread_id":"fixture-session"}'`
	}
	script := `#!/bin/sh
output=''
schema=''
read_only='false'
json_mode='false'
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) output="$2"; shift 2 ;;
    --output-schema) schema="$2"; shift 2 ;;
    --sandbox) [ "$2" = "read-only" ] && read_only='true'; shift 2 ;;
    --json) json_mode='true'; shift ;;
    *) shift ;;
  esac
done
[ -s "$schema" ] || exit 21
[ "$read_only" = "true" ] || exit 22
[ "$json_mode" = "true" ] || exit 23
cat >/dev/null
` + sessionLine + `
printf '%s' '` + resultJSON + `' > "$output"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fixture codex: %v", err)
	}
	return path
}
