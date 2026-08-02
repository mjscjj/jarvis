package api

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	hertzconsts "code.byted.org/middleware/hertz/byted/consts"
	hertzctx "code.byted.org/middleware/hertz/byted/middlewares/server/ctx"
	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/app/server"
	"code.byted.org/middleware/hertz/pkg/common/ut"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

func TestAPINotFoundPrecedesRootStaticFS(t *testing.T) {
	h := server.New()
	h.Use(hertzctx.Ctx(true))
	h.GET("/api/resources", func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"code": 0})
	})
	h.Any("/api/*path", apiNotFound())
	h.StaticFS("/", &app.FS{Root: t.TempDir(), IndexNames: []string{"index.html"}})

	known := ut.PerformRequest(h.Engine, "GET", "/api/resources", nil).Result()
	if known.StatusCode() != consts.StatusOK {
		t.Fatalf("known API status = %d body=%s", known.StatusCode(), known.Body())
	}

	unknown := ut.PerformRequest(h.Engine, "GET", "/api/managed-resources", nil).Result()
	if unknown.StatusCode() != consts.StatusNotFound {
		t.Fatalf("unknown API status = %d body=%s", unknown.StatusCode(), unknown.Body())
	}
	var payload struct {
		Code  int    `json:"code"`
		Msg   string `json:"msg"`
		LogID string `json:"logid"`
	}
	if err := json.Unmarshal(unknown.Body(), &payload); err != nil {
		t.Fatalf("decode unknown API response: %v; body=%s", err, unknown.Body())
	}
	if payload.Code != 40400 || payload.Msg != "api route not found" {
		t.Fatalf("unknown API payload = %+v", payload)
	}
	headerLogID := string(unknown.Header.Peek(hertzconsts.TT_LOGID_HEADER_KEY))
	if !regexp.MustCompile(`^02[0-9a-f]{51}$`).MatchString(headerLogID) {
		t.Fatalf("unknown API header LogID = %q", headerLogID)
	}
	if payload.LogID != headerLogID {
		t.Fatalf("unknown API body LogID = %q, header LogID = %q", payload.LogID, headerLogID)
	}
}
