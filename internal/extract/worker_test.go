package extract

import (
	"context"
	"errors"
	"testing"
	"time"

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
	result  *ExtractionResult
	err     error
	prompts []Prompt
}

func (f *fakeModelExtractor) Extract(_ context.Context, prompt Prompt) (*ExtractionResult, error) {
	f.prompts = append(f.prompts, prompt)
	return f.result, f.err
}

type fakeMemorySearcher struct {
	inputs []memory.SearchInput
	err    error
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
	worker, err := NewWorker(store, model, memories, validWorkerOptions())
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
	worker, err := NewWorker(store, model, &fakeMemorySearcher{}, validWorkerOptions())
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

func validWorkerOptions() WorkerOptions {
	return WorkerOptions{
		Load:            LoadOptions{BatchMessages: 100, ContextMessages: 20, ContextWindow: 2 * time.Hour, OpenTodoLimit: 50},
		PrincipalOpenID: "ou_owner", ModelName: "model", MemoryTopK: 8,
		MemoryThreshold: 0.5, MaxPromptChars: 60_000, Location: time.UTC,
	}
}
