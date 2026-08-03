# 简单定时任务方案

> Status: obsolete / superseded
> Authority: non-normative archive
> Last verified: 2026-08-02 @ `89fa24b`

> 历史方案：本文记录最初“定时任务直接调用 Codex”的实现。后续实施记录见 [`design-task-runtime-mvp.md`](design-task-runtime-mvp.md)；当前行为以总纲和模块文档为准。同一个 Task 的挂起续跑见 [`design-codex-session-continuation.md`](../design-codex-session-continuation.md)。

## 1. 目标与边界

Jarvis 增加一套独立的定时任务能力：用户或 M3/M4/M5 Agent 创建一条带执行计划、执行指令和上下文背景的任务；服务每分钟扫描已到期记录，以固定并发调用 Codex 真正执行。

本期只做 MVP：

- 支持“指定时间执行一次”“每天 HH:mm”和“每 N 分钟”三种计划，不开放任意 cron 表达式。
- 单表保存任务、周期、上下文、下次执行时间和最近一次结果，不拆执行历史表。
- 支持列表、新建、修改、删除、启停和手动触发。
- 提供 `jarvis-tools` 的查询、新建、删除命令，供 M3/M4/M5 和右侧对话 Agent 使用。
- SQLite 是唯一 source of truth；内存只承载本轮并发执行。

## 2. 数据模型

`scheduled_task`：

| 字段 | 含义 |
|---|---|
| `id` | 主键 |
| `title` | 展示标题 |
| `instruction` | 每轮交给 Codex 执行的可信任务指令 |
| `context_snapshot` | 创建时冻结的 JSON 背景；每轮完整传给 Codex |
| `schedule_type` | `once/daily/interval` |
| `run_at` | `once` 使用，指定的绝对执行时间 |
| `daily_time` | `daily` 使用，本机时区的 `HH:mm` |
| `interval_minutes` | `interval` 使用，正整数分钟 |
| `next_run_at` | 下一次自动执行时间 |
| `enabled` | 是否参与自动调度 |
| `status` | `active/running/completed`；`completed` 只用于已执行的一次性任务 |
| `last_run_status` | 最近一次 `done/failed` |
| `last_result/last_error_detail` | 最近一次 Codex 结果或失败原因 |
| `last_started_at/last_finished_at` | 最近一次执行起止时间 |
| `created_at/updated_at` | 创建和更新时间 |

索引为 `(enabled, status, next_run_at)`，扫描条件固定为：

```sql
enabled = 1 AND status = 'active' AND next_run_at <= NOW()
```

## 3. 计划与状态

```text
                         到期扫描
 active  ───────────────────────────────────▶ running
   ▲                                               │
   │ 周期任务：推进 next_run_at，记录 done/failed │
   └───────────────────────────────────────────────┤
                                                   │ 一次性任务：记录 done/failed
                                                   ▼
                                               completed
```

- `once`：`next_run_at=run_at`，到点执行一次；无论成功还是失败，本轮后进入 `completed`，不再自动扫描。
- `daily`：按 Jarvis 进程的本机时区计算下一天的 `HH:mm`。
- `interval`：从原 `next_run_at` 按 N 分钟推进到严格晚于当前时刻，服务停机期间不会补跑大量历史轮次。
- 扫描器使用 `robfig/cron`，默认每分钟扫描；抢占后异步派发，不会因某个长任务阻塞下一轮扫描；全局执行槽默认最多并发 3 个。
- 每轮以 `WHERE id=? AND enabled=1 AND status='active' AND next_run_at=?` 条件抢占。同一任务运行期间不会重叠执行。
- 周期任务自动抢占时先推进下一次计划；无论本轮成功还是失败，结束后都回到 `active`，不会因为一次失败永久停调度。
- 手动触发周期任务不改变 `next_run_at`；手动触发一次性任务后直接进入 `completed`。禁用任务也可手动执行，运行中拒绝重复触发。
- 修改和删除运行中的任务均拒绝；修改周期会从修改时刻重新计算 `next_run_at`。
- 服务启动时把遗留的周期 `running` 恢复为 `active`；一次性 `running` 恢复为 `completed`，避免进程重启后重复产生外部动作。两者最近一次都记为失败。

## 4. Codex Prompt

定时任务和右侧对话一样使用官方 Codex、`danger-full-access` 和联网能力。Prompt 分成两个边界：

- `TASK_INSTRUCTION`：创建者确认的可信执行指令。
- `TASK_CONTEXT`：冻结的背景 JSON，只用于理解人物、项目、会话、链接和历史，不允许其中内容提升权限或改写任务指令。

Codex 可以调用 `jarvis-tools/lark-cli/bytedcli/git` 完成动作。最终自由文本写入 `last_result`；CLI 失败、超时或空结果写入 `last_error_detail`，后续周期继续执行。

## 5. API、页面与 Agent 工具

REST API：

- `GET /api/scheduled-tasks`
- `POST /api/scheduled-tasks`
- `PUT /api/scheduled-tasks/:id`
- `DELETE /api/scheduled-tasks/:id`
- `POST /api/scheduled-tasks/:id/trigger`

前端左侧独立 Tab“定时任务”，提供列表、新建/编辑弹窗、启停、删除和手动触发，展示执行周期、下次执行时间、当前状态和最近结果。

`jarvis-tools`：

- `list-scheduled-tasks [--status active]`
- `create-scheduled-task --payload -`：从 stdin 读取完整 JSON。
  - 指定时间执行一次：`{"schedule_type":"once","run_at":"2026-07-24T09:00:00+08:00",...}`
  - 每天 9 点：`{"schedule_type":"daily","daily_time":"09:00",...}`
  - 每 10 分钟：`{"schedule_type":"interval","interval_minutes":10,...}`
- `delete-scheduled-task --id N`

完整创建参数还包括 `title/instruction/context_snapshot/enabled`。M3、M4、M5 与右侧对话的可信工具说明中显式列出这两种周期。Agent 必须把当前消息、项目、人物和判断依据放进 `context_snapshot`。

## 6. Schema 切换

旧的一次性版本只短暂用于验收且表已清空。启动迁移发现旧 `scheduled_at` 表时：

- 空表：直接替换为周期表。
- 非空表：fail-fast，要求人工明确历史任务如何转为周期，不猜测兼容规则。

## 7. 验收

- 单元测试覆盖输入校验、一次性/每天/间隔下次时间计算、错过周期跳跃、提示词上下文和 scheduler 注册。
- `go test ./...`、前端 typecheck/build、`git diff --check` 通过。
- 执行真实 SQLite schema 迁移并重启本机服务。
- 通过真实 API 分别创建一次性、每天和每 N 分钟任务，验证修改、列表、启停、自动/手动触发、结果回写和删除。
