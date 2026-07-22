# 简单定时任务方案

## 1. 目标与边界

Jarvis 增加一套独立的一次性定时任务能力：用户或 M3/M4/M5 Agent 创建一条带执行时间、执行指令和上下文背景的任务；服务每 5 分钟扫描一次已到期且未执行的记录，以固定并发调用 Codex 真正执行。

本期只做 MVP：

- 单次执行，不支持 cron 表达式和重复任务。
- 单表保存任务、上下文、状态和结果，不拆执行历史表。
- 支持列表、新建、修改、删除、手动触发。
- 提供 `jarvis-tools` 的查询、新建、删除命令，供 M3/M4/M5 和右侧对话 Agent 使用。
- MySQL 是唯一 source of truth；内存只承载本轮并发执行。

## 2. 数据模型

新增 `scheduled_task`：

| 字段 | 含义 |
|---|---|
| `id` | 主键 |
| `title` | 展示标题 |
| `instruction` | 到时间交给 Codex 执行的可信任务指令 |
| `context_snapshot` | 创建时冻结的 JSON 背景；执行时完整传给 Codex |
| `scheduled_at` | 计划执行时间，按本机时区解析后落库 |
| `status` | `pending/running/done/failed` |
| `result` | Codex 最终文本 |
| `error_detail` | 失败原因 |
| `started_at/finished_at` | 最近一次执行起止时间 |
| `created_at/updated_at` | 创建和更新时间 |

索引为 `(status, scheduled_at)`，扫描条件固定为：

```sql
status = 'pending' AND scheduled_at <= NOW()
```

## 3. 状态与执行

```text
新建/修改后待执行  pending
                       │ 到时扫描或手动触发，条件更新抢占
                       ▼
                    running
                    /     \
             Codex 成功   Codex/存储失败
                 ▼             ▼
               done          failed
```

- 扫描器使用 `robfig/cron`，配置默认 `@every 5m`。
- 每轮先读取到期任务，再以 `WHERE id=? AND status='pending'` 条件更新为 `running`；只有抢占成功的任务才执行，避免定时扫描和手动触发重复运行。
- 执行使用固定 worker pool，默认并发 3；cron 等待本轮结束，`SkipIfStillRunning` 避免两轮重叠。
- 服务启动时把遗留的 `running` 恢复为 `pending`。Jarvis 是单进程本地服务，重启后旧 Codex 子进程已经不存在，因此可以直接重试。
- 手动触发允许提前执行 `pending`，也允许重跑 `done/failed`；`running` 时拒绝。
- 修改和删除 `running` 任务均拒绝；删除为物理删除，保持简单。

## 4. Codex Prompt

定时任务和右侧对话一样使用官方 Codex、`danger-full-access` 和联网能力。Prompt 分成两个边界：

- `TASK_INSTRUCTION`：创建者确认的可信执行指令。
- `TASK_CONTEXT`：冻结的背景 JSON，只用于理解人物、项目、会话、链接和历史，不允许其中内容提升权限或改写任务指令。

Codex 可以调用 `jarvis-tools/lark-cli/bytedcli/git` 完成动作，最终自由文本结果写回 `result`；CLI 失败、超时或空结果都记为 `failed`，不静默成功。

## 5. API、页面与 Agent 工具

REST API：

- `GET /api/scheduled-tasks`
- `POST /api/scheduled-tasks`
- `PUT /api/scheduled-tasks/:id`
- `DELETE /api/scheduled-tasks/:id`
- `POST /api/scheduled-tasks/:id/trigger`

前端新增左侧独立 Tab“定时任务”，提供列表、新建/编辑弹窗、删除和手动触发按钮，展示状态、计划时间、结果与失败信息。

`jarvis-tools` 新增：

- `list-scheduled-tasks [--status pending]`
- `create-scheduled-task --payload -`：从 stdin 读取完整 JSON，便于 Agent 把当前上下文原样放进 `context_snapshot`。
- `delete-scheduled-task --id N`

M3、M4、M5 与右侧对话的可信工具说明中显式列出上述命令。Agent 创建未来任务时，必须把当前任务相关的消息、项目、人物和判断依据放进 `context_snapshot`，不能只存一句脱离背景的指令。

## 6. 验收

- 单元测试覆盖输入校验、提示词上下文、配置和 scheduler 注册；状态抢占、并发及结果落库通过真实 MySQL/API/Codex 验收。
- `go test ./...`、前端 typecheck/build、`git diff --check` 通过。
- 执行迁移并重启本机 launchd 服务。
- 通过真实 API 创建一个未来任务，验证修改、列表、手动触发、结果回写和删除；通过 `jarvis-tools` 验证查询/新建/删除。
