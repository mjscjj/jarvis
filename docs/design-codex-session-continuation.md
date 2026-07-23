# Codex Session 挂起与定时续跑

## 1. 结论

Jarvis 的长期任务不靠一个 Codex 进程持续 `sleep`，也不把任务摘要交给另一个 Agent。Agent 暂时无法继续时，创建一次性定时触发并结束当前 Turn；到期后使用 `codex exec resume <session_id>` 恢复原 Codex Session。

```text
Task #123
  ├─ ExecutionRun #1 ─ Codex Session A ─ yield-until ─ waiting
  ├─ ScheduledTask ─ 到期只负责唤醒
  └─ ExecutionRun #2 ─ Codex Session A ─ 继续执行
```

`ExecutionRun` 表示一次进程级执行，Codex Session 表示跨 Run 的连续上下文。一个 Task 可以有多次 Run，但主动等待前后的 `codex_session_id` 必须相同。

## 2. 边界

- `Task` 保存用户真正要完成的目标。
- `ExecutionRun` 保存一次 Codex Turn 的输入、Session ID、结果和耗时。
- `ScheduledTask` 只保存未来触发，不重新解释或复制业务任务。
- Codex Session 保存完整对话和工具调用历史。
- 文件、外部系统状态和数据库仍是事实来源；Session 不等于冻结操作系统进程。

秒级、必须在当前命令内观察的等待可以短暂 poll。分钟级及以上等待必须调用 `yield-until`，释放当前 Codex 进程。

## 3. Agent 工具

M5 Runner 注入：

```text
JARVIS_TASK_ID=<current task id>
```

Agent 调用：

```bash
jarvis-tools yield-until \
  --at '2026-07-23T16:30:00+08:00' \
  --reason '会议仍在进行，届时重新检查会议状态和妙记'
```

工具完成以下操作：

1. 校验当前 Task 存在且状态为 `executing`。
2. 创建 `dispatch_kind=resume_task` 的一次性 ScheduledTask。
3. ScheduledTask 先进入 `binding`，此时调度器不能抢占。
4. 返回 `scheduled_task_id/wake_at/reason`。

Agent 调用成功后停止继续操作，并返回：

```json
{
  "outcome": "waiting",
  "summary": "会议尚未结束，已预约稍后继续检查",
  "failure_reason": "",
  "needs_followup": "",
  "enrichments": [],
  "waiting": {
    "scheduled_task_id": 789,
    "wake_at": "2026-07-23T16:30:00+08:00",
    "reason": "会议仍在进行"
  }
}
```

## 4. 绑定 Session

Task 执行不再使用 `codex exec --ephemeral`。Codex CLI 输出的 `thread.started.thread_id` 保存到 `ExecutionRun.codex_session_id`。

Agent 正常结束本轮后，M5 按顺序：

1. 保存 `ExecutionRun(status=waiting, codex_session_id=...)`。
2. 校验 Agent 返回的 ScheduledTask 属于当前 Task。
3. 把 ScheduledTask 的 `source_run_id` 绑定到本次 ExecutionRun。
4. 将 ScheduledTask 从 `binding` 改为 `active`。
5. 将 Task 从 `executing` 改为 `waiting`。

`binding` 消除了“定时器先到期、Session 尚未落库”的竞争窗口。绑定失败直接报错，不创建新 Session 兜底。

## 5. 到期恢复

调度器仍按 `(enabled, status, next_run_at)` 抢占。`dispatch_kind` 决定到期动作：

- `create_task`：现有行为，为一次独立计划物化新 Task。
- `resume_task`：恢复已有 Task，不创建新 Task。

恢复步骤：

1. 校验 ScheduledTask 已绑定 `subject_id` 和 `source_run_id`。
2. 校验源 ExecutionRun 为 `waiting` 且持有非空 Session ID。
3. 乐观更新 Task：`waiting → executing`。
4. 后台执行：

```bash
codex exec resume <session_id> --json ... -
```

5. 新建另一条 ExecutionRun，但继续使用同一个 Session ID。
6. Codex 完成、再次等待、需要人工或失败时，继续走统一结果协议。

调度器只负责成功启动恢复过程，不等待长时间的模型执行。

## 6. 状态

Task：

```text
pending → executing → done
                    → failed
                    → awaiting_approval
                    → waiting → executing
```

ExecutionRun：

```text
running → succeeded
        → waiting
        → failed
```

Agent 结果：

| `outcome` | 含义 |
|---|---|
| `completed` | 目标已经真实完成并验证 |
| `waiting` | 已成功创建未来唤醒 |
| `needs_human` | 必须等待人工批准或补充 |
| `failed` | 当前目标确定失败 |

`outcome=waiting` 必须携带有效的 `scheduled_task_id/wake_at/reason`。只有 Codex 进程正常退出但没有完成目标时，不能把非空文本当成完成。

## 7. 幂等与失败

- 一个源 ExecutionRun 最多绑定一个续接 ScheduledTask，数据库唯一约束使用 `source_run_id`。
- ScheduledTask 重复扫描由 `active → running` 的条件更新拦截。
- Task 重复恢复由 `waiting → executing` 的版本条件拦截。
- Jarvis 在等待期间重启：`active` ScheduledTask 仍由 MySQL 恢复。
- 到期时 Session 缺失：本轮触发失败并明确记录，不新建 Session。
- 同一 Task 同时只允许一个未完成的续接计划。
- Agent 调用工具后没有返回合法 `waiting` 结果：ScheduledTask 不会误触发；Task 进入完成、失败或审批状态时，该 `binding` 计划会被明确关闭并记录原因。若进程在绑定前退出，启动恢复也会将它标记失败。
- `code_change` 等待前会持久化 base branch 和工作 branch；恢复时若仓库已被切到其他分支则直接失败，仍在原分支则继续完成 commit、diff、push/MR。
- 外部条件仍不满足：同一 Session 可以再次调用 `yield-until`。
- Agent 判断目标不再可做：返回 `failed` 或 `needs_human`，不无限重试。

## 8. 会议纪要验收

1. Task 要求会议结束后生成纪要。
2. 首次 Run 发现会议仍在进行，调用 `yield-until`。
3. Task 进入 `waiting`，当前 Codex 进程退出。
4. 到期后产生第二条 ExecutionRun。
5. 两条 Run 的 `codex_session_id` 相同。
6. 妙记仍未生成时再次等待。
7. 妙记生成后整理纪要并验证产物。
8. 只有最后一次 Run 将 Task 标记为 `done`。
