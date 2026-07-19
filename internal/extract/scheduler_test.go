package extract

import (
	"context"
	"io"
	"log"
	"testing"
)

func TestExtractSchedulerRejectsInvalidSpec(t *testing.T) {
	worker, err := NewWorker(&fakePipelineStore{}, &fakeModelExtractor{}, &fakeMemorySearcher{}, &fakeCandidateDeduplicator{}, validWorkerOptions())
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := StartScheduler(context.Background(), worker, "invalid", log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("StartScheduler() accepted invalid schedule")
	}
}
