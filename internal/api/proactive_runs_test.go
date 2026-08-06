package api

import (
	"context"
	"errors"
	"testing"

	"jarvis/internal/insight"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeProactiveRunReader struct {
	limit int
	id    uint64
	err   error
}

func (f *fakeProactiveRunReader) ProactiveRuns(_ context.Context, limit int) ([]insight.ProactiveRunRow, error) {
	f.limit = limit
	return []insight.ProactiveRunRow{{ID: 7, Status: "succeeded"}}, f.err
}

func (f *fakeProactiveRunReader) ProactiveRun(_ context.Context, id uint64) (*insight.ProactiveRunDetail, error) {
	f.id = id
	return &insight.ProactiveRunDetail{ProactiveRunRow: insight.ProactiveRunRow{ID: id}, Input: "input"}, f.err
}

func TestGetDebugProactiveRunsAndDetail(t *testing.T) {
	reader := &fakeProactiveRunReader{}
	h := server.New()
	h.GET("/api/debug/proactive-runs", GetDebugProactiveRuns(reader))
	h.GET("/api/debug/proactive-runs/:run_id", GetDebugProactiveRun(reader))

	list := ut.PerformRequest(h.Engine, "GET", "/api/debug/proactive-runs?limit=25", nil).Result()
	if list.StatusCode() != consts.StatusOK || reader.limit != 25 {
		t.Fatalf("list status=%d limit=%d body=%s", list.StatusCode(), reader.limit, list.Body())
	}
	detail := ut.PerformRequest(h.Engine, "GET", "/api/debug/proactive-runs/7", nil).Result()
	if detail.StatusCode() != consts.StatusOK || reader.id != 7 {
		t.Fatalf("detail status=%d id=%d body=%s", detail.StatusCode(), reader.id, detail.Body())
	}
}

func TestGetDebugProactiveRunMapsBadIDAndNotFound(t *testing.T) {
	reader := &fakeProactiveRunReader{err: insight.ErrProactiveRunNotFound}
	h := server.New()
	h.GET("/api/debug/proactive-runs/:run_id", GetDebugProactiveRun(reader))
	if response := ut.PerformRequest(h.Engine, "GET", "/api/debug/proactive-runs/nope", nil).Result(); response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("bad id status=%d body=%s", response.StatusCode(), response.Body())
	}
	reader.err = errors.Join(insight.ErrProactiveRunNotFound, errors.New("id=9"))
	if response := ut.PerformRequest(h.Engine, "GET", "/api/debug/proactive-runs/9", nil).Result(); response.StatusCode() != consts.StatusNotFound {
		t.Fatalf("not found status=%d body=%s", response.StatusCode(), response.Body())
	}
}
