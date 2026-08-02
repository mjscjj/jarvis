package execute

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const codexFixtureTimeout = 30 * time.Second

func TestCodexDeciderUsesReadOnlyStructuredContract(t *testing.T) {
	resultJSON := `{"disposition":"ready","plan":{"summary":"inspect fixture","steps":["inspect"],"future_field":{"free":true}},"payload":{"summary":"ready","blocks":[{"kind":"evidence","label":"fixture","content":{"score":"not a fixed DTO"}}]}}`
	bin := writeCodexFixture(t, resultJSON, true)
	decider, err := NewCodexDecider(CodexOptions{Bin: bin, Model: "fixture-model", Timeout: codexFixtureTimeout, Sandbox: "read-only", ReasoningEffort: "low"})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	result, err := decider.Decide(context.Background(), CodexInput{Prompt: "judge synthetic Todo", RepoPath: t.TempDir()})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result.SessionID != "fixture-session" {
		t.Fatalf("result = %#v", result)
	}
	if got := string(result.Decision.Plan); !strings.Contains(got, `"future_field"`) {
		t.Fatalf("open plan lost fields: %s", got)
	}
	if got := string(result.Decision.Payload); !strings.Contains(got, `"score":"not a fixed DTO"`) {
		t.Fatalf("open payload lost fields: %s", got)
	}
}

func TestCodexDeciderRejectsMissingSession(t *testing.T) {
	resultJSON := `{"disposition":"drop","plan":null,"payload":{"summary":"不值得做","blocks":[{"kind":"reason","label":"理由","content":"闲聊，没有行动"}]}}`
	decider, err := NewCodexDecider(CodexOptions{Bin: writeCodexFixture(t, resultJSON, false), Model: "fixture-model", Timeout: codexFixtureTimeout, Sandbox: "read-only", ReasoningEffort: "low"})
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
		{name: "unknown shell field", raw: `{"disposition":"drop","plan":null,"payload":{"summary":"x"},"extra":1}`, want: "unknown field"},
		{name: "ready without plan", raw: `{"disposition":"ready","plan":null,"payload":{"summary":"x"}}`, want: "requires plan"},
		{name: "ready with empty plan", raw: `{"disposition":"ready","plan":{},"payload":{"summary":"x"}}`, want: "empty object"},
		{name: "missing payload", raw: `{"disposition":"drop","plan":null}`, want: "payload is required"},
		{name: "null payload", raw: `{"disposition":"drop","plan":null,"payload":null}`, want: "must not be null"},
		{name: "invalid disposition", raw: `{"disposition":"bogus","plan":null,"payload":{"summary":"x"}}`, want: "invalid disposition"},
		// The Todo-level confirmation queue is gone, so its dispositions must fail.
		{name: "retired need_review", raw: `{"disposition":"need_review","plan":{"a":1},"payload":{"summary":"x"}}`, want: "invalid disposition"},
		{name: "retired need_info", raw: `{"disposition":"need_info","plan":null,"payload":{"summary":"x"}}`, want: "invalid disposition"},
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

func TestDecodeCodexDecisionAcceptsOpenPlanAndPayload(t *testing.T) {
	tests := map[string]string{
		"string plan":   `{"disposition":"ready","plan":"调查、验证并汇报","payload":{"unknown":{"nested":[1,true]}}}`,
		"array plan":    `{"disposition":"ready","plan":["调查",{"verify":true}],"payload":["风险待确认",{"kind":"future_kind"}]}`,
		"open question": `{"disposition":"ready","plan":"先查清仓库再动手","payload":"需要 principal 确认目标仓库"}`,
		"drop":          `{"disposition":"drop","plan":null,"payload":"闲聊，不值得做"}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			decision, err := decodeCodexDecision([]byte(raw))
			if err != nil {
				t.Fatalf("decodeCodexDecision() error = %v", err)
			}
			if len(decision.Payload) == 0 {
				t.Fatal("payload was lost")
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
