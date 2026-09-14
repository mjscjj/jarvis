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

func Block(stage string) (string, error) {
	switch stage {
	case StageOKRReview:
		// The advisory review reaches only OKR data; it has no reason to touch
		// Feishu, the world model or code, so it is not given that vocabulary.
		return okrReviewBlock(), nil
	case StageExtract, StageExecute, StageChat, StageFactEngine, StageProactive, StageMeetingSweep, StageMorningBrief:
	default:
		return "", fmt.Errorf("unknown tool catalog stage %q", stage)
	}
	return strings.Join([]string{
		"BEGIN_AVAILABLE_TOOLS",
		"各阶段共享工具能力；职责、调查深度和停止条件由当前阶段指引决定，不是工具权限。",
		"- jarvis-tools：查询和维护 Jarvis 内部状态。`--help` 查看能力组，`help <group>` 查找命令，`<command> --help` 查看输入、返回和机器约束。已知命令可直接使用，不必重复读取目录。",
		"  world：身份、项目、人物、关键事项、群、资料、实体页、跨模块关系和进度评估；evidence：消息、采集资料、Fact 和事件；task：Todo、Task、交办、运行记录与恢复；schedule：独立调度和当前 Task 的等待恢复；memory：长期行为偏好；skill：发现与读取领域流程；notify：Principal 通知及回执。",
		"- get-world-overview：实时薄目录；get-task/get-todo --context evidence：统一原始现场。大材料支持 --offset/--length，详见命令帮助。",
		"- M3 查询原始返回保存在 JARVIS_EVIDENCE_DIR；CLI 返回与模型评论分开看，评论属于准入审计。",
		"- 先读列表或索引，再读取命中对象；完整证据、冻结上下文和大段输出按需展开。参数及错误以目标命令帮助和服务端返回为准。",
		"- lark-cli：飞书操作。先读取匹配的官方 Skill；可用 `lark-cli skills list` / `skills read <域名>` 发现流程，`lark-cli schema <method>` 查看参数。",
		"- bytedcli：内部研发信息。用 `bytedcli --help` 选择领域，再逐级用 `bytedcli --json <命令路径> --help` 查看目标帮助，不加载全量手册。",
		"- git：本地代码仓库与版本历史。",
		"END_AVAILABLE_TOOLS",
	}, "\n"), nil
}

// okrReviewBlock describes the split between the generic OKR and Biz OKR
// scripts. The OKR review agent only uses their read commands; write
// commands remain subject to the stage prompt even when their ownership is
// called out here.
func okrReviewBlock() string {
	lines := []string{
		"BEGIN_AVAILABLE_TOOLS（工具能力说明由工具层维护，不属于系统角色提示词。）",
		"当前阶段：" + StageOKRReview,
		"两个脚本都已在 PATH 中，服务地址由 `JARVIS_API_BASE` 环境变量提供，直接执行即可，不需要自己解析配置或构建任何东西。所有命令输出 JSON。",
		"- okr-module-tools：通用 OKR 定义、Metric/Point/Owner 拆解和正式进展。`scope` / `board` 读季度结构；`find-krs` / `get-kr` 定位 KR；`weeks`、`progress-board`、`get-weekly-kr` 读取正式周次和进展；`replace-kr` 是通用拆解写入口，当前 OKR Review 阶段不调用写命令。",
		"- biz-okr-tools：Biz 业务组合视图。Plan 评审对象已由服务完整注入；`board [--quarter Q] [--week YYYY-Www]` 读包含标签、评分与 Meego 信息的周报；`get-kr` / `get-weekly-kr` 读 Biz 组合 KR；`comments`、`follow-ups` 读取协作信息；`people-search` 查人。",
		"- `find-krs` 单次输出可能上千行。先用它定位 ID，再用 `get-kr` / `get-weekly-kr` 取需要的那一个，不要把整季数据全部读进来。",
		"END_AVAILABLE_TOOLS",
	}
	return strings.Join(lines, "\n")
}
