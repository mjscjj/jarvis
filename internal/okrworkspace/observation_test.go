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
	point := domain.KRPoint{ID: "point-1", KRID: "kr-1", Kind: domain.PointKindStrategy, Title: "发布", MeegoWorkItemID: "wi-42"}
	if err := db.Create(&point).Error; err != nil {
		t.Fatal(err)
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
}
