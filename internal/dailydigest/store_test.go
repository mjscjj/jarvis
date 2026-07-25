package dailydigest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE daily_digest (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			digest_date DATE NOT NULL,
			summary TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			trigger_type TEXT NOT NULL DEFAULT 'manual',
			source_count INTEGER NOT NULL DEFAULT 0,
			source_coverage JSON,
			engine TEXT NOT NULL,
			error_detail TEXT,
			started_at DATETIME,
			cutoff_at DATETIME,
			generated_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(scope, scope_id, digest_date)
		)
	`).Error; err != nil {
		t.Fatalf("create daily digest table: %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	store, err := NewStore(db, location)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func TestClaimGenerationManualAndSchedulePolicy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const date = "2026-07-23"

	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerManual, true); err != nil {
		t.Fatalf("first manual claim: %v", err)
	}
	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerManual, true); !errors.Is(err, ErrAlreadyGenerating) {
		t.Fatalf("duplicate manual claim = %v", err)
	}
	cutoff, err := time.ParseInLocation("2006-01-02 15:04", "2026-07-23 19:00", store.location)
	if err != nil {
		t.Fatalf("parse cutoff: %v", err)
	}
	if err := store.SetDone(ctx, ScopePerson, "ou_me", date, "总结", 2, SourceCoverage{
		"jarvis_tasks": {Status: "ok", Count: 2},
	}, cutoff); err != nil {
		t.Fatalf("set done: %v", err)
	}
	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerSchedule, false); !errors.Is(err, ErrAlreadyDone) {
		t.Fatalf("schedule claim after done = %v", err)
	}
	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerManual, true); err != nil {
		t.Fatalf("manual recompute claim: %v", err)
	}
	view, err := store.GetByScopeDate(ctx, ScopePerson, "ou_me", date)
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if view.Status != StatusGenerating || view.TriggerType != TriggerManual || view.StartedAt == nil {
		t.Fatalf("view after recompute = %#v", view)
	}
}

func TestRecoverInterruptedGeneration(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_recover", "2026-07-22", TriggerSchedule, false); err != nil {
		t.Fatalf("claim: %v", err)
	}
	recovered, err := store.RecoverInterruptedGeneration(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	view, err := store.GetByScopeDate(ctx, ScopePerson, "ou_recover", "2026-07-22")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Status != StatusFailed || view.ErrorDetail == nil {
		t.Fatalf("view after recovery = %#v", view)
	}
}

func TestScheduledClaimDoesNotRetryFailedAttempt(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const date = "2026-07-25"

	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerSchedule, false); err != nil {
		t.Fatalf("first schedule claim: %v", err)
	}
	if err := store.SetFailed(ctx, ScopePerson, "ou_me", date, "collector stopped"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerSchedule, false); !errors.Is(err, ErrAlreadyAttempted) {
		t.Fatalf("schedule retry after failed = %v, want ErrAlreadyAttempted", err)
	}
	view, err := store.GetByScopeDate(ctx, ScopePerson, "ou_me", date)
	if err != nil {
		t.Fatalf("get failed digest: %v", err)
	}
	if view.Status != StatusFailed || view.ErrorDetail == nil || *view.ErrorDetail != "collector stopped" {
		t.Fatalf("automatic retry mutated failed digest: %#v", view)
	}
	if err := store.ClaimGeneration(ctx, ScopePerson, "ou_me", date, TriggerManual, true); err != nil {
		t.Fatalf("manual retry after failed: %v", err)
	}
}
