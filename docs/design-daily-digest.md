# 每日进度总结（Daily Digest）设计

> Status: current
> Authority: normative design
> Last verified: 2026-08-07

把「进度」页从现在的**任务数量播报**（按天 Count Todo/Task/消息，再把数字翻译成一段话）升级为**内容层面的每日进度总结**：对「我」个人和「关键群」各自，按自然日生成一段可读的进度摘要。

完整背景见 [README.md](../README.md) 与 [docs/00-overview.md](00-overview.md)。本文只覆盖每日总结这一功能。

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

### 2.1 个人进度：一个 Skill 主控、一次并行取证、直接成稿

一级数据源只保留三类。文档、会议、MR、Commit 等是各 collector 的内部检查项，
不再作为互相割裂的一级来源：

| 一级来源 | 执行者 | 内容 |
|---|---|---|
| `jarvis_internal` | Go 确定性查询 | 本人消息、当天 TodoEvent、TaskEvent、ExecutionRun；当天的 Fact 作为主体上下文，并先写入当日证据文件 |
| `feishu_work` | Skill 主控派生的独立 subagent | 消息/线程、文档、日历、会议、妙记、任务、OKR 等；以 `lark-cli` 能力地图为导航 |
| `engineering_execution` | Skill 主控派生的独立 subagent | Codex sessions、仓库、MR/CR、Commit、测试、部署和运行验收；以 `bytedcli`、Git 和本地代码能力为导航 |

Go 只负责硬边界：确定身份、自然日窗口、截止时间、运行 ID，写入 Jarvis
确定性证据并启动一次顶层 Codex。顶层 Codex 在真实 workspace 中只注入轻量
`.agents/skills/summarize-person-day/SKILL.md`，其他参考按 lane 读取：

1. 只并行启动一个飞书 collector 和一个工程 collector；
2. collector 不允许继续派生 agent，每条 lane 只做一次 seed-driven 调查；
3. 两条 lane 完成后，顶层 Agent 一次读取本轮三份证据，直接归并并写
   `99-report.md`；
4. 不设二次调查、独立审阅、分章节写手或中间分析文件；冲突和缺口直接进入报告的
   数据覆盖区；
5. 所有外部系统默认只读；原始证据追加保存，重算不覆盖旧证据。

Jarvis 内部事实严格按事件时间查询，不使用“`updated_at` 当天 OR 当前未结束”
这种混合口径：

- 消息：`create_time ∈ [day_start, cutoff)`，倒序取 `limit+1`，截断必须标 `partial`，
  再反转成时间正序；
- Todo：读取 `todo_event.created_at` 及事件发生时落下的不可变语义快照，不回读
  Todo 当前行；同一 Todo 的多条状态事件聚合成一条生命周期证据，快照只保留一次；
- Task：读取 `task_event.occurred_at`，状态以事件自身为准；同一 Task 的状态事件聚合
  成一条生命周期证据；
- 执行：读取当天开始或结束的 `execution_run`，同一 Task 的多次 Run 聚合为一次执行
  时间线，保留各 Run ID 和最终结果；
- 事实：`fact.occurred_at` 只作状态上下文，因其没有 actor，不能直接归因；
- 历史仍开放的 Todo/Task 不计入当天事实和 `source_count`。

飞书取证先做一轮有界的当日清点：本人发出消息数、显式 @ 本人的消息数与去重人数、
本人涉及的会议数与时长、本人新建或实质编辑的文档数；再以 Jarvis Seed 为入口补齐
关键消息线程、会议和直接引用的文档、任务与人员。会议按
`meeting_id → minute_token/note_id → transcript` 读取最佳可用材料；不默认扫描整个
Drive、任务、OKR、审批或邮箱，也不为日报批量导出或复算 Base/Sheets 数据。无权限、
未就绪或检索失败显式标记 `partial/error`。没有覆盖完整当日的统计只能写成“至少 N”
或“未知”，不能伪装成精确值。

工程取证统计当前可达范围内的本人 Commit、MR/CR、Review、Agent Run、测试、部署与
发布，再从 Seed 中的 Task、Run、Session、仓库、Commit 和 MR/CR 标识出发，只读取
直接相关的实际产物、远端状态、测试、部署或运行结果；不默认遍历全部 Codex
transcript、全部本地仓库或 bytedcli 能力域。已有 Run 结果时只读取最终输出/effects
与直接关联测试，不回放整段 session。必须区分本人直接完成、本人委派 Agent 完成、
协作、仅被分配和仅参与讨论。仓库范围不完整时，Commit 与 MR/CR 统计必须标成下界。

最终日报先回答“今天到底发生了什么”，再做判断。九个一级区块是：

1. `今日数据`
2. `今天的会议`
3. `消息与协作`
4. `项目与工作进展`
5. `已完成事项`
6. `待讨论事项`
7. `后续计划`
8. `关联、洞察与其他发现`
9. `数据说明`

