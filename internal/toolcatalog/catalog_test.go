package toolcatalog

import (
	"strings"
	"testing"
)

func TestBackgroundStagesExposeTheSameMachineCapabilities(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		StageExtract, StageExecute, StageFactEngine,
		StageProactive, StageMeetingSweep, StageMorningBrief,
	} {
		block, err := Block(stage)
		if err != nil {
			t.Fatalf("Block(%q): %v", stage, err)
		}
		for _, required := range []string{
			"同一套工具能力", "参数和运行环境校验", "通用世界实体", "关系",
			"query-messages", "get-message", "get-todo-event", "get-task-event", "query-captured-resources", "get-captured-resource",
			"list-facts", "get-page", "update-page", "list-pages", "list-page-revisions", "resolve-world-node",
			"list-relations --node-type TYPE --node-id ID", "--node-types TYPE[,TYPE...]", "yield-until", "JARVIS_TASK_ID",
			"get-world-progress", "create-world-progress", "update-world-progress",
			"get-shared-memory", "append-shared-memory", "set-shared-memory", "2000",
			"JARVIS_AGENT_STAGE=proactive", "bytedcli --json <领域> --help", "不要加载全量帮助",
		} {
			if !strings.Contains(block, required) {
				t.Fatalf("Block(%q) missing %q:\n%s", stage, required, block)
			}
		}
		if strings.Contains(block, "--all-help") {
			t.Fatalf("Block(%q) recommends eager bytedcli help:\n%s", stage, block)
		}
	}
}

func TestChatCatalogIsConciseAndPointsToDiscovery(t *testing.T) {
	t.Parallel()
	block, err := Block(StageChat)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"jarvis-tools --help", "lark-cli skills list/read", "bytedcli --json --all-help",
		"git", "AGENTS.md", "jarvis-deploy --skip-pull", "skip-chat-restart",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("chat block missing %q:\n%s", required, block)
		}
	}
	for _, verbose := range []string{
		"同一套工具能力", "query-captured-resources", "create-world-progress",
		"append-shared-memory", "JARVIS_AGENT_STAGE=proactive",
	} {
		if strings.Contains(block, verbose) {
			t.Fatalf("chat block retained execution manual %q:\n%s", verbose, block)
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
