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
		"- jarvis-tools：查询和维护 Jarvis 的通用世界实体、关系、资源、消息、共享记忆、Skills、线索、任务与定时触发。先运行 `jarvis-tools --help` 看能力分组，再按子命令 `--help` 获取参数和机器约束。可选业务模块的能力只从当前已启用的 Skill 获取。",
		"- list/query 命令返回紧凑摘要，get 命令返回单个对象详情；大段 prompt、run output、资源正文需要通过对应显式参数或 get 命令加载。",
		"- 实体的长期事实用 `get-page` / `update-page` 读写，索引用 `list-pages`，历史明细用 `list-facts` 按主体和日期下钻。",
		"- 查线索或任务先用 `list-todos` / `list-tasks`，命中后再用 `get-todo` / `get-task`。`get-task` 默认不加载 prompt 和完整 run output。",
		"- 查本地已采集对话先用 `query-messages`；查附件与文档引用先用 `query-captured-resources`，命中后再用 `get-captured-resource` 加载正文。",
		"- `yield-until` 需要 Task runner 注入 `JARVIS_TASK_ID`，创建归属当前 Task 的恢复触发；`create-scheduled-task` 创建独立触发。",
		"- `create-task` / `start-task` / `update-task` / `close-task` 需要 `JARVIS_AGENT_STAGE=proactive`，其它阶段调用会被命令拒绝。",
		"- lark-cli：查询或操作飞书。先用 `lark-cli skills list` 查看能力目录并选定域，再用 `lark-cli skills read <域名>` 查工作流、`lark-cli schema <method>` 查单 API 参数；匹配到飞书 Skill 时先读取 Skill。",
		"- lark-cli 默认用本机已登录的身份（`--as user` 是机器所有者，`--as bot` 是 Jarvis Bot）。给单条命令设 `LARKSUITE_CLI_APP_ID` + `LARKSUITE_CLI_USER_ACCESS_TOKEN` 可改用指定用户的 access token，此时该命令绕过本机凭证。这两个变量一旦进入 shell 环境，lark-cli 即进入 user strict 模式，同环境下所有 `--as bot` 命令都会被拒绝，因此只作单条命令前缀使用，不要 export。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。命令清单 `bytedcli --json --all-help`，单命令参数 `bytedcli --json <子命令路径> --help`。",
		"- git：查询和操作本地代码仓库。",
	}
	lines = append(lines, "END_AVAILABLE_TOOLS")
	return strings.Join(lines, "\n"), nil
}
