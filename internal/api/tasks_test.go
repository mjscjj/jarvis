package api

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"jarvis/internal/execute"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeTaskService struct {
	filter execute.TaskFilter
	finish execute.FinishInput
	err    error
}

func (f *fakeTaskService) ListTasks(_ context.Context, filter execute.TaskFilter) (*execute.TaskList, error) {
	f.filter = filter
	if f.err != nil {
		return nil, f.err
	}
	return &execute.TaskList{Items: []execute.TaskView{{ID: 8, Status: "pending"}}, Total: 1, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (f *fakeTaskService) Finish(_ context.Context, input execute.FinishInput) (*execute.TaskView, error) {
	f.finish = input
	if f.err != nil {
		return nil, f.err
	}
	return &execute.TaskView{ID: input.TaskID, Status: input.Status, Version: input.ExpectedVersion + 1}, nil
}

func TestListTasksDefaultsToPending(t *testing.T) {
	service := &fakeTaskService{}
	h := server.New()
	h.GET("/api/tasks", ListTasks(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/tasks", nil).Result()
	if response.StatusCode() != consts.StatusOK || fmt.Sprint(service.filter.Statuses) != "[pending]" {
		t.Fatalf("status=%d filter=%#v body=%s", response.StatusCode(), service.filter, response.Body())
	}
}

func TestFinishTask(t *testing.T) {
	service := &fakeTaskService{}
	h := server.New()
	h.POST("/api/tasks/:task_id/finish", FinishTask(service))
	body := []byte(`{"expected_version":0,"status":"done","result":{"summary":"completed manually"}}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/tasks/8/finish", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if service.finish.TaskID != 8 || service.finish.Status != "done" || service.finish.ExpectedVersion != 0 {
		t.Fatalf("finish input = %#v", service.finish)
	}
}

func TestFinishTaskRejectsUnknownField(t *testing.T) {
	h := server.New()
	h.POST("/api/tasks/:task_id/finish", FinishTask(&fakeTaskService{}))
	body := []byte(`{"expected_version":0,"status":"done","result":{"summary":"done"},"force":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/tasks/8/finish", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}

func TestFinishTaskMapsConflict(t *testing.T) {
	service := &fakeTaskService{err: fmt.Errorf("%w: synthetic", execute.ErrVersionConflict)}
	h := server.New()
	h.POST("/api/tasks/:task_id/finish", FinishTask(service))
	body := []byte(`{"expected_version":2,"status":"failed","result":{"error":"manual failure"}}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/tasks/8/finish", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusConflict {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
