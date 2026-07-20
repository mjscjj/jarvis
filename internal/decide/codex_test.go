package decide

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexDeciderUsesReadOnlyStructuredContract(t *testing.T) {
	resultJSON := `{"disposition":"ready","confidence_factors":[{"name":"slots","score":0.9,"basis":"complete"}],"risk_factors":[{"name":"irreversible","score":0.2,"basis":"read only"}],"confidence_basis":"synthetic evidence","clarifications":[],"recommended_review":false,"proposed_plan":{"summary":"inspect fixture","steps":["inspect"],"parameters":[],"basis":[]},"plan_is_clear":true,"evidence_gathered":[]}`
	bin := writeCodexFixture(t, resultJSON, true)
	decider, err := NewCodexDecider(CodexOptions{Bin: bin, Model: "fixture-model", Timeout: 10 * time.Second, Sandbox: "read-only", ReasoningEffort: "low"})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	result, err := decider.Decide(context.Background(), CodexInput{Prompt: "judge synthetic Todo", RepoPath: t.TempDir()})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result.SessionID != "fixture-session" || !result.Decision.PlanIsClear || result.Decision.ConfidenceFactors[0].Score != 0.9 {
		t.Fatalf("result = %#v", result)
	}
	if result.Decision.ProposedPlan == nil || result.Decision.ProposedPlan.Steps[0] != "inspect" {
		t.Fatalf("proposed_plan = %#v", result.Decision.ProposedPlan)
	}
}

func TestCodexDeciderRejectsMissingSession(t *testing.T) {
	resultJSON := `{"disposition":"need_info","confidence_factors":[{"name":"slots","score":0.9,"basis":"complete"}],"risk_factors":[{"name":"irreversible","score":0.2,"basis":"read only"}],"confidence_basis":"synthetic evidence","clarifications":[{"question":"需要哪些信息?","hint":""}],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false,"evidence_gathered":[]}`
	decider, err := NewCodexDecider(CodexOptions{Bin: writeCodexFixture(t, resultJSON, false), Model: "fixture-model", Timeout: 10 * time.Second, Sandbox: "read-only", ReasoningEffort: "low"})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	_, err = decider.Decide(context.Background(), CodexInput{Prompt: "judge synthetic Todo"})
	if err == nil || !strings.Contains(err.Error(), "thread.started") {
		t.Fatalf("Decide() error = %v, want missing thread.started", err)
	}
}

func TestDecodeCodexDecisionFailsFast(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "unknown field", raw: `{"confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","clarifications":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false,"extra":1}`, want: "unknown field"},
		{name: "score out of range", raw: `{"confidence_factors":[{"name":"a","score":2,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","clarifications":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false}`, want: "outside [0,1]"},
		{name: "ready without plan", raw: `{"disposition":"ready","confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","clarifications":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":true,"evidence_gathered":[]}`, want: "requires proposed_plan"},
		{name: "need_info without clarification", raw: `{"disposition":"need_info","confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","clarifications":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false,"evidence_gathered":[]}`, want: "requires at least one clarification"},
		{name: "invalid disposition", raw: `{"disposition":"bogus","confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","clarifications":[],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false,"evidence_gathered":[]}`, want: "invalid disposition"},
		{name: "clarification blank question", raw: `{"confidence_factors":[{"name":"a","score":1,"basis":"x"}],"risk_factors":[{"name":"b","score":1,"basis":"x"}],"confidence_basis":"x","clarifications":[{"question":"  ","hint":""}],"recommended_review":false,"proposed_plan":null,"plan_is_clear":false}`, want: "question is blank"},
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

// codexSessionID 必须能处理超长单行 JSONL：investigate 类任务 traex 会把大段工具
// 输出塞进一条事件，之前用 bufio.Scanner+1MB 上限会报 "token too long" 死循环。
func TestCodexSessionIDHandlesHugeLine(t *testing.T) {
	hugeText, err := json.Marshal(strings.Repeat("x", 4<<20)) // 4MB，远超旧 1MB 上限
	if err != nil {
		t.Fatalf("marshal huge text: %v", err)
	}
	stream := `{"type":"thread.started","thread_id":"sess-huge"}` + "\n" +
		`{"type":"item.completed","item":{"type":"command_output","text":` + string(hugeText) + `}}` + "\n" +
		`{"type":"turn.completed"}` + "\n"
	sessionID, err := codexSessionID([]byte(stream))
	if err != nil {
		t.Fatalf("codexSessionID() error = %v", err)
	}
	if sessionID != "sess-huge" {
		t.Fatalf("sessionID = %q, want sess-huge", sessionID)
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
