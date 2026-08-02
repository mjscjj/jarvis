package proactive

import (
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE proactive_run (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trigger_type TEXT NOT NULL,
		engine TEXT NOT NULL,
		model TEXT NOT NULL,
		status TEXT NOT NULL,
		input TEXT NOT NULL,
		output TEXT,
		error_detail TEXT,
		started_at DATETIME NOT NULL,
		finished_at DATETIME,
		duration_ms INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestStoreRecordsCompleteSuccessfulRun(t *testing.T) {
	store, db := newTestStore(t)
	startedAt := time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC)
	id, err := store.Start(t.Context(), TriggerSchedule, "traex", "DeepSeek-V4-Pro", "complete input", startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Succeed(t.Context(), id, "complete output", startedAt.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var record domain.ProactiveRun
	if err := db.First(&record, id).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != RunStatusSucceeded || record.Output == nil || *record.Output != "complete output" || record.ErrorDetail != nil {
		t.Fatalf("record = %+v", record)
	}
	if record.DurationMS == nil || *record.DurationMS != 1500 {
		t.Fatalf("duration_ms = %v", record.DurationMS)
	}
}

func TestStoreRecordsFailureAndRejectsSecondFinish(t *testing.T) {
	store, db := newTestStore(t)
	startedAt := time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC)
	id, err := store.Start(t.Context(), TriggerManual, "traex", "model", "input", startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(t.Context(), id, "runner timeout", startedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Succeed(t.Context(), id, "late output", startedAt.Add(2*time.Second)); err == nil {
		t.Fatal("second finish unexpectedly succeeded")
	}
	var record domain.ProactiveRun
	if err := db.First(&record, id).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != RunStatusFailed || record.ErrorDetail == nil || *record.ErrorDetail != "runner timeout" || record.Output != nil {
		t.Fatalf("record = %+v", record)
	}
}
