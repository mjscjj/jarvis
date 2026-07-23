# 每日进度总结（Daily Digest）设计

把「进度」页从现在的**任务数量播报**（按天 Count Todo/Task/消息，再把数字翻译成一段话）升级为**内容层面的每日进度总结**：对「我」个人和「关键群」各自，按自然日生成一段可读的进度摘要。

完整背景见 `[README.md](../README.md)` 与 `[docs/00-overview.md](00-overview.md)`。本文只覆盖每日总结这一功能。

## 0. 目标与范围

- **只做两类总结**：
  - **个人（我）每日进度**：我这一天推进了什么——不止「我发的消息」，而是尽量还原「我今天做的所有事」（消息、完成的 Task、编辑的文档、会议、代码 MR/commit）。
  - **关键群每日进度**：仅对 `is_key_group=1` 的群，总结该群当天讨论/推进了什么。
- **时间粒度**：自然日（配置时区 `Asia/Shanghai` 的 00:00–24:00），复用现有 digest 的日历日 bucket。
- **触发**：个人总结每晚 **19:00** cron 自动生成当天；页面也可**手动触发/重算**。19:00 生成的是「截至生成时刻」的当天进度；手动重算可刷新到最新。群总结保留手动入口，不与个人 Codex 定时任务耦合。
- **不做**（本期）：非关键群的群总结、多用户、历史版本留存（重算直接覆盖）。

## 1. 两类总结统一使用 Codex（关键决策）

| 维度 | 引擎 | 理由 |
|---|---|---|
| **个人进度** | **codex agent**（`execute` 段的官方 codex，`danger-full-access`+联网） | 自跑 `lark-cli` / `bytedcli` / `git` 收集跨渠道证据，并加载 `summarize-person-day` Skill |
| **关键群进度** | **codex agent**（与个人共用同一个 runner） | 群消息只是调查入口；加载 `feishu-group-daily-summary` Skill，补拉完整消息、线程、文档、commit/MR 和相关材料 |

## 2. 数据源

### 2.1 个人进度：三类来源、两路并行采集、集中汇总

一级数据源只保留三类。文档、会议、MR、Commit 等是各 collector 的内部检查项，
不再作为互相割裂的一级来源：

| 一级来源 | 执行者 | 内容 |
|---|---|---|
| `jarvis_internal` | Go 确定性查询 | 本人消息、当天 TodoEvent、TaskEvent、ExecutionRun；ProjectEvent 仅作项目上下文 |
| `feishu_work` | 独立 Codex collector | Jarvis 消息对应的回复/线程上下文、文档、日历发现、会议/妙记逐字稿；不重复产出本人消息 |
| `engineering_execution` | 独立 Codex collector | Codex sessions、MR/CR、Commit、测试、部署和运行验收 |

主控先确定身份映射、自然日窗口、截止时间、每个来源的完整性检查和访问上限。
Jarvis 查询完成后，飞书与工程两个 collector subagent 并行执行；两者只返回带
稳定 ID、时间、归因、原始引用和覆盖缺口的 EvidenceCard，不写最终总结，也不做
跨来源推断。全部 collector 到齐后，主控 synthesis agent 才做项目归属、跨源去重、
成果判定和最终写作。

Jarvis 内部事实严格按事件时间查询，不使用“`updated_at` 当天 OR 当前未结束”
这种混合口径：

- 消息：`create_time ∈ [day_start, cutoff)`，倒序取 `limit+1`，截断必须标 `partial`，
  再反转成时间正序；
- Todo：读取 `todo_event.created_at` 及事件发生时落下的不可变语义快照，不回读
  Todo 当前行；历史事件若没有快照则显式标 `partial`，本期不猜测或回填旧数据；
- Task：读取 `task_event.occurred_at`，状态以事件自身为准；
- 执行：读取当天开始或结束的 `execution_run`，与 TaskEvent 通过 `run_id` 归并；
- 项目：`project_event.occurred_at` 只作状态上下文，因其没有 actor，不能直接归因；
- 历史仍开放的 Todo/Task 不计入当天事实和 `source_count`。

