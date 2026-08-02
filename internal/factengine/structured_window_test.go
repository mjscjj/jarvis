package factengine

import (
	"testing"
	"time"
)

func TestStructuredMaterialWindowEndKeepsAllItemsButBoundsCombinedPrompt(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
	at := func(i int) time.Time { return times[i] }

	large := []int{30_000, 30_000}
	if end := structuredMaterialWindowEnd(0, 2, 40, time.UTC, at, func(i int) int { return large[i] }); end != 1 {
		t.Fatalf("large material window end=%d, want 1", end)
	}

	// An individual item is never truncated or dropped, even if it alone is
	// larger than the combined-window target.
	oversized := []int{80_000, 100}
	if end := structuredMaterialWindowEnd(0, 2, 40, time.UTC, at, func(i int) int { return oversized[i] }); end != 1 {
		t.Fatalf("oversized first item window end=%d, want 1", end)
	}

	small := []int{1_000, 1_000}
	if end := structuredMaterialWindowEnd(0, 2, 40, time.UTC, at, func(i int) int { return small[i] }); end != 2 {
		t.Fatalf("small material window end=%d, want 2", end)
	}
}
