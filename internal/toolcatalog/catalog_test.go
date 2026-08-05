package toolcatalog

import (
	"strings"
	"testing"
)

func TestEveryStageExposesTheSameJarvisToolPrinciplesAndCapabilities(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{StageExtract, StageExecute, StageChat} {
		block, err := Block(stage)
		if err != nil {
			t.Fatalf("Block(%q): %v", stage, err)
		}
		for _, required := range []string{
			"简单优先", "渐进式加载", "同一套工具能力", "不按阶段隐藏工具",
			"query-messages", "query-captured-resources", "get-captured-resource",
			"list-facts", "list-relations", "yield-until", "追加共享记忆", "修改 Todo 状态",
		} {
			if !strings.Contains(block, required) {
				t.Fatalf("Block(%q) missing %q:\n%s", stage, required, block)
			}
		}
	}
}

func TestUnknownStageFails(t *testing.T) {
	t.Parallel()
	if _, err := Block("unknown"); err == nil {
		t.Fatal("Block(unknown) unexpectedly succeeded")
	}
}

func TestProactiveStageRequiresTaskHandoffForExternalWork(t *testing.T) {
	block, err := Block(StageProactive)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"create-task", "start-task", "close-task", "不得直接执行外部动作", "内部世界模型", "理由与证据"} {
		if !strings.Contains(block, required) {
			t.Fatalf("proactive block missing %q:\n%s", required, block)
		}
	}
}

func TestMorningBriefStageIsReadMostlyWithPrincipalDeliveryOnly(t *testing.T) {
	block, err := Block(StageMorningBrief)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"晨间简报默认只读", "Principal 本人", "不得创建 Task", "本地 Markdown",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("morning brief block missing %q:\n%s", required, block)
		}
	}
}
