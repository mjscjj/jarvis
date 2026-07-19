package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/decide"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeConfirmationService struct {
	approveInput decide.ApproveInput
	rejectInput  decide.RejectInput
	approveErr   error
	rejectErr    error
}

func (f *fakeConfirmationService) Approve(_ context.Context, input decide.ApproveInput) (*decide.TaskView, error) {
	f.approveInput = input
	if f.approveErr != nil {
		return nil, f.approveErr
	}
	return &decide.TaskView{ID: 11, TodoID: input.TodoID, Plan: input.Plan, ConfirmedAt: time.Unix(1, 0)}, nil
}

func (f *fakeConfirmationService) Reject(_ context.Context, input decide.RejectInput) (*decide.RejectResult, error) {
	f.rejectInput = input
	if f.rejectErr != nil {
		return nil, f.rejectErr
	}
	return &decide.RejectResult{TodoID: input.TodoID, Status: "dismissed", Version: input.ExpectedVersion + 1}, nil
}

func TestApproveConfirmation(t *testing.T) {
	service := &fakeConfirmationService{}
	h := server.New()
	h.POST("/api/confirmations/:todo_id/approve", ApproveConfirmation(service))

	body := []byte(`{"expected_version":0,"plan":{"steps":["draft"]}}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/confirmations/7/approve", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.approveInput.TodoID != 7 || service.approveInput.ExpectedVersion != 0 || service.approveInput.Channel != "backend" {
		t.Fatalf("input = %#v", service.approveInput)
	}
	var plan map[string]any
	if err := json.Unmarshal(service.approveInput.Plan, &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
}

func TestApproveConfirmationRequiresVersionAndRejectsUnknownFields(t *testing.T) {
	service := &fakeConfirmationService{}
	h := server.New()
	h.POST("/api/confirmations/:todo_id/approve", ApproveConfirmation(service))

	for _, body := range [][]byte{
		[]byte(`{"plan":{"steps":["draft"]}}`),
		[]byte(`{"expected_version":0,"plan":{"steps":["draft"]},"force":true}`),
	} {
		response := ut.PerformRequest(h.Engine, "POST", "/api/confirmations/7/approve", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
		if response.StatusCode() != consts.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.StatusCode(), response.Body())
		}
	}
}

func TestApproveConfirmationMapsConflict(t *testing.T) {
	service := &fakeConfirmationService{approveErr: fmt.Errorf("%w: stale", decide.ErrVersionConflict)}
	h := server.New()
	h.POST("/api/confirmations/:todo_id/approve", ApproveConfirmation(service))
	body := []byte(`{"expected_version":1,"plan":{"steps":["draft"]}}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/confirmations/7/approve", &ut.Body{
		Body: bytes.NewReader(body), Len: len(body),
	}).Result()
	if response.StatusCode() != consts.StatusConflict {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestRejectConfirmation(t *testing.T) {
	service := &fakeConfirmationService{}
	h := server.New()
	h.POST("/api/confirmations/:todo_id/reject", RejectConfirmation(service))
	body := []byte(`{"expected_version":2,"reason":"not actionable"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/confirmations/9/reject", &ut.Body{
		Body: bytes.NewReader(body), Len: len(body),
	}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.rejectInput.TodoID != 9 || service.rejectInput.ExpectedVersion != 2 || service.rejectInput.Reason != "not actionable" || service.rejectInput.Channel != "backend" {
		t.Fatalf("input = %#v", service.rejectInput)
	}
}
