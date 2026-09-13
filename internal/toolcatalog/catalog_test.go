package toolcatalog

import (
	"strings"
	"testing"
	"unicode/utf8"
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

func TestCatalogIsSharedDiscoveryNotCommandManual(t *testing.T) {
	base, err := Block(StageExtract)
	if err != nil {
		t.Fatal(err)
	}
	if size := utf8.RuneCountInString(base); size > 900 {
		t.Fatalf("tool discovery has %d characters", size)
	}
	for _, stage := range []string{StageExecute, StageChat, StageFactEngine, StageProactive, StageMeetingSweep, StageMorningBrief} {
		block, err := Block(stage)
		if err != nil || block != base {
			t.Fatalf("stage %s differs from shared discovery: %v", stage, err)
		}
	}
	for _, want := range []string{"help <group>", "<command> --help", "world", "evidence", "task", "schedule", "memory", "skill", "notify"} {
		if !strings.Contains(base, want) {
			t.Errorf("discovery missing %q", want)
		}
	}
	for _, detail := range []string{"jarvis-chat", "idempotency_key", "expected_version", "--payload", "当前阶段："} {
		if strings.Contains(base, detail) {
			t.Errorf("command or stage detail leaked into discovery: %q", detail)
		}
	}
}

func TestUnknownStageFails(t *testing.T) {
	t.Parallel()
	if _, err := Block("unknown"); err == nil {
		t.Fatal("Block(unknown) unexpectedly succeeded")
	}
}
