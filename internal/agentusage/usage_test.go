package agentusage

import (
	"context"
	"testing"
)

func TestParseCodexJSONLSumsTurnsWithoutDoubleCountingSubsets(t *testing.T) {
	t.Parallel()
	usage, err := ParseCodexJSONL([]byte(`{"type":"thread.started","thread_id":"x"}
{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":20,"reasoning_output_tokens":5}}
{"type":"turn.completed","usage":{"input_tokens":30,"cached_input_tokens":10,"output_tokens":7,"reasoning_output_tokens":2}}
`))
	if err != nil {
		t.Fatalf("ParseCodexJSONL() error = %v", err)
	}
	if !usage.Reported || usage.InputTokens != 130 || usage.CachedInputTokens != 50 || usage.OutputTokens != 27 || usage.ReasoningOutputTokens != 7 {
		t.Fatalf("usage = %+v", usage)
	}
	if got := usage.TotalTokens(); got != 157 {
		t.Fatalf("TotalTokens() = %d, want 157", got)
	}
}

func TestCollectorAccumulatesLogicalRun(t *testing.T) {
	t.Parallel()
	ctx, collector := WithCollector(context.Background())
	for _, usage := range []Usage{
		{InputTokens: 10, OutputTokens: 2, Reported: true},
		{InputTokens: 20, CachedInputTokens: 5, OutputTokens: 3, ReasoningOutputTokens: 1, Reported: true},
	} {
		if err := Record(ctx, usage); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	if got := collector.Total(); got.InputTokens != 30 || got.OutputTokens != 5 || got.TotalTokens() != 35 {
		t.Fatalf("collector total = %+v", got)
	}
}

func TestUsageRejectsOverlappingSubsetLargerThanTotal(t *testing.T) {
	t.Parallel()
	if err := (Usage{InputTokens: 1, CachedInputTokens: 2, Reported: true}).Validate(); err == nil {
		t.Fatal("Validate() accepted cached input greater than input")
	}
}
