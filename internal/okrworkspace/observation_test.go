package okrworkspace

import (
	"testing"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStoreMeegoObservationUsesSuppliedSnapshotWithoutExternalReader(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	objective := domain.Objective{ID: "objective-1", Quarter: "2026-Q3", Title: "增长"}
	kr := domain.KR{ID: "kr-1", ObjectiveID: objective.ID, Title: "发布"}
	point := domain.KRPoint{
		ID: "point-1", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "发布",
		MeegoWorkItemID: "wi-42", MeegoURL: "https://meego.example.com/wi-42",
	}
	week := domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: "2026-W35", OpenedBy: "test"}
	for _, value := range []any{&objective, &kr, &point, &week} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 8, 28, 6, 0, 0, 0, time.UTC)
	progress := domain.KRProgress{
		ID: "progress-1", PointID: point.ID, Week: "2026-W35", Status: domain.StatusInProgress,
		Text: "页面进展", Docs: []domain.DocLink{}, Images: []domain.ImageRef{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&progress).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.StoreMeegoObservation(t.Context(), MeegoObservationInput{
		PointID: point.ID, WorkItemID: point.MeegoWorkItemID, Week: "2026-W35", ObservedAt: now,
		Remote: PreviewContent{Title: "发布", Status: "完成", Progress: "远端已完成", UpdatedAt: now.Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview == nil || !result.Preview.Diff.StatusChanged || !result.Preview.Diff.ProgressChanged || result.Sync == nil || result.Sync.Status != "healthy" {
		t.Fatalf("StoreMeegoObservation() = %+v", result)
	}
	preview, err := service.MeegoPreview(t.Context(), point.ID, "2026-W35")
	if err != nil || preview.Remote.Progress != "远端已完成" {
		t.Fatalf("MeegoPreview() = %+v, %v", preview, err)
	}
	confirmed, err := service.ConfirmMeegoProgress(t.Context(), point.ID, ConfirmMeegoProgressInput{
		ExpectedVersion: 0, Week: week.Week, UpdatedBy: "agent:task-1", MeegoWorkItemID: point.MeegoWorkItemID,
		Status: domain.StatusInProgress, Text: "Meego 进展已确认",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmMeegoProgress(t.Context(), point.ID, ConfirmMeegoProgressInput{
		ExpectedVersion: 0, Week: week.Week, UpdatedBy: "agent:stale", MeegoWorkItemID: point.MeegoWorkItemID,
		Status: domain.StatusDone, Text: "旧页面覆盖",
	}); err != ErrConflict {
		t.Fatalf("second first-write Meego error = %v, want conflict", err)
	}
	confirmed, err = service.ConfirmMeegoProgress(t.Context(), point.ID, ConfirmMeegoProgressInput{
		ExpectedVersion: 1, Week: week.Week, UpdatedBy: "agent:task-1", MeegoWorkItemID: point.MeegoWorkItemID,
		Status: domain.StatusDone, Text: "Meego 进展已完成",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := confirmed.Points[0].Entries
	if len(entries) != 2 || entries[1].Version != 2 || entries[1].Status != domain.StatusDone || len(entries[1].Docs) != 1 || entries[1].Docs[0].URL != point.MeegoURL {
		t.Fatalf("confirmed Meego progress = %+v", entries)
	}
}
