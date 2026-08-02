# 定时编排统一进入 Task 执行：MVP 方案

> Status: implemented-history
> Authority: non-normative archive
> Last verified: 2026-08-02 @ `89fa24b`

> 历史设计：本文记录 2026-07-23 定时任务统一进入 Task/M5 的实施方案。功能已落地，模块命名保留当时状态；当前架构以 `README.md`、`docs/00-overview.md` 和 `docs/modules/` 为准。

## 1. 结论

Jarvis 保留 `ScheduledTask` 作为时间触发器，把实际工作统一交给 Task/M5。定时任务到点后创建一条独立 Task，并通过现有 Pipeline 唤醒 M5；它不再自己拼 Prompt、调用 Codex 或判断执行成败。

本次只补齐统一入口所需的最小边界，不建设插件化执行框架：

- 新增统一 Task Factory，供 M4、定时任务和手动 API 创建 Task。
- Task 支持非 Todo 来源，并记录来源和触发批次。
- Task 增加 `standard/direct` 两种执行模式。
- 定时任务每次触发创建独立 Task，随后立即入队。
- M5 继续复用现有 Store、Pipeline、CodexRunner、Prompt、审批和执行历史。

## 2. 当前问题

当前定时任务拥有独立执行链路：

```text
到期扫描 → ScheduledTask 抢占 → 自己拼 Prompt → Codex.RunTextSandbox
        → Codex 返回非空文本即记 done
```

这条链路绕过 Task 状态机、`ExecutionRun`、`TaskEvent`、结构化成功判定、Skills、工作规则和执行补充。Codex 即使返回“任务失败”，定时任务仍可能记成 `done`。

Task/M5 已具备所需执行能力，但入口受两个限制：

- `task.todo_id` 必填，非 Todo 来源不能直接创建 Task。
- 除 `code_change` 外，M5 默认先跑 propose；已经在创建定时任务时获批的动作会重复审批。

## 3. MVP 目标流程

```text
M4 确认 ───────────────┐
手动 POST /api/tasks ──┼─→ Task Factory → Task(pending) → Pipeline.TaskReady
ScheduledTask 到点 ────┘                              │
                                                      ▼
                                            M5 AgentExecutor
                                                      │
                              TaskEvent + ExecutionRun + done/failed
```

职责保持单一：

- M4 判断是否应该做，并固化普通 Task。
- ScheduledTask 只判断是否到点，并物化一次执行。
- Task Factory 校验并创建标准 Task。
- M5 按 Task 中已经确定的执行模式执行。

## 4. 数据模型

### 4.1 Task

在现有 Task 上增加：

| 字段 | 含义 |
|---|---|
| `todo_id` | 改为可空；飞书线索任务仍关联 Todo |
| `target` | 任务目标；参与 `action_hash` 校验 |
| `source_type` | `todo/scheduled_task/manual` |
| `source_id` | 来源记录 ID |
| `occurrence_key` | 一次触发的唯一标识 |
| `execution_mode` | `standard/direct`；两者都不能绕过副作用审批 |
| `approval_ref` | 已批准方案的来源标识；当前不作为免审批凭据 |

`standard` 和历史 `direct` 都使用相同安全边界：`code_change` 直接执行并以 MR 作为审核门，其他类型先 propose。只有纯只读任务可在 propose 阶段完成；本地文件或外部对象的任何修改都必须等待批准。

定时任务使用 `(source_type, source_id, occurrence_key)` 唯一键防止同一轮重复创建 Task。

### 4.2 ScheduledTask

保留现有周期、标题、指令和背景字段，新增：

| 字段 | 含义 |
|---|---|
| `action_type` | 生成 Task 时使用；默认 `agent_task` |
| `last_task_id` | 最近一次触发生成的 Task |

`last_run_status` 从“执行结果”改为“触发结果”：成功表示 Task 已创建并入队，实际执行结果以 `last_task_id` 指向的 Task 为准。MVP 暂不重命名数据库列，避免无价值迁移。

