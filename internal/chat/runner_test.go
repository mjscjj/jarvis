package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunnerPreservesStartupError(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "codex")
	const detail = "Error: thread/resume failed: no rollout found for thread id old-traex-thread"
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' '"+detail+"' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner, err := newRunner(bin, "fixture-model", "read-only", "medium", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = runner.Stream(context.Background(), "你好", "old-traex-thread", "", func(Event) error {
		t.Fatal("failed startup must not emit a chat event")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), detail) {
		t.Fatalf("error = %v, want original CLI startup error", err)
	}
	if strings.Contains(err.Error(), "missing thread.started") {
		t.Fatalf("startup failure was masked by JSONL validation: %v", err)
	}
}

func TestRunnerArgsIncludeImageForNewAndResumedTurns(t *testing.T) {
	t.Parallel()
	runner := &runner{
		model:           "fixture-model",
		sandbox:         "read-only",
		reasoningEffort: "high",
	}
	for _, test := range []struct {
		name     string
		threadID string
	}{
		{name: "new turn"},
		{name: "resumed turn", threadID: "thread-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := runner.args(test.threadID, "/tmp/screenshot.png")
			joined := strings.Join(args, "\x00")
			if !strings.Contains(joined, "--image\x00/tmp/screenshot.png") {
				t.Fatalf("args = %q, want image path", args)
			}
			if args[len(args)-1] != "-" {
				t.Fatalf("args = %q, stdin prompt marker must remain last", args)
			}
		})
	}
	if args := runner.args("", ""); slices.Contains(args, "--image") {
		t.Fatalf("args = %q, image flag must be absent without an image", args)
	}
}

func TestIsUnresumableThread(t *testing.T) {
	t.Parallel()
	if !isUnresumableThread(fmt.Errorf("codex chat exited abnormally: exit status 1: Error: thread/resume: thread/resume failed: no rollout found for thread id old-id (code -32600)")) {
		t.Fatal("want unresumable for missing Codex rollout")
	}
	if isUnresumableThread(fmt.Errorf("codex chat exited abnormally: exit status 1: auth failed")) {
		t.Fatal("auth failure must not be treated as a missing thread")
	}
	if isUnresumableThread(nil) {
		t.Fatal("nil error is not unresumable")
	}
}

// realCodexJSONL 是实跑 codex `exec --json`（gpt-5.5）灌一句 prompt 后的真实
// stdout 样本（见包注释）。用它锚定解析：thread_id 来自 thread.started，
// 对话文本来自 item.completed 的 agent_message.text（此构建不发 item.delta）。
const realCodexJSONL = `{"type":"thread.started","thread_id":"019f7b20-ae0a-7fd3-b338-b4bd706d7fcd"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"1+1等于2。"}}
{"type":"turn.completed","usage":{"input_tokens":22302,"cached_input_tokens":5504,"output_tokens":56,"reasoning_output_tokens":43}}
`

func collect(t *testing.T, jsonl string) (threadID string, deltas []string, err error) {
	t.Helper()
	emit := func(ev Event) error {
		switch ev.Kind {
		case EventThread:
			threadID = ev.ThreadID
		case EventDelta:
			deltas = append(deltas, ev.Text)
		default:
			t.Fatalf("unexpected event kind %q", ev.Kind)
		}
		return nil
	}
	err = parseCodexStream(strings.NewReader(jsonl), emit)
	return threadID, deltas, err
}

func TestParseCodexStreamRealSample(t *testing.T) {
	t.Parallel()
	threadID, deltas, err := collect(t, realCodexJSONL)
	if err != nil {
		t.Fatalf("parseCodexStream() error = %v", err)
	}
	if threadID != "019f7b20-ae0a-7fd3-b338-b4bd706d7fcd" {
		t.Fatalf("thread_id = %q, want the sample thread_id", threadID)
	}
	if len(deltas) != 1 || deltas[0] != "1+1等于2。" {
		t.Fatalf("deltas = %v, want single %q", deltas, "1+1等于2。")
	}
}

// 兼容开启流式增量的 codex 构建：item.delta 逐条吐，且不重复发 completed
// （codex 对同一消息要么发 delta 要么发 completed，二选一）。
func TestParseCodexStreamDeltaEvents(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"thread.started","thread_id":"tid-1"}
{"type":"turn.started"}
{"type":"item.delta","delta":"你好"}
{"type":"item.delta","delta":"，世界"}
{"type":"item.delta","delta":""}
{"type":"turn.completed","usage":{}}
`
	threadID, deltas, err := collect(t, jsonl)
	if err != nil {
		t.Fatalf("parseCodexStream() error = %v", err)
	}
	if threadID != "tid-1" {
		t.Fatalf("thread_id = %q, want tid-1", threadID)
	}
	want := []string{"你好", "，世界"}
	if len(deltas) != len(want) {
		t.Fatalf("deltas = %v, want %v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Fatalf("deltas[%d] = %q, want %q", i, deltas[i], want[i])
		}
	}
}

// item.completed 里非 agent_message 的 item（如工具/命令）不进对话流。
func TestParseCodexStreamIgnoresNonAgentItems(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"thread.started","thread_id":"tid-2"}
{"type":"item.completed","item":{"type":"command_execution","text":"ls -la"}}
{"type":"item.completed","item":{"type":"agent_message","text":"完成"}}
`
	_, deltas, err := collect(t, jsonl)
	if err != nil {
		t.Fatalf("parseCodexStream() error = %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "完成" {
		t.Fatalf("deltas = %v, want single %q", deltas, "完成")
	}
}

