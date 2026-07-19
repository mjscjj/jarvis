package extract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/internal/extract/tools"
	"jarvis/internal/memory"
)

type fakePipelineStore struct {
	batches      []ChatBatch
	loadErr      error
	persistErr   error
	persistCalls int
	results      []UnitExtraction
}

func (f *fakePipelineStore) LoadPendingChats(context.Context, LoadOptions) ([]ChatBatch, error) {
	return f.batches, f.loadErr
}

func (f *fakePipelineStore) PersistChat(_ context.Context, _ ChatBatch, results []UnitExtraction, _ string) (PersistStats, error) {
	f.persistCalls++
	f.results = results
	if f.persistErr != nil {
		return PersistStats{}, f.persistErr
	}
	return PersistStats{Created: 2, Updated: 1}, nil
}

type fakeModelExtractor struct {
	result    *ExtractionResult
	err       error
	prompts   []Prompt
	maxRounds []int
	boxes     []ToolBox
}

func (f *fakeModelExtractor) ExtractWithTools(_ context.Context, prompt Prompt, box ToolBox, maxRounds int) (*ExtractionResult, error) {
	f.prompts = append(f.prompts, prompt)
	f.maxRounds = append(f.maxRounds, maxRounds)
	f.boxes = append(f.boxes, box)
	return f.result, f.err
}

// fakeToolBox is a no-op ToolBox for worker wiring tests.
type fakeToolBox struct{}

func (fakeToolBox) Specs() []tools.Spec { return nil }

func (fakeToolBox) Invoke(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// fakeToolBoxBuilder records the units it built a box for.
type fakeToolBoxBuilder struct {
	err   error
	built int
}

func (f *fakeToolBoxBuilder) Build(ChatBatch, ConversationUnit) (ToolBox, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.built++
	return fakeToolBox{}, nil
}

type fakeMemorySearcher struct {
	inputs []memory.SearchInput
	err    error
}

type fakeCandidateDeduplicator struct {
	inputs []Candidate
	err    error
}

func (f *fakeCandidateDeduplicator) Resolve(_ context.Context, candidate Candidate, _ *uint64) (SemanticResolution, error) {
	f.inputs = append(f.inputs, candidate)
	if f.err != nil {
		return SemanticResolution{}, f.err
	}
	return SemanticResolution{Vector: []float32{1}}, nil
}

func (f *fakeMemorySearcher) Search(_ context.Context, input memory.SearchInput) (*memory.SearchResponse, error) {
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return nil, f.err
	}
	return &memory.SearchResponse{Results: []map[string]any{}}, nil
}

func TestWorkerExtractOncePersistsWholeChat(t *testing.T) {
	projectID := uint64(9)
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_1", ProjectID: &projectID},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "连通性消息，无行动项", CreateTime: 1_700_000_000_000,
			IsNew: true, Extractable: true,
		}}}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true, CreateTime: 1_700_000_000_000},
	}}}
	model := &fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{}}}
	memories := &fakeMemorySearcher{}
	toolBox := &fakeToolBoxBuilder{}
	worker, err := NewWorker(store, model, memories, &fakeCandidateDeduplicator{}, toolBox, validWorkerOptions())
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	stats, err := worker.ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats.ChatsLoaded != 1 || stats.ChatsProcessed != 1 || stats.Units != 1 || stats.Created != 2 || stats.Updated != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if store.persistCalls != 1 || len(store.results) != 1 || len(model.prompts) != 1 || len(memories.inputs) != 1 {
		t.Fatalf("calls: persist=%d results=%d prompts=%d memories=%d", store.persistCalls, len(store.results), len(model.prompts), len(memories.inputs))
	}
	if got := memories.inputs[0].Filters["project_id"]; got != projectID {
		t.Fatalf("memory filters = %#v", memories.inputs[0].Filters)
	}
	if toolBox.built != 1 || len(model.boxes) != 1 || model.boxes[0] == nil {
		t.Fatalf("tool box wiring: built=%d boxes=%d", toolBox.built, len(model.boxes))
	}
	if len(model.maxRounds) != 1 || model.maxRounds[0] != validWorkerOptions().MaxToolRounds {
		t.Fatalf("max rounds passed = %#v", model.maxRounds)
	}
}

func TestWorkerDoesNotAdvanceWatermarkAfterModelFailure(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_1"},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "请跟进", IsNew: true, Extractable: true,
		}}}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true},
	}}}
	model := &fakeModelExtractor{err: errors.New("model unavailable")}
	worker, err := NewWorker(store, model, &fakeMemorySearcher{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, validWorkerOptions())
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := worker.ExtractOnce(context.Background()); err == nil {
		t.Fatal("ExtractOnce() accepted model failure")
	}
	if store.persistCalls != 0 {
		t.Fatalf("PersistChat() calls = %d, want 0", store.persistCalls)
	}
}

func TestWorkerDoesNotPersistAfterSemanticDedupFailure(t *testing.T) {
	candidate := strictCandidate()
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_1"},
		Units: []ConversationUnit{{
			Key: "chat",
			Messages: []MessageContext{{
				MessageID: "om_1", ChatID: "oc_1", Content: candidate.SourceQuote,
				CreateTime: 1_700_000_000_000, IsNew: true, Extractable: true,
			}},
		}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true, CreateTime: 1_700_000_000_000},
	}}}
	dedup := &fakeCandidateDeduplicator{err: errors.New("qdrant unavailable")}
	worker, err := NewWorker(
		store,
		&fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{candidate}}},
		&fakeMemorySearcher{}, dedup, &fakeToolBoxBuilder{}, validWorkerOptions(),
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := worker.ExtractOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "qdrant unavailable") {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if store.persistCalls != 0 || len(dedup.inputs) != 1 {
		t.Fatalf("persistCalls=%d dedup.inputs=%d", store.persistCalls, len(dedup.inputs))
	}
}

func validWorkerOptions() WorkerOptions {
	return WorkerOptions{
		Load:            LoadOptions{BatchMessages: 100, ContextMessages: 20, ContextWindow: 2 * time.Hour, OpenTodoLimit: 50},
		PrincipalOpenID: "ou_owner", ModelName: "model", MemoryTopK: 8,
		MemoryThreshold: 0.5, MaxPromptChars: 60_000, MaxToolRounds: 5, Location: time.UTC,
	}
}
