package toolcatalog

import (
	"strings"
	"testing"
)

func TestEveryStageExposesTheSameMachineCapabilities(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		StageExtract, StageExecute, StageChat, StageFactEngine,
		StageProactive, StageMeetingSweep, StageMorningBrief,
	} {
		block, err := Block(stage)
		if err != nil {
			t.Fatalf("Block(%q): %v", stage, err)
		}
		for _, required := range []string{
			"同一套工具能力", "参数、环境和权限硬校验",
			"query-messages", "query-captured-resources", "get-captured-resource",
			"list-facts", "list-relations", "yield-until", "JARVIS_TASK_ID",
			"JARVIS_AGENT_STAGE=proactive",
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

func TestToolCatalogDoesNotOwnStageJudgmentOrPolicy(t *testing.T) {
	for _, stage := range []string{
		StageExtract, StageExecute, StageChat, StageFactEngine,
		StageProactive, StageMeetingSweep, StageMorningBrief,
	} {
		block, err := Block(stage)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"Task 准入", "最短证据链", "证据足够", "主动发散", "值得推进",
			"只有已查证完成", "跨日、沉默", "唯一预授权", "不得直接执行外部动作",
			"最多 10 个", "最多 50 个", "不创建或推进 Task", "需要补证据时",
		} {
			if strings.Contains(block, forbidden) {
				t.Fatalf("Block(%q) contains stage policy %q:\n%s", stage, forbidden, block)
			}
		}
	}
}
