package api

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"jarvis/internal/observability"
)

func TestHealthFailureCarriesLogID(t *testing.T) {
	h := server.New()
	h.Use(observability.Middleware())
	h.GET("/healthz", Health(nil))

	response := ut.PerformRequest(h.Engine, "GET", "/healthz", nil).Result()
	if response.StatusCode() != consts.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	logID := string(response.Header.Peek(observability.HeaderLogID))
	if !regexp.MustCompile(`^\d{13}[0-9a-f]{16}$`).MatchString(logID) {
		t.Fatalf("response LogID = %q, want a generated LogID", logID)
	}
	var payload struct {
		LogID string `json:"logid"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.LogID != logID {
		t.Fatalf("body LogID = %q, header LogID = %q", payload.LogID, logID)
	}
}
