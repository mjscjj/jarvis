package okrworkspace

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace/domain"
)

func TestPlanLifecycleKeepsOfficialOKRRowsUntouched(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.Objective{ID: "o-real", Title: "正式目标", Quarter: "2026-Q3"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.KR{ID: "kr-real", ObjectiveID: "o-real", Title: "正式 KR"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	created, err := service.CreatePlan(t.Context(), CreatePlanInput{
		Quarter: "2026-Q3",
		Title:   "Q3 Draft",
		Content: PlanContentView{Objectives: []PlanObjectiveView{{
			ID: "plan-o-1", Title: "计划目标", KRs: []PlanKRView{{
				ID: "plan-kr-1", Title: "计划 KR", Owners: []OwnerView{{OpenID: "ou_a", Name: "甲"}},
				Tags:    []TagView{{Type: domain.TagTypeBusinessCategory, Value: "增长"}, {Type: domain.TagTypePriority, Value: "p0"}},
				Metrics: []MetricView{{ID: "plan-m-1", Text: "核心目标 100", Light: domain.LightGreen}},
				Points:  []PlanPointView{{ID: "plan-p-1", Kind: domain.PointKindStrategy, Title: "策略 KR", MeegoWorkItemID: "MEEGO-1", MeegoURL: "https://meego.test/MEEGO-1", Owners: []OwnerView{{Name: "乙"}}, Tags: []TagView{{Type: "custom", Value: "待评审"}}}},
			}},
		}}},
		CreatedBy: "ou_editor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 0 || created.Content.Objectives[0].KRs[0].Owners[0].Name != "甲" {
		t.Fatalf("created plan = %+v", created)
	}

	replaced, err := service.ReplacePlan(t.Context(), created.ID, ReplacePlanInput{
		ExpectedVersion: created.Version,
		Title:           "Q3 Final Draft",
		Content:         created.Content,
		UpdatedBy:       "ou_editor_2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Version != 1 || replaced.Title != "Q3 Final Draft" || replaced.UpdatedBy != "ou_editor_2" {
		t.Fatalf("replaced plan = %+v", replaced)
	}
	if _, err := service.ReplacePlan(t.Context(), created.ID, ReplacePlanInput{
		ExpectedVersion: 0,
		Title:           "stale",
		Content:         created.Content,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replace error = %v, want conflict", err)
	}

	if err := service.DeletePlan(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	var objectiveCount, krCount int64
	if err := db.Model(&domain.Objective{}).Where("id = ?", "o-real").Count(&objectiveCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KR{}).Where("id = ?", "kr-real").Count(&krCount).Error; err != nil {
		t.Fatal(err)
	}
	if objectiveCount != 1 || krCount != 1 {
		t.Fatalf("deleting a plan changed official OKR rows: objectives=%d krs=%d", objectiveCount, krCount)
	}
}

func TestPlanAllowsDraftPlaceholders(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	created, err := service.CreatePlan(t.Context(), CreatePlanInput{
		Quarter: "2026-Q3",
		Title:   "draft placeholders",
		Content: PlanContentView{Objectives: []PlanObjectiveView{{
			ID: "plan-o-1", Title: "计划目标", KRs: []PlanKRView{{
				ID: "plan-kr-1", Title: "计划 KR",
				Tags:    []TagView{{Type: domain.TagTypeBusinessCategory, Value: "增长"}, {Type: domain.TagTypePriority, Value: "p1"}},
				Metrics: []MetricView{{ID: "plan-m-1", Text: " ", Light: domain.LightGreen}},
				Points: []PlanPointView{
					{ID: "plan-p-1", Kind: domain.PointKindStrategy, Title: " "},
					{ID: "plan-p-2", Kind: domain.PointKindProduct, Title: ""},
				},
			}},
		}}},
		CreatedBy: "ou_editor",
	})
	if err != nil {
		t.Fatal(err)
	}
	kr := created.Content.Objectives[0].KRs[0]
	if kr.Metrics[0].Text != "" || kr.Points[0].Title != "" || kr.Points[1].Title != "" {
		t.Fatalf("draft placeholders should be trimmed but preserved: %+v", kr)
	}
}

func TestLegacyPlanContentBackfillsIntoRelationalDefinitions(t *testing.T) {
	db := openWorkspaceTestDB(t)
	legacy := domain.OKRPlan{
		ID: "plan-legacy", Quarter: "2026-Q3", Title: "legacy", Content: datatypes.JSON(`{"objectives":[{"id":"plan-o-legacy","title":"目标","krs":[{"id":"plan-kr-legacy","title":"关键结果","owners":[{"open_id":"ou_a","name":"甲"}],"metrics":[{"id":"plan-m-legacy","text":"指标","light":"green"}],"points":[{"id":"plan-p-legacy","kind":"strategy","title":"动作","meego_work_item_id":"MEEGO-42","meego_url":"https://meego.test/MEEGO-42","owners":[],"tags":[]}],"tags":[{"type":"business_category","value":"增长"}]}]}]}`),
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillPlanDefinitions(db); err != nil {
		t.Fatal(err)
	}
	// Startup migration may run again at final launch; it must not duplicate
	// the already materialized relational definitions.
	if err := backfillPlanDefinitions(db); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.GetPlan(t.Context(), legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	var expected PlanContentView
	if err := json.Unmarshal(legacy.Content, &expected); err != nil {
		t.Fatal(err)
	}
	expected, err = normalizePlanContent(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Content, expected) {
		t.Fatalf("relational round trip changed plan content:\nwant=%+v\n got=%+v", expected, plan.Content)
	}
	var objective domain.Objective
	if err := db.First(&objective, "id = ?", "plan-o-legacy").Error; err != nil {
		t.Fatal(err)
	}
	if objective.PlanID != legacy.ID {
		t.Fatalf("objective plan scope = %q", objective.PlanID)
	}
	var objectiveCount, krCount int64
	if err := db.Model(&domain.Objective{}).Where("plan_id = ?", legacy.ID).Count(&objectiveCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KR{}).Where("objective_id = ?", objective.ID).Count(&krCount).Error; err != nil {
		t.Fatal(err)
	}
	if objectiveCount != 1 || krCount != 1 {
		t.Fatalf("idempotent backfill duplicated rows: objectives=%d krs=%d", objectiveCount, krCount)
	}
	if _, err := service.GetBizCoreKR(t.Context(), "plan-kr-legacy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("plan KR leaked into formal OKR read: %v", err)
	}
	board, err := service.CoreBoard(t.Context(), legacy.Quarter)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Objectives) != 0 {
		t.Fatalf("plan objective leaked into formal board: %+v", board.Objectives)
	}
}

func TestPlanObjectiveWritesConflictOnlyWithinOneObjective(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "blocks", Content: PlanContentView{Objectives: []PlanObjectiveView{
		{ID: "plan-o-1", Title: "一号", KRs: []PlanKRView{{ID: "plan-kr-1", Title: "KR1"}}},
		{ID: "plan-o-2", Title: "二号", KRs: []PlanKRView{{ID: "plan-kr-2", Title: "KR2"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	first := created.Content.Objectives[0]
	second := created.Content.Objectives[1]
	first.Title = "一号-更新"
	if _, err := service.UpdatePlanObjective(t.Context(), created.ID, first.ID, PlanObjectiveWriteInput{ExpectedVersion: first.Version, Objective: first, UpdatedBy: "a"}); err != nil {
		t.Fatal(err)
	}
	second.Title = "二号-更新"
	if _, err := service.UpdatePlanObjective(t.Context(), created.ID, second.ID, PlanObjectiveWriteInput{ExpectedVersion: second.Version, Objective: second, UpdatedBy: "b"}); err != nil {
		t.Fatal(err)
	}
	first.Title = "旧版本"
	if _, err := service.UpdatePlanObjective(t.Context(), created.ID, first.ID, PlanObjectiveWriteInput{ExpectedVersion: 0, Objective: first, UpdatedBy: "stale"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale objective update error = %v", err)
	}
	loaded, err := service.GetPlan(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Content.Objectives[0].Title != "一号-更新" || loaded.Content.Objectives[1].Title != "二号-更新" {
		t.Fatalf("objective blocks were not isolated: %+v", loaded.Content.Objectives)
	}
}

func TestPlanValidationRejectsStructuralPointTags(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreatePlan(t.Context(), CreatePlanInput{
		Quarter: "2026-Q3",
		Title:   "invalid",
		Content: PlanContentView{Objectives: []PlanObjectiveView{{
			ID: "o", Title: "O", KRs: []PlanKRView{{
				ID: "kr", Title: "KR",
				Tags:   []TagView{{Type: domain.TagTypeBusinessCategory, Value: "业务"}, {Type: domain.TagTypePriority, Value: "p1"}},
				Points: []PlanPointView{{ID: "p", Kind: domain.PointKindProduct, Title: "产品 KR", Tags: []TagView{{Type: domain.TagTypePriority, Value: "p2"}}}},
			}},
		}}},
	})
	if err == nil {
		t.Fatal("CreatePlan accepted a structural tag on point")
	}
}
