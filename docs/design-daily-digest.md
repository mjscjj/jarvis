# 每日进度总结（Daily Digest）设计

把「进度」页从现在的**任务数量播报**（按天 Count Todo/Task/消息，再把数字翻译成一段话）升级为**内容层面的每日进度总结**：对「我」个人和「关键群」各自，按自然日生成一段可读的进度摘要。

完整背景见 `[README.md](../README.md)` 与 `[docs/00-overview.md](00-overview.md)`。本文只覆盖每日总结这一功能。

## 0. 目标与范围

- **只做两类总结**：
  - **个人（我）每日进度**：我这一天推进了什么——不止「我发的消息」，而是尽量还原「我今天做的所有事」（消息、完成的 Task、编辑的文档、会议、代码 MR/commit）。
  - **关键群每日进度**：仅对 `is_key_group=1` 的群，总结该群当天讨论/推进了什么。
- **时间粒度**：自然日（配置时区 `Asia/Shanghai` 的 00:00–24:00），复用现有 digest 的日历日 bucket。
- **触发**：每晚 **19:00** cron 自动生成当天；页面也可**手动触发/重算**。19:00 生成的是「截至生成时刻」的当天进度，摘要里标注生成时间；手动重算可刷新到最新。
- **不做**（本期）：非关键群的群总结、多用户、历史版本留存（重算直接覆盖）。

## 1. 两类总结用不同引擎（关键决策）

| 维度 | 引擎 | 理由 |
|---|---|---|
| **个人进度** | **codex agent**（`execute` 段的 codex，`danger-full-access`+联网） | 需要自跑 `lark-cli` / `bytedcli` / `git` 实时去捞「我今天做的事」，是 agent 活，qwen 单次调用做不到 |
| **关键群进度** | **qwen model API 单次调用**（`model` 段 `qwen-plus`） | 群消息内容已在 `message` 表，只是「把当天 N 条消息归纳成一段话」，纯文本归纳，轻量便宜 |

## 2. 数据源

### 2.1 个人进度（codex agent 收集）

Jarvis 先把**已有且可靠**的数据喂进 prompt 打底，再让 agent 自跑工具补充外部数据：

**打底（Jarvis 直接查库，快而准）**：
- 我发的消息：`message` 表 `WHERE sender_open_id = <principal> AND create_time ∈ [day)`。
- 我完成的 Task：`task` 表当天 `done/failed` 的记录（比飞书 task 准）。

**agent 自跑工具补充（prompt 里给命令引导，agent 用 `execute` 的 danger-full-access 跑）**：
- **我编辑的文档**：`lark-cli drive +search --mine --sort edit_time --as user`，再按 `result_meta.update_time_iso` 过滤当天、`edit_user_id` 标注是否本人。
  - 已知能力上限：`--mine` 是「我拥有」，`edit_user_id` 是「最后编辑人」，故「我改过但归属他人 / 我改后他人又改」的文档会漏或算到他人名下。飞书无「编辑历史含我」的精确接口，接受该口径。（服务端 `my_edit_time` 过滤在本租户失效，不用。）
- **我的日历/会议**：`lark-cli calendar +agenda --start <day> --end <day>`。
- **我参与/组织的会议、我拥有的妙记**：`lark-cli vc +search --participant-ids <me> --start --end`、`lark-cli minutes +search --owner-ids me --start --end`。
- **我的 MR（跨仓库）**：`bytedcli --json codebase search mr --author @me --updated-since <t> --updated-until <t>`。
- **本地仓库 commit**：对已 clone 仓库 `git log --author=chujiejie.1 --since --until`（仅本地有的仓库）。

**不承诺**：我发起的审批（无跨定义按天查）；跨所有远端仓库的全量 commit（无全局按天接口）。

### 2.2 关键群进度（Jarvis 查库 + qwen 总结）

- 群集合：`feishu_group` 表 `is_key_group = 1`。
- 每个关键群当天消息：`WHERE group_id = ? AND create_time ∈ [day)`，复用 `internal/extract/tools/query_chat_history` 式查询（升序、正文按条截断）。
- 消息量上限：每群每天最多喂 N 条（默认 200）、每条截断到约 800 字，超出丢弃并在摘要里说明「消息过多，仅总结前 N 条」。

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
| `source_count` | int | 纳入的活动/消息条数，便于展示与判空 |
| `engine` | varchar(16) | `codex` / `qwen`，便于排查 |
| `error_detail` | text | 失败原因（fail 时） |
| `generated_at` | datetime | 生成完成时刻（19:00 触发时体现「截至此刻」） |
| `created_at` / `updated_at` | datetime | |

