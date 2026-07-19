package memory

import (
	"context"
	"io"
	"log"
	"testing"
	"time"
)

func TestStartScheduler(t *testing.T) {
	worker, err := NewWorker(&fakeStore{}, &fakeAdder{}, WorkerOptions{
		BatchLimit: 10, WindowGap: time.Minute, WindowMaxMessages: 40, Location: time.UTC,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	scheduler, err := StartScheduler(context.Background(), worker, "@every 10m", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("StartScheduler() error = %v", err)
	}
	if got := len(scheduler.Entries()); got != 1 {
		t.Fatalf("scheduler entries = %d, want 1", got)
	}
	<-scheduler.Stop().Done()
}

func TestStartSchedulerRejectsInvalidSpec(t *testing.T) {
	worker, err := NewWorker(&fakeStore{}, &fakeAdder{}, WorkerOptions{
		BatchLimit: 10, WindowGap: time.Minute, WindowMaxMessages: 40, Location: time.UTC,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := StartScheduler(context.Background(), worker, "invalid", log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("StartScheduler() accepted invalid spec")
	}
}
