package toolcatalog

import (
	"strings"
	"testing"
)

func TestBlockSupportsAgentStages(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		StageExtract, StageExecute, StageFactEngine,
		StageProactive, StageMeetingSweep, StageMorningBrief,
	} {
		block, err := Block(stage)
		if err != nil || block == "" {
			t.Fatalf("Block(%q): %v", stage, err)
		}
		for _, required := range []string{
			"同一套工具能力", "参数和运行环境校验", "通用世界实体", "关系",
			"query-messages", "get-message", "get-todo-event", "get-task-event", "query-captured-resources", "get-captured-resource",
			"list-facts", "get-page", "update-page", "list-pages", "list-page-revisions", "resolve-world-node",
			"list-relations --node-type TYPE --node-id ID", "--node-types TYPE[,TYPE...]", "yield-until", "JARVIS_TASK_ID",
			"get-world-progress", "create-world-progress", "update-world-progress",
			"get-shared-memory", "append-shared-memory", "set-shared-memory", "2000",
			"对所有 Agent 阶段开放", "JARVIS_AGENT_STAGE", "不是权限身份",
			"create-task", "source_type=manual|proactive", "supplement-task", "resume-task",
			"bytedcli --json <领域> --help", "不要加载全量帮助",
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

func TestUnknownStageFails(t *testing.T) {
	t.Parallel()
	if _, err := Block("unknown"); err == nil {
		t.Fatal("Block(unknown) unexpectedly succeeded")
	}
}
