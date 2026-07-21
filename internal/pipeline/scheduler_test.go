package pipeline

import (
	"context"
	"io"
	"log"
	"testing"
)

func TestStartSchedulerRejectsInvalidEnabledStageSpec(t *testing.T) {
	coordinator, err := newCoordinator(&fakeExtractor{}, nil, nil, nil, pipelineTestOptions())
	if err != nil {
		t.Fatalf("newCoordinator() error = %v", err)
	}
	if _, err := StartScheduler(
		context.Background(), coordinator,
		ScheduleConfig{Extract: "invalid"},
		log.New(io.Discard, "", 0),
	); err == nil {
		t.Fatal("StartScheduler() accepted invalid extract schedule")
	}
}
