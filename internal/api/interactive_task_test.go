package api

import (
	"bytes"
	"context"
	"testing"

	"jarvis/internal/capture"
	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type interactiveCapturerStub struct {
	input capture.InteractiveMessage
}

func (s *interactiveCapturerStub) CaptureInteractive(_ context.Context, input capture.InteractiveMessage) (*domain.Message, error) {
	s.input = input
	groupID := uint64(7)
	return &domain.Message{MessageID: input.MessageID, ChatID: input.ChatID, GroupID: &groupID, Content: input.Content}, nil
}

type interactiveSubmitterStub struct {
	input taskcreate.Input
}

func (s *interactiveSubmitterStub) SubmitIdempotent(_ context.Context, input taskcreate.Input) (*domain.Task, bool, error) {
	s.input = input
	return &domain.Task{ID: 17, Status: "pending"}, true, nil
}

func TestRelayInteractiveTaskCarriesFetchedConversationIDs(t *testing.T) {
	capturer, submitter := &interactiveCapturerStub{}, &interactiveSubmitterStub{}
	h := server.New()
	h.POST("/internal/interactive-task", RelayInteractiveTask(capturer, submitter, "relay-secret"))
	body := []byte(`{
		"message_id":"om_anchor","chat_id":"oc_topic","chat_mode":"group","chat_name":"话题群",
		"sender_open_id":"ou_user","sender_name":"发起人","message_type":"text",
		"content":"当前请求","content_raw":"{\"text\":\"当前请求\"}","mentions":[],
		"parent_id":"om_previous","root_id":"om_root","thread_id":"omt_topic","create_time":3000,
		"recent_messages":[{
			"message_id":"om_previous","sender_open_id":"ou_other","sender_name":"同事","sender_type":"user",
			"message_type":"text","content":"上一条","content_raw":"{\"text\":\"上一条\"}","mentions":[],
			"parent_id":"","root_id":"om_root","thread_id":"omt_topic","create_time":2000
		}]
	}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/interactive-task", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: cardApprovalRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusCreated {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if len(capturer.input.RecentMessages) != 1 || capturer.input.RecentMessages[0].MessageID != "om_previous" {
		t.Fatalf("captured history = %#v", capturer.input.RecentMessages)
	}
	wantIDs := []string{"om_previous", "om_anchor"}
	if len(submitter.input.ConversationMessageIDs) != len(wantIDs) {
		t.Fatalf("conversation ids = %#v", submitter.input.ConversationMessageIDs)
	}
	for index := range wantIDs {
		if submitter.input.ConversationMessageIDs[index] != wantIDs[index] {
			t.Fatalf("conversation ids = %#v", submitter.input.ConversationMessageIDs)
		}
	}
	if submitter.input.ActorType != "user" || submitter.input.EventDetail["channel"] != "cc_connect" || submitter.input.EventDetail["message_id"] != "om_anchor" {
		t.Fatalf("Task event attribution = actor:%q detail:%#v", submitter.input.ActorType, submitter.input.EventDetail)
	}
}
