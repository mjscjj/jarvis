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

	created, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "Q3 Draft", CreatedBy: "editor@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	created, err = service.CreatePlanObjective(t.Context(), created.ID, PlanObjectiveView{
		ID: "plan-o-1", Title: "计划目标", KRs: []PlanKRView{{
			ID: "plan-kr-1", Title: "计划 KR", Owners: []OwnerView{{Email: "a@example.test", Name: "甲"}},
			Tags:    []TagView{{Type: domain.TagTypeBusinessCategory, Value: "增长"}, {Type: domain.TagTypePriority, Value: "p0"}},
			Metrics: []MetricView{{ID: "plan-m-1", Text: "核心目标 100", Light: domain.LightGreen}},
			Points:  []PlanPointView{{ID: "plan-p-1", Kind: domain.PointKindStrategy, Title: "策略 KR", MeegoWorkItemID: "MEEGO-1", MeegoURL: "https://meego.test/MEEGO-1", Owners: []OwnerView{{Email: "b@example.test", Name: "乙"}}, Tags: []TagView{{Type: "custom", Value: "待评审"}}}},
		}},
	}, "editor@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.Objectives[0].KRs[0].Owners[0].Name != "甲" {
		t.Fatalf("created plan = %+v", created)
	}
	kr := created.Objectives[0].KRs[0]
	if kr.Owners[0].Email == "" || kr.Points[0].Owners[0].Email == "" {
		t.Fatalf("plan owner namespaces = kr:%+v point:%+v", kr.Owners, kr.Points[0].Owners)
	}

	if err := service.DeletePlan(t.Context(), created.ID, created.DeleteToken); err != nil {
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

	created, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "draft placeholders"})
	if err != nil {
		t.Fatal(err)
	}
	created, err = service.CreatePlanObjective(t.Context(), created.ID, PlanObjectiveView{
		ID: "plan-o-1", Title: "计划目标", KRs: []PlanKRView{{
			ID: "plan-kr-1", Title: "计划 KR",
			Tags:    []TagView{{Type: domain.TagTypeBusinessCategory, Value: "增长"}, {Type: domain.TagTypePriority, Value: "p1"}},
			Metrics: []MetricView{{ID: "plan-m-1", Text: " ", Light: domain.LightGreen}},
			Points: []PlanPointView{
				{ID: "plan-p-1", Kind: domain.PointKindStrategy, Title: " "},
				{ID: "plan-p-2", Kind: domain.PointKindProduct, Title: ""},
			},
		}},
	}, "editor@example.test")
	if err != nil {
		t.Fatal(err)
	}
	kr := created.Objectives[0].KRs[0]
	if kr.Metrics[0].Text != "" || kr.Points[0].Title != "" || kr.Points[1].Title != "" {
		t.Fatalf("draft placeholders should be trimmed but preserved: %+v", kr)
	}
}

func TestPlanObjectiveWritesConflictOnlyWithinOneObjective(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "blocks"})
	if err != nil {
		t.Fatal(err)
	}
	created, err = service.CreatePlanObjective(t.Context(), created.ID, PlanObjectiveView{ID: "plan-o-1", Title: "一号", KRs: []PlanKRView{{ID: "plan-kr-1", Title: "KR1"}}}, "a")
	if err != nil {
		t.Fatal(err)
	}
	created, err = service.CreatePlanObjective(t.Context(), created.ID, PlanObjectiveView{ID: "plan-o-2", Title: "二号", KRs: []PlanKRView{{ID: "plan-kr-2", Title: "KR2"}}}, "a")
	if err != nil {
		t.Fatal(err)
	}
	first := created.Objectives[0]
	second := created.Objectives[1]
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
	if loaded.Objectives[0].Title != "一号-更新" || loaded.Objectives[1].Title != "二号-更新" {
		t.Fatalf("objective blocks were not isolated: %+v", loaded.Objectives)
	}
	if loaded.Objectives[0].KRs[0].Version != first.KRs[0].Version || loaded.Objectives[1].KRs[0].Version != second.KRs[0].Version {
		t.Fatalf("objective-only edits advanced unrelated KR versions: %+v", loaded.Objectives)
	}
}

