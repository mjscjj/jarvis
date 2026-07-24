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
	switch stage {
	case StageExtract:
		purpose = "补全行动线索的项目、人物、会话、文档和代码背景；把查到的关键事实写入候选 context。"
	case StageDecide:
		purpose = "补全决策证据和可执行计划；只做查询和备料，不执行 M5 才应发生的外部动作。"
	case StageExecute:
		purpose = "完成任务、核验结果；需要等待时暂停当前 Task，避免创建重复任务。"
	case StageChat:
		purpose = "按用户请求查询或操作本机与外部系统。"
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}

	jarvisTool := "- jarvis-tools：查询 Jarvis 的项目、人物、群、项目资源、共享记忆、Skills 和定时任务。先运行 `jarvis-tools --help`，再按子命令 `--help` 获取当前参数。"
	if stage == StageExecute || stage == StageChat {
		jarvisTool = "- jarvis-tools：查询 Jarvis 上下文，并按任务需要追加共享记忆、管理独立定时触发或暂停当前 Task。先运行 `jarvis-tools --help`，再按子命令 `--help` 获取当前参数。"
	}
	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + stage,
		"使用目的：" + purpose,
		jarvisTool,
		"- lark-cli：查询或操作飞书。先运行 `lark-cli --help` 或对应 domain 的 `--help`；匹配到飞书 Skill 时先读取 Skill。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。先运行 `bytedcli --help` 或对应子命令 `--help`。",
		"- git：查询和操作本地代码仓库。",
	}
	if stage == StageExecute {
		lines = append(lines,
			"- 当前 Task 需要等待未来条件时，使用 `jarvis-tools yield-until --help`，成功后停止本轮并返回 waiting；只有独立的新动作才创建 scheduled task。",
		)
	}
	lines = append(lines, "END_AVAILABLE_TOOLS")
	return strings.Join(lines, "\n"), nil
}