## 5. 统一 Task Factory

Factory 接收已确定的任务，不做业务决策：

```go
type CreateInput struct {
    TodoID         *uint64
    Title          string
    ActionType     string
    Target         string
    Background     json.RawMessage
    Plan           json.RawMessage
    ConfirmedBy    string
    ProjectID      *uint64
    SourceType     string
    SourceID       *uint64
    OccurrenceKey  *string
    ExecutionMode  string
    ApprovalRef    *string
}
```

Factory 负责：

1. 校验必填字段和 JSON 对象。
2. 校验 `source_type`、`execution_mode`。
3. 计算 `action_hash`。
4. 按来源批次防重。
5. 创建 `Task(status=pending)` 和 `TaskEvent(created)`。

Todo 固化器在同一事务中把 `extracted` 更新为 `auto` 并调用 Factory 建 Task。

## 6. 定时触发

定时任务到点后：

1. 按现有乐观条件抢占 ScheduledTask。
2. 以原 `next_run_at` 生成 `occurrence_key`。
3. 调用 Factory 创建 `execution_mode=standard` 的 Task。
4. 调用 `Pipeline.TaskReady(task_id, version)`。
5. 保存 `last_task_id` 和触发结果。

每轮生成独立 Task。周期任务不复用旧 Task，避免不同日期的状态、补充信息和执行历史混在一起。

服务在 Task 创建后、入队前崩溃不会丢任务：Task 已持久化为 `pending`，现有 M5 补偿扫描会继续执行。

## 7. 通用 API

新增：

```text
POST /api/tasks
```

请求只提交任务内容：

```json
{
  "title": "检查项目状态并生成结论",
  "action_type": "agent_task",
  "target": "Agent Runtime 项目",
  "background": {"snapshot_version": "v1"},
  "plan": {"instruction": "检查最新状态并输出结论"},
  "execution_mode": "standard",
  "project_id": 1
}
```

服务端固定 `source_type=manual`、`confirmed_by=user`，创建后立即入队。调用方不能伪造 Todo 或 ScheduledTask 来源。

## 8. M5 改动

M5 使用统一审批分支：

```text
action_type=code_change
  → runOnce，以 MR 作为审核门

其他 action_type（不区分 standard/direct）
  → propose；纯只读可完成，任何修改进入 approve/apply
```

执行前重算 `action_hash`，不一致时 fail-fast。新增 `agent_task` 作为通用 Agent 任务类型；它使用现有 `danger-full-access` Agent Engine，不增加专用工作流。

M5 使用结构化 outcome 判定。`waiting` 通过同一 Codex Session 的定时续跑继续，详见 [`design-codex-session-continuation.md`](../design-codex-session-continuation.md)。针对会议 ID、消息 ID、文档 revision 等确定性回执的强校验另立需求，本次不建设通用 Verifier 框架。

## 9. 迁移

- 现有 Task 的 `todo_id` 原样保留。
- 从关联 Todo 回填 Task 的 `target/source_type/source_id`；该映射确定，不猜历史语义。
- 现有 Task 使用 `execution_mode=standard`，行为不变。
- 现有 ScheduledTask 保留；后续触发改走 Task。
- 不迁移 ScheduledTask 的旧 `last_result` 到 ExecutionRun。

## 10. 验收

必须覆盖：

1. M4 创建的 Task 行为不变。
2. `POST /api/tasks` 创建后进入 M5。
3. `direct` 非代码任务同样进入 propose，不能绕过副作用审批。
4. ScheduledTask 到点只创建一条 Task，同一 occurrence 不重复创建。
5. ScheduledTask 的 `last_task_id` 可定位真实 Task 结果。
6. Codex 返回 `outcome=failed` 时 Task 进入 `failed`；返回 `outcome=waiting` 时 Task 持久化等待并可恢复原 Session。
7. 现有 Go 测试、前端构建和真实本机服务迁移通过。
