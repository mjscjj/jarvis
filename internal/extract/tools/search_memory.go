package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/memory"
)

// SearchMemoryName is the function name exposed to the model.
const SearchMemoryName = "search_memory"

// memorySearcher is the mem0 subset the tool needs, declared as an interface so
// the tool stays unit-testable without a live sidecar.
type memorySearcher interface {
	Search(context.Context, memory.SearchInput) (*memory.SearchResponse, error)
}

// SearchMemoryTool lets the model retrieve long-term memory (mem0) on demand
// with its own query. The retrieval scope (filters), default top_k and
// threshold are fixed by the caller per extraction unit, so the model controls
// only the natural-language query, not the scope — keeping results relevant to
// the current chat/project.
type SearchMemoryTool struct {
	memory    memorySearcher
	filters   map[string]any
	defaultK  int
	maxK      int
	threshold float64
	timeout   time.Duration
}

// NewSearchMemoryTool builds the tool. filters is the fixed retrieval scope
// (e.g. {"chat_id": ...} or {"project_id": ...}); defaultK/maxK bound top_k;
// threshold is the similarity floor; timeout bounds one sidecar call.
func NewSearchMemoryTool(searcher memorySearcher, filters map[string]any, defaultK, maxK int, threshold float64, timeout time.Duration) (*SearchMemoryTool, error) {
	if searcher == nil {
		return nil, fmt.Errorf("search_memory searcher is nil")
	}
	if defaultK <= 0 {
		return nil, fmt.Errorf("search_memory default top_k must be positive")
	}
	if maxK < defaultK {
		return nil, fmt.Errorf("search_memory max top_k must be >= default top_k")
	}
	if threshold < 0 || threshold > 1 {
		return nil, fmt.Errorf("search_memory threshold must be between 0 and 1")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("search_memory timeout must be positive")
	}
	scoped := make(map[string]any, len(filters))
	for k, v := range filters {
		scoped[k] = v
	}
	return &SearchMemoryTool{
		memory: searcher, filters: scoped, defaultK: defaultK,
		maxK: maxK, threshold: threshold, timeout: timeout,
	}, nil
}

func (t *SearchMemoryTool) Name() string { return SearchMemoryName }

func (t *SearchMemoryTool) Description() string {
	return "检索长期记忆(mem0)，用于回忆当前群/项目的历史事实与背景（如项目负责人、既往决策、仓库约定）。" +
		"检索范围已按当前会话固定，你只需给出自然语言 query。返回结果仅作背景，不得当作新的 [new] 证据。"
}

func (t *SearchMemoryTool) Schema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"query", "top_k"},
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "自然语言检索问题，描述你想回忆的背景。",
			},
			"top_k": map[string]any{
				"type":        []string{"integer", "null"},
				"minimum":     1,
				"description": fmt.Sprintf("返回条数；null 表示用默认值 %d，上限 %d。", t.defaultK, t.maxK),
			},
		},
	}
}

type searchMemoryArgs struct {
	Query string `json:"query"`
	TopK  *int   `json:"top_k"`
}

type searchMemoryResult struct {
	Query   string           `json:"query"`
	Count   int              `json:"count"`
	Results []map[string]any `json:"results"`
}

func (t *SearchMemoryTool) Invoke(ctx context.Context, arguments json.RawMessage) (json.RawMessage, error) {
	args, err := decodeToolArgs[searchMemoryArgs](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, fmt.Errorf("search_memory query must be non-blank")
	}
	topK := t.defaultK
	if args.TopK != nil {
		if *args.TopK <= 0 {
			return nil, fmt.Errorf("search_memory top_k must be positive")
		}
		topK = *args.TopK
		if topK > t.maxK {
			topK = t.maxK
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	response, err := t.memory.Search(callCtx, memory.SearchInput{
		Query:     args.Query,
		Filters:   t.filters,
		TopK:      topK,
		Threshold: t.threshold,
		Rerank:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("search_memory query=%q: %w", args.Query, err)
	}
	if response == nil {
		return nil, fmt.Errorf("search_memory query=%q: nil response", args.Query)
	}
	result := searchMemoryResult{Query: args.Query, Count: len(response.Results), Results: response.Results}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("search_memory encode result: %w", err)
	}
	return encoded, nil
}
