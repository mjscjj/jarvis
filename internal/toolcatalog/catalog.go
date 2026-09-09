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
	StageOKRReview    = "okr_review"
)

// Block returns the trusted tool catalog for one agent stage. The catalog only
// describes capabilities and machine-enforced contracts. Stage role, judgment,
// stopping conditions and write policy belong to that stage's system prompt.
func Block(stage string) (string, error) {
	switch stage {
	case StageOKRReview:
		// The advisory review reaches only OKR data; it has no reason to touch
		// Feishu, the world model or code, so it is not given that vocabulary.
		return okrReviewBlock(), nil
	case StageChat:
		return chatBlock(), nil
	case StageExtract, StageExecute, StageFactEngine, StageProactive, StageMeetingSweep, StageMorningBrief:
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}

	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + stage,
		"各阶段看到同一套工具能力；允许执行哪些动作由当前阶段的系统提示词决定，命令自身仍会执行参数和运行环境校验。",
		"- jarvis-tools：查询和维护 Jarvis 的通用世界实体、关系、资源、消息、共享记忆、Skills、线索、任务与定时触发。先运行 `jarvis-tools --help` 看能力分组，再按子命令 `--help` 获取参数和机器约束。可选业务模块的能力只从当前已启用的 Skill 获取。",
		"- 共享记忆只保存 Principal 明确要求长期记住的行为偏好；用 `get-shared-memory` 读取，`append-shared-memory` 追加，`set-shared-memory` 整理，总长度不超过 2000 字。",
		"- list/query 命令返回紧凑摘要，get 命令返回单个对象详情；大段 prompt、run output、资源正文需要通过对应显式参数或 get 命令加载。",
		"- 实体当前状态和长期事实用 `get-page` / `update-page` 读写，`list-pages` 是可搜索、自动翻页的页面索引，`list-page-revisions` 读取历史正文。跨模块节点用 `resolve-world-node` 从各自真源解析；一跳关系用 `list-relations --node-type TYPE --node-id ID` 双向读取，多类节点的相关边用 `--node-types TYPE[,TYPE...]` 读取。`list-facts` 返回证据索引：一句锚点加原始材料指针；按 `source_kind` / `source_id` 用 `get-message`、`get-todo-event`、`get-task-event`、`get-task-run` 或 `get-captured-resource` 读取原文。",
		"- Jarvis 对项目或 OKR 主体的周期判断用 `get-world-progress` 读取，用 `create-world-progress` / `update-world-progress` 持久化；它不替代外部系统的正式进展。",
		"- `list-todos` / `list-tasks` 返回摘要和来源消息 ID；用 `--query` 搜索、`--source-message-id` 精确匹配原生消息 ID，可加 `--project-id` / `--group-id`，用 `--page` / `--limit` 翻页。Task 查询覆盖所有来源和完成、失败状态。",
		"- `get-task` / `get-todo --id ID` 默认给来源原文与简短说明；`--context conversation|background|SECTION` 直接读冻结会话、背景或指定区块，`--message-id ID` 读冻结快照中的单条原文，`--context full` 显式展开全部。Todo 后续读取可带 `--revision` 防止跨修订混读。",
		"- `list-task-runs --id TASK_ID --page N --limit N` 查历史摘要；`get-task-run --id RUN_ID` 读完整结果和 effects，`--include-prompt` 才加载该 run 的 prompt。",
		"- 查本地已采集对话先用 `query-messages`，已知数据库 ID 时用 `get-message`；查附件与文档引用先用 `query-captured-resources`，命中后再用 `get-captured-resource` 加载正文。",
		"- `yield-until` 需要 Task runner 注入 `JARVIS_TASK_ID`，创建归属当前 Task 的恢复触发；`create-scheduled-task` 创建独立触发。",
		"- `create-task` / `start-task` / `update-task` / `close-task` 对所有 Agent 阶段开放；是否调用由当前阶段提示词决定，服务端仍校验参数、状态与乐观锁版本，并按 `JARVIS_AGENT_STAGE` 留痕。",
		"- lark-cli：查询或操作飞书。先用 `lark-cli skills list` 查看能力目录并选定域，再用 `lark-cli skills read <域名>` 查工作流、`lark-cli schema <method>` 查单 API 参数；匹配到飞书 Skill 时先读取 Skill。",
		"- lark-cli 默认用本机已登录的身份（`--as user` 是机器所有者，`--as bot` 是 Jarvis Bot）。给单条命令设 `LARKSUITE_CLI_APP_ID` + `LARKSUITE_CLI_USER_ACCESS_TOKEN` 可改用指定用户的 access token，此时该命令绕过本机凭证。这两个变量一旦进入 shell 环境，lark-cli 即进入 user strict 模式，同环境下所有 `--as bot` 命令都会被拒绝，因此只作单条命令前缀使用，不要 export。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。先用 `bytedcli --help` 查看领域，再用 `bytedcli --json <领域> --help` 查看该领域命令，最后用 `bytedcli --json <子命令路径> --help` 查看参数；不要加载全量帮助。",
		"- git：查询和操作本地代码仓库。",
	}
	lines = append(lines, "END_AVAILABLE_TOOLS")
	return strings.Join(lines, "\n"), nil
}