func TestPlanKRWritesConflictOnlyWithinOneKR(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q4", Title: "concurrent KRs"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{
		ID: "plan-o", Title: "AI 提效", KRs: []PlanKRView{
			{ID: "plan-kr-a", Title: "KR A", Points: []PlanPointView{{ID: "plan-p-a", Kind: domain.PointKindStrategy, Title: "A point"}}},
			{ID: "plan-kr-b", Title: "KR B", Points: []PlanPointView{{ID: "plan-p-b", Kind: domain.PointKindProduct, Title: "B point"}}},
		},
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	staleObjective := plan.Objectives[0]
	krA, krB := staleObjective.KRs[0], staleObjective.KRs[1]

	krA.Title = "KR A by editor A"
	if _, err := service.UpdatePlanKR(t.Context(), plan.ID, krA.ID, PlanKRWriteInput{
		ExpectedVersion: krA.Version, ExpectedStructureToken: krA.StructureToken, KR: krA, UpdatedBy: "editor-a",
	}); err != nil {
		t.Fatal(err)
	}
	krB.Title = "KR B by editor B"
	updated, err := service.UpdatePlanKR(t.Context(), plan.ID, krB.ID, PlanKRWriteInput{
		ExpectedVersion: krB.Version, ExpectedStructureToken: krB.StructureToken, KR: krB, UpdatedBy: "editor-b",
	})
	if err != nil {
		t.Fatalf("sibling KR edit should not conflict: %v", err)
	}
	if updated.Objectives[0].Version != staleObjective.Version || updated.Objectives[0].KRs[0].Title != krA.Title || updated.Objectives[0].KRs[1].Title != krB.Title {
		t.Fatalf("KR-scoped writes were not isolated: %+v", updated.Objectives[0])
	}

	if _, err := service.UpdatePlanKR(t.Context(), plan.ID, krA.ID, PlanKRWriteInput{
		ExpectedVersion: krA.Version, ExpectedStructureToken: krA.StructureToken, KR: krA, UpdatedBy: "stale",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale same-KR update error = %v, want ErrConflict", err)
	}
	staleObjective.Title = "stale legacy O write"
	if _, err := service.UpdatePlanObjective(t.Context(), plan.ID, staleObjective.ID, PlanObjectiveWriteInput{
		ExpectedVersion: staleObjective.Version, ExpectedStructureToken: staleObjective.StructureToken, Objective: staleObjective, UpdatedBy: "legacy",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("legacy O snapshot overwrote newer KR versions: %v", err)
	}

	currentKR := updated.Objectives[0].KRs[0]
	pointTitle := "point changed independently"
	if _, err := service.PatchPlanPointDefinition(t.Context(), plan.ID, "plan-p-a", PatchPointDefinitionInput{
		ExpectedVersion: currentKR.Points[0].Version, Title: &pointTitle, UpdatedBy: "point-editor",
	}); err != nil {
		t.Fatal(err)
	}
	currentKR.Points = nil
	if _, err := service.UpdatePlanKR(t.Context(), plan.ID, currentKR.ID, PlanKRWriteInput{
		ExpectedVersion: currentKR.Version, ExpectedStructureToken: currentKR.StructureToken, KR: currentKR, UpdatedBy: "stale-remover",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale KR removal after point edit error = %v, want ErrConflict", err)
	}
	loaded, err := service.GetPlan(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if point := loaded.Objectives[0].KRs[0].Points[0]; point.Title != pointTitle {
		t.Fatalf("stale KR removal changed the point: %+v", point)
	}
}

func TestPlanPointDefinitionPatchesDoNotOverwriteSiblingPoints(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "concurrent points"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{
		ID: "plan-o", Title: "O", KRs: []PlanKRView{{
			ID: "plan-kr", Title: "KR", Points: []PlanPointView{
				{ID: "strategy-point", Kind: domain.PointKindStrategy, Title: "旧策略 KR"},
				{ID: "product-point", Kind: domain.PointKindProduct, Title: "旧产品 KR"},
			},
		}},
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	initialPlanVersion := plan.Version
	initialObjectiveVersion := plan.Objectives[0].Version
	initialKRVersion := plan.Objectives[0].KRs[0].Version

	strategyTitle := "战宇琼填写的策略 KR"
	strategyResult, err := service.PatchPlanPointDefinition(t.Context(), plan.ID, "strategy-point", PatchPointDefinitionInput{ExpectedVersion: 0, Title: &strategyTitle, UpdatedBy: "strategy-editor"})
	if err != nil {
		t.Fatal(err)
	}
	productTitle := "罗沙填写的产品 KR"
	owners := []OwnerView{{Email: "luosha@example.test", Name: "罗沙"}}
	productResult, err := service.PatchPlanPointDefinition(t.Context(), plan.ID, "product-point", PatchPointDefinitionInput{ExpectedVersion: 0, Title: &productTitle, Owners: &owners, UpdatedBy: "product-editor"})
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := service.GetPlan(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	points := loaded.Objectives[0].KRs[0].Points
	if points[0].Title != strategyTitle || points[1].Title != productTitle || len(points[1].Owners) != 1 || points[1].Owners[0].Name != "罗沙" {
		t.Fatalf("independent point patches overwrote each other: %+v", points)
	}
	if strategyResult.Version != 1 || productResult.Version != 1 {
		t.Fatalf("point versions were not advanced independently: strategy=%+v product=%+v", strategyResult, productResult)
	}
	if loaded.Version != initialPlanVersion || loaded.Objectives[0].Version != initialObjectiveVersion || loaded.Objectives[0].KRs[0].Version != initialKRVersion {
		t.Fatalf("point patches changed parent versions: plan=%+v", loaded)
	}
	staleTitle := "旧页面试图覆盖"
	current, err := service.PatchPlanPointDefinition(t.Context(), plan.ID, "strategy-point", PatchPointDefinitionInput{ExpectedVersion: 0, Title: &staleTitle})
	if !errors.Is(err, ErrConflict) || current.Version != 1 || current.Title != strategyTitle {
		t.Fatalf("stale point patch = %+v, %v; want current point conflict", current, err)
	}

	staleObjective := plan.Objectives[0]
	staleObjective.Title = "O 的独立修改"
	updatedPlan, err := service.UpdatePlanObjective(t.Context(), plan.ID, staleObjective.ID, PlanObjectiveWriteInput{ExpectedVersion: initialObjectiveVersion, Objective: staleObjective, UpdatedBy: "objective-editor"})
	if err != nil {
		t.Fatal(err)
	}
	updatedPoints := updatedPlan.Objectives[0].KRs[0].Points
	if updatedPoints[0].Title != strategyTitle || updatedPoints[1].Title != productTitle || len(updatedPoints[1].Owners) != 1 {
		t.Fatalf("stale objective snapshot overwrote point definitions: %+v", updatedPoints)
	}
}

func TestCommittedPointDefinitionPatchRejectsPlanPoint(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{
		ID: "plan-o", Title: "O", KRs: []PlanKRView{{ID: "plan-kr", Title: "KR", Points: []PlanPointView{{ID: "plan-point", Kind: domain.PointKindProduct, Title: "旧标题"}}}},
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	title := "越过 Plan 边界"
	if _, err := service.PatchPointDefinition(t.Context(), "plan-point", PatchPointDefinitionInput{Title: &title}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("committed endpoint accepted a Plan point: %v", err)
	}
}

func TestPlanObjectiveReorderReturnsPersistedCanonicalPlan(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "reorder"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"o-a", "o-b", "o-c"} {
		plan, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{ID: id, Title: id}, "creator")
		if err != nil {
			t.Fatal(err)
		}
	}

	reordered, err := service.ReorderPlanObjectives(t.Context(), plan.ID, []string{"o-c", "o-a", "o-b"}, plan.Version, "reorderer")
	if err != nil {
		t.Fatal(err)
	}
	if reordered.UpdatedBy != "reorderer" || reordered.Version != plan.Version+1 {
		t.Fatalf("reordered plan metadata = %+v", reordered)
	}
	if got := []string{reordered.Objectives[0].ID, reordered.Objectives[1].ID, reordered.Objectives[2].ID}; got[0] != "o-c" || got[1] != "o-a" || got[2] != "o-b" {
		t.Fatalf("reordered objectives = %v", got)
	}
	if _, err := service.ReorderPlanObjectives(t.Context(), plan.ID, []string{"o-a", "o-b", "o-c"}, plan.Version, "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale plan order error = %v, want ErrConflict", err)
	}

	if _, err := service.ReorderPlanObjectives(t.Context(), plan.ID, []string{"o-a", "o-b"}, reordered.Version, "stale"); err == nil {
		t.Fatal("partial Plan order was accepted")
	}
	loaded, err := service.GetPlan(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Objectives[0].ID != "o-c" || loaded.Objectives[1].ID != "o-a" || loaded.Objectives[2].ID != "o-b" {
		t.Fatalf("invalid reorder changed persisted order: %+v", loaded.Objectives)
	}
}

func TestPlanValidationRejectsStructuralPointTags(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreatePlanObjective(t.Context(), created.ID, PlanObjectiveView{
		ID: "o", Title: "O", KRs: []PlanKRView{{
			ID: "kr", Title: "KR",
			Tags:   []TagView{{Type: domain.TagTypeBusinessCategory, Value: "业务"}, {Type: domain.TagTypePriority, Value: "p1"}},
			Points: []PlanPointView{{ID: "p", Kind: domain.PointKindProduct, Title: "产品 KR", Tags: []TagView{{Type: domain.TagTypePriority, Value: "p2"}}}},
		}},
	}, "tester")
	if err == nil {
		t.Fatal("CreatePlan accepted a structural tag on point")
	}
}
