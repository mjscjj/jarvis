package proactive

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
	return "NOTHING", nil
}

func (w *countingWorker) Run(context.Context, string) (string, error) {
	w.calls.Add(1)
	select {
	case w.called <- struct{}{}:
	default:
	}
	return "NOTHING", nil
}

func TestSchedulerRunsFirstHeartbeatAfterStartupDelay(t *testing.T) {
	worker := &countingWorker{called: make(chan struct{}, 1)}
	scheduler, err := StartScheduler(t.Context(), worker, "@every 1h", 20*time.Millisecond, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Stop()
	select {
	case <-worker.called:
	case <-time.After(time.Second):
		t.Fatal("startup heartbeat did not run")
	}
	if got := worker.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestSchedulerStopBeforeDelayPreventsStartupHeartbeat(t *testing.T) {
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

func TestSchedulerSkipsOverlappingHeartbeat(t *testing.T) {
	worker := &blockingWorker{started: make(chan struct{}, 1), release: make(chan struct{})}
	scheduler, err := StartScheduler(t.Context(), worker, "@every 1h", time.Millisecond, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.started:
	case <-time.After(time.Second):
		t.Fatal("startup heartbeat did not begin")
	}

	// The startup run and recurring runs share the same wrapped cron.Job.
	// A second invocation must return without entering the worker.
	scheduler.job.Run()
	if got := worker.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 while first heartbeat is still running", got)
	}
	close(worker.release)
	scheduler.Stop()
}
