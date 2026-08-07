// Package agentusage owns the small, provider-independent token-usage shape
// persisted by the M3 and M5 run records.
package agentusage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Usage contains non-overlapping totals. CachedInputTokens is a subset of
// InputTokens and ReasoningOutputTokens is a subset of OutputTokens, so
// TotalTokens deliberately sums only input + output.
type Usage struct {
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	Reported              bool
}

func (u Usage) TotalTokens() int64 { return u.InputTokens + u.OutputTokens }

func (u Usage) Validate() error {
	if u.InputTokens < 0 || u.CachedInputTokens < 0 || u.OutputTokens < 0 || u.ReasoningOutputTokens < 0 {
		return fmt.Errorf("agent token usage must not be negative: %+v", u)
	}
	if u.CachedInputTokens > u.InputTokens {
		return fmt.Errorf("cached input tokens %d exceed input tokens %d", u.CachedInputTokens, u.InputTokens)
	}
	if u.ReasoningOutputTokens > u.OutputTokens {
		return fmt.Errorf("reasoning output tokens %d exceed output tokens %d", u.ReasoningOutputTokens, u.OutputTokens)
	}
	return nil
}

func (u *Usage) Add(next Usage) error {
	if err := next.Validate(); err != nil {
		return err
	}
	u.InputTokens += next.InputTokens
	u.CachedInputTokens += next.CachedInputTokens
	u.OutputTokens += next.OutputTokens
	u.ReasoningOutputTokens += next.ReasoningOutputTokens
	u.Reported = u.Reported || next.Reported
	return u.Validate()
}

type collectorKey struct{}

// Collector accumulates all model calls made within one logical run. This is
// needed because M3 can use multiple tool rounds and semantic-dedup calls.
type Collector struct {
	mu    sync.Mutex
	total Usage
}

func WithCollector(ctx context.Context) (context.Context, *Collector) {
	collector := &Collector{}
	return context.WithValue(ctx, collectorKey{}, collector), collector
}

func Record(ctx context.Context, usage Usage) error {
	if err := usage.Validate(); err != nil {
		return err
	}
	collector, _ := ctx.Value(collectorKey{}).(*Collector)
	if collector == nil {
		return nil
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	return collector.total.Add(usage)
}

func (c *Collector) Total() Usage {
	if c == nil {
		return Usage{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// ParseCodexJSONL sums every turn.completed usage envelope in one Codex CLI
// stdout stream. A stream without usage stays unreported so old CLI versions
// can be distinguished from a real zero-token response.
func ParseCodexJSONL(output []byte) (Usage, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var total Usage
	for {
		var event struct {
			Type  string          `json:"type"`
			Usage json.RawMessage `json:"usage"`
		}
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Usage{}, fmt.Errorf("decode codex JSONL usage: %w", err)
		}
		if event.Type != "turn.completed" || len(event.Usage) == 0 || bytes.Equal(event.Usage, []byte("null")) {
			continue
		}
		var raw struct {
			InputTokens           int64 `json:"input_tokens"`
			CachedInputTokens     int64 `json:"cached_input_tokens"`
			OutputTokens          int64 `json:"output_tokens"`
			ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
		}
		if err := json.Unmarshal(event.Usage, &raw); err != nil {
			return Usage{}, fmt.Errorf("decode codex turn.completed usage: %w", err)
		}
		if err := total.Add(Usage{
			InputTokens: raw.InputTokens, CachedInputTokens: raw.CachedInputTokens,
			OutputTokens: raw.OutputTokens, ReasoningOutputTokens: raw.ReasoningOutputTokens,
			Reported: true,
		}); err != nil {
			return Usage{}, fmt.Errorf("validate codex turn.completed usage: %w", err)
		}
	}
	return total, nil
}
