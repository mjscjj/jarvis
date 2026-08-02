package insight

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newProactiveDebugService(t *testing.T) (*DebugService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ProactiveRun{}); err != nil {
		t.Fatal(err)
	}
	return &DebugService{db: db}, db
}

func TestProactiveRunsListsSummaryAndLoadsFullDetail(t *testing.T) {
	service, db := newProactiveDebugService(t)
	output := "full output"
	finishedAt := time.Date(2026, 8, 3, 2, 1, 0, 0, time.UTC)
	duration := int64(60000)
	record := domain.ProactiveRun{
		ID:          7,
		TriggerType: "schedule", Engine: "traex", Model: "DeepSeek-V4-Pro", Status: "succeeded",
		Input: "full input", Output: &output, StartedAt: finishedAt.Add(-time.Minute),
		FinishedAt: &finishedAt, DurationMS: &duration,
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	rows, err := service.ProactiveRuns(t.Context(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != record.ID || rows[0].Model != "DeepSeek-V4-Pro" || rows[0].FinishedAt == nil {
		t.Fatalf("rows = %+v", rows)
	}
	detail, err := service.ProactiveRun(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Input != "full input" || detail.Output == nil || *detail.Output != output {
		t.Fatalf("detail = %+v", detail)
	}
	if _, err := service.ProactiveRun(t.Context(), record.ID+1); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing detail error = %v", err)
	}
}
