package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOKRPlanRoutesUseOwnLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateBizOKR(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Objective{ID: "o-official", Quarter: "2026-Q3", Title: "正式 O"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.KR{ID: "kr-official", ObjectiveID: "o-official", Title: "正式 KR"}).Error; err != nil {
		t.Fatal(err)
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	h := server.New()
	if err := RegisterBizOKRModuleRoutes(h, BizOKRModuleDependencies{
		Workspace: workspace, Identity: identity, Documents: weeklyPreviewDocumentStub{},
		People: newTestOKRPeopleResolver(t, &stubOKRPeopleSearcher{}), PreviewReview: previewReviewServiceStub(t, workspace),
		Enabled: func(context.Context) (bool, error) { return enabled, nil },
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		body   string
		status int
	}{
		{"unknown field", `{"quarter":"2026-Q3","title":"Plan","extra":1,"content":{"objectives":[]}}`, 400},
		{"bad quarter", `{"quarter":"Q3","title":"Plan","content":{"objectives":[]}}`, 400},
		{"empty title", `{"quarter":"2026-Q3","title":" ","content":{"objectives":[]}}`, 400},
	} {
		response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/plans", &ut.Body{Body: strings.NewReader(test.body), Len: len(test.body)}).Result()
		if response.StatusCode() != test.status {
			t.Fatalf("%s: status=%d body=%s", test.name, response.StatusCode(), response.Body())
		}
	}

	createBody := `{"quarter":"2026-Q3","title":"Q3 Plan","content":{"objectives":[{"id":"plan-o","title":"计划 O","krs":[{"id":"plan-kr","title":"计划 KR","owners":[{"open_id":"ou_a","name":"甲"}],"metric_note":"口径","metrics":[{"id":"plan-m","text":"核心目标","light":"green","images":[]}],"points":[{"id":"plan-p","kind":"product","title":"产品 KR","owners":[],"tags":[]}],"tags":[{"type":"business_category","value":"直播"},{"type":"priority","value":"p1"}]}]}]}}`
	response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/plans", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}).Result()
	if response.StatusCode() != 201 {
		t.Fatalf("create status=%d body=%s", response.StatusCode(), response.Body())
	}
	var created struct {
		Data okrworkspace.PlanView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.ID == "" || created.Data.Version != 0 || created.Data.CreatedBy != "jarvis" {
		t.Fatalf("created plan = %+v", created.Data)
	}

	listResponse := ut.PerformRequest(h.Engine, "GET", "/api/biz-okr/plans?quarter=2026-Q3", nil).Result()
	if listResponse.StatusCode() != 200 {
		t.Fatalf("list status=%d body=%s", listResponse.StatusCode(), listResponse.Body())
	}
	var listed struct {
		Data okrworkspace.PlanListView `json:"data"`
	}
	if err := json.Unmarshal(listResponse.Body(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data.Plans) != 1 || listed.Data.Plans[0].ObjectiveCnt != 1 || listed.Data.Plans[0].KRCnt != 1 {
		t.Fatalf("listed plans = %+v", listed.Data)
	}

	for _, route := range h.Engine.Routes() {
		if route.Method == "PUT" && route.Path == "/api/biz-okr/plans/:plan_id" {
			t.Fatal("legacy full-plan replacement route is still registered")
		}
	}

	deleteResponse := ut.PerformRequest(h.Engine, "DELETE", "/api/biz-okr/plans/"+created.Data.ID, nil).Result()
	if deleteResponse.StatusCode() != 200 {
		t.Fatalf("delete status=%d body=%s", deleteResponse.StatusCode(), deleteResponse.Body())
	}
	var objectiveCount, krCount int64
	if err := db.Model(&domain.Objective{}).Where("id = ?", "o-official").Count(&objectiveCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KR{}).Where("id = ?", "kr-official").Count(&krCount).Error; err != nil {
		t.Fatal(err)
	}
	if objectiveCount != 1 || krCount != 1 {
		t.Fatalf("plan delete touched official rows: objectives=%d krs=%d", objectiveCount, krCount)
	}

	enabled = false
	disabledResponse := ut.PerformRequest(h.Engine, "GET", "/api/biz-okr/plans?quarter=2026-Q3", nil).Result()
	if disabledResponse.StatusCode() != 404 {
		t.Fatalf("disabled status=%d body=%s", disabledResponse.StatusCode(), disabledResponse.Body())
	}
}
