package capture

import (
	"context"
	"io"
	"log"
	"testing"
)

func TestStartScheduler(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	scheduler, err := StartScheduler(context.Background(), &Service{}, ScheduleConfig{
		Discover: "@every 6h",
		Scan:     "@every 5m",
	}, logger)
	if err != nil {
		t.Fatalf("StartScheduler() error = %v", err)
	}
	if got := len(scheduler.Entries()); got != 2 {
		t.Fatalf("scheduler entries = %d, want 2", got)
	}
	<-scheduler.Stop().Done()
}

func TestStartSchedulerRejectsInvalidSpec(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	_, err := StartScheduler(context.Background(), &Service{}, ScheduleConfig{
		Discover: "not-a-cron",
		Scan:     "@every 5m",
	}, logger)
	if err == nil {
		t.Fatal("StartScheduler() accepted invalid schedule")
	}
}
