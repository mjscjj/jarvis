package api

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestOKRActivityRecordsSuccessfulWritesAndServesCurrentScope(t *testing.T) {
	store, err := okrworkspace.NewActivityStore(filepath.Join(t.TempDir(), "activity"))
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.POST("/write/:progress_id", recordOKRActivity(store, okrActivitySpec{
		Surface: "weekly", Action: "progress_updated", TargetParam: "progress_id",
	}, func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"ok": true}})
	}))
	h.POST("/failed", recordOKRActivity(store, okrActivitySpec{
		Surface: "weekly", Action: "progress_updated",
	}, func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusConflict, map[string]any{"code": 409, "msg": "conflict"})
	}))
	h.GET("/activity", GetOKRActivities(store))

	body := `{"quarter":"2026-Q3","week":"2026-W36","text":"完成灰度"}`
	response := ut.PerformRequest(h.Engine, "POST", "/write/progress-1", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("write status=%d body=%s", response.StatusCode(), response.Body())
	}
	failed := ut.PerformRequest(h.Engine, "POST", "/failed", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if failed.StatusCode() != consts.StatusConflict {
		t.Fatalf("failed status=%d", failed.StatusCode())
	}
	listed := ut.PerformRequest(h.Engine, "GET", "/activity?surface=weekly&quarter=2026-Q3&week=2026-W36", nil).Result()
	if listed.StatusCode() != consts.StatusOK || !strings.Contains(string(listed.Body()), "更新进展：完成灰度") || !strings.Contains(string(listed.Body()), "progress-1") {
		t.Fatalf("activity status=%d body=%s", listed.StatusCode(), listed.Body())
	}
	entries, err := store.List(okrworkspace.ActivityQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded entries = %+v", entries)
	}
}
