// Package toolcatalog owns the prompt-facing description of tools available to
// Jarvis agents. System prompts define role and behavior; tool descriptions stay
// here so they can evolve with the tools without becoming user-edited persona.
package toolcatalog

import (
	"fmt"
	"strings"
)

const (
	StageExtract = "extract"
	StageDecide  = "decide"
	StageExecute = "execute"
	StageChat    = "chat"
)

// Block returns the trusted tool catalog for one agent stage.
func Block(stage string) (string, error) {
	var purpose string
	usage := "用法：目标导向、主动发散——为查清一个事实或办成一件事，主动组合多个工具、顺藤摸瓜多跳查询；一条路查不到就换工具或换角度，不要浅尝辄止。能查到的绝不留给用户问。"
	switch stage {
	case StageExtract:
		purpose = "补全行动线索的项目、人物、会话、文档和代码背景；把查到的关键事实写入候选 context。"
	case StageDecide:
		purpose = "只判断线索是否值得交给 M5；不形成可执行计划，不执行外部动作。"
		usage = "用法：默认不调用工具。只有一次低成本查询就可能改变 ready / drop 的价值判断时才查询；不要多跳调查，不要为了补齐目标、边界或执行方案而查询，这些由 M5 完成。"
	case StageExecute:
		purpose = "完成任务、核验结果；需要等待时暂停当前 Task，避免创建重复任务。"
	case StageChat:
		purpose = "按用户请求查询或操作本机与外部系统。"
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}

	jarvisTool := "- jarvis-tools：查询 Jarvis 的项目、人物、群、项目资源、共享记忆、Skills 和定时任务。先运行 `jarvis-tools --help`，再按子命令 `--help` 获取当前参数。"
	if stage == StageExecute || stage == StageChat {
		jarvisTool = "- jarvis-tools：查询 Jarvis 上下文，并按任务需要追加共享记忆、投递线索、管理独立定时触发或暂停当前 Task。先运行 `jarvis-tools --help`，再按子命令 `--help` 获取当前参数。"
	}
	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + stage,
		"使用目的：" + purpose,
		usage,
		jarvisTool,
		"- lark-cli：查询或操作飞书。先看工作规则里的能力地图选定域，再 `lark-cli skills read <域名>` 查用法、`lark-cli schema <method>` 查单 API 参数；匹配到飞书 Skill 时先读取 Skill。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。命令清单 `bytedcli --json --all-help`，单命令参数 `bytedcli --json <子命令路径> --help`。",
		"- git：查询和操作本地代码仓库。",
	}
	// Facts are available wherever the agent may learn something worth keeping.
	// StageDecide is excluded on purpose: it is a cheap value judgment that does
	// not investigate, so it has nothing of its own to record.
	if stage == StageExtract || stage == StageExecute || stage == StageChat {
		lines = append(lines,
			"- 查一个项目、群或人身上已经发生过什么时，使用 `jarvis-tools list-facts --help`；确实学到了值得明天回看的事实（定下来的决定和口径、真正的交付、卡住的原因、方向变化、别人承诺负责的事）时，使用 `jarvis-tools append-fact --help` 就地记一条，绑到它真正属于的项目/群/人身上。事实不是执行日志：不要给每条命令都记一条，也不要攒到最后补。",
		)
	}
	if stage == StageExecute {
		lines = append(lines,
			"- 当前 Task 需要等待未来条件时，使用 `jarvis-tools yield-until --help`，成功后停止本轮并返回 waiting；只有独立的新动作才创建 scheduled task。",
			"- 采集类任务把观察到的事实交回流水线时，使用 `jarvis-tools append-clue --help`：只报你确实看到的事实，不替 M3 判断含义，也不顺手去抓后续材料。同一事实可反复投递，服务端按 (source, external_id) 幂等。",
			"- 调查后发现这件事眼下不需要任何人动手（别人已经处理、结论已经达成、只是背景信息）时，使用 `jarvis-tools set-todo-status --help` 把来源 Todo 置为 observing 并写清理由：线索会留在视野里，以后有新证据会重新判断。这不是「等 principal 回复」的出路——需要他拍板的事仍然带着调查结果去问。",
		)
	}
	lines = append(lines, "END_AVAILABLE_TOOLS")
	return strings.Join(lines, "\n"), nil
}
