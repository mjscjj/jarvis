package capture

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

const testPrincipalOpenID = "ou_principal"

func TestAuthorizeCardApprovalDecodesApproveAndReject(t *testing.T) {
	for _, action := range []string{"approve", "reject"} {
		event := CardActionEvent{
			Type:        cardActionEventType,
			OperatorID:  testPrincipalOpenID,
			ActionTag:   "button",
			ActionValue: `{"action":"` + action + `","task_id":7}`,
		}
		got, err := AuthorizeCardApproval(event, testPrincipalOpenID)
		if err != nil {
			t.Fatalf("AuthorizeCardApproval(%s) error = %v", action, err)
		}
		if got.Action != action || got.TaskID != 7 {
			t.Fatalf("AuthorizeCardApproval(%s) = %#v", action, got)
		}
	}
}

func TestAuthorizeCardApprovalDecodesNamespacedAction(t *testing.T) {
	event := CardActionEvent{
		Type:        cardActionEventType,
		OperatorID:  testPrincipalOpenID,
		ActionTag:   "button",
		ActionValue: `{"action":"jarvis_approval","decision":"reject","task_id":7}`,
	}
	got, err := AuthorizeCardApproval(event, testPrincipalOpenID)
	if err != nil {
		t.Fatalf("AuthorizeCardApproval() error = %v", err)
	}
	if got.Action != "reject" || got.TaskID != 7 {
		t.Fatalf("AuthorizeCardApproval() = %#v", got)
	}
}

func TestAuthorizeCardApprovalRejectsNonPrincipalAndBadValues(t *testing.T) {
	tests := []struct {
		name      string
		event     CardActionEvent
		principal string
	}{
		{
			name:      "other clicker",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  "ou_someone_else",
				ActionTag:   "button",
				ActionValue: `{"action":"approve","task_id":7}`,
			},
		},
		{
			name:      "unconfigured principal",
			principal: "",
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "button",
				ActionValue: `{"action":"approve","task_id":7}`,
			},
		},
		{
			name:      "empty value",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "button",
				ActionValue: "",
			},
		},
		{
			name:      "unknown action",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "button",
				ActionValue: `{"action":"delete","task_id":7}`,
			},
		},
		{
			name:      "zero task id",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "button",
				ActionValue: `{"action":"approve","task_id":0}`,
			},
		},
		{
			name:      "malformed json",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "button",
				ActionValue: `{"action":`,
			},
		},
		{
			name:      "non button action",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "checker",
				ActionValue: `{"action":"approve","task_id":7}`,
			},
		},
		{
			name:      "form submit",
			principal: testPrincipalOpenID,
			event: CardActionEvent{
				OperatorID:  testPrincipalOpenID,
				ActionTag:   "button",
				ActionValue: `{"action":"approve","task_id":7}`,
				FormValue:   `{"reason":"x"}`,
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := AuthorizeCardApproval(testCase.event, testCase.principal); err == nil {
				t.Fatalf("AuthorizeCardApproval(%#v) unexpectedly succeeded", testCase.event)
			}
		})
	}
}

func TestValidateCardActionEventRejectsMalformedControlFields(t *testing.T) {
	tests := []struct {
		name  string
		event CardActionEvent
	}{
		{name: "wrong type", event: CardActionEvent{Type: "other", OperatorID: "ou_1"}},
		{name: "blank operator", event: CardActionEvent{Type: cardActionEventType, OperatorID: ""}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateCardActionEvent(testCase.event); err == nil {
				t.Fatalf("validateCardActionEvent(%#v) unexpectedly succeeded", testCase.event)
			}
		})
	}
}

func TestStartCardActionConsumerDeliversAndStopsByEOF(t *testing.T) {
	script := writeEventConsumerFixture(t, `#!/bin/sh
printf '%s\n' '[event] connecting' >&2
printf '%s\n' '[event] ready event_key=card.action.trigger' >&2
printf '%s\n' '{"type":"card.action.trigger","event_id":"evt_1","operator_id":"ou_principal","message_id":"om_1","action_tag":"button","action_value":"{\"action\":\"approve\",\"task_id\":7}"}'
cat >/dev/null
`)
	handler := &recordingCardActionHandler{received: make(chan struct{})}
	consumer, err := StartCardActionConsumer(
		context.Background(), handler,
		CardActionConsumerOptions{Bin: script, Profile: "cli_fixture", ReadyTimeout: 2 * time.Second},
		log.New(io.Discard, "", 0),
	)
	if err != nil {
		t.Fatalf("StartCardActionConsumer() error = %v", err)
	}
	select {
	case <-handler.received:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not deliver stdout event")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := consumer.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := consumer.Err(); err != nil {
		t.Fatalf("consumer.Err() = %v", err)
	}
	if got := handler.Last(); got.ActionValue == "" || !strings.Contains(got.ActionValue, `"task_id":7`) {
		t.Fatalf("handler last event = %#v", got)
	}
}

func TestStartCardActionConsumerSurfacesPreReadyFailure(t *testing.T) {
	script := writeEventConsumerFixture(t, `#!/bin/sh
printf '%s\n' '{"ok":false,"error":{"type":"validation","subtype":"failed_precondition"}}' >&2
exit 2
`)
	_, err := StartCardActionConsumer(
		context.Background(), &recordingCardActionHandler{},
		CardActionConsumerOptions{Bin: script, Profile: "cli_fixture", ReadyTimeout: 2 * time.Second},
		log.New(io.Discard, "", 0),
	)
	if err == nil || !strings.Contains(err.Error(), "failed_precondition") {
		t.Fatalf("StartCardActionConsumer() error = %v, want structured stderr", err)
	}
}

type recordingCardActionHandler struct {
	mu       sync.Mutex
	last     CardActionEvent
	received chan struct{}
	once     sync.Once
}

func (h *recordingCardActionHandler) HandleCardAction(_ context.Context, event CardActionEvent) error {
	h.mu.Lock()
	h.last = event
	h.mu.Unlock()
	if h.received != nil {
		h.once.Do(func() { close(h.received) })
	}
	return nil
}

func (h *recordingCardActionHandler) Last() CardActionEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}
