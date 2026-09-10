package okrworkspace

import (
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestStalePlanSnapshotCannotDeleteCollaboratorChanges(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "并发删除保护", CreatedBy: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{
		ID: "guard-plan-o", Title: "O", KRs: []PlanKRView{{
			ID: "guard-plan-kr", Title: "KR", Points: []PlanPointView{{ID: "guard-plan-point", Kind: domain.PointKindProduct, Title: "旧内容"}},
		}},
	}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	staleToken := plan.DeleteToken
	newTitle := "协作者刚保存的新内容"
	if _, err := service.PatchPlanPointDefinition(t.Context(), plan.ID, "guard-plan-point", PatchPointDefinitionInput{ExpectedVersion: 0, Title: &newTitle, UpdatedBy: "collaborator"}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeletePlan(t.Context(), plan.ID, staleToken); err != ErrConflict {
		t.Fatalf("stale plan delete error = %v, want ErrConflict", err)
	}
	current, err := service.GetPlan(t.Context(), plan.ID)
	if err != nil {
		t.Fatalf("plan was deleted after stale request: %v", err)
	}
	if got := current.Objectives[0].KRs[0].Points[0].Title; got != newTitle {
		t.Fatalf("point title = %q, want %q", got, newTitle)
	}
	if err := service.DeletePlan(t.Context(), plan.ID, current.DeleteToken); err != nil {
		t.Fatalf("fresh plan delete: %v", err)
	}
}

func TestStaleWeekSnapshotCannotDeleteCollaboratorChanges(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "guard-week-o", Quarter: "2026-Q3", Title: "O"}
	kr := domain.KR{ID: "guard-week-kr", ObjectiveID: objective.ID, Title: "KR"}
	point := domain.KRPoint{ID: "guard-week-point", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "Point"}
	for _, value := range []any{&objective, &kr, &point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: domain.WeekTemplateClassic, OpenedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	stale, err := service.Board(t.Context(), objective.Quarter, "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		Week: "2026-W36", ExpectedVersion: 0, MetricNote: "协作者刚写的核心数据", UpdatedBy: "collaborator",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteWeek(t.Context(), objective.Quarter, "2026-W36", stale.DeleteToken); err != ErrConflict {
		t.Fatalf("stale week delete error = %v, want ErrConflict", err)
	}
	current, err := service.Board(t.Context(), objective.Quarter, "2026-W36")
	if err != nil {
		t.Fatalf("week was deleted after stale request: %v", err)
	}
	if got := current.Objectives[0].KRs[0].MetricNote; got != "协作者刚写的核心数据" {
		t.Fatalf("weekly core = %q", got)
	}
	if _, err := service.DeleteWeek(t.Context(), objective.Quarter, "2026-W36", current.DeleteToken); err != nil {
		t.Fatalf("fresh week delete: %v", err)
	}
}

func TestStalePlanStructureCannotRemoveCollaboratorEditedPoint(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q3", Title: "结构删除保护"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{
		ID: "structure-o", Title: "O", KRs: []PlanKRView{{
			ID: "structure-kr", Title: "KR", Points: []PlanPointView{{ID: "structure-point", Kind: domain.PointKindStrategy, Title: "旧内容"}},
		}},
	}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	stale := plan.Objectives[0]
	newTitle := "协作者刚保存的 Point"
	if _, err := service.PatchPlanPointDefinition(t.Context(), plan.ID, "structure-point", PatchPointDefinitionInput{ExpectedVersion: 0, Title: &newTitle}); err != nil {
		t.Fatal(err)
	}
	stale.KRs[0].Points = nil
	_, err = service.UpdatePlanObjective(t.Context(), plan.ID, stale.ID, PlanObjectiveWriteInput{
		ExpectedVersion: stale.Version, ExpectedStructureToken: stale.StructureToken, Objective: stale,
	})
	if err != ErrConflict {
		t.Fatalf("stale structure removal error = %v, want ErrConflict", err)
	}
	current, err := service.GetPlan(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := current.Objectives[0].KRs[0].Points[0].Title; got != newTitle {
		t.Fatalf("collaborator point = %q, want %q", got, newTitle)
	}
}

func TestStaleKRSnapshotCannotDeleteCollaboratorEditedPoint(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "guard-kr-o", Quarter: "2026-Q3", Title: "O"}
	kr := domain.KR{ID: "guard-kr", ObjectiveID: objective.ID, Title: "KR"}
	point := domain.KRPoint{ID: "guard-kr-point", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "旧内容"}
	for _, value := range []any{&objective, &kr, &point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := service.GetCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	newTitle := "协作者刚保存的 Point"
	if _, err := service.PatchPointDefinition(t.Context(), point.ID, PatchPointDefinitionInput{ExpectedVersion: 0, Title: &newTitle}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteKR(t.Context(), kr.ID, DeleteKRInput{ExpectedVersion: stale.Version, DeleteToken: stale.DeleteToken}); err != ErrConflict {
		t.Fatalf("stale KR delete error = %v, want ErrConflict", err)
	}
	current, err := service.GetCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := current.Points[0].Title; got != newTitle {
		t.Fatalf("collaborator point = %q, want %q", got, newTitle)
	}
}

func TestStaleKRSnapshotCannotRemoveCollaboratorEditedPoint(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "guard-structure-o", Quarter: "2026-Q3", Title: "O"}
	kr := domain.KR{ID: "guard-structure-kr", ObjectiveID: objective.ID, Title: "KR"}
	point := domain.KRPoint{ID: "guard-structure-point", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "旧内容"}
	for _, value := range []any{&objective, &kr, &point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := service.GetBizCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	newTitle := "协作者刚保存的 Point"
	if _, err := service.PatchPointDefinition(t.Context(), point.ID, PatchPointDefinitionInput{ExpectedVersion: 0, Title: &newTitle}); err != nil {
		t.Fatal(err)
	}
	stale.Points = nil
	if _, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: stale.Version,
		DeleteToken:     stale.DeleteToken,
		Title:           stale.Title,
		MetricNote:      stale.MetricNote,
		Metrics:         stale.Metrics,
		Points:          stale.Points,
		Tags:            stale.Tags,
		Owners:          stale.Owners,
	}); err != ErrConflict {
		t.Fatalf("stale point removal error = %v, want ErrConflict", err)
	}
	current, err := service.GetBizCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Points) != 1 || current.Points[0].Title != newTitle {
		t.Fatalf("collaborator point was removed or overwritten: %+v", current.Points)
	}
}