`今日数据` 用紧凑表格列消息、@、会议、文档、Commit、MR/CR、Agent Run 等可得统计；
`今天的会议` 逐场写讨论、结论、计划、Todo 和未定问题；`消息与协作` 按“谁找了我 /
我找了谁”及人员话题归并，不抄聊天流水；后续分别提炼已完成结果、需要人参与判断的
讨论项、可直接执行的后续计划和开放式发现。
主文使用真实人名、项目名、数字和当前状态，不在每句话后堆证据 ID。证据索引与覆盖
缺口集中放在 `数据说明`。

Go 不要求 collector/synthesis 严格 JSON DTO，只校验机器必须消费的最小投影：报告
日期、九个一级标题顺序、当次运行 ID、三类来源覆盖状态和证据数量。完整语义始终保留
在 Markdown 中。

**不承诺**：我发起的审批（无跨定义按天查）；跨所有远端仓库的全量 commit（无全局按天接口）。

### 2.2 关键群进度（Jarvis 查库打底 + codex 调查总结）

- 群集合：`feishu_group` 表 `is_key_group = 1`。
- 每个关键群当天消息：`WHERE group_id = ? AND create_time ∈ [day)`，作为快速打底（升序、保留 message/thread 标识、正文按条截断）。
- 打底消息量上限：每群每天最多 N 条（默认 200）、每条截断到约 800 字。超出只截断打底，不代表证据截断；codex 必须按 Skill 用 `lark-cli` 拉全窗口并展开关键线程。
- codex 按消息线索读取飞书文档、commit/MR 和其他关键材料，输出有结论、事实边界和材料链接的群总结。

## 3. 存储

个人日报以本地 Markdown 为事实真源，按天存放：

```text
data/personal-daily/YYYY-MM-DD/
├── 00-context.md
├── 10-evidence-jarvis.md
├── 20-evidence-feishu*.md
├── 30-evidence-engineering*.md
└── 99-report.md
```

`99-report.md` 是该自然日的当前正式稿；原始证据按波次追加，手动重算使用新的
`refresh` 文件，不覆盖旧证据。`daily_digest` 表继续承担现有 API/UI 的状态与缓存投影：
一天一 scope 一行，重算 upsert 覆盖摘要缓存，但不删除 Markdown 证据底账。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uint64 PK | |
| `scope` | varchar(16) | `person` / `group` |
| `scope_id` | varchar(64) | person=principal open_id；group=`feishu_group.id` 的字符串 |
| `digest_date` | date | 自然日（本地时区） |
| `summary` | mediumtext | `99-report.md` 的展示缓存，供现有 API/UI 直接读取 |
| `status` | varchar(16) | `pending` / `generating` / `done` / `failed`（异步生成状态） |
| `trigger_type` | varchar(16) | `manual` / `schedule` |
| `source_count` | int | 当日目录中有效证据条目的最小投影计数 |
| `source_coverage` | json | 从当日目录投影的三类来源 `status/count/note` |
| `engine` | varchar(16) | `codex` |
| `error_detail` | text | 失败原因（fail 时） |
| `started_at` | datetime | 本轮生成开始时刻 |
| `cutoff_at` | datetime | 本轮证据截止时刻 |
| `generated_at` | datetime | 生成完成时刻（19:00 触发时体现「截至此刻」） |
| `created_at` / `updated_at` | datetime | |

- 唯一索引 `uk_scope_date (scope, scope_id, digest_date)`：保证一天一 scope 一行，重算 upsert。
- 注册进 `domain.CoreModels()` 走 AutoMigrate。

## 4. 触发与并发

- **自动**：`internal/dailydigest` 的 cron scheduler 每晚 **19:00** 只生成个人总结。服务在
  当天计划点之后启动且当天从未尝试时补跑一次；当天已有 done、failed 或 generating
  记录都跳过，失败后只接受页面手动重试，避免每次重启重复消耗。
- **手动**：前端按钮触发单条（某 scope 某天）生成/重算，**异步**（同 M5 任务执行模式）：API 立即置 `generating` 返回，后台跑 Codex，前端安静轮询状态。
- **并发**：数据库条件更新原子抢占 `(scope, scope_id, digest_date)`。手动允许覆盖 `done/failed`，定时不覆盖 `done`，任何入口都不允许抢占 `generating`。服务启动时把旧进程遗留的 `generating` 标为失败。
- **超时**：个人日报使用独立 runner，正常目标 3–5 分钟，默认 600 秒硬上限；配置
  小于 300 秒直接拒绝启动。外层超时只负责终止失控运行。

## 5. 改动范围

