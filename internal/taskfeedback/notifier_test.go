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

func TestNotifierAddsOnItThenRepliesAndUpdatesResult(t *testing.T) {
	runner := &fakeRunner{responses: []any{
		map[string]any{"data": map[string]any{"reaction_id": "reaction_on_it"}},
		map[string]any{"data": map[string]any{"message_id": "om_result"}},
		map[string]any{"data": map[string]any{}},
	}}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	reaction, err := notifier.AddProcessingReaction(t.Context(), execute.TaskFeedbackTarget{SourceMessageID: "om_source", ReplyInThread: true})
	if err != nil {
		t.Fatalf("AddProcessingReaction() error = %v", err)
	}
	if reaction.ReactionID != "reaction_on_it" {
		t.Fatalf("reaction = %#v", reaction)
	}
	delivery, err := notifier.ReplyResult(t.Context(), 17, execute.TaskFeedbackTarget{SourceMessageID: "om_source", ReplyInThread: true}, "已经处理完成")
	if err != nil {
		t.Fatalf("ReplyResult() error = %v", err)
	}
	if delivery.MessageID != "om_result" || delivery.Preview != "已经处理完成" {
		t.Fatalf("delivery = %#v", delivery)
	}
	if preview, err := notifier.UpdateResult(t.Context(), delivery.MessageID, "已经更新结果"); err != nil || preview != "已经更新结果" {
		t.Fatalf("UpdateResult() = %q, %v", preview, err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(runner.calls))
	}
	reactionCall := strings.Join(runner.calls[0], "\n")
	for _, want := range []string{"im\nreactions\ncreate", `"message_id":"om_source"`, `"emoji_type":"OnIt"`, "--as\nbot"} {
		if !strings.Contains(reactionCall, want) {
			t.Fatalf("reaction args missing %q: %s", want, reactionCall)
		}
	}
	replyCall := strings.Join(runner.calls[1], "\n")
	for _, want := range []string{"+messages-reply", "om_source", "已经处理完成", "jarvis-task-17-result", "--reply-in-thread"} {
		if !strings.Contains(replyCall, want) {
			t.Fatalf("reply args missing %q: %s", want, replyCall)
		}
	}
	updateCall := strings.Join(runner.calls[2], "\n")
	for _, want := range []string{"api\nPUT", "/open-apis/im/v1/messages/om_result", "已经更新结果"} {
		if !strings.Contains(updateCall, want) {
			t.Fatalf("update args missing %q: %s", want, updateCall)
		}
	}
}

func TestNotifierMainChatResultDoesNotForceThread(t *testing.T) {
	runner := &fakeRunner{responses: []any{map[string]any{"data": map[string]any{"message_id": "om_result"}}}}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	if _, err := notifier.ReplyResult(t.Context(), 18, execute.TaskFeedbackTarget{SourceMessageID: "om_source"}, "完成"); err != nil {
		t.Fatalf("ReplyResult() error = %v", err)
	}
	if got := strings.Join(runner.calls[0], "\n"); strings.Contains(got, "--reply-in-thread") {
		t.Fatalf("main-chat reply unexpectedly forced into thread: %s", got)
	}
}

// The notifier never invents text of its own: an empty user_message must reach
// Feishu as no call at all, not as a status placeholder like "处理完成。".
func TestNotifierRefusesEmptyUserMessage(t *testing.T) {
	runner := &fakeRunner{}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	if _, err := notifier.ReplyResult(t.Context(), 19, execute.TaskFeedbackTarget{SourceMessageID: "om_source"}, "  "); err == nil {
		t.Fatal("ReplyResult() error = nil, want empty text rejected")
	}
	if _, err := notifier.UpdateResult(t.Context(), "om_result", ""); err == nil {
		t.Fatal("UpdateResult() error = nil, want empty text rejected")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("empty user_message reached Feishu: %#v", runner.calls)
	}
}
