package meetingsweep

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"
)

type countingWorker struct {
	calls  atomic.Int32
	called chan struct{}
}

func (w *countingWorker) Run(context.Context, string) (string, error) {
	w.calls.Add(1)
	select {
	case w.called <- struct{}{}:
	default:
	}
	return "no meetings", nil
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
	return "no meetings", nil
}

func TestSchedulerRunsFirstSweepAfterStartupDelay(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	scheduler, err := StartScheduler(t.Context(), worker, "@every 1h", 20*time.Millisecond, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Stop()
	select {
	case <-worker.called:
	case <-time.After(time.Second):
		t.Fatal("startup sweep did not run")
	}
	if got := worker.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestSchedulerStopBeforeDelayPreventsStartupSweep(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	scheduler, err := StartScheduler(t.Context(), worker, "@every 1h", time.Hour, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Stop()
	if got := worker.calls.Load(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestSchedulerSkipsOverlappingSweep(t *testing.T) {
	worker := &blockingWorker{started: make(chan struct{}, 1), release: make(chan struct{})}
	scheduler, err := StartScheduler(t.Context(), worker, "@every 1h", time.Millisecond, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.started:
	case <-time.After(time.Second):
		t.Fatal("startup sweep did not begin")
	}
	scheduler.job.Run()
	if got := worker.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 while first sweep is still running", got)
	}
	close(worker.release)
	scheduler.Stop()
}

func TestSchedulerRejectsInvalidInput(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	if _, err := StartScheduler(t.Context(), worker, "", time.Second, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("expected error for empty spec")
	}
	if _, err := StartScheduler(t.Context(), worker, "not-a-cron", time.Second, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("expected error for invalid spec")
	}
	if _, err := StartScheduler(t.Context(), worker, "@every 1h", 0, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("expected error for non-positive startup delay")
	}
}
