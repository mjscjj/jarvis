package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/decide"
	"jarvis/internal/extract"

	"code.byted.org/middleware/hertz/pkg/app/server"
	"code.byted.org/middleware/hertz/pkg/common/ut"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

type fakeConfirmationService struct {
	approveInput    decide.ApproveInput
	rejectInput     decide.RejectInput
	supplementInput decide.SupplementInput
	approveErr      error
	rejectErr       error
	supplementErr   error
}

type fakeConfirmationReader struct {
	filter extract.TodoListFilter
	status string
}

func (f *fakeConfirmationReader) ListTodos(_ context.Context, filter extract.TodoListFilter) (*extract.TodoList, error) {
	f.filter = filter
	return &extract.TodoList{Items: []extract.TodoView{{ID: 7, Status: "need_decision"}}, Total: 1, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (f *fakeConfirmationReader) GetTodo(_ context.Context, id uint64) (*extract.TodoView, error) {
	if id != 7 {
		return nil, fmt.Errorf("%w: id=%d", extract.ErrTodoNotFound, id)
	}
	return &extract.TodoView{ID: id, Status: f.status}, nil
}

func (f *fakeConfirmationReader) GetConfirmation(_ context.Context, id uint64) (*decide.ConfirmationDetail, error) {
	if id != 7 {
		return nil, fmt.Errorf("%w: id=%d", decide.ErrTodoNotFound, id)
	}
	if f.status != "need_info" && f.status != "need_decision" {
		return nil, fmt.Errorf("%w: status=%s", decide.ErrInvalidTransition, f.status)
	}
	return &decide.ConfirmationDetail{Todo: &extract.TodoView{ID: id, Status: f.status}}, nil
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

func (f *fakeConfirmationService) Supplement(_ context.Context, input decide.SupplementInput) (*decide.SupplementResult, error) {
	f.supplementInput = input
	if f.supplementErr != nil {
		return nil, f.supplementErr
	}
	return &decide.SupplementResult{TodoID: input.TodoID, Status: "extracted", Version: input.ExpectedVersion + 1}, nil
}

func TestListConfirmationsDefaultsToPendingStatuses(t *testing.T) {
	reader := &fakeConfirmationReader{}
	h := server.New()
	h.GET("/api/confirmations", ListConfirmations(reader))
	response := ut.PerformRequest(h.Engine, "GET", "/api/confirmations?page=2&page_size=10", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if fmt.Sprint(reader.filter.Statuses) != "[need_info need_decision]" || reader.filter.Page != 2 || reader.filter.PageSize != 10 {
		t.Fatalf("filter = %#v", reader.filter)
	}
}

func TestListConfirmationsRejectsNonPendingStatus(t *testing.T) {
	h := server.New()
	h.GET("/api/confirmations", ListConfirmations(&fakeConfirmationReader{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/confirmations?status=confirmed", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGetConfirmation(t *testing.T) {
	h := server.New()
	h.GET("/api/confirmations/:todo_id", GetConfirmation(&fakeConfirmationReader{status: "need_info"}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/confirmations/7", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGetConfirmationRejectsSettledTodo(t *testing.T) {
	h := server.New()
	h.GET("/api/confirmations/:todo_id", GetConfirmation(&fakeConfirmationReader{status: "confirmed"}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/confirmations/7", nil).Result()
	if response.StatusCode() != consts.StatusConflict {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
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
