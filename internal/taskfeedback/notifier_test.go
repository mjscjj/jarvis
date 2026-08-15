package taskfeedback

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

type fakeRunner struct {
	responses []any
	calls     [][]string
}

func (f *fakeRunner) Run(_ context.Context, out any, args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	response := map[string]any{}
	if len(f.responses) > 0 {
		response = f.responses[0].(map[string]any)
		f.responses = f.responses[1:]
	}
	raw, _ := json.Marshal(response)
	return json.Unmarshal(raw, out)
}

func TestNotifierRepliesOnceThenUpdatesSameMessage(t *testing.T) {
	runner := &fakeRunner{responses: []any{
		map[string]any{"data": map[string]any{"message_id": "om_progress"}},
		map[string]any{"data": map[string]any{}},
	}}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	delivery, err := notifier.ReplyProcessing(t.Context(), 17, execute.TaskFeedbackTarget{SourceMessageID: "om_source", ReplyInThread: true})
	if err != nil {
		t.Fatalf("ReplyProcessing() error = %v", err)
	}
	if delivery.MessageID != "om_progress" {
		t.Fatalf("delivery = %#v", delivery)
	}
	if err := notifier.Update(t.Context(), delivery.MessageID, "done", "已经处理完成"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(runner.calls))
	}
	first := strings.Join(runner.calls[0], "\n")
	for _, want := range []string{"+messages-reply", "om_source", "正在处理中", "jarvis-task-17-progress", "--reply-in-thread"} {
		if !strings.Contains(first, want) {
			t.Fatalf("reply args missing %q: %s", want, first)
		}
	}
	second := strings.Join(runner.calls[1], "\n")
	for _, want := range []string{"api\nPUT", "/open-apis/im/v1/messages/om_progress", "已经处理完成"} {
		if !strings.Contains(second, want) {
			t.Fatalf("update args missing %q: %s", want, second)
		}
	}
}

func TestNotifierMainChatReplyDoesNotForceThread(t *testing.T) {
	runner := &fakeRunner{responses: []any{map[string]any{"data": map[string]any{"message_id": "om_progress"}}}}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	if _, err := notifier.ReplyProcessing(t.Context(), 18, execute.TaskFeedbackTarget{SourceMessageID: "om_source"}); err != nil {
		t.Fatalf("ReplyProcessing() error = %v", err)
	}
	if got := strings.Join(runner.calls[0], "\n"); strings.Contains(got, "--reply-in-thread") {
		t.Fatalf("main-chat reply unexpectedly forced into thread: %s", got)
	}
}

func TestRenderStatus(t *testing.T) {
	for _, test := range []struct {
		status, summary, want string
	}{
		{"executing", "ignored", "正在处理中"},
		{"awaiting_approval", "方案已准备", "等待你的确认"},
		{"done", "一百万", "一百万"},
		{"failed", "调用失败", "处理失败：调用失败"},
	} {
		got, err := render(test.status, test.summary)
		if err != nil || !strings.Contains(got, test.want) {
			t.Fatalf("render(%s) = %q, %v; want %q", test.status, got, err, test.want)
		}
	}
}
