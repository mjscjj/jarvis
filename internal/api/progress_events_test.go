package api

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"jarvis/internal/progress"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeProgressService struct {
	taskID     uint64
	factFilter progress.FactFilter
	factInputs []progress.FactInput
	factInput  progress.FactInput
	err        error
}

func (f *fakeProgressService) ListTaskEvents(_ context.Context, taskID uint64) ([]progress.TaskEventView, error) {
	f.taskID = taskID
	return []progress.TaskEventView{{TaskID: taskID, EventType: "created"}}, f.err
}

func (f *fakeProgressService) AppendFact(_ context.Context, input progress.FactInput) (*progress.FactView, error) {
	f.factInputs = append(f.factInputs, input)
	f.factInput = input
	return &progress.FactView{
		SubjectType: input.SubjectType, SubjectID: input.SubjectID, Description: input.Description,
	}, f.err
}

func (f *fakeProgressService) ListFacts(_ context.Context, filter progress.FactFilter) ([]progress.FactView, error) {
	f.factFilter = filter
	return []progress.FactView{{
		SubjectType: filter.SubjectType, SubjectID: filter.SubjectID, Description: "项目已创建。",
	}}, f.err
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

func TestAppendFact(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.POST("/api/facts", AppendFact(svc))
	body := []byte(`{"subject_type":"group","subject_id":3,"description":"接口已经完成，下一步联调。"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/facts", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if svc.factInput.SubjectType != "group" || svc.factInput.SubjectID != 3 ||
		svc.factInput.Description != "接口已经完成，下一步联调。" {
		t.Fatalf("input=%#v", svc.factInput)
	}
}

func TestAppendFacts(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.POST("/api/facts/batch", AppendFacts(svc))
	body := []byte(`[
		{"subject_type":"group","subject_id":3,"description":"事实 A"},
		{"subject_type":"task","subject_id":4,"description":"事实 B"}
	]`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/facts/batch", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if len(svc.factInputs) != 2 {
		t.Fatalf("factInputs = %#v", svc.factInputs)
	}
	if svc.factInputs[0].SubjectType != "group" || svc.factInputs[1].SubjectType != "task" {
		t.Fatalf("fact order/input=%#v", svc.factInputs)
	}
	bodyStr := string(response.Body())
	if !strings.Contains(bodyStr, "\"items\":") {
		t.Fatalf("response missing items: %s", bodyStr)
	}
}

func TestAppendFactsRejectsEmptyBatch(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.POST("/api/facts/batch", AppendFacts(svc))
	body := []byte(`[]`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/facts/batch", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if len(svc.factInputs) != 0 {
		t.Fatalf("factInputs should be empty: %#v", svc.factInputs)
	}
}

func TestAppendFactRejectsUnknownField(t *testing.T) {
	h := server.New()
	h.POST("/api/facts", AppendFact(&fakeProgressService{}))
	body := []byte(`{"subject_type":"project","subject_id":3,"description":"接口完成。","unknown":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/facts", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}

func TestListFactsMapsNotFound(t *testing.T) {
	svc := &fakeProgressService{err: progress.ErrNotFound}
	h := server.New()
	h.GET("/api/facts", ListFacts(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/facts?subject_type=project&subject_id=9", nil).Result()
	if response.StatusCode() != consts.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}

// TestListFactsParsesWindow checks the day-window bounds reach the service, since
// that is how a caller asks for one natural day.
func TestListFactsParsesWindow(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.GET("/api/facts", ListFacts(svc))
	url := "/api/facts?subject_type=group&subject_id=4" +
		"&from=2026-08-02T00:00:00%2B08:00&until=2026-08-03T00:00:00%2B08:00&limit=20"
	response := ut.PerformRequest(h.Engine, "GET", url, nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if svc.factFilter.From == nil || svc.factFilter.Until == nil {
		t.Fatalf("filter window not parsed: %#v", svc.factFilter)
	}
	if !svc.factFilter.From.Equal(time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("from = %s, want 2026-08-01T16:00:00Z", svc.factFilter.From)
	}
	if svc.factFilter.Limit != 20 || svc.factFilter.SubjectID != 4 {
		t.Fatalf("filter = %#v", svc.factFilter)
	}
}

func TestListFactsRejectsMissingSubject(t *testing.T) {
	h := server.New()
	h.GET("/api/facts", ListFacts(&fakeProgressService{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/facts?subject_type=project", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}

func TestListFactsParsesSourceKindFilters(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.GET("/api/facts", ListFacts(svc))
	url := "/api/facts?subject_type=group&subject_id=4&source_kind=rollup&exclude_source_kind=m3"
	response := ut.PerformRequest(h.Engine, "GET", url, nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if svc.factFilter.SourceKind == nil || *svc.factFilter.SourceKind != "rollup" {
		t.Fatalf("SourceKind = %#v", svc.factFilter.SourceKind)
	}
	if svc.factFilter.ExcludeSourceKind == nil || *svc.factFilter.ExcludeSourceKind != "m3" {
		t.Fatalf("ExcludeSourceKind = %#v", svc.factFilter.ExcludeSourceKind)
	}
}

// Keep source_kind parsing covered even when only one side is set.
func TestListFactsParsesExcludeSourceKindAlone(t *testing.T) {
	svc := &fakeProgressService{}
	h := server.New()
	h.GET("/api/facts", ListFacts(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/facts?subject_type=group&subject_id=4&exclude_source_kind=rollup", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if svc.factFilter.SourceKind != nil {
		t.Fatalf("SourceKind should be nil: %#v", svc.factFilter.SourceKind)
	}
	if svc.factFilter.ExcludeSourceKind == nil || *svc.factFilter.ExcludeSourceKind != "rollup" {
		t.Fatalf("ExcludeSourceKind = %#v", svc.factFilter.ExcludeSourceKind)
	}
}
