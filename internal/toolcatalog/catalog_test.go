package toolcatalog

import (
	"strings"
	"testing"
)

func TestEveryStageExposesTheSameJarvisToolPrinciplesAndCapabilities(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{StageExtract, StageExecute, StageChat, StageFactEngine} {
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

func TestFactEngineStageOwnsInternalWorldModelButNotExternalWork(t *testing.T) {
	block, err := Block(StageFactEngine)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"通用查询及 CRUD", "先查现值", "立即读回", "最终 `facts` 数组",
		"不创建或推进 Task", "不修改外部系统",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("factengine block missing %q:\n%s", required, block)
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
	for _, required := range []string{"create-task", "start-task", "update-task", "close-task", "跨日、沉默或没有新证据不是关闭依据", "不得直接执行外部动作", "factengine 的主要任务", "可以直接使用通用 CRUD", "辅助动作"} {
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
