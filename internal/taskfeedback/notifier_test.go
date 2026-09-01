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

func TestNotifierAddsOnItReactionAsBot(t *testing.T) {
	runner := &fakeRunner{responses: []any{
		map[string]any{"data": map[string]any{"reaction_id": "reaction_on_it"}},
	}}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	reaction, err := notifier.AddProcessingReaction(t.Context(), execute.TaskFeedbackTarget{SourceMessageID: "om_source"})
	if err != nil {
		t.Fatalf("AddProcessingReaction() error = %v", err)
	}
	if reaction.ReactionID != "reaction_on_it" {
		t.Fatalf("reaction = %#v", reaction)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.calls))
	}
	reactionCall := strings.Join(runner.calls[0], "\n")
	for _, want := range []string{"im\nreactions\ncreate", `"message_id":"om_source"`, `"emoji_type":"OnIt"`, "--as\nbot"} {
		if !strings.Contains(reactionCall, want) {
			t.Fatalf("reaction args missing %q: %s", want, reactionCall)
		}
	}
}

func TestNotifierRejectsMissingReactionID(t *testing.T) {
	runner := &fakeRunner{responses: []any{map[string]any{"data": map[string]any{}}}}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	if _, err := notifier.AddProcessingReaction(t.Context(), execute.TaskFeedbackTarget{SourceMessageID: "om_source"}); err == nil {
		t.Fatal("AddProcessingReaction() error = nil, want missing reaction_id rejected")
	}
}

func TestNotifierRejectsMissingSourceMessageID(t *testing.T) {
	runner := &fakeRunner{}
	notifier, err := NewNotifier(runner)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	if _, err := notifier.AddProcessingReaction(t.Context(), execute.TaskFeedbackTarget{}); err == nil {
		t.Fatal("AddProcessingReaction() error = nil, want empty source rejected")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("empty source reached Feishu: %#v", runner.calls)
	}
}
