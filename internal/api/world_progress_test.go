package api

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/internal/worldprogress"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeWorldProgressService struct {
	id           uint64
	filter       worldprogress.Filter
	createInput  worldprogress.CreateInput
	updateInput  worldprogress.UpdateInput
	errorOnWrite error
}

func (f *fakeWorldProgressService) Get(_ context.Context, id uint64) (*worldprogress.View, error) {
	f.id = id
	return &worldprogress.View{ID: id, SubjectType: "okr_point", SubjectID: "point-1", PeriodKey: "2026-W36"}, nil
}

func (f *fakeWorldProgressService) GetBySubjectPeriod(_ context.Context, filter worldprogress.Filter) (*worldprogress.View, error) {
	f.filter = filter
	return &worldprogress.View{ID: 3, SubjectType: filter.SubjectType, SubjectID: filter.SubjectID, PeriodKey: filter.PeriodKey}, nil
}

func (f *fakeWorldProgressService) ListByPeriod(_ context.Context, periodKey string) ([]worldprogress.View, error) {
	f.filter.PeriodKey = periodKey
	return []worldprogress.View{{ID: 4, SubjectType: "okr_kr", SubjectID: "kr-1", PeriodKey: periodKey}}, nil
}

func (f *fakeWorldProgressService) Create(_ context.Context, input worldprogress.CreateInput) (*worldprogress.View, error) {
	f.createInput = input
	if f.errorOnWrite != nil {
		return nil, f.errorOnWrite
	}
	return &worldprogress.View{ID: 3, SubjectType: input.SubjectType, SubjectID: input.SubjectID, PeriodKey: input.PeriodKey}, nil
}

func (f *fakeWorldProgressService) Update(_ context.Context, id uint64, input worldprogress.UpdateInput) (*worldprogress.View, error) {
	f.id = id
	f.updateInput = input
	if f.errorOnWrite != nil {
		return nil, f.errorOnWrite
	}
	return &worldprogress.View{ID: id, Signal: input.Signal, Summary: input.Summary}, nil
}

func TestWorldProgressHandlers(t *testing.T) {
	service := &fakeWorldProgressService{}
	h := server.New()
	h.GET("/api/world-progress", GetWorldProgressBySubjectPeriod(service))
	h.GET("/api/world-progress/period/:period_key", ListWorldProgressByPeriod(service))
	h.GET("/api/world-progress/:world_progress_id", GetWorldProgress(service))
	h.POST("/api/world-progress", CreateWorldProgress(service))
	h.PUT("/api/world-progress/:world_progress_id", UpdateWorldProgress(service))

	response := ut.PerformRequest(h.Engine, "GET", "/api/world-progress?subject_type=okr_point&subject_id=point-1&period_key=2026-W36", nil).Result()
	if response.StatusCode() != consts.StatusOK || service.filter.SubjectID != "point-1" {
		t.Fatalf("query status=%d filter=%#v body=%s", response.StatusCode(), service.filter, response.Body())
	}
	response = ut.PerformRequest(h.Engine, "GET", "/api/world-progress/period/2026-W36", nil).Result()
	if response.StatusCode() != consts.StatusOK || service.filter.PeriodKey != "2026-W36" {
		t.Fatalf("list status=%d period=%q body=%s", response.StatusCode(), service.filter.PeriodKey, response.Body())
	}
	response = ut.PerformRequest(h.Engine, "GET", "/api/world-progress/3", nil).Result()
	if response.StatusCode() != consts.StatusOK || service.id != 3 {
		t.Fatalf("get status=%d id=%d body=%s", response.StatusCode(), service.id, response.Body())
	}

	createBody := []byte(`{
		"expected_version":0,"subject_type":"okr_point","subject_id":"point-1",
		"period_key":"2026-W36","signal":"yellow","summary":"客户 C 尚未启动。",
		"evidence":{"refs":["fact:12"]},"evidence_until":"2026-09-06T09:00:00Z"
	}`)
	response = ut.PerformRequest(h.Engine, "POST", "/api/world-progress", &ut.Body{Body: bytes.NewReader(createBody), Len: len(createBody)}).Result()
	if response.StatusCode() != consts.StatusCreated || service.createInput.ExpectedVersion == nil || *service.createInput.ExpectedVersion != 0 {
		t.Fatalf("create status=%d input=%#v body=%s", response.StatusCode(), service.createInput, response.Body())
	}
	if service.createInput.EvidenceUntil == nil || !service.createInput.EvidenceUntil.Equal(time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("evidence_until=%v", service.createInput.EvidenceUntil)
	}

	updateBody := []byte(`{"expected_version":0,"signal":"green","summary":"按计划。","evidence":{},"evidence_until":"2026-09-06T10:00:00Z"}`)
	response = ut.PerformRequest(h.Engine, "PUT", "/api/world-progress/3", &ut.Body{Body: bytes.NewReader(updateBody), Len: len(updateBody)}).Result()
	if response.StatusCode() != consts.StatusOK || service.updateInput.Summary != "按计划。" {
		t.Fatalf("update status=%d input=%#v body=%s", response.StatusCode(), service.updateInput, response.Body())
	}
}

func TestWorldProgressHandlersRejectBadInputAndMapErrors(t *testing.T) {
	service := &fakeWorldProgressService{}
	h := server.New()
	h.GET("/api/world-progress/:world_progress_id", GetWorldProgress(service))
	h.POST("/api/world-progress", CreateWorldProgress(service))

	if response := ut.PerformRequest(h.Engine, "GET", "/api/world-progress/nope", nil).Result(); response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("invalid id status=%d body=%s", response.StatusCode(), response.Body())
	}
	body := []byte(`{"expected_version":0,"unknown":true}`)
	if response := ut.PerformRequest(h.Engine, "POST", "/api/world-progress", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result(); response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", response.StatusCode(), response.Body())
	}

	cases := []struct {
		err    error
		status int
	}{
		{worldprogress.ErrInvalidInput, consts.StatusBadRequest},
		{worldprogress.ErrNotFound, consts.StatusNotFound},
		{worldprogress.ErrConflict, consts.StatusConflict},
		{worldprogress.ErrSubjectUnavailable, consts.StatusConflict},
	}
	for _, testCase := range cases {
		service.errorOnWrite = errors.Join(testCase.err, errors.New("details"))
		valid := []byte(`{"expected_version":0,"subject_type":"okr_point","subject_id":"point-1","period_key":"2026-W36","signal":"green","summary":"ok","evidence":{},"evidence_until":"2026-09-06T10:00:00Z"}`)
		response := ut.PerformRequest(h.Engine, "POST", "/api/world-progress", &ut.Body{Body: bytes.NewReader(valid), Len: len(valid)}).Result()
		if response.StatusCode() != testCase.status || !strings.Contains(string(response.Body()), "details") {
			t.Fatalf("error=%v status=%d body=%s", testCase.err, response.StatusCode(), response.Body())
		}
	}
}
