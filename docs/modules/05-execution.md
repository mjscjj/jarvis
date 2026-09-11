# M5 执行环节

> Status: current
> Authority: normative module guide
> Last verified: 2026-09-10
> Code source: `internal/execute/`, `internal/taskcreate/`, `internal/scheduledtask/`, `internal/effectops/`

执行环节接管 `pending` Task：调查真实状态、确定目标和动作、判断具体副作用要不要先问 principal，并把事项推进到真实结果、等待或明确阻塞。

## 1. Task 来源与上下文

Task 可来自：

- `todo`：`extracted` Todo 经无模型固化步骤创建；
- `scheduled_task`：到期物化；
- `manual`：通过 API 创建。

所有来源都把原始语义和创建时事实冻结在同一个宽松 `source_payload`，结构为 `source + capture + annotation`。Todo 来源机械复制 `Todo.content`；scheduled、manual 和 proactive 来源在创建 Task 时由程序组装同一外壳。Go 和前端都不把 `source` 或 `annotation` 解释成固定执行计划。

首轮执行 prompt 默认只放：

- Task ID、来源、标题/目标 hint、当前状态、当前 summary、项目绑定和显式 `repo_path`；
- `annotation.brief`、`annotation.scene`、直接来源消息原文；没有消息来源时放原始 `source`；
- 可展开的 capture 区块、会话消息数和读取命令；
- execution supplements；
- 历史 run 数量及最近一次 ID/状态，不放 run 正文；
- 当前主动程度、shared memory、M5 rules、Skills、工具目录和审批政策。

完整 Candidate、周边会话、实体背景、相关 Todo/Task 和历史 run 正文都按需读取。`get-task --context ...` 展开冻结区块，`--message-id` 读取单条冻结消息；`list-tasks` / `list-todos` 可按关键词、来源消息、项目或群筛选；`list-task-runs` 返回分页概要，`get-task-run` 再读完整结果、错误和 effects，只有 `--include-prompt` 才展开 prompt。需要实体当前状态时由 M5 读取 summary 页或 Fact，而不是在启动时重新拼一个 `current_world`。

调用方明确选定仓库时，M5 消费 `repo_path`；否则继承 Jarvis 当前工作目录并自行定位，不从 capture 的项目资料猜默认值。

执行进程可以自行派生只读的子 agent 去做素材密集的调查，只把带出处的结论收回主上下文；派生规则写在 `m5-system-prompt.md`，Go 侧不感知也不调度。要不要问 principal、终态裁决、`effects` 申报、`progress_summary` 和 `yield-until` 不下放——可恢复的 Session 属于主进程。

上游 `source_payload` 中的来源和 capture 是冻结证据，不是最终执行契约。M5 可根据调查调整目标与动作，变化通过 supplements、运行记录、状态、结果和 Task summary 留痕，不回写来源证据。

## 2. 执行 phases

| Phase | 作用 |
|---|---|
| `execute` | 首轮调查、决定并执行，或提出问题/等待 |
| `resume_waiting` | 等待到期后续跑同一 Session |
| `resume_human` | principal 回答后续跑同一 Session |

没有独立的 apply 阶段：回答直接回到提问的那个 Session，它带着当时的全部调查继续，而不是照着一份冻结的稿子重放。

三种 phase 都重新读取 `conf/prompts/initiative-level.md`（quiet / normal / active），通过与后台预览相同的 `prompttemplate.Render` 注入。三档执行扩展与通知尺度归 `conf/rules/m5.md`；审批尺度仍归原审批策略。正在运行的一轮保持已组装的选择，等待和人工回答恢复时带上新选择，保留原 Session 的授权与动作证据。切档不取消已有 Task 或撤回问题卡。

安静档把一般发现留在 Task，普通档保留当前主动增益与送达尺度，活跃档增加有依据的准备与建议。明确交付和真实对外动作回执三档仍履行；需要人工决定的必要动作正常提问，消息工具和 runtime 不按档位拦截。

## 3. Outcome 与 Task 状态

```text
pending -> executing
  completed               -> done
  observing               -> observing（有来源 Todo 时同步回 observing）
  waiting                 -> waiting -> resume_waiting -> executing
  needs_human             -> needs_human -> resume_human -> executing
  failed / 运行错误        -> failed
```

`observing` 表示调查后确认事项真实但当前不需要任何人行动，不是完成也不是失败。

## 4. 提问（含请示副作用）

所有 action_type 走同一执行入口。Agent 根据 [`conf/prompts/m5-approval-policy.md`](../../conf/prompts/m5-approval-policy.md) 判断即将发生的具体副作用：不用问就继续执行并核验；要问就不执行该副作用，返回 `outcome=needs_human` 加一份 `question`。

