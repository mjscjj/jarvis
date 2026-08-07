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

// Block returns the trusted tool catalog for one agent stage.
func Block(stage string) (string, error) {
	var purpose, usage string
	switch stage {
	case StageExtract:
		purpose = "完成 Task 准入：只查询责任归属、当前状态、已有 Todo/Task、明确项目归属和作出准入判断所缺的决定性证据。"
		usage = "准入导向、最短证据链——只为判断是否与 Principal 有关、是否尚未闭环、是否需要 Principal/Jarvis 介入、是否已经完成或重复而查询；证据足够选择 extracted、observing 或不输出时立即停止。"
	case StageExecute:
		purpose = "完成任务、核验结果；需要等待时暂停当前 Task，避免创建重复任务。"
		usage = "目标导向、主动发散——为查清事实或办成任务，主动组合多个工具、顺藤摸瓜多跳查询；一条路查不到就换工具或角度。能查到的不要留给用户问。"
	case StageChat:
		purpose = "按用户请求查询或操作本机与外部系统。"
		usage = "目标导向、主动发散——为回答用户或完成用户请求，按需组合多个工具；一条路查不到就换工具或角度。"
	case StageFactEngine:
		purpose = "阅读增量材料，按需查证并维护 Jarvis 内部世界模型；事实、当前画像、关系和资料都由 Agent 自己判断是否需要写入。"
		usage = "建模导向——围绕增量材料按需查证来源和当前状态，维护有用的世界事实，不扩展成外部执行任务。"
	case StageProactive:
		purpose = "定时读取世界模型、看护未闭环事项，并把值得推进的外部工作创建成普通 Task 交给强 M5；调查中可按需维护内部世界状态。"
		usage = "巡视导向——比较当前世界状态与未闭环事项，调查到足以决定创建、推进、更新或关闭 Task，并遵守本阶段的外部动作边界。"
	case StageMeetingSweep:
		purpose = "定时查找最近结束的飞书会议和未来待参加的会议日程，把每场会作为一条线索投递给 M2/M3；只采集，不分析。"
		usage = "采集导向——只取得会议或日程的原始事实并投递，不分析价值、不生成总结、不执行后续动作。"
	case StageMorningBrief:
		purpose = "每个工作日开工前生成晨间作战简报：读世界状态与日历，选出最多三个今日结果，写本地 Markdown 并只给 Principal 本人发一条飞书私聊。"
		usage = "简报导向——查询当天容量、承诺和变化，生成并交付一份有证据的晨间简报，不扩大成其它执行任务。"
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
		"用法：" + usage,
		"- jarvis-tools：查询 Jarvis 的项目、人物、群背景、关系、资源、消息、共享记忆、Skills、线索、任务与定时触发；允许写入的阶段也用它维护对应对象。先运行 `jarvis-tools --help` 看能力分组，再按子命令 `--help` 获取当前参数。",
		"- 查主体历史用 `list-facts`，查主体关系用 `list-relations`；需要维护世界模型时使用对应 create/update/delete 子命令。",
		"- 查线索或任务先用 `list-todos` / `list-tasks`，命中后再用 `get-todo` / `get-task`。`get-task` 默认不加载 prompt 和完整 run output。",
		"- 查本地已采集对话先用 `query-messages`；查附件与文档引用先用 `query-captured-resources`，命中后再用 `get-captured-resource` 加载正文。",
		"- 当前 Task 需要等待未来条件时使用 `yield-until`；独立的新动作才创建 scheduled task。",
		"- lark-cli：查询或操作飞书。先用 `lark-cli skills list` 查看能力目录并选定域，再用 `lark-cli skills read <域名>` 查工作流、`lark-cli schema <method>` 查单 API 参数；匹配到飞书 Skill 时先读取 Skill。",
		"- bytedcli：查询内部代码、commit、MR、issue 等研发信息。命令清单 `bytedcli --json --all-help`，单命令参数 `bytedcli --json <子命令路径> --help`。",
		"- git：查询和操作本地代码仓库。",
	}
	if stage == StageExtract {
		lines = append(lines,
			"- M3 只做准入调查：不制定执行方案，不执行外部写，不创建或推进 Task，不修改 Todo、事实、共享记忆、代码、文档、日历或消息。",
			"- 已有摘要足够时不要调用工具；需要补证据时优先查询现有 Todo/Task、消息和明确归属，不为丰富 payload 展开长链路调查。",
		)
	} else if stage != StageMeetingSweep {
		lines = append(lines,
			"- 可按当前阶段边界和语义需要投递线索、追加共享记忆、记录事实或修改 Todo 状态；工具层只提供能力和留痕，不替 Agent 做语义判断。",
		)
	}
	if stage == StageProactive {
		lines = append(lines,
			"- 未闭环关键事项最多 10 个、启用资源最多 50 个；列表默认按最近活跃时间倒序。只有新证据确认对象仍具持续价值时才使用 `touch-key-matter` / `touch-resource`，不得因读取过就刷新活跃时间。",
			"- 主动巡视发现需要对外推进的工作时，必须使用 `jarvis-tools create-task --payload ...` 创建普通 Task；不得直接执行外部动作。",
			"- 对今天已有的 pending Task，使用 `jarvis-tools start-task --id ...` 交给强 M5；不要创建重复 Task。",
			"- Task 的目标表达、当前进展或后续执行指示因新证据变化时，优先使用 `jarvis-tools update-task --id ... --payload ...` 维护；不得改写冻结的 source_payload/background。",
			"- 只有已查证完成、明确取消、客观失效或被仍存活的 Task 完整取代时，才使用 `jarvis-tools close-task --id ... --payload ...` 收口；跨日、沉默或没有新证据不是关闭依据。",
			"- factengine 的主要任务是持续维护人物、项目、群、资料、事实和关系；这不是对主动巡视的写入禁令。巡视调查中发现明确且有用的变化时，可以直接使用通用 CRUD 维护并立即读回，也可以按判断走统一线索入口。",
			"- 世界模型维护只是巡视的辅助动作；不要为了补全模型扩大本轮范围或偏离看护、推进未闭环工作的主要任务。",
		)
	}
	if stage == StageFactEngine {
		lines = append(lines,
			"- 事实建模 Agent 可以使用项目、人物、群、Principal、资料和关系的通用查询及 CRUD 命令维护 Jarvis 内部认知；先查现值，确认有新增或变化后再写，并立即读回。",
			"- 发生过的决定、交付、进展、阻塞、承诺或方向变化使用 `append-fact` 直接写入；重放材料时先查询近期事实，不重复写。",
			"- 本阶段不创建或推进 Task，不修改外部系统。需要补证据时可以只读查询 lark-cli、bytedcli 和 git。",
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
