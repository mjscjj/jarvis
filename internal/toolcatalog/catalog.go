// Package toolcatalog owns the prompt-facing description of tools available to
// Jarvis agents. System prompts define role and behavior; tool descriptions stay
// here so they can evolve with the tools without becoming user-edited persona.
package toolcatalog

import (
	"fmt"
	"strings"
)

const (
	StageExtract      = "extract"
	StageExecute      = "execute"
	StageChat         = "chat"
	StageFactEngine   = "factengine"
	StageProactive    = "proactive"
	StageMeetingSweep = "meeting_sweep"
	StageMorningBrief = "morning_brief"
)

// Block returns the trusted tool catalog for one agent stage. The catalog only
// describes capabilities and machine-enforced contracts. Stage role, judgment,
// stopping conditions and write policy belong to that stage's system prompt.
func Block(stage string) (string, error) {
	switch stage {
	case StageExtract, StageExecute, StageChat, StageFactEngine, StageProactive, StageMeetingSweep, StageMorningBrief:
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}

	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + stage,
		"各阶段看到同一套工具能力；允许执行哪些动作由当前阶段的系统提示词决定，命令自身仍会执行参数、环境和权限硬校验。",
		"- jarvis-tools：查询和维护 Jarvis 的项目、人物、群背景、关系、资源、消息、共享记忆、Skills、线索、任务与定时触发。先运行 `jarvis-tools --help` 看能力分组，再按子命令 `--help` 获取参数和机器约束。",
		"- list/query 命令返回紧凑摘要，get 命令返回单个对象详情；大段 prompt、run output、资源正文需要通过对应显式参数或 get 命令加载。",
		"- 查主体历史用 `list-facts`，查主体关系用 `list-relations`；世界模型对象由对应 create/update/delete 子命令维护。",
		"- 查线索或任务先用 `list-todos` / `list-tasks`，命中后再用 `get-todo` / `get-task`。`get-task` 默认不加载 prompt 和完整 run output。",
		"- 查本地已采集对话先用 `query-messages`；查附件与文档引用先用 `query-captured-resources`，命中后再用 `get-captured-resource` 加载正文。",
		"- `yield-until` 需要 Task runner 注入 `JARVIS_TASK_ID`，创建归属当前 Task 的恢复触发；`create-scheduled-task` 创建独立触发。",
		"- `create-task` / `start-task` / `update-task` / `close-task` 需要 `JARVIS_AGENT_STAGE=proactive`，其它阶段调用会被命令拒绝。",
		"- lark-cli：查询或操作飞书。先用 `lark-cli skills list` 查看能力目录并选定域，再用 `lark-cli skills read <域名>` 查工作流、`lark-cli schema <method>` 查单 API 参数；匹配到飞书 Skill 时先读取 Skill。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。命令清单 `bytedcli --json --all-help`，单命令参数 `bytedcli --json <子命令路径> --help`。",
		"- git：查询和操作本地代码仓库。",
	}
	lines = append(lines, "END_AVAILABLE_TOOLS")
	return strings.Join(lines, "\n"), nil
}
