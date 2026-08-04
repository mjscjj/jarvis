package morningbrief

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// midnightSpec makes the startup catch-up gate deterministic: today's slot is
// always already in the past, whatever hour the test runs at.
const midnightSpec = "0 0 * * *"

type countingWorker struct {
	calls     atomic.Int32
	called    chan struct{}
	hasBrief  bool
	askedDays atomic.Int32
}

func (w *countingWorker) Run(context.Context, string) (string, error) {
	w.calls.Add(1)
	select {
	case w.called <- struct{}{}:
	default:
	}
	return "brief ok", nil
}

func (w *countingWorker) HasBriefFor(time.Time) bool {
	w.askedDays.Add(1)
	return w.hasBrief
}

type blockingWorker struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (w *blockingWorker) Run(context.Context, string) (string, error) {
	w.calls.Add(1)
	select {
	case w.started <- struct{}{}:
	default:
	}
	<-w.release
	return "brief ok", nil
}

func (w *blockingWorker) HasBriefFor(time.Time) bool { return false }

func TestSchedulerCatchesUpWhenTodaysBriefIsMissing(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	scheduler, err := StartScheduler(t.Context(), worker, midnightSpec, 20*time.Millisecond, time.UTC, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Stop()
	select {
	case <-worker.called:
	case <-time.After(time.Second):
		t.Fatal("startup catch-up did not run")
	}
	if got := worker.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestSchedulerSkipsCatchUpWhenTodaysBriefExists(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1), hasBrief: true}
	scheduler, err := StartScheduler(t.Context(), worker, midnightSpec, time.Millisecond, time.UTC, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Stop()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && worker.askedDays.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := worker.askedDays.Load(); got != 1 {
		t.Fatalf("HasBriefFor calls = %d, want 1", got)
	}
	if got := worker.calls.Load(); got != 0 {
		t.Fatalf("calls = %d, want 0 because today's brief already exists", got)
	}
}

func TestSchedulerStopBeforeDelayPreventsCatchUp(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	scheduler, err := StartScheduler(t.Context(), worker, midnightSpec, time.Hour, time.UTC, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Stop()
	if got := worker.calls.Load(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestSchedulerSkipsOverlappingBrief(t *testing.T) {
	worker := &blockingWorker{started: make(chan struct{}, 1), release: make(chan struct{})}
	scheduler, err := StartScheduler(t.Context(), worker, midnightSpec, time.Millisecond, time.UTC, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.started:
	case <-time.After(time.Second):
		t.Fatal("startup brief did not begin")
	}
	scheduler.job.Run()
	if got := worker.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 while first brief is still running", got)
	}
	close(worker.release)
	scheduler.Stop()
}

func TestSchedulerRejectsInvalidInput(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	logger := log.New(io.Discard, "", 0)
	if _, err := StartScheduler(t.Context(), worker, "", time.Second, time.UTC, logger); err == nil {
		t.Fatal("expected error for empty spec")
	}
	if _, err := StartScheduler(t.Context(), worker, "not-a-cron", time.Second, time.UTC, logger); err == nil {
		t.Fatal("expected error for invalid cron")
	}
	if _, err := StartScheduler(t.Context(), worker, midnightSpec, 0, time.UTC, logger); err == nil {
		t.Fatal("expected error for non-positive startup delay")
	}
	if _, err := StartScheduler(t.Context(), worker, midnightSpec, time.Second, nil, logger); err == nil {
		t.Fatal("expected error for nil location")
	}
}

func TestCatchUpDueFollowsScheduleNotAWeekdayRuleInGo(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	workdayMornings, err := cron.ParseStandard("30 8 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"weekday after the slot", time.Date(2026, 8, 4, 9, 0, 0, 0, location), true},
		{"weekday exactly at the slot", time.Date(2026, 8, 4, 8, 30, 0, 0, location), true},
		{"weekday before the slot", time.Date(2026, 8, 4, 7, 59, 0, 0, location), false},
		{"saturday", time.Date(2026, 8, 8, 20, 0, 0, 0, location), false},
		{"sunday", time.Date(2026, 8, 9, 20, 0, 0, 0, location), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := catchUpDue(workdayMornings, testCase.now, location); got != testCase.want {
				t.Fatalf("catchUpDue(%s) = %t, want %t", testCase.now.Format(time.RFC3339), got, testCase.want)
			}
		})
	}
}
