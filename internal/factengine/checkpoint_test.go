package factengine

import (
	"context"
	"errors"
	"testing"
	"time"
)

type failOnUnitExtractor struct {
	failKey string
}

func (f failOnUnitExtractor) Extract(_ context.Context, _ string, unit SourceUnit) ([]ExtractedFact, error) {
	if unit.Key == f.failKey {
		return nil, errors.New("model failed")
	}
	return nil, nil
}

func TestOrderedEventSourceCheckpointsEverySuccessfulWindow(t *testing.T) {
	first := SourceUnit{Source: SourceTodo, Key: "todo_events:1-3", LastID: 3, OccurredAt: time.Now().UTC(), Body: "first"}
	second := SourceUnit{Source: SourceTodo, Key: "todo_events:4-6", LastID: 6, OccurredAt: time.Now().UTC(), Body: "second"}
	store := &fakeStore{cursorSeeded: true}
	store.sources = []MaterialSource{{
		Name: SourceTodo, CheckpointEachUnit: true,
		MaxID: func(context.Context) (uint64, error) { return 6, nil },
		Units: func(context.Context, uint64, int, WindowOptions) ([]SourceUnit, uint64, error) {
			return []SourceUnit{first, second}, 6, nil
		},
	}}
	worker := newTestWorker(t, store, failOnUnitExtractor{failKey: second.Key}, &fakeAppender{})

	if _, err := worker.ExtractOnce(context.Background()); err == nil {
		t.Fatal("ExtractOnce() error = nil, want second-window failure")
	}
	if len(store.advanced) != 1 || store.advanced[0] != first.LastID {
		t.Fatalf("advanced cursors = %v, want [%d]", store.advanced, first.LastID)
	}
}
