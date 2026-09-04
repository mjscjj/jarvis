package cardask

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

type fakeResumer struct {
	taskID   uint64
	version  int32
	response string
	channel  string
	calls    int
	err      error
}

func (f *fakeResumer) KickResumeAfterHuman(_ context.Context, taskID uint64, version int32, response, channel string) (*execute.ExecuteResult, error) {
	f.calls++
	f.taskID, f.version, f.response, f.channel = taskID, version, response, channel
	if f.err != nil {
		return nil, f.err
	}
	return &execute.ExecuteResult{TaskID: taskID, Status: "executing"}, nil
}

type fakeSnapshots struct{ err error }

func (f *fakeSnapshots) QuestionSnapshot(context.Context, uint64) (execute.QuestionNotification, error) {
	if f.err != nil {
		return execute.QuestionNotification{}, f.err
	}
	return testNotice(), nil
}

type fakeCards struct{ outcome string }

func (f *fakeCards) AnsweredCard(_ execute.QuestionNotification, outcome string) (json.RawMessage, error) {
	f.outcome = outcome
	return json.RawMessage(`{"schema":"2.0"}`), nil
}

func testHandler(t *testing.T, resumer Resumer) (*Handler, *fakeCards) {
	t.Helper()
	cards := &fakeCards{}
	handler, err := NewRelayHandler(resumer, &fakeSnapshots{}, cards, "ou_principal", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewRelayHandler() error = %v", err)
	}
	return handler, cards
}

func clickEvent() CardActionEvent {
	return CardActionEvent{
		EventID: "evt_1", OperatorID: "ou_principal", MessageID: "om_1", ChatID: "oc_1",
		ActionTag:   "button",
		ActionValue: `{"action":"jarvis_approval","task_id":7,"version":12,"clicked":"send"}`,
		FormValue: map[string]any{
			"send": "发出去", "chat": "评测群", "note": "措辞再软一点",
		},
	}
}

// TestProcessCardActionHandsWholeAnswerToTheSession is the core of the ask
// mechanism: Jarvis interprets nothing. The button name and every field value
// go back to the Codex session that wrote the question, which decides what they
// mean.
func TestProcessCardActionHandsWholeAnswerToTheSession(t *testing.T) {
	resumer := &fakeResumer{}
	handler, cards := testHandler(t, resumer)
	if _, err := handler.ProcessCardAction(context.Background(), clickEvent()); err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if resumer.taskID != 7 || resumer.version != 12 || resumer.channel != "feishu_card" {
		t.Fatalf("resume = %#v", resumer)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(resumer.response), &answer); err != nil {
		t.Fatalf("answer is not JSON: %q", resumer.response)
	}
	if answer["clicked"] != "send" || answer["chat"] != "评测群" || answer["note"] != "措辞再软一点" {
		t.Fatalf("answer = %#v", answer)
	}
	// Feishu echoes the submit button into form_value; "clicked" is the single
	// answer to which button was pressed.
	if _, echoed := answer["send"]; echoed {
		t.Fatalf("answer must not repeat the clicked button as a field: %#v", answer)
	}
	if !strings.Contains(cards.outcome, "评测群") {
		t.Fatalf("answered card must echo the choice: %q", cards.outcome)
	}
}

// TestProcessCardActionRejectsForeignOperator keeps the card usable only by the
// principal, whatever it happens to ask.
func TestProcessCardActionRejectsForeignOperator(t *testing.T) {
	resumer := &fakeResumer{}
	handler, _ := testHandler(t, resumer)
	event := clickEvent()
	event.OperatorID = "ou_someone_else"
	if _, err := handler.ProcessCardAction(context.Background(), event); err == nil {
		t.Fatal("a non-principal click must be refused")
	}
	if resumer.calls != 0 {
		t.Fatalf("resume ran for a foreign operator: %#v", resumer)
	}
}

func TestProcessCardActionRejectsMalformedClicks(t *testing.T) {
	cases := map[string]func(*CardActionEvent){
		"not a button": func(e *CardActionEvent) { e.ActionTag = "select_static" },
		"empty value":  func(e *CardActionEvent) { e.ActionValue = "" },
		"foreign namespace": func(e *CardActionEvent) {
			e.ActionValue = `{"action":"perm:allow","task_id":7,"version":12,"clicked":"send"}`
		},
		"no clicked button": func(e *CardActionEvent) {
			e.ActionValue = `{"action":"jarvis_approval","task_id":7,"version":12,"clicked":""}`
		},
		"no task": func(e *CardActionEvent) {
			e.ActionValue = `{"action":"jarvis_approval","task_id":0,"version":12,"clicked":"send"}`
		},
		"no version": func(e *CardActionEvent) {
			e.ActionValue = `{"action":"jarvis_approval","task_id":7,"version":0,"clicked":"send"}`
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			resumer := &fakeResumer{}
			handler, _ := testHandler(t, resumer)
			event := clickEvent()
			mutate(&event)
			if _, err := handler.ProcessCardAction(context.Background(), event); err == nil {
				t.Fatalf("%s must be refused", name)
			}
			if resumer.calls != 0 {
				t.Fatalf("%s reached the executor", name)
			}
		})
	}
}

// TestProcessCardActionTreatsStaleClickAsAlreadyAnswered: the card carries the
// Task version it was rendered for, so a second click loses the optimistic lock
// and must read as "already handled" rather than as a failure.
func TestProcessCardActionTreatsStaleClickAsAlreadyAnswered(t *testing.T) {
	handler, cards := testHandler(t, &fakeResumer{err: execute.ErrVersionConflict})
	card, err := handler.ProcessCardAction(context.Background(), clickEvent())
	if err != nil {
		t.Fatalf("stale click error = %v, want the answered card", err)
	}
	if card == nil || !strings.Contains(cards.outcome, "已经回答过") {
		t.Fatalf("stale outcome = %q", cards.outcome)
	}
}
