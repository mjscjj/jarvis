package toolcatalog

import (
	"strings"
	"testing"
)

func TestDecisionStagesExposeQueryOnlyJarvisTools(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{StageExtract, StageDecide} {
		block, err := Block(stage)
		if err != nil {
			t.Fatalf("Block(%q): %v", stage, err)
		}
		for _, forbidden := range []string{"追加共享记忆", "管理独立定时触发", "暂停当前 Task", "yield-until"} {
			if strings.Contains(block, forbidden) {
				t.Fatalf("Block(%q) contains write capability %q:\n%s", stage, forbidden, block)
			}
		}
		if !strings.Contains(block, "项目资源") {
			t.Fatalf("Block(%q) does not advertise project resources:\n%s", stage, block)
		}
	}
}

func TestExecuteStageExposesTaskControls(t *testing.T) {
	t.Parallel()
	block, err := Block(StageExecute)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"追加共享记忆", "管理独立定时触发", "yield-until"} {
		if !strings.Contains(block, required) {
			t.Fatalf("execute block missing %q:\n%s", required, block)
		}
	}
}

func TestUnknownStageFails(t *testing.T) {
	t.Parallel()
	if _, err := Block("unknown"); err == nil {
		t.Fatal("Block(unknown) unexpectedly succeeded")
	}
}
