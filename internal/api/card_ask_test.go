package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"jarvis/internal/cardask"
	"jarvis/internal/execute"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeCardAskProcessor struct {
	event cardask.CardActionEvent
	card  json.RawMessage
	err   error
}

func (f *fakeCardAskProcessor) ProcessCardAction(_ context.Context, event cardask.CardActionEvent) (json.RawMessage, error) {
	f.event = event
	return f.card, f.err
}

// TestRelayCardAskForwardsWholeClick pins that the relay authenticates and then
// hands the click through untouched: which button was pressed and what was
// typed or selected are the model's own vocabulary, so the transport must not
// reshape them.
func TestRelayCardAskForwardsWholeClick(t *testing.T) {
	processor := &fakeCardAskProcessor{card: json.RawMessage(`{"schema":"2.0","body":{"elements":[]}}`)}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardAsk(processor, "relay-secret"))
	body := []byte(`{
		"event_id":"evt_1",
		"operator_id":"ou_principal",
		"message_id":"om_1",
		"chat_id":"oc_1",
		"action_tag":"button",
		"action_value":{"action":"jarvis_approval","task_id":7,"version":12,"clicked":"send"},
		"form_value":{"chat":"评测群","note":"措辞再软一点"}
	}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: jarvisRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var action struct {
		Action  string `json:"action"`
		TaskID  uint64 `json:"task_id"`
		Version int32  `json:"version"`
		Clicked string `json:"clicked"`
	}
	if err := json.Unmarshal([]byte(processor.event.ActionValue), &action); err != nil {
		t.Fatalf("decode forwarded action: %v", err)
	}
	if action.Action != "jarvis_approval" || action.TaskID != 7 || action.Version != 12 || action.Clicked != "send" {
		t.Fatalf("forwarded action = %#v", action)
	}
	if processor.event.MessageID != "om_1" || processor.event.FormValue["note"] != "措辞再软一点" {
		t.Fatalf("forwarded event = %#v", processor.event)
	}
}

func TestRelayCardAskRejectsBadSecret(t *testing.T) {
	processor := &fakeCardAskProcessor{}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardAsk(processor, "relay-secret"))
	body := []byte(`{"action_value":{"action":"jarvis_approval","task_id":7,"clicked":"send"}}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: jarvisRelaySecretHeader, Value: "wrong"},
	).Result()
	if response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if processor.event.OperatorID != "" {
		t.Fatalf("processor called for bad secret: %#v", processor.event)
	}
}

func TestRelayCardAskMapsExecutionConflict(t *testing.T) {
	processor := &fakeCardAskProcessor{err: execute.ErrInvalidTransition}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardAsk(processor, "relay-secret"))
	body := []byte(`{"action_value":{"action":"jarvis_approval","task_id":7,"clicked":"send"}}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: jarvisRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusConflict || !errors.Is(processor.err, execute.ErrInvalidTransition) {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
