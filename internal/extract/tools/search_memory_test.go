package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"jarvis/internal/memory"
)

type stubSearcher struct {
	inputs   []memory.SearchInput
	response *memory.SearchResponse
	err      error
}

func (s *stubSearcher) Search(_ context.Context, input memory.SearchInput) (*memory.SearchResponse, error) {
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func newTestMemoryTool(t *testing.T, searcher memorySearcher) *SearchMemoryTool {
	t.Helper()
	tool, err := NewSearchMemoryTool(searcher, map[string]any{"chat_id": "oc_1"}, 5, 20, 0.5, time.Second)
	if err != nil {
		t.Fatalf("NewSearchMemoryTool() error = %v", err)
	}
	return tool
}

func TestSearchMemoryUsesFixedFiltersAndDefaultTopK(t *testing.T) {
	searcher := &stubSearcher{response: &memory.SearchResponse{Results: []map[string]any{{"memory": "x"}}}}
	tool := newTestMemoryTool(t, searcher)
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"谁负责这个仓库","top_k":null}`))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(searcher.inputs) != 1 {
		t.Fatalf("searcher called %d times", len(searcher.inputs))
	}
	got := searcher.inputs[0]
	if got.Filters["chat_id"] != "oc_1" || got.TopK != 5 || got.Threshold != 0.5 {
		t.Fatalf("search input = %#v", got)
	}
	var result searchMemoryResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchMemoryCapsTopKToMax(t *testing.T) {
	searcher := &stubSearcher{response: &memory.SearchResponse{Results: []map[string]any{}}}
	tool := newTestMemoryTool(t, searcher)
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"q","top_k":999}`)); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if searcher.inputs[0].TopK != 20 {
		t.Fatalf("top_k = %d, want capped to 20", searcher.inputs[0].TopK)
	}
}

func TestSearchMemoryRejectsBlankQuery(t *testing.T) {
	tool := newTestMemoryTool(t, &stubSearcher{})
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"  ","top_k":null}`)); err == nil {
		t.Fatal("Invoke accepted blank query")
	}
}

func TestSearchMemoryRejectsUnknownArgs(t *testing.T) {
	tool := newTestMemoryTool(t, &stubSearcher{})
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"q","top_k":null,"scope":"all"}`)); err == nil {
		t.Fatal("Invoke accepted unknown argument field")
	}
}