飞书 collector 必须拉全分页。会议是个人总结的第一优先级，最终固定单列
`会议与妙记`：先拉全本人参与的会议，再逐场解析
`meeting_id → minute_token/note_id → transcript`；AI 摘要、
章节和 Todo 只作导航。妙记无权限、未就绪或检索失败时保留会议事实并显式标记
`partial/error`，不能静默变成 `empty`。

工程 collector 同时调查 agent sessions、精确远端 MR/CR revision、Commit 以及
测试/部署/运行验收。必须区分本人直接完成、本人委派 Agent 完成、协作、仅被分配
和仅参与讨论；session 标题、Commit 或 MR 存在都不能自动升级为“完成”。

分析统一使用 `Activity → Output → Observed Outcome`，不强行补齐没有证据的阶段。
Decision、Commitment、Risk 是正交事实：proposal 不是 accepted decision，assignment
不是 accepted commitment。最终固定六段：

1. `会议与妙记`：逐场写时间、标题、时长、结论、决策和我的行动项，末尾汇总总场次与总时长；
2. `今日结论`：最多三条最强 output/outcome；
3. `按项目变化`：同一工作项跨来源只写一次；
4. `决策与承诺`：仅写有接受证据的决策和承诺；
5. `风险与阻塞`：写影响、责任人、缓解和证据缺口；
6. `数据覆盖`：三类来源、截止时间及 partial/error/unavailable。

Collector 与 synthesis 都返回严格 JSON，并由 Go 使用未知字段拒绝、尾随内容拒绝、
枚举/必填/唯一 Evidence ID/时间窗/引用校验 fail-fast。分析框架由
`.agents/skills/summarize-person-day` 维护，服务启动时加载同一 Skill 和数据合同。

**不承诺**：我发起的审批（无跨定义按天查）；跨所有远端仓库的全量 commit（无全局按天接口）。

### 2.2 关键群进度（Jarvis 查库打底 + codex 调查总结）

- 群集合：`feishu_group` 表 `is_key_group = 1`。
- 每个关键群当天消息：`WHERE group_id = ? AND create_time ∈ [day)`，作为快速打底（升序、保留 message/thread 标识、正文按条截断）。
- 打底消息量上限：每群每天最多 N 条（默认 200）、每条截断到约 800 字。超出只截断打底，不代表证据截断；codex 必须按 Skill 用 `lark-cli` 拉全窗口并展开关键线程。
- codex 按消息线索读取飞书文档、commit/MR 和其他关键材料，输出有结论、事实边界和材料链接的群总结。

## 3. 存储

新表 `daily_digest`，一天一 scope 一行，重算 upsert 覆盖（不留历史版本）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uint64 PK | |
| `scope` | varchar(16) | `person` / `group` |
| `scope_id` | varchar(64) | person=principal open_id；group=`feishu_group.id` 的字符串 |
| `digest_date` | date | 自然日（本地时区） |
| `summary` | mediumtext | 生成的一段中文进度总结 |
| `status` | varchar(16) | `pending` / `generating` / `done` / `failed`（异步生成状态） |
| `trigger_type` | varchar(16) | `manual` / `schedule` |
| `source_count` | int | 通过日期、身份和 schema 校验后交给 synthesis 的唯一 EvidenceCard 数量；开放上下文不计入 |
| `source_coverage` | json | 每个数据源的 `status/count/note` |
| `engine` | varchar(16) | `codex` |
| `error_detail` | text | 失败原因（fail 时） |
| `started_at` | datetime | 本轮生成开始时刻 |
| `cutoff_at` | datetime | 本轮证据截止时刻 |
| `generated_at` | datetime | 生成完成时刻（19:00 触发时体现「截至此刻」） |
| `created_at` / `updated_at` | datetime | |

- 唯一索引 `uk_scope_date (scope, scope_id, digest_date)`：保证一天一 scope 一行，重算 upsert。
- 注册进 `domain.CoreModels()` 走 AutoMigrate。

## 4. 触发与并发