**后端（Go）**：
1. `internal/domain/models.go`：加 `DailyDigest` model + `CoreModels()` 注册。
2. 新包 `internal/dailydigest/`：
   - `store.go`：`daily_digest` 读写（get by scope+date、upsert、置状态）。
   - `person.go`：个人总结——查库打底并写入当日 Jarvis 证据 + 在真实 workspace 启动顶层 Skill agent + 校验当次 Markdown 产物 + 投影入库。
   - `group.go`：关键群总结——查库打底 + 加载群总结 Skill + codex 自跑工具调查 + 严格 JSON 解码 + 落库。
   - `service.go`：编排（生成单条 / 批量生成当天 / 读取），异步 kick。
   - `scheduler.go`：19:00 cron（照 `memory/scheduler.go`）。
3. `internal/api/`：`GET /api/daily-digests?date=`（读当天全部 scope）、`POST /api/daily-digests/generate`（按 scope+date 异步生成/重算）；`router.go` 注册 + deps 注入。
4. `cmd/jarvis-server/main.go`：构造 dailydigest service（每日总结使用独立的宽松超时 runner，不与 M5 任务争用超时配置；注入 workspace、两个 Skill 目录、principal_open_id、is_key_group 群查询）+ 起 19:00 scheduler + 接进 API deps。
5. `conf/config.yaml` + `internal/config`：加 dailydigest 配置段（schedule 默认 `0 19 * * *`、`timeout_seconds` 默认 600、每群打底消息上限、群并发度、enable 开关）。

**前端（React/TS）**：
6. `web/src/types.ts`：加 `DailyDigest` 类型。
7. `web/src/api.ts`：加 `getDailyDigests(date)` / `generateDailyDigest(scope, scopeId, date)`。
8. `web/src/Progress.tsx` 与 `web/src/review/`：回顾页第一层横向排列每日总结、会议总结、群总结、文档、代码；每日总结第二层按日期横排，群总结第二层按关键群横排。个人报告按安全 Markdown 渲染，生成期间静默轮询。

### 5.1 回顾页中的会议总结

会议总结不扩展 `daily_digest` 的 scope，也不新增会议专用生成器。会议继续走唯一的
`feishu_meeting clue → M3 → M5` 流水线；M5 确实形成会议总结时，在现有开放
`enrichments` 中写入 `kind=meeting_summary` 的完整自然语言正文。

`internal/insight/MeetingReviewService` 只读投影已有事实：按自然日读取会议线索，通过
`source_message_ids` 关联 Todo/Task，再从 ExecutionRun 中读取最近一次明确标记的
`meeting_summary` 和同轮 `effects`。`GET /api/review/meetings?date=YYYY-MM-DD`
供前端按会议横向展示；没有会议时返回空列表，没有明确总结产物时正文为空，不使用
Task 进度或普通 run summary 猜测会议总结。

## 6. 待观察 / 后续增强

- 非关键群总结（本期不做，放开即把群集合从 `is_key_group` 换成 `related_group`）。
- codex 调用需要自跑多条 CLI，分钟级；个人 cron 与两类手动触发都靠异步+轮询承接。
- 文档编辑口径的能力上限（见 2.1）。
- 19:00 之后的当天活动不计入自动生成，靠手动重算补。

## 7. 实现状态（2026-07-25）

本期设计已实现：

- 后端已完成个人定时/手动统一生成入口、原子防重、启动补跑、重启恢复；个人总结改为一个顶层 Skill agent 驱动，Go 只保留日期、身份、运行新鲜度、覆盖状态等硬边界。
- `summarize-person-day` Skill 已打包自然日边界、三类来源、`lark-cli`/`bytedcli`
  能力地图、两条一次性并行取证、当日数据统计、逐场会议、双向消息协作、项目进展与
  开放式洞察。
- 个人日报已按 `data/personal-daily/YYYY-MM-DD/` 持久化紧凑上下文、三类证据和正式稿；
  数据库只作 API/UI 状态与摘要缓存。
- 已新增 `feishu-group-daily-summary` Skill；群总结由 Codex 拉全群消息、展开关键线程并按需读取文档、commit/MR 和其他材料。
- 前端「进度」页已完成安全 Markdown 展示、立即生成/重新生成/重试、后台静默轮询、触发类型、证据截止时间和各来源状态展示；旧数量统计保留为辅助 Tab。
- 配置已默认启用每日 19:00 个人总结调度；日报 runner 的硬上限为 600 秒，个人与群
  总结都使用 `danger-full-access` 官方 Codex，群总结仍只手动触发。

验收要求：`go test ./...`、`npm run typecheck`、`npm test`、`npm run build`、Skill 官方校验与脚本语法检查全部通过；本机必须通过 `./scripts/rebuild-server.sh` 重建并重启，再真实验证 `/healthz`、`/api/daily-digests`、Progress 页面和一次个人 Markdown 生成链路。
