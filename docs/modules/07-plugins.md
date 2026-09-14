# 插件系统

> Status: current
> Authority: normative
> Last verified: 2026-09-11
> Code source: `internal/plugin/`, `internal/delegation/`, `conf/skills.yaml`

Jarvis 插件按需开启外围采集或业务能力，不拥有第二套事件判断或任务执行链路。

未来不把这里扩展成动态插件平台。功能分享采用
[Agent 可安装功能源码包](../proposals/plugin-extension.md)：源码包可以携带后端、API、页面和
Skills，由 Agent 合并进目标 Jarvis。当前实现仍使用 Go 内置注册表与 `conf/skills.yaml`。

采集插件接入外部来源：

```text
Plugin lifecycle -> Scheduled Task -> Collector Skill -> /api/clues -> M2 -> M3 -> M5
```

能力插件复用已有事实和主链路：

```text
已有 Message / clue -> M3 + plugin prompt -> Todo -> Task -> M5 + plugin prompt
```

## 内置插件

Codebase、Meego、Oncall、“我的交办”和“产品流程”随仓库安装，但对新用户默认关闭。用户在“插件”
页面主动开启。前三者是采集插件：需要额外身份时先完成对应授权，授权成功后创建周期
采集任务并立即执行一次。“我的交办”是能力插件，启用后直接激活阶段规则。

“产品流程”是能力插件，汇总产品文档阅读、Review、Skill 维护及用户配置的定时任务。
Skills 正文由 Agent 持续维护仓库文件，人也可在页面查看、编辑与启停；两者共用同一份 Markdown 真源。计划使用通用 ScheduledTask，
到期生成普通 Task，由 M5 读取所选 Skill；插件页面与任务中心编辑同一记录。
启用本身不自动创建计划或执行 Review。详见 [产品流程](modules/product-management.md)。

“插件”入口始终显示；某个插件开启后才在左侧“插件”下显示同名二级入口，
关闭后立即移除。插件自己的配置和运行详情只放在该二级页面，不塞进管理列表。

关闭插件会停止后续采集并从 Agent 可用 Skill 中隐藏对应能力。已经进入 M2 的
原始线索、Todo、Task 和执行记录继续保留，作为历史证据和审计记录。
此开关控制后续采集和新组装的 Skill 目录，不自动取消已有 Task 或其等待续跑，
也不会移除运行中 Session 已收到的正文；它不是本机 Agent 的工具权限边界。

用户创建的计划可通过 `context_snapshot.plugin` 标记插件归属。每次派发前读取插件开关，
关闭时跳过该轮，不删除计划或历史 Task；若指定 `context_snapshot.skill`，还检查该 Skill
当前是否可用于 execute。普通无绑定计划不受影响，已在等待的 Task 仍走原续跑机制。

“我的交办”复用 M3 的 Todo 作为交办主体。M3 输出 `action_type=delegated_followup`
后即可展示，不依赖 Task 是否创建或成功。原始来源与完整理解保存在 Todo.content，
独立可变进展在 delegation_progress（以 todo_id 为主键）；没有进展行表示尚未核验。

M5 Task 只做一次检查并回写原待办。检查 completed/failed/waiting 均不推导对方是否交付。
后续新证据可由 M3 输出带 annotation.delegation_id 的普通检查线索，proactive 也可根据
当前进展按需创建携带 source.delegation_id 的普通 Task；跨次检查沿用同一个 Todo ID。
首次执行和等待/人工恢复都读取当前 Skills。关闭插件停止新的识别和巡视派单；已派出的
单次工作按原目标收口，记录保留，开关不自动结束交办。

## 所有权

- `internal/plugin`：Manifest、启停状态、授权探测、调度绑定和 Skill 门禁；Manifest
  区分需要定时采集的 collector 与只贡献阶段能力的 capability。
- `.agents/skills/*-clue-collector`：各来源的查询步骤和原始线索投递方法。
- `.agents/skills/my-delegations-*`：启用后进入 M3、M5 和 proactive 的识别、单次核验与巡视规则。
- `internal/delegation`：以 Todo 为主体的列表、宽松核验进展、闭环标记与关联 Task 查询；不驱动 Task 状态。
- `internal/scheduledtask`：通用时间触发和 Task 固化，不理解插件类型。
- `internal/capture`：统一接收 `/api/clues`，不理解 Codebase、Meego 或 Oncall。
- M3/M5：结合上下文判断线索是否值得行动，以及如何执行。

插件不建立来源专用业务表，不把外部对象转换成固定 Go DTO，也不绕过通用
M2 → M3 → M5 流水线。

## 持久化

`plugin_installation` 只保存程序必须消费的状态：

- `plugin_id`
- `enabled`
- `revision`
- `config`（插件自有的宽松 JSON）
- `scheduled_task_id`
- 创建和更新时间

Manifest 和 Skill 文件是仓库真源；外部原始事实保存在现有 clue 消息中。
“我的交办”只新增 delegation_progress：todo_id、content（宽松 JSON）、closed_at、version、
创建/更新时间。负责人、期限、交付标准、证据等不拆字段。历史更新复用 TodoEvent；
每次检查的执行记录继续使用 Task/ExecutionRun。列表只投影短摘要，详情返回完整原文。

旧版 delegated_followup Todo 自动成为同一条可见交办，无需复制或重建背景。旧 Task 状态
不会填入交办的完成状态；没有新核验记录时显示待核验。旧 waiting Task 恢复时按更新后的
插件规则完成单次检查，不再仅因对方尚未交付而无限等待。既有历史不删除。

导航通过 `/api/plugin-installations` 读取本地开关；外部授权探测只在插件详情接口执行。

## 授权

授权提供者使用代码内固定命令，不从 Manifest 或模型输出执行命令：

- Codebase：`bytedcli` ByteCloud 登录态，与 Jarvis 登录复用；设备授权不使用 `--session`
- Meego：Meego 官方设备授权
- Oncall：`lark-cli` 当前默认用户的飞书 IM 授权

先检查已有授权，仅在明确缺少授权时发起登录；网络或命令错误直接展示。
完成授权后重新检查实际能力，通过后才标记已授权。

启用与授权是两个独立状态。插件可以处于“已开启、待授权”，但此时不会创建
可运行的采集计划。

能力插件不声明授权 provider，启用后直接进入 ready；其权限说明只描述运行时会使用
的已有能力，不触发授权流程。

Oncall 默认按 `oncall`、`值班` 匹配当前用户可见群的名称和描述。用户可以在
插件页面增删 `config.search_terms`；保存后配置进入同一个定时 Task 的自然语言
指令，由 `oncall-clue-collector` 解释并通过通用 clue 入口投递原始群消息。
