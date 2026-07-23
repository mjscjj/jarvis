package api

import (
	"encoding/json"
	"regexp"
	"testing"

	hertzconsts "code.byted.org/middleware/hertz/byted/consts"
	hertzctx "code.byted.org/middleware/hertz/byted/middlewares/server/ctx"
	"code.byted.org/middleware/hertz/pkg/app/server"
	"code.byted.org/middleware/hertz/pkg/common/ut"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

func TestHealthFailureCarriesLogID(t *testing.T) {
	h := server.New()
	h.Use(hertzctx.Ctx(true))
	h.GET("/healthz", Health(nil))

	response := ut.PerformRequest(h.Engine, "GET", "/healthz", nil).Result()
	if response.StatusCode() != consts.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	logID := string(response.Header.Peek(hertzconsts.TT_LOGID_HEADER_KEY))
	if !regexp.MustCompile(`^02[0-9a-f]{51}$`).MatchString(logID) {
		t.Fatalf("response LogID = %q, want standard ByteDance LogID", logID)
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
