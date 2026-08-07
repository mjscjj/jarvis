package insight

import (
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMonitoringAggregatesCoarseM3AndM5Metrics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScanRecord{}, &domain.ExtractionRun{}, &domain.ExecutionRun{}, &domain.Task{}); err != nil {
		t.Fatal(err)
	}
	service := &DebugService{db: db}
	from := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	inside := from.Add(time.Hour)
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	finished := inside.Add(time.Second)
	for _, run := range []domain.ScanRecord{
		{ScanType: "chat", Status: "ok", InsertedCount: 6, StartedAt: inside.In(shanghai), FinishedAt: timePtr(finished.In(shanghai)), DurationMS: i32(500)},
		{ScanType: "chat", Status: "error", InsertedCount: 0, StartedAt: inside.Add(time.Minute), FinishedAt: &finished, DurationMS: i32(1500)},
	} {
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []domain.ExtractionRun{
		{ChatID: "a", Status: "succeeded", MessageCount: 12, TodoCount: 3, InputTokens: i64(100), OutputTokens: i64(20), StartedAt: inside, FinishedAt: &finished, DurationMs: i64(1000)},
		{ChatID: "b", Status: "failed", MessageCount: 8, TodoCount: 0, StartedAt: inside.Add(time.Minute), FinishedAt: &finished, DurationMs: i64(3000)},
		{ChatID: "old", Status: "succeeded", MessageCount: 99, StartedAt: from.Add(-time.Hour), FinishedAt: &finished, DurationMs: i64(9000)},
	} {
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []domain.ExecutionRun{
		{TaskID: 1, ActionType: "x", Stage: "execute", Sandbox: "read-only", Status: "succeeded", Prompt: "p", InputTokens: i64(200), OutputTokens: i64(50), StartedAt: inside, FinishedAt: &finished, DurationMs: i64(2000)},
		{TaskID: 1, ActionType: "x", Stage: "execute", Sandbox: "read-only", Status: "succeeded", Prompt: "p", InputTokens: i64(40), OutputTokens: i64(10), StartedAt: inside.Add(time.Minute), FinishedAt: &finished, DurationMs: i64(4000)},
		{TaskID: 2, ActionType: "x", Stage: "execute", Sandbox: "read-only", Status: "failed", Prompt: "p", StartedAt: inside.Add(2 * time.Minute), FinishedAt: &finished, DurationMs: i64(6000)},
	} {
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := service.Monitoring(t.Context(), from, from.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.M3.ChatCount != 2 || snapshot.M3.RunCount != 2 || snapshot.M3.ProcessedMessages != 12 || snapshot.M3.TodosCreated != 3 || snapshot.M3.FailedRuns != 1 {
		t.Fatalf("M3 = %+v", snapshot.M3)
	}
	if snapshot.M2.InsertedMessages != 6 || snapshot.M2.RunCount != 2 || snapshot.M2.FailedRuns != 1 || snapshot.M2.AverageDurationMS == nil || *snapshot.M2.AverageDurationMS != 1000 {
		t.Fatalf("M2 = %+v", snapshot.M2)
	}
	if len(snapshot.M2.Series) != 24 || snapshot.M2.Series[1].ScopeCount != 6 || snapshot.M2.Series[1].RunCount != 2 || snapshot.M2.Series[1].FailedRuns != 1 {
		t.Fatalf("M2 series = %+v", snapshot.M2.Series)
	}
	if snapshot.M3.AverageDurationMS == nil || *snapshot.M3.AverageDurationMS != float64(13000)/3 || snapshot.M3.MaxDurationMS == nil || *snapshot.M3.MaxDurationMS != 9000 {
		t.Fatalf("M3 durations = %+v", snapshot.M3)
	}
	if snapshot.M3.TotalTokens == nil || *snapshot.M3.TotalTokens != 120 || snapshot.M3.TokenCoverageComplete {
		t.Fatalf("M3 token coverage = %+v", snapshot.M3)
	}
	if snapshot.M5.ProcessedTasks != 2 || snapshot.M5.RunCount != 3 || snapshot.M5.FailedRuns != 1 || snapshot.M5.AverageDurationMS == nil || *snapshot.M5.AverageDurationMS != 4000 {
		t.Fatalf("M5 = %+v", snapshot.M5)
	}
	if snapshot.M5.TotalTokens == nil || *snapshot.M5.TotalTokens != 300 || snapshot.M5.TokenCoverageComplete {
		t.Fatalf("M5 token coverage = %+v", snapshot.M5)
	}
	if snapshot.Bucket != "1h0m0s" || len(snapshot.M3.Series) != 24 || snapshot.M3.Series[1].RunCount != 2 || snapshot.M3.Series[1].FailedRuns != 1 {
		t.Fatalf("M3 series = bucket %q points=%+v", snapshot.Bucket, snapshot.M3.Series)
	}
}

func i64(value int64) *int64             { return &value }
func i32(value int32) *int32             { return &value }
func timePtr(value time.Time) *time.Time { return &value }