// fail-fast：缺 thread.started 视为错误，不静默返回空 thread_id。
func TestParseCodexStreamMissingThread(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}
`
	_, _, err := collect(t, jsonl)
	if err == nil || !strings.Contains(err.Error(), "thread.started") {
		t.Fatalf("err = %v, want missing thread.started error", err)
	}
}

// fail-fast：thread.started 缺 thread_id 是错误。
func TestParseCodexStreamBlankThreadID(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"thread.started","thread_id":""}
`
	_, _, err := collect(t, jsonl)
	if err == nil || !strings.Contains(err.Error(), "thread_id") {
		t.Fatalf("err = %v, want missing thread_id error", err)
	}
}

// fail-fast：坏 JSON 直接报错，不吞。
func TestParseCodexStreamMalformedJSON(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"thread.started","thread_id":"tid"}
{not json}
`
	_, _, err := collect(t, jsonl)
	if err == nil || !strings.Contains(err.Error(), "decode codex JSONL stream") {
		t.Fatalf("err = %v, want decode error", err)
	}
}

const realCursorJSONL = `{"type":"system","subtype":"init","session_id":"213a0df3-b9a4-4bc8-9eae-a5da002441d1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"CURSOR_"}]},"session_id":"213a0df3-b9a4-4bc8-9eae-a5da002441d1","timestamp_ms":1}
{"type":"tool_call","subtype":"completed","session_id":"213a0df3-b9a4-4bc8-9eae-a5da002441d1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}]},"session_id":"213a0df3-b9a4-4bc8-9eae-a5da002441d1","timestamp_ms":2}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"CURSOR_OK"}]},"session_id":"213a0df3-b9a4-4bc8-9eae-a5da002441d1"}
{"type":"result","subtype":"success","is_error":false,"result":"CURSOR_OK","session_id":"213a0df3-b9a4-4bc8-9eae-a5da002441d1"}
`

func TestParseCursorStreamRealSample(t *testing.T) {
	t.Parallel()
	var threadID string
	var deltas []string
	err := parseCursorStream(strings.NewReader(realCursorJSONL), func(event Event) error {
		switch event.Kind {
		case EventThread:
			threadID = event.ThreadID
		case EventDelta:
			deltas = append(deltas, event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "cursor_213a0df3-b9a4-4bc8-9eae-a5da002441d1" {
		t.Fatalf("thread_id = %q", threadID)
	}
	if !slices.Equal(deltas, []string{"CURSOR_", "OK"}) {
		t.Fatalf("deltas = %#v, want partial chunks without duplicate final text", deltas)
	}
}

func TestCursorRunnerArgsAndChannelOwnership(t *testing.T) {
	t.Parallel()
	runner := &runner{provider: providerCursor, model: "claude-opus-5-high", timeout: time.Second}
	args := runner.args("cursor_213a0df3-b9a4-4bc8-9eae-a5da002441d1", "/tmp/screenshot.png")
	joined := strings.Join(args, "\x00")
	for _, want := range []string{
		"--model\x00claude-opus-5-high",
		"--force\x00--sandbox\x00disabled",
		"--approve-mcps\x00--trust",
		"--resume\x00213a0df3-b9a4-4bc8-9eae-a5da002441d1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args = %q, missing %q", args, want)
		}
	}
	if strings.Contains(joined, "/tmp/screenshot.png") {
		t.Fatalf("Cursor image path belongs in the prompt, not args: %q", args)
	}
	if err := runner.Stream(context.Background(), "hello", "old-codex-thread", "", func(Event) error { return nil }); !errors.Is(err, errUnresumableThread) {
		t.Fatalf("cross-channel Stream error = %v, want errUnresumableThread", err)
	}
}

func TestParseCursorStreamWithoutPartialsUsesCompleteMessage(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"sid"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"完整答案"}]}}
{"type":"result","is_error":false}
`
	var deltas []string
	err := parseCursorStream(strings.NewReader(jsonl), func(event Event) error {
		if event.Kind == EventDelta {
			deltas = append(deltas, event.Text)
		}
		return nil
	})
	if err != nil || !slices.Equal(deltas, []string{"完整答案"}) {
		t.Fatalf("deltas=%#v err=%v", deltas, err)
	}
}

func TestNewRunnerValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		bin             string
		model           string
		sandbox         string
		reasoningEffort string
		wantErr         string
	}{
		{name: "blank bin", bin: "", model: "m", sandbox: "read-only", reasoningEffort: "low", wantErr: "bin is required"},
		{name: "bad sandbox", bin: "codex", model: "m", sandbox: "nope", reasoningEffort: "low", wantErr: "sandbox must be"},
		{name: "bad reasoning", bin: "codex", model: "m", sandbox: "read-only", reasoningEffort: "nope", wantErr: "reasoning_effort must be"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := newRunner(tt.bin, tt.model, tt.sandbox, tt.reasoningEffort, 1)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("newRunner() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