`question` 是一份宽松 JSON：`title`、`body`（请示副作用时必须包含将要写出或发出的完整原文）和 `fields`。字段类型只有 `button` / `select` / `multi_select` / `input` / `link` 五种，因为每种对应一个具体的飞书控件；文案、选项和它们各自的含义全部由模型自己写。校验只卡"这张卡能不能被回答"：有标题、至少一个按钮、控件有名字且不重名、选择类有选项、链接有 URL。

落地顺序是先持久化后投递：Task 落到 `needs_human` 之后，`internal/cardask` 才渲染卡片并发给 principal，卡片里带着已持久化的 Task version。点击经 CC Connect 转回来，答案打包成 `{"clicked": 按钮名, 其余控件名: 值}` 交给 `KickResumeAfterHuman`，由原 Session 继续。

代码不解释答案，也不提供批准/驳回接口：它只提供一个可以停下来的状态、一个回答入口，以及事件与 `effects` 留痕。风险判断和答案含义都在模型那边。

## 5. 等待与人工回复

长等待由 Agent 调 `jarvis-tools yield-until` 创建绑定 ScheduledTask。Task 保存等待来源 run 和 Codex Session；到期后调 `exec resume`，不是从头重跑。

`needs_human` 也保存 Session。principal 回复写入 execution supplements 后，从暂停点继续，回答问题不自动等于批准副作用。

## 6. 进度、运行与 effects

- Task `summary` / `last_progress_at`：整个事项目前进展；summary 未变化时不伪造“有新进展”。
- ExecutionRun `summary`：本次运行做了什么。
- ExecutionRun 还记录 stage、sandbox、prompt、session、output、effects、repo、时间与错误。
- effects 使用严格外壳 `kind/title/url/target/preview/extra`，其中 `kind` 开放；未知 kind 保留。它是 Agent 声明，不是独立 verifier 的 receipt。
- 代码分支、commit、push、MR 等交付结果写进 effects，不再有专用 Git 列或 Go 编排。

飞书消息同样是 M5 显式选择并执行的工具动作：普通一对一或群聊会话消息读取 `feishu-send-message`，面向多个独立收件人的通知读取 `feishu-broadcast`，后者固定使用专用通知 Bot 直接私聊而不建助手群。M5 先确定受众、位置和完整文案，再按审批策略判断这一次具体发送要不要先问 principal。普通或个性化直发以真实 `om_...` 和读回结果为准，同文案批量广播以 `bm-...` 和发送进度为准；runtime 不根据来源会话、outcome 或 execution output 字段自动发送消息。

`internal/taskfeedback` 只负责在 execute 和 resume 开始时，尝试给来源飞书消息添加 `OnIt` reaction。它是 best-effort 的开始确认：Bot 不在来源会话时失败只记日志，不影响 M5；它不承载业务结果，也不提供文字 fallback。问题卡是另一条机器协议，仍由 runtime 在 `question`、`needs_human` 和 Task version 持久化后投递。

factengine 从 `message`、TodoEvent 和 TaskEvent 三类材料蒸馏 Fact；来源清单由服务启动层显式装配，Worker 不依赖 GORM Store 提供注册表。ExecutionRun 本身仍不作为独立来源。

## 7. 接口与运维

Task 执行接口包括：runs、events、output、execute、interrupt、rerun、resume、finish 和 supplement。已产生外部效果上的用户操作独立放在 `internal/effectops`；当前包括 message recall。完整分组见 [HTTP API](../reference/http-api.md)。

实时推进由 `pipeline.Coordinator` 触发；`execute.schedule` 恢复漏通知和过期 `executing`。配置在 `execute.*`，运行时 overlay 保存后需重启。
`execute.concurrency` 控制自动 M5 worker 池大小，实时 TaskReady 和补偿扫描发现的 pending Task 共用这组 worker；每个 Task 仍通过 `pending -> executing` 的 version 条件更新抢占，避免同一个 Task 被重复执行。
任务列表页支持一次选择最多 5 个 pending Task 并批量启动；它复用单 Task execute API，实际运行仍由每个 Task 自己的状态/version 抢占保护。

重建服务前必须确认没有活跃 Task，见 [运行与部署](../reference/operations.md)。

## 8. 当前缺口

- effects 未对外部系统 receipt 做独立核验。
- M5 直接发送普通消息后、最终 effects 落盘前仍有崩溃窗口；Skill 的查重、稳定幂等键和读回只能降低重复概率，不是 exactly-once outbox。
- ExecutionRun 尚未作为独立 factengine 来源。
