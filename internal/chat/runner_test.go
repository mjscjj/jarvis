package chat

import (
	"strings"
	"testing"
)

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
