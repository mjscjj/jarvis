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

func Block(stage string) (string, error) {
	switch stage {
	case StageExtract, StageExecute, StageChat, StageFactEngine, StageProactive, StageMeetingSweep, StageMorningBrief:
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}
	return strings.Join([]string{
		"BEGIN_AVAILABLE_TOOLS",
		"各阶段共享工具能力；职责、调查深度和停止条件由当前阶段指引决定，不是工具权限。",
		"- jarvis-tools：查询和维护 Jarvis 内部状态。`--help` 查看能力组，`help <group>` 查找命令，`<command> --help` 查看输入、返回和机器约束。已知命令可直接使用，不必重复读取目录。",
		"  world：身份、项目、人物、关键事项、群、资料和实体页；evidence：消息、采集资料、Fact 和事件；task：Todo、Task、交办、运行记录与恢复；schedule：独立调度和当前 Task 的等待恢复；memory：长期行为偏好；skill：发现与读取领域流程；notify：Principal 通知及回执。",
		"- 先读列表或索引，再读取命中对象；完整证据、冻结上下文和大段输出按需展开。参数及错误以目标命令帮助和服务端返回为准。",
		"- lark-cli：飞书操作。先读取匹配的官方 Skill；可用 `lark-cli skills list` / `skills read <域名>` 发现流程，`lark-cli schema <method>` 查看参数。",
		"- bytedcli：内部研发信息。用 `bytedcli --help` 选择领域，再逐级用 `bytedcli --json <命令路径> --help` 查看目标帮助，不加载全量手册。",
		"- git：本地代码仓库与版本历史。",
		"END_AVAILABLE_TOOLS",
	}, "\n"), nil
}
