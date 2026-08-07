package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"jarvis/internal/cardapproval"
	"jarvis/internal/execute"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeCardApprovalProcessor struct {
	event cardapproval.CardActionEvent
	card  json.RawMessage
	err   error
}

func (f *fakeCardApprovalProcessor) ProcessCardAction(_ context.Context, event cardapproval.CardActionEvent) (json.RawMessage, error) {
	f.event = event
	return f.card, f.err
}

func TestRelayCardApprovalAuthenticatesAndMapsNamespace(t *testing.T) {
	processor := &fakeCardApprovalProcessor{card: json.RawMessage(`{"schema":"2.0","body":{"elements":[]}}`)}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardApproval(processor, "relay-secret"))
	body := []byte(`{
		"event_id":"evt_1",
		"operator_id":"ou_principal",
		"message_id":"om_1",
		"chat_id":"oc_1",
		"action_tag":"button",
		"action_value":{"action":"jarvis_approval","decision":"approve","task_id":7,"version":12}
	}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: cardApprovalRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var action struct {
		Action  string `json:"action"`
		TaskID  uint64 `json:"task_id"`
		Version int32  `json:"version"`
	}
	if err := json.Unmarshal([]byte(processor.event.ActionValue), &action); err != nil {
		t.Fatalf("decode mapped action: %v", err)
	}
	if action.Action != "approve" || action.TaskID != 7 || action.Version != 12 || processor.event.MessageID != "om_1" {
		t.Fatalf("mapped event = %#v action=%#v", processor.event, action)
	}
}

func TestRelayCardApprovalRejectsBadSecret(t *testing.T) {
	processor := &fakeCardApprovalProcessor{}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardApproval(processor, "relay-secret"))
	body := []byte(`{"action_value":{"action":"jarvis_approval","decision":"approve","task_id":7}}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: cardApprovalRelaySecretHeader, Value: "wrong"},
	).Result()
	if response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if processor.event.OperatorID != "" {
		t.Fatalf("processor called for bad secret: %#v", processor.event)
	}
}

func TestRelayCardApprovalRejectsWrongNamespace(t *testing.T) {
	processor := &fakeCardApprovalProcessor{}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardApproval(processor, "relay-secret"))
	body := []byte(`{"action_value":{"action":"perm:allow","decision":"approve","task_id":7}}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: cardApprovalRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}

func TestRelayCardApprovalMapsExecutionConflict(t *testing.T) {
	processor := &fakeCardApprovalProcessor{err: execute.ErrInvalidTransition}
	h := server.New()
	h.POST("/internal/card-approval/callback", RelayCardApproval(processor, "relay-secret"))
	body := []byte(`{"action_value":{"action":"jarvis_approval","decision":"reject","task_id":7}}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/card-approval/callback",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: cardApprovalRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusConflict || !errors.Is(processor.err, execute.ErrInvalidTransition) {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
