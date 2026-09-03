package okrworkspace

import (
	"errors"
	"testing"

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
				Points:  []PlanPointView{{ID: "plan-p-1", Kind: domain.PointKindStrategy, Title: "策略 KR", Owners: []OwnerView{{Name: "乙"}}, Tags: []TagView{{Type: "custom", Value: "待评审"}}}},
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
