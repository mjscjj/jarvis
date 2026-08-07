package api

import (
	"context"
	"testing"
	"time"

	"jarvis/internal/insight"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type monitoringReaderStub struct {
	from  time.Time
	until time.Time
}

func (s *monitoringReaderStub) Monitoring(_ context.Context, from, until time.Time) (*insight.MonitoringSnapshot, error) {
	s.from, s.until = from, until
	return &insight.MonitoringSnapshot{From: from.Format(time.RFC3339), Until: until.Format(time.RFC3339)}, nil
}

func TestGetDebugMonitoringValidatesAndForwardsRange(t *testing.T) {
	reader := &monitoringReaderStub{}
	h := server.Default()
	h.GET("/api/debug/monitoring", GetDebugMonitoring(reader))

	response := ut.PerformRequest(h.Engine, "GET", "/api/debug/monitoring?from=2026-08-07T00%3A00%3A00%2B08%3A00&until=2026-08-08T00%3A00%3A00%2B08%3A00", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if got := reader.until.Sub(reader.from); got != 24*time.Hour {
		t.Fatalf("forwarded range = %s, want 24h", got)
	}

	bad := ut.PerformRequest(h.Engine, "GET", "/api/debug/monitoring?from=nope&until=2026-08-08T00%3A00%3A00Z", nil).Result()
	if bad.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("bad range status = %d, want 400", bad.StatusCode())
	}
}
