package api

import (
	"strings"
	"testing"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProductFeedbackHTTPFlow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.Migrate(db); err != nil {
		t.Fatal(err)
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.GET("/feedback", ListProductFeedback(workspace, false))
	h.POST("/feedback", CreateProductFeedback(workspace, false))
	h.POST("/feedback/:feedback_id/replies", ReplyProductFeedback(workspace, false))
	h.PUT("/feedback/:feedback_id/plus-one", AddProductFeedbackPlusOne(workspace, false))
	h.DELETE("/feedback/:feedback_id/plus-one", RemoveProductFeedbackPlusOne(workspace, false))
	h.PATCH("/feedback/:feedback_id/status", SetProductFeedbackStatus(workspace, false))

	createBody := `{"title":"页面保存失败","content":"点击后没有提示","images":[],"source_context":{"view_state":{"tab":"okr-plan"}}}`
	created := ut.PerformRequest(h.Engine, "POST", "/feedback", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}).Result()
	if created.StatusCode() != consts.StatusCreated || !strings.Contains(string(created.Body()), `"author":{"name":"Jarvis"}`) || !strings.Contains(string(created.Body()), `"can_resolve":true`) {
		t.Fatalf("create status=%d body=%s", created.StatusCode(), created.Body())
	}
	const feedbackIDPrefix = `"id":"`
	body := string(created.Body())
	start := strings.Index(body, feedbackIDPrefix)
	if start < 0 {
		t.Fatalf("created response has no id: %s", body)
	}
	start += len(feedbackIDPrefix)
	end := strings.Index(body[start:], `"`)
	feedbackID := body[start : start+end]

	replyBody := `{"content":"补充：刷新后仍然存在"}`
	replied := ut.PerformRequest(h.Engine, "POST", "/feedback/"+feedbackID+"/replies", &ut.Body{Body: strings.NewReader(replyBody), Len: len(replyBody)}).Result()
	if replied.StatusCode() != consts.StatusCreated || !strings.Contains(string(replied.Body()), "补充：刷新后仍然存在") {
		t.Fatalf("reply status=%d body=%s", replied.StatusCode(), replied.Body())
	}
	liked := ut.PerformRequest(h.Engine, "PUT", "/feedback/"+feedbackID+"/plus-one", nil).Result()
	if liked.StatusCode() != consts.StatusOK || !strings.Contains(string(liked.Body()), `"my_plus_one":true`) {
		t.Fatalf("plus one status=%d body=%s", liked.StatusCode(), liked.Body())
	}

	statusBody := `{"expected_version":1,"resolved":true}`
	resolved := ut.PerformRequest(h.Engine, "PATCH", "/feedback/"+feedbackID+"/status", &ut.Body{Body: strings.NewReader(statusBody), Len: len(statusBody)}).Result()
	if resolved.StatusCode() != consts.StatusOK || !strings.Contains(string(resolved.Body()), `"resolved":true`) {
		t.Fatalf("resolve status=%d body=%s", resolved.StatusCode(), resolved.Body())
	}
	listed := ut.PerformRequest(h.Engine, "GET", "/feedback?resolved=true&sort=popular", nil).Result()
	if listed.StatusCode() != consts.StatusOK || !strings.Contains(string(listed.Body()), `"total":1`) || !strings.Contains(string(listed.Body()), "页面保存失败") {
		t.Fatalf("list status=%d body=%s", listed.StatusCode(), listed.Body())
	}

	unliked := ut.PerformRequest(h.Engine, "DELETE", "/feedback/"+feedbackID+"/plus-one", nil).Result()
	if unliked.StatusCode() != consts.StatusOK || !strings.Contains(string(unliked.Body()), `"my_plus_one":false`) {
		t.Fatalf("remove plus one status=%d body=%s", unliked.StatusCode(), unliked.Body())
	}
}

func TestProductFeedbackStatusRequiresExplicitResolvedValue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.Migrate(db); err != nil {
		t.Fatal(err)
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.PATCH("/feedback/:feedback_id/status", SetProductFeedbackStatus(workspace, false))
	body := `{"expected_version":1}`
	response := ut.PerformRequest(h.Engine, "PATCH", "/feedback/missing/status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest || !strings.Contains(string(response.Body()), "resolved is required") {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
