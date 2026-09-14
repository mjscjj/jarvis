package api

import (
	"context"
	"encoding/json"
	"fmt"
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
		{"unknown field", `{"quarter":"2026-Q3","title":"Plan","extra":true}`, 400},
		{"legacy content field", `{"quarter":"2026-Q3","title":"Plan","content":{"objectives":[]}}`, 400},
		{"bad quarter", `{"quarter":"Q3","title":"Plan"}`, 400},
		{"empty title", `{"quarter":"2026-Q3","title":" "}`, 400},
	} {
		response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/plans", &ut.Body{Body: strings.NewReader(test.body), Len: len(test.body)}).Result()
		if response.StatusCode() != test.status {
			t.Fatalf("%s: status=%d body=%s", test.name, response.StatusCode(), response.Body())
		}
	}

	createBody := `{"quarter":"2026-Q3","title":"Q3 Plan"}`
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
	objectiveBody := `{"id":"plan-o","title":"计划 O","krs":[{"id":"plan-kr","title":"计划 KR","owners":[{"email":"a@example.test","name":"甲"}],"metric_note":"口径","metrics":[{"id":"plan-m","text":"核心目标","light":"green","images":[]}],"points":[{"id":"plan-p","kind":"product","title":"产品 KR","owners":[],"tags":[]}],"tags":[{"type":"business_category","value":"直播"},{"type":"priority","value":"p1"}]}]}`
	objectiveResponse := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/plans/"+created.Data.ID+"/objectives", &ut.Body{Body: strings.NewReader(objectiveBody), Len: len(objectiveBody)}).Result()
	if objectiveResponse.StatusCode() != 201 {
		t.Fatalf("create objective status=%d body=%s", objectiveResponse.StatusCode(), objectiveResponse.Body())
	}
	pointPatchBody := `{"title":"更新后的产品 KR","owners":[{"email":"b@example.test","name":"乙"}]}`
	pointPatchResponse := ut.PerformRequest(h.Engine, "PATCH", "/api/biz-okr/plans/"+created.Data.ID+"/points/plan-p/definition", &ut.Body{Body: strings.NewReader(pointPatchBody), Len: len(pointPatchBody)}).Result()
	if pointPatchResponse.StatusCode() != 200 {
		t.Fatalf("patch plan point status=%d body=%s", pointPatchResponse.StatusCode(), pointPatchResponse.Body())
	}
	planResponse := ut.PerformRequest(h.Engine, "GET", "/api/biz-okr/plans/"+created.Data.ID, nil).Result()
	var patchedPlan struct {
		Data okrworkspace.PlanView `json:"data"`
	}
	if err := json.Unmarshal(planResponse.Body(), &patchedPlan); err != nil {
		t.Fatal(err)
	}
	if point := patchedPlan.Data.Objectives[0].KRs[0].Points[0]; point.Title != "更新后的产品 KR" || len(point.Owners) != 1 || point.Owners[0].Name != "乙" {
		t.Fatalf("patched plan point = %+v", point)
	}
	reorderBody := fmt.Sprintf(`{"ids":["plan-o"],"expected_version":%d}`, patchedPlan.Data.Version)
	reorderResponse := ut.PerformRequest(h.Engine, "PUT", "/api/biz-okr/plans/"+created.Data.ID+"/objectives/order", &ut.Body{Body: strings.NewReader(reorderBody), Len: len(reorderBody)}).Result()
	if reorderResponse.StatusCode() != 200 {
		t.Fatalf("reorder objective status=%d body=%s", reorderResponse.StatusCode(), reorderResponse.Body())
	}
	var reordered struct {
		Data okrworkspace.PlanView `json:"data"`
	}
	if err := json.Unmarshal(reorderResponse.Body(), &reordered); err != nil {
		t.Fatal(err)
	}
	if reordered.Data.ID != created.Data.ID || len(reordered.Data.Objectives) != 1 || reordered.Data.Objectives[0].ID != "plan-o" {
		t.Fatalf("reordered plan response = %+v", reordered.Data)
	}

	commentsResponse := ut.PerformRequest(h.Engine, "GET", "/api/biz-okr/plans/"+created.Data.ID+"/comments", nil).Result()
	if commentsResponse.StatusCode() != 200 {
		t.Fatalf("list plan comments status=%d body=%s", commentsResponse.StatusCode(), commentsResponse.Body())
	}
	commentBody := `{"target_type":"kr","target_id":"plan-kr","target_title":"计划 KR","content":"补充口径"}`
	commentResponse := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/plans/"+created.Data.ID+"/comments", &ut.Body{Body: strings.NewReader(commentBody), Len: len(commentBody)}).Result()
	if commentResponse.StatusCode() != 200 {
		t.Fatalf("create plan comment status=%d body=%s", commentResponse.StatusCode(), commentResponse.Body())
	}
	commentsResponse = ut.PerformRequest(h.Engine, "GET", "/api/biz-okr/plans/"+created.Data.ID+"/comments", nil).Result()
	var comments struct {
		Data okrworkspace.CommentList `json:"data"`
	}
	if err := json.Unmarshal(commentsResponse.Body(), &comments); err != nil {
		t.Fatal(err)
	}
	if comments.Data.PlanID != created.Data.ID || comments.Data.Count != 1 || len(comments.Data.Comments) != 1 {
		t.Fatalf("listed plan comments = %+v", comments.Data)
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

	currentPlan, err := workspace.GetPlan(t.Context(), created.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	deleteBody := `{"delete_token":"` + currentPlan.DeleteToken + `"}`
	deleteResponse := ut.PerformRequest(h.Engine, "DELETE", "/api/biz-okr/plans/"+created.Data.ID, &ut.Body{Body: strings.NewReader(deleteBody), Len: len(deleteBody)}).Result()
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
