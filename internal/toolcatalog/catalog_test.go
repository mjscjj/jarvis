package toolcatalog

import (
	"testing"
)

func TestBlockSupportsAgentStages(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		StageExtract, StageExecute, StageChat, StageFactEngine,
		StageProactive, StageMeetingSweep, StageMorningBrief,
	} {
		if block, err := Block(stage); err != nil || block == "" {
			t.Fatalf("Block(%q): %v", stage, err)
		}
	}
}

func TestUnknownStageFails(t *testing.T) {
	t.Parallel()
	if _, err := Block("unknown"); err == nil {
		t.Fatal("Block(unknown) unexpectedly succeeded")
	}
}
