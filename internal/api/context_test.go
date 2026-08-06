package api

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"jarvis/internal/contextsnap"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type contextAssemblerStub struct {
	options contextsnap.AssembleOptions
}

func (s *contextAssemblerStub) AssembleConversation(_ context.Context, options contextsnap.AssembleOptions) (json.RawMessage, error) {
	s.options = options
	return json.RawMessage(`{"snapshot_version":"v1","principal":{"name":"我"}}`), nil
}

func TestAssembleContextPassesScopeAndReturnsJSON(t *testing.T) {
	stub := &contextAssemblerStub{}
	h := server.New()
	h.POST("/api/context", AssembleContext(stub))
	body := []byte(`{"chat_id":"oc_runtime","project_id":45,"request_context":{"message_id":"om_1"}}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/context", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status = %d, body = %s", response.StatusCode(), response.Body())
	}
	if stub.options.ChatID != "oc_runtime" || stub.options.ProjectID == nil || *stub.options.ProjectID != 45 || string(stub.options.RequestContext) != `{"message_id":"om_1"}` {
		t.Fatalf("options = %#v", stub.options)
	}
	var payload struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || !bytes.Contains(payload.Data, []byte(`"snapshot_version":"v1"`)) {
		t.Fatalf("payload = %s", response.Body())
	}
}

func TestAssembleContextRejectsUnknownFields(t *testing.T) {
	h := server.New()
	h.POST("/api/context", AssembleContext(&contextAssemblerStub{}))
	body := []byte(`{"chat_id":"oc_runtime","unexpected":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/context", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != 400 {
		t.Fatalf("status = %d, body = %s", response.StatusCode(), response.Body())
	}
}
