package okrworkspace

import (
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestProgressEntryCRUDChangesOnlyOneWeeklyRecord(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-progress", Quarter: "2026-Q3", Title: "增长"}
	kr := domain.KR{ID: "kr-progress", ObjectiveID: objective.ID, Title: "发布"}
	point := domain.KRPoint{ID: "point-progress", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "灰度"}
	week := domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: "2026-W36", OpenedBy: "test"}
	for _, value := range []any{&objective, &kr, &point, &week} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	created, err := service.CreateProgressEntry(t.Context(), point.ID, ProgressEntryInput{
		ID: "agent-progress-1", ExpectedVersion: 0, Week: week.Week, Status: domain.StatusInProgress,
		Text: "完成第一阶段", Docs: []domain.DocLink{{ID: "doc-1", Title: "证据", URL: "https://example.com"}},
		Images: []domain.ImageRef{}, Source: "agent", NeedsReview: true, UpdatedBy: "agent:task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || len(created.Points[0].Entries) != 1 || created.Points[0].Entries[0].Text != "完成第一阶段" {
		t.Fatalf("created = %+v", created)
	}
	idempotent, err := service.CreateProgressEntry(t.Context(), point.ID, ProgressEntryInput{
		ID: "agent-progress-1", ExpectedVersion: 0, Week: week.Week, Status: domain.StatusInProgress,
		Text: "完成第一阶段", Docs: []domain.DocLink{{ID: "doc-1", Title: "证据", URL: "https://example.com"}},
		Images: []domain.ImageRef{}, Source: "agent", NeedsReview: true, UpdatedBy: "agent:task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Version != 1 {
		t.Fatalf("idempotent create changed version: %+v", idempotent)
	}

	updated, err := service.UpdateProgressEntry(t.Context(), "agent-progress-1", ProgressEntryInput{
		ExpectedVersion: 1, Week: week.Week, Status: domain.StatusDone, Text: "第一阶段已完成",
		Docs: []domain.DocLink{}, Images: []domain.ImageRef{}, Source: "agent", UpdatedBy: "agent:task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Points[0].Entries[0].Status != domain.StatusDone || updated.Points[0].Entries[0].NeedsReview {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := service.UpdateProgressEntry(t.Context(), "agent-progress-1", ProgressEntryInput{
		ExpectedVersion: 1, Week: week.Week, Status: domain.StatusDone, Text: "旧版本覆盖", Source: "agent", UpdatedBy: "agent:task-1",
	}); err != ErrConflict {
		t.Fatalf("stale update error = %v, want ErrConflict", err)
	}

	deleted, err := service.DeleteProgressEntry(t.Context(), "agent-progress-1", DeleteProgressEntryInput{ExpectedVersion: 2, UpdatedBy: "agent:task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Version != 3 || len(deleted.Points[0].Entries) != 0 {
		t.Fatalf("deleted = %+v", deleted)
	}
}

func TestProgressEntryRequiresOpenedWeek(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-closed", Quarter: "2026-Q3", Title: "增长"}
	kr := domain.KR{ID: "kr-closed", ObjectiveID: objective.ID, Title: "发布"}
	point := domain.KRPoint{ID: "point-closed", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "灰度"}
	for _, value := range []any{&objective, &kr, &point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateProgressEntry(t.Context(), point.ID, ProgressEntryInput{
		ID: "agent-progress-closed", Week: "2026-W36", Status: domain.StatusInProgress, Text: "不应写入", Source: "agent", UpdatedBy: "agent:task-1",
	}); err == nil {
		t.Fatal("create progress succeeded for unopened week")
	}
}
