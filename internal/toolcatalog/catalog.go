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
	StageProactive    = "proactive"
	StageMeetingSweep = "meeting_sweep"
	StageMorningBrief = "morning_brief"
)

// Block returns the trusted tool catalog for one agent stage.
func Block(stage string) (string, error) {
	var purpose string
	switch stage {
	case StageExtract:
		purpose = "补全行动线索的项目、人物、会话、文档和代码背景；把查到的关键事实写入候选 context。"
	case StageExecute:
		purpose = "完成任务、核验结果；需要等待时暂停当前 Task，避免创建重复任务。"
	case StageChat:
		purpose = "按用户请求查询或操作本机与外部系统。"
	case StageProactive:
		purpose = "定时审视全局，维护 Jarvis 内部世界模型，并把值得推进的外部工作创建成普通 Task 交给强 M5。"
	case StageMeetingSweep:
		purpose = "定时查找最近结束的飞书会议和未来待参加的会议日程，把每场会作为一条线索投递给 M2/M3；只采集，不分析。"
	case StageMorningBrief:
		purpose = "每个工作日开工前生成晨间作战简报：读世界状态与日历，选出最多三个今日结果，写本地 Markdown 并只给 Principal 本人发一条飞书私聊。"
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}

	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + stage,
		"使用目的：" + purpose,
		"原则一，简单优先：按具体意图使用具体命令，不搭通用 API 转发层，不为了猜测中的未来场景增加抽象、兼容或 fallback。",
		"原则二，渐进式加载：先 list/query 看紧凑摘要，再用 get 读取命中的完整对象；大段 prompt、run output、资源正文只在确有需要时显式加载。",
		"各阶段使用同一套工具能力，代码不按阶段隐藏工具；具体写入边界由当前阶段的系统提示词决定。",
		"用法：目标导向、主动发散——为查清一个事实或办成一件事，主动组合多个工具、顺藤摸瓜多跳查询；一条路查不到就换工具或换角度。能查到的不要留给用户问。",
		"- jarvis-tools：查询和维护 Jarvis 的项目、人物、群背景、关系、资源、消息、共享记忆、Skills、线索、任务与定时触发。先运行 `jarvis-tools --help` 看能力分组，再按子命令 `--help` 获取当前参数。",
		"- 查主体历史用 `list-facts`，查主体关系用 `list-relations`；需要维护世界模型时使用对应 create/update/delete 子命令。",
		"- 查线索或任务先用 `list-todos` / `list-tasks`，命中后再用 `get-todo` / `get-task`。`get-task` 默认不加载 prompt 和完整 run output。",
		"- 查本地已采集对话先用 `query-messages`；查附件与文档引用先用 `query-captured-resources`，命中后再用 `get-captured-resource` 加载正文。",
		"- 当前 Task 需要等待未来条件时使用 `yield-until`；独立的新动作才创建 scheduled task。",
		"- 可按语义需要投递线索、追加共享记忆、记录事实或修改 Todo 状态；工具层只提供能力和留痕，不替 Agent 做语义判断。",
		"- lark-cli：查询或操作飞书。先看工作规则里的能力地图选定域，再 `lark-cli skills read <域名>` 查用法、`lark-cli schema <method>` 查单 API 参数；匹配到飞书 Skill 时先读取 Skill。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。命令清单 `bytedcli --json --all-help`，单命令参数 `bytedcli --json <子命令路径> --help`。",
		"- git：查询和操作本地代码仓库。",
	}
	if stage == StageProactive {
		lines = append(lines,
			"- 主动巡视发现需要对外推进的工作时，必须使用 `jarvis-tools create-task --payload ...` 创建普通 Task；不得直接执行外部动作。",
			"- 对今天已有的 pending Task，使用 `jarvis-tools start-task --id ...` 交给强 M5；不要创建重复 Task。",
			"- 对已过期或已查证无需继续的现有 Task，使用 `jarvis-tools close-task --id ... --payload ...` 收口，并把理由与证据写全；close 只改变 Jarvis 内部状态，不得伪造外部完成。",
			"- 主动巡视可以直接维护 Jarvis 内部世界模型；外部动作与复杂执行仍必须交给 M5。",
		)
	}
	if stage == StageMorningBrief {
		lines = append(lines,
			"- 晨间简报默认只读：可以查 Task/Todo/Fact/消息/日历/工程状态，并写本地 Markdown 证据与正式稿。",
			"- 唯一预授权外部副作用：给 Principal 本人发送一条 Jarvis Bot 私聊晨报。不得给其他人/群发消息，不得创建 Task，不得改日历、代码或文档。",
		)
	}
	lines = append(lines, "END_AVAILABLE_TOOLS")
	return strings.Join(lines, "\n"), nil
}
