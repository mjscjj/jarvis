package extract

import (
	"fmt"
	"testing"
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPipelineStorePersistsExtractionRunMetrics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ExtractionRun{}); err != nil {
		t.Fatal(err)
	}
	store := &PipelineStore{db: db}
	started := time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC)
	runID, err := store.StartExtractionRun(t.Context(), "chat-1", started)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishExtractionRun(t.Context(), runID, ExtractionRunFinish{
		Status: "succeeded", MessageCount: 12, TodoCount: 3,
		Usage:      agentusage.Usage{InputTokens: 100, CachedInputTokens: 20, OutputTokens: 30, ReasoningOutputTokens: 10, Reported: true},
		FinishedAt: started.Add(1500 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	var run domain.ExtractionRun
	if err := db.First(&run, runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" || run.MessageCount != 12 || run.TodoCount != 3 || run.DurationMs == nil || *run.DurationMs != 1500 {
		t.Fatalf("run = %+v", run)
	}
	if run.InputTokens == nil || *run.InputTokens != 100 || run.OutputTokens == nil || *run.OutputTokens != 30 {
		t.Fatalf("run token usage = %+v", run)
	}
}