// chatBlock keeps interactive conversation responsive. Detailed workflows live
// in each tool's help and matching Skill, so the entire execution-stage manual
// does not need to be repeated in every new chat.
func chatBlock() string {
	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（按需发现，不要先介绍工具。）",
		"当前阶段：" + StageChat,
		"- jarvis-tools：查询或维护 Jarvis 数据；先用 `jarvis-tools --help`，再查看所需子命令。",
		"- lark-cli：操作飞书；先用 `lark-cli skills list/read` 匹配 Skill，单 API 参数用 `lark-cli schema`。用户身份和 token 前缀由本轮身份块提供。",
		"- bytedcli：查询内部研发信息；用 `bytedcli --json --all-help` 发现命令。",
		"- git：查询或操作本地代码仓库；同时遵守仓库 AGENTS.md。",
		"- 部署统一使用 `./scripts/jarvis-deploy --skip-pull`。仅改主服务、前端或配置时加 `--skip-chat-restart`；改到对话服务需整体重启时，先告知用户本轮会中断。",
		"END_AVAILABLE_TOOLS",
	}
	return strings.Join(lines, "\n")
}

// okrReviewBlock describes the split between the generic OKR and Biz OKR
// scripts. The Preview review agent only uses their read commands; write
// commands remain subject to the stage prompt even when their ownership is
// called out here.
func okrReviewBlock() string {
	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + StageOKRReview,
		"两个脚本都已在 PATH 中，服务地址由 `JARVIS_API_BASE` 环境变量提供，直接执行即可，不需要自己解析配置或构建任何东西。所有命令输出 JSON。",
		"- okr-module-tools：通用 OKR 定义、Metric/Point/Owner 拆解和正式进展。`scope` / `board` 读季度结构；`find-krs` / `get-kr` 定位 KR；`weeks`、`progress-board`、`get-weekly-kr` 读取正式周次和进展；`replace-kr` 是通用拆解写入口，当前 Preview review 阶段不调用写命令。",
		"- biz-okr-tools：Biz 业务组合视图。`board [--quarter Q] [--week YYYY-Www]` 读包含标签、评分与 Meego 信息的周报；`get-kr` / `get-weekly-kr` 读 Biz 组合 KR；`comments`、`follow-ups` 读取协作信息；`people-search` 查人。",
		"- `find-krs` 单次输出可能上千行。先用它定位 ID，再用 `get-kr` / `get-weekly-kr` 取需要的那一个，不要把整季数据全部读进来。",
		"END_AVAILABLE_TOOLS",
	}
	return strings.Join(lines, "\n")
}