- **自动**：`internal/dailydigest` 的 cron scheduler 每晚 **19:00** 只生成个人总结。服务在当天计划点之后启动且当天没有结果时补跑一次；当天已有手动结果则跳过。
- **手动**：前端按钮触发单条（某 scope 某天）生成/重算，**异步**（同 M5 任务执行模式）：API 立即置 `generating` 返回，后台跑 codex，前端轮询状态。
- **并发**：数据库条件更新原子抢占 `(scope, scope_id, digest_date)`。手动允许覆盖 `done/failed`，定时不覆盖 `done`，任何入口都不允许抢占 `generating`。服务启动时把旧进程遗留的 `generating` 标为失败。

## 5. 改动范围

**后端（Go）**：
1. `internal/domain/models.go`：加 `DailyDigest` model + `CoreModels()` 注册。
2. 新包 `internal/dailydigest/`：
   - `store.go`：`daily_digest` 读写（get by scope+date、upsert、置状态）。
   - `person.go`：个人总结——查库打底（我的消息+Task）+ 构建 codex prompt（含 lark-cli/bytedcli/git 命令引导）+ 调 codex runner + 落库。
   - `group.go`：关键群总结——查库打底 + 加载群总结 Skill + codex 自跑工具调查 + 严格 JSON 解码 + 落库。
   - `service.go`：编排（生成单条 / 批量生成当天 / 读取），异步 kick。
   - `scheduler.go`：19:00 cron（照 `memory/scheduler.go`）。
3. `internal/api/`：`GET /api/daily-digests?date=`（读当天全部 scope）、`POST /api/daily-digests/generate`（按 scope+date 异步生成/重算）；`router.go` 注册 + deps 注入。
4. `cmd/jarvis-server/main.go`：构造 dailydigest service（个人/群共用 execute codex runner，注入两个 Skill 目录、principal_open_id、is_key_group 群查询）+ 起 19:00 scheduler + 接进 API deps。
5. `conf/config.yaml` + `internal/config`：加 dailydigest 配置段（schedule 默认 `0 19 * * *`、每群打底消息上限、群并发度、enable 开关）。

**前端（React/TS）**：
6. `web/src/types.ts`：加 `DailyDigest` 类型。
7. `web/src/api.ts`：加 `getDailyDigests(date)` / `generateDailyDigest(scope, scopeId, date)`。
8. `web/src/Progress.tsx`：改造成「按日期选择 + 我的进度卡片 + 各关键群卡片」，每卡片显示 summary / 生成时间 / 状态，缺失或想刷新时有「生成 / 重算」按钮（异步 + 轮询）。保留或移除旧的数字表由实现时定（倾向保留为辅助小结）。

## 6. 待观察 / 后续增强

- 非关键群总结（本期不做，放开即把群集合从 `is_key_group` 换成 `related_group`）。
- codex 调用需要自跑多条 CLI，分钟级；个人 cron 与两类手动触发都靠异步+轮询承接。
- 文档编辑口径的能力上限（见 2.1）。
- 19:00 之后的当天活动不计入自动生成，靠手动重算补。

## 7. 实现状态（2026-07-23）

本期设计已实现：

- 后端已完成个人定时/手动统一生成入口、原子防重、启动补跑、重启恢复；个人总结按三类来源执行，飞书与工程 collector 并行，主控集中归并并校验证据引用。
- `summarize-person-day` Skill 统一自然日边界、三类来源、collector 合同、项目归属、跨来源去重、`Activity → Output → Observed Outcome` 分析及会议优先的最终六段式输出。
- 已新增 `feishu-group-daily-summary` Skill；群总结由 Codex 拉全群消息、展开关键线程并按需读取文档、commit/MR 和其他材料。
- 前端「进度」页已完成立即生成/重新生成/重试、生成中轮询、触发类型、证据截止时间和各来源状态展示；旧数量统计保留为辅助 Tab。
- 配置已默认启用每日 19:00 个人总结调度；个人与群总结都使用 `danger-full-access` 官方 Codex，群总结仍只手动触发。

验收：`go test ./...`、`npm run build` 通过；本机 launchd 前后端重启后，`/healthz`、`/api/daily-digests` 与 Progress 页面真实加载通过。实际生成会调用 Codex 并写入当天摘要；个人可手动或由 19:00 cron 触发，群总结只从页面手动触发。