- 唯一索引 `uk_scope_date (scope, scope_id, digest_date)`：保证一天一 scope 一行，重算 upsert。
- 注册进 `domain.CoreModels()` 走 AutoMigrate。

## 4. 触发与并发

- **自动**：新增 `internal/dailydigest` 的 cron scheduler，每晚 **19:00** 跑一次（`robfig/cron`，`SkipIfStillRunning`+`Recover`，照 `internal/memory/scheduler.go`）。目标日 = 当天。批量生成：个人 1 条 + 每个关键群各 1 条。
- **手动**：前端按钮触发单条（某 scope 某天）生成/重算，**异步**（同 M5 任务执行模式）：API 立即置 `generating` 返回，后台跑 codex/qwen，前端轮询状态。
- **并发**：个人（codex，重）与群（qwen，轻）可分开；一轮 cron 内群总结可有界并发（默认 2，类比 M5）。个人总结 1 条无需并发。

## 5. 改动范围

**后端（Go）**：
1. `internal/domain/models.go`：加 `DailyDigest` model + `CoreModels()` 注册。
2. 新包 `internal/dailydigest/`：
   - `store.go`：`daily_digest` 读写（get by scope+date、upsert、置状态）。
   - `person.go`：个人总结——查库打底（我的消息+Task）+ 构建 codex prompt（含 lark-cli/bytedcli/git 命令引导）+ 调 codex runner + 落库。
   - `group.go`：关键群总结——查库取当天消息 + qwen 纯文本总结 + 落库。
   - `service.go`：编排（生成单条 / 批量生成当天 / 读取），异步 kick。
   - `scheduler.go`：19:00 cron（照 `memory/scheduler.go`）。
3. `internal/extract/provider/client.go`：加一个不带结构化 schema 的纯文本 `Complete` 方法（底层复用 `postChatCompletion`），供群总结用。
4. `internal/api/`：`GET /api/daily-digests?date=`（读当天全部 scope）、`POST /api/daily-digests/generate`（按 scope+date 异步生成/重算）；`router.go` 注册 + deps 注入。
5. `cmd/jarvis-server/main.go`：构造 dailydigest service（注入 db、location、codex runner、qwen client、principal_open_id、is_key_group 群查询）+ 起 19:00 scheduler + 接进 API deps。
6. `conf/config.yaml` + `internal/config`：加 dailydigest 配置段（schedule 默认 `0 19 * * *`、每群消息上限、群并发度、enable 开关）。

**前端（React/TS）**：
7. `web/src/types.ts`：加 `DailyDigest` 类型。
8. `web/src/api.ts`：加 `getDailyDigests(date)` / `generateDailyDigest(scope, scopeId, date)`。
9. `web/src/Progress.tsx`：改造成「按日期选择 + 我的进度卡片 + 各关键群卡片」，每卡片显示 summary / 生成时间 / 状态，缺失或想刷新时有「生成 / 重算」按钮（异步 + 轮询）。保留或移除旧的数字表由实现时定（倾向保留为辅助小结）。

## 6. 待观察 / 后续增强

- 非关键群总结（本期不做，放开即把群集合从 `is_key_group` 换成 `related_group`）。
- 个人总结的 codex 调用较慢（自跑多条 CLI，分钟级），cron 场景可接受；手动触发靠异步+轮询体验。
- 文档编辑口径的能力上限（见 2.1）。
- 19:00 之后的当天活动不计入自动生成，靠手动重算补。

## 7. 实现状态（2026-07-22）

本期设计已实现：

- 后端已完成 `daily_digest` 模型与迁移、个人 Codex 生成、关键群 Qwen 生成、异步状态机、单条/批量编排、19:00 scheduler，以及 GET/POST API。
- 前端「进度」页已完成按日期的个人/关键群摘要卡片、生成/重算、生成中轮询、失败详情和生成元信息展示；旧数量统计保留为辅助 Tab。
- 配置已默认启用每日 19:00 调度，个人总结使用 `danger-full-access` Codex，关键群总结使用 `qwen-plus`。

验收：`go test ./...`、`npm run build` 通过；本机 launchd 前后端重启后，`/healthz`、`/api/daily-digests` 与 Progress 页面真实加载通过。实际生成会调用 Codex/Qwen 并写入当天摘要，留给页面手动触发或 19:00 cron。
