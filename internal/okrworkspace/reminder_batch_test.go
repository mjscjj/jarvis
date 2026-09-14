package okrworkspace

import (
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestReminderBatchCountsOnlySendableRecipients(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	objective := domain.Objective{ID: "o-reminder", Title: "方向", Quarter: "2026-Q3"}
	if err := db.Create(&objective).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		krID, pointID, name, openID string
	}{
		{krID: "kr-sendable", pointID: "point-sendable", name: "甲", openID: "a@example.test"},
		{krID: "kr-unresolved", pointID: "point-unresolved", name: "乙"},
	} {
		if err := db.Create(&domain.KR{ID: item.krID, ObjectiveID: objective.ID, Title: item.krID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&domain.KRPoint{ID: item.pointID, KRID: item.krID, Kind: domain.PointKindStrategy, Title: item.pointID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&domain.KROwner{KRID: item.krID, Name: item.name, Email: item.openID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: domain.WeekTemplateClassic, OpenedBy: "owner@example.test"}); err != nil {
		t.Fatal(err)
	}
	batch, err := service.GenerateReminderBatch(t.Context(), objective.Quarter, "2026-W36", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if batch.RecipientCount != 1 || batch.MissingCount != 2 {
		t.Fatalf("batch counts = recipients:%d missing:%d", batch.RecipientCount, batch.MissingCount)
	}
}
