package api

import (
	"bytes"
	"context"
	"testing"

	"jarvis/internal/progress"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeProgressService struct {
	taskID       uint64
	projectID    uint64
	projectInput progress.ProjectEventInput
	err          error
}

func (f *fakeProgressService) ListTaskEvents(_ context.Context, taskID uint64) ([]progress.TaskEventView, error) {
	f.taskID = taskID
	return []progress.TaskEventView{{TaskID: taskID, EventType: "created"}}, f.err
}

func (f *fakeProgressService) AppendProjectEvent(_ context.Context, input progress.ProjectEventInput) (*progress.ProjectEventView, error) {
	f.projectInput = input
	return &progress.ProjectEventView{ProjectID: input.ProjectID, Description: input.Description}, f.err
}

func (f *fakeProgressService) ListProjectEvents(_ context.Context, projectID uint64) ([]progress.ProjectEventView, error) {
	f.projectID = projectID
	return []progress.ProjectEventView{{ProjectID: projectID, Description: "项目已创建。"}}, f.err
}

func TestListTaskEvents(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.GET("/api/tasks/:task_id/events", ListTaskEvents(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/tasks/7/events", nil).Result()
	if response.StatusCode() != consts.StatusOK || svc.taskID != 7 {
		t.Fatalf("status=%d task_id=%d body=%s", response.StatusCode(), svc.taskID, response.Body())
	}
}

func TestAppendProjectEvent(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.POST("/api/projects/:project_id/events", AppendProjectEvent(svc))
	body := []byte(`{"description":"接口已经完成，下一步联调。"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/projects/3/events", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if svc.projectInput.ProjectID != 3 || svc.projectInput.Description != "接口已经完成，下一步联调。" {
		t.Fatalf("input=%#v", svc.projectInput)
	}
}

func TestAppendProjectEventRejectsUnknownField(t *testing.T) {
	h := server.New()
	h.POST("/api/projects/:project_id/events", AppendProjectEvent(&fakeProgressService{}))
	body := []byte(`{"description":"接口完成。","unknown":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/projects/3/events", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}

func TestListProjectEventsMapsNotFound(t *testing.T) {
	svc := &fakeProgressService{err: progress.ErrNotFound}
	h := server.New()
	h.GET("/api/projects/:project_id/events", ListProjectEvents(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/projects/9/events", nil).Result()
	if response.StatusCode() != consts.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
