package background

import (
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"
)

func TestOKRWeeklyViewKeepsCurrentConclusionBesideFactHistory(t *testing.T) {
	db := openBackgroundTestDB(t)
	from := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	until := from.Add(7 * 24 * time.Hour)
	old := from.Add(-14 * 24 * time.Hour)
	okrSummary := "当前结论：灰度已覆盖一半用户。"
	projectSummary := "当前结论：等待下一轮容量评估。"

	okr := domain.OKR{Title: "稳定完成季度发布", Cycle: "2026 Q3", Status: "推进中", Summary: &okrSummary, CreatedAt: old, UpdatedAt: old}
	if err := db.Create(&okr).Error; err != nil {
		t.Fatalf("create OKR: %v", err)
	}
	project := domain.Project{Name: "容量治理", Role: "owner", Status: "active", Priority: 1, Summary: &projectSummary, OKRID: &okr.ID, CreatedAt: old, UpdatedAt: old}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	due := from.Add(-24 * time.Hour)
	matter := domain.KeyMatter{Title: "完成扩容", Status: "推进中", ProjectID: &project.ID, DueAt: &due, LastActiveAt: old, CreatedAt: old, UpdatedAt: old}
	if err := db.Create(&matter).Error; err != nil {
		t.Fatalf("create key matter: %v", err)
	}

	events, err := progress.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	rootAt := from.Add(10 * time.Hour)
	if _, err := events.AppendFact(t.Context(), progress.FactInput{
		SubjectType: "okr", SubjectID: okr.ID, Description: "灰度覆盖率提升到 50%。", OccurredAt: &rootAt,
	}); err != nil {
		t.Fatalf("append OKR fact: %v", err)
	}
	matterAt := from.Add(20 * time.Hour)
	if _, err := events.AppendFact(t.Context(), progress.FactInput{
		SubjectType: "key_matter", SubjectID: matter.ID, Description: "容量审批阻塞，需要本周升级。", OccurredAt: &matterAt,
	}); err != nil {
		t.Fatalf("append key matter fact: %v", err)
	}

	service, err := NewOKRService(db)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return from.Add(3 * 24 * time.Hour) }
	view, err := service.WeeklyView(t.Context(), okr.ID, from, until)
	if err != nil {
		t.Fatalf("WeeklyView() error = %v", err)
	}
	if view.ChangeCount != 2 || view.RiskCount != 1 || view.StalledCount != 1 || len(view.Items) != 3 {
		t.Fatalf("weekly counts/items = changes:%d risk:%d stalled:%d items:%d", view.ChangeCount, view.RiskCount, view.StalledCount, len(view.Items))
	}
	if view.Items[0].Summary == nil || *view.Items[0].Summary != okrSummary || view.Items[0].Signal != "steady" {
		t.Fatalf("OKR weekly item = %+v, want current Page conclusion and steady signal", view.Items[0])
	}
	if view.Items[1].Signal != "stalled" || view.Items[1].ParentID == nil || *view.Items[1].ParentID != okr.ID {
		t.Fatalf("project weekly item = %+v, want stalled child of OKR", view.Items[1])
	}
	if view.Items[2].Signal != "risk" || len(view.Items[2].Facts) != 1 || view.Items[2].ParentID == nil || *view.Items[2].ParentID != project.ID {
		t.Fatalf("key matter weekly item = %+v, want risk child with Fact history", view.Items[2])
	}
}

func TestOKRWeeklyViewRejectsNonWeeklyWindow(t *testing.T) {
	service, err := NewOKRService(openBackgroundTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := service.WeeklyView(t.Context(), 1, from, from.Add(9*24*time.Hour)); err == nil {
		t.Fatal("WeeklyView() accepted a window longer than 8 days")
	}
}
