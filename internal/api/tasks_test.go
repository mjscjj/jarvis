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
	filter     execute.TaskFilter
	finish     execute.FinishInput
	supplement execute.SupplementInput
	err        error
}

func (f *fakeTaskService) ListTasks(_ context.Context, filter execute.TaskFilter) (*execute.TaskList, error) {
	f.filter = filter
	if f.err != nil {
		return nil, f.err
	}
	return &execute.TaskList{Items: []execute.TaskView{{ID: 8, Status: "pending"}}, Total: 1, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (f *fakeTaskService) ListRuns(_ context.Context, taskID uint64) (*execute.RunList, error) {
	if f.err != nil {
		return nil, f.err
	}
	branch := "jarvis/task-8"
	return &execute.RunList{Items: []execute.RunView{{ID: 3, TaskID: taskID, ActionType: "code_change", Status: "succeeded", Branch: &branch}}}, nil
}

func (f *fakeTaskService) Finish(_ context.Context, input execute.FinishInput) (*execute.TaskView, error) {
	f.finish = input
	if f.err != nil {
		return nil, f.err
	}
	return &execute.TaskView{ID: input.TaskID, Status: input.Status, Version: input.ExpectedVersion + 1}, nil
}

func (f *fakeTaskService) Supplement(_ context.Context, input execute.SupplementInput) (*execute.TaskView, error) {
	f.supplement = input
	if f.err != nil {
		return nil, f.err
	}
	return &execute.TaskView{
		ID: input.TaskID, Status: "pending", Version: input.ExpectedVersion + 1,
		ExecutionSupplements: []execute.ExecutionSupplement{{Note: input.Note, At: "2026-07-20T00:00:00Z"}},
	}, nil
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

func TestListTaskRuns(t *testing.T) {
	service := &fakeTaskService{}
	h := server.New()
	h.GET("/api/tasks/:task_id/runs", ListTaskRuns(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/tasks/8/runs", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode())
	}
	if !bytes.Contains(response.Body(), []byte(`"action_type":"code_change"`)) {
		t.Fatalf("body missing run item: %s", response.Body())
	}
}

func TestListTaskRunsRejectsBadID(t *testing.T) {
	h := server.New()
	h.GET("/api/tasks/:task_id/runs", ListTaskRuns(&fakeTaskService{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/tasks/0/runs", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode())
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
	// Manual "手动完成" must be tagged stage=manual_done so the UI separates it
	// from a codex-driven done (stage=executed).
	if !bytes.Contains(service.finish.Result, []byte(`"stage":"manual_done"`)) {
		t.Fatalf("finish result missing manual_done stage: %s", service.finish.Result)
	}
}

// TestFinishTaskFailedTagsManualStage guards that a manual "失败" click is stored
// as stage=manual_failed, so it is distinguishable from a real codex execution
// failure (stage=executed) in the task backlog UI.
func TestFinishTaskFailedTagsManualStage(t *testing.T) {
	service := &fakeTaskService{}
	h := server.New()
	h.POST("/api/tasks/:task_id/finish", FinishTask(service))
	body := []byte(`{"expected_version":0,"status":"failed","result":{"error":"我手动标记失败"}}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/tasks/8/finish", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if !bytes.Contains(service.finish.Result, []byte(`"stage":"manual_failed"`)) {
		t.Fatalf("failed finish result missing manual_failed stage: %s", service.finish.Result)
	}
	if !bytes.Contains(service.finish.Result, []byte(`"error":"我手动标记失败"`)) {
		t.Fatalf("failed finish result dropped the error field: %s", service.finish.Result)
	}
}

// TestTagResultStage covers the pure stage-tagging helper: it injects stage,
// preserves caller fields, never overwrites an explicit stage, and fails fast on
// non-object JSON.
func TestTagResultStage(t *testing.T) {
	tagged, err := tagResultStage([]byte(`{"error":"boom"}`), "manual_failed")
	if err != nil {
		t.Fatalf("tagResultStage() error = %v", err)
	}
	if !bytes.Contains(tagged, []byte(`"stage":"manual_failed"`)) || !bytes.Contains(tagged, []byte(`"error":"boom"`)) {
		t.Fatalf("tagged = %s", tagged)
	}
	// An explicit stage from the caller wins (not overwritten).
	kept, err := tagResultStage([]byte(`{"stage":"custom"}`), "manual_failed")
	if err != nil || !bytes.Contains(kept, []byte(`"stage":"custom"`)) {
		t.Fatalf("explicit stage overwritten: %s err=%v", kept, err)
	}
	for name, raw := range map[string]string{"empty": "", "array": `[1,2]`, "garbage": `nope`} {
		t.Run(name, func(t *testing.T) {
			if _, err := tagResultStage([]byte(raw), "manual_failed"); err == nil {
				t.Fatalf("tagResultStage(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

func TestManualStage(t *testing.T) {
	if manualStage("failed") != "manual_failed" {
		t.Fatalf("manualStage(failed) = %q", manualStage("failed"))
	}
	if manualStage("done") != "manual_done" {
		t.Fatalf("manualStage(done) = %q", manualStage("done"))
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

func TestSupplementTask(t *testing.T) {
	service := &fakeTaskService{}
	h := server.New()
	h.POST("/api/tasks/:task_id/supplement", SupplementTask(service))
	body := []byte(`{"expected_version":1,"note":"优先用季度模板"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/tasks/8/supplement", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if service.supplement.TaskID != 8 || service.supplement.ExpectedVersion != 1 || service.supplement.Note != "优先用季度模板" {
		t.Fatalf("supplement input = %#v", service.supplement)
	}
}
