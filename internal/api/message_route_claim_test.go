package api

import (
	"bytes"
	"context"
	"testing"

	"jarvis/internal/capture"
	"jarvis/internal/domain"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type messageRouteClaimerStub struct {
	input capture.RouteClaimMessage
}

func (s *messageRouteClaimerStub) CaptureRouteClaim(_ context.Context, input capture.RouteClaimMessage) (*domain.Message, error) {
	s.input = input
	return &domain.Message{MessageID: input.MessageID, ExtractionSkipped: true}, nil
}

func TestClaimMessageRouteClaimsOnlyCurrentMessage(t *testing.T) {
	claimer := &messageRouteClaimerStub{}
	h := server.New()
	h.POST("/internal/message-routing/claim", ClaimMessageRoute(claimer, "relay-secret"))
	body := []byte(`{
		"message_id":"om_anchor","chat_id":"oc_topic","chat_mode":"group","chat_name":"话题群",
		"sender_open_id":"ou_user","sender_name":"发起人","message_type":"text",
		"content":"当前请求","content_raw":"{\"text\":\"当前请求\"}","mentions":[],
		"parent_id":"om_previous","root_id":"om_root","thread_id":"omt_topic","create_time":3000
	}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/message-routing/claim", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: jarvisRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if claimer.input.MessageID != "om_anchor" || claimer.input.ThreadID != "omt_topic" {
		t.Fatalf("claim input = %#v", claimer.input)
	}
}

func TestClaimMessageRouteRejectsHistoryAndWrongSecret(t *testing.T) {
	for _, test := range []struct {
		name   string
		secret string
		body   string
		status int
	}{
		{name: "history", secret: "relay-secret", body: `{"message_id":"om_1","recent_messages":[]}`, status: consts.StatusBadRequest},
		{name: "secret", secret: "wrong", body: `{}`, status: consts.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := server.New()
			h.POST("/internal/message-routing/claim", ClaimMessageRoute(&messageRouteClaimerStub{}, "relay-secret"))
			body := []byte(test.body)
			response := ut.PerformRequest(
				h.Engine, "POST", "/internal/message-routing/claim", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
				ut.Header{Key: jarvisRelaySecretHeader, Value: test.secret},
			).Result()
			if response.StatusCode() != test.status {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
}
