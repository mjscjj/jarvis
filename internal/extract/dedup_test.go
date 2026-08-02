package extract

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"jarvis/internal/semantic"
)

type fakeSemanticEmbedder struct {
	text   string
	vector []float32
	err    error
}

func (f *fakeSemanticEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.text = text
	return f.vector, f.err
}

type fakeSemanticSearcher struct {
	matches    []semantic.Match
	err        error
	actionType string
}

func (f *fakeSemanticSearcher) Search(_ context.Context, _ []float32, _ *uint64, actionType string) ([]semantic.Match, error) {
	f.actionType = actionType
	return f.matches, f.err
}

type fakeSemanticTodoLoader struct {
	todos map[uint64]*SemanticTodo
	err   error
}

func (f *fakeSemanticTodoLoader) LoadSemanticTodo(_ context.Context, id uint64) (*SemanticTodo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.todos[id], nil
}

type fakeSemanticAdjudicator struct {
	answers []bool
	err     error
	calls   int
}

func (f *fakeSemanticAdjudicator) SameAction(_ context.Context, _ Candidate, _ SemanticTodo) (bool, error) {
	position := f.calls
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.answers[position], nil
}

func TestDeduplicatorResolvesLLMConfirmedNeighbor(t *testing.T) {
	candidate := validCandidate()
	projectID := uint64(9)
	existing := semanticTodoFixture(t, candidate, &projectID, 41)
	existing.DedupFingerprint = strings.Repeat("a", 64)
	embedder := &fakeSemanticEmbedder{vector: []float32{0.1, 0.2}}
	searcher := &fakeSemanticSearcher{matches: []semantic.Match{{TodoID: existing.ID, Fingerprint: existing.DedupFingerprint, Score: 0.91}}}
	adjudicator := &fakeSemanticAdjudicator{answers: []bool{true}}
	dedup, err := NewDeduplicator(embedder, searcher, &fakeSemanticTodoLoader{todos: map[uint64]*SemanticTodo{existing.ID: existing}}, adjudicator)
	if err != nil {
		t.Fatalf("NewDeduplicator() error = %v", err)
	}
	resolution, err := dedup.Resolve(context.Background(), candidate, &projectID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolution.MatchedTodoID == nil || *resolution.MatchedTodoID != existing.ID || len(resolution.Vector) != 2 {
		t.Fatalf("resolution = %#v", resolution)
	}
	if adjudicator.calls != 1 || searcher.actionType != candidate.ActionType || !strings.Contains(embedder.text, candidate.Title) {
		t.Fatalf("calls=%d action_type=%q text=%q", adjudicator.calls, searcher.actionType, embedder.text)
	}
}

func TestDeduplicatorDoesNotMergeRejectedNeighbor(t *testing.T) {
	candidate := validCandidate()
	existing := semanticTodoFixture(t, candidate, nil, 41)
	existing.DedupFingerprint = strings.Repeat("b", 64)
	dedup, err := NewDeduplicator(
		&fakeSemanticEmbedder{vector: []float32{1}},
		&fakeSemanticSearcher{matches: []semantic.Match{{TodoID: existing.ID, Fingerprint: existing.DedupFingerprint}}},
		&fakeSemanticTodoLoader{todos: map[uint64]*SemanticTodo{existing.ID: existing}},
		&fakeSemanticAdjudicator{answers: []bool{false}},
	)
	if err != nil {
		t.Fatalf("NewDeduplicator() error = %v", err)
	}
	resolution, err := dedup.Resolve(context.Background(), candidate, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolution.MatchedTodoID != nil {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestDeduplicatorFailsOnAdjudicationError(t *testing.T) {
	candidate := validCandidate()
	existing := semanticTodoFixture(t, candidate, nil, 41)
	existing.DedupFingerprint = strings.Repeat("c", 64)
	dedup, err := NewDeduplicator(
		&fakeSemanticEmbedder{vector: []float32{1}},
		&fakeSemanticSearcher{matches: []semantic.Match{{TodoID: existing.ID, Fingerprint: existing.DedupFingerprint}}},
		&fakeSemanticTodoLoader{todos: map[uint64]*SemanticTodo{existing.ID: existing}},
		&fakeSemanticAdjudicator{err: errors.New("model unavailable")},
	)
	if err != nil {
		t.Fatalf("NewDeduplicator() error = %v", err)
	}
	if _, err := dedup.Resolve(context.Background(), candidate, nil); err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestDeduplicatorSkipsLLMForExactFingerprint(t *testing.T) {
	candidate := validCandidate()
	fingerprint, err := Fingerprint(&candidate, nil)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	existing := semanticTodoFixture(t, candidate, nil, 41)
	existing.DedupFingerprint = fingerprint
	adjudicator := &fakeSemanticAdjudicator{}
	dedup, err := NewDeduplicator(
		&fakeSemanticEmbedder{vector: []float32{1}},
		&fakeSemanticSearcher{matches: []semantic.Match{{TodoID: existing.ID, Fingerprint: fingerprint}}},
		&fakeSemanticTodoLoader{todos: map[uint64]*SemanticTodo{existing.ID: existing}}, adjudicator,
	)
	if err != nil {
		t.Fatalf("NewDeduplicator() error = %v", err)
	}
	resolution, err := dedup.Resolve(context.Background(), candidate, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolution.MatchedTodoID == nil || *resolution.MatchedTodoID != existing.ID || adjudicator.calls != 0 {
		t.Fatalf("resolution=%#v adjudicator.calls=%d", resolution, adjudicator.calls)
	}
}

func TestDeduplicatorTreatsMaterializedStatusAsActive(t *testing.T) {
	candidate := validCandidate()
	fingerprint, err := Fingerprint(&candidate, nil)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	existing := semanticTodoFixture(t, candidate, nil, 111)
	existing.Status = "materialized"
	existing.DedupFingerprint = fingerprint
	dedup, err := NewDeduplicator(
		&fakeSemanticEmbedder{vector: []float32{1}},
		&fakeSemanticSearcher{matches: []semantic.Match{{TodoID: existing.ID, Fingerprint: fingerprint}}},
		&fakeSemanticTodoLoader{todos: map[uint64]*SemanticTodo{existing.ID: existing}},
		&fakeSemanticAdjudicator{},
	)
	if err != nil {
		t.Fatalf("NewDeduplicator() error = %v", err)
	}
	resolution, err := dedup.Resolve(context.Background(), candidate, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolution.MatchedTodoID == nil || *resolution.MatchedTodoID != existing.ID {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestActiveTodoStatusesOnlyContainsCurrentLifecycle(t *testing.T) {
	want := []string{"extracted", "materialized", "observing"}
	if got := ActiveTodoStatuses(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveTodoStatuses() = %v, want %v", got, want)
	}
}

func semanticTodoFixture(t *testing.T, candidate Candidate, projectID *uint64, id uint64) *SemanticTodo {
	t.Helper()
	fingerprint, err := Fingerprint(&candidate, projectID)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	return &SemanticTodo{
		ID: id, ActionType: candidate.ActionType, Title: candidate.Title, Description: candidate.Payload,
		Target: candidate.Target, ProjectID: copyUint64(projectID), Status: "extracted", DedupFingerprint: fingerprint,
	}
}
