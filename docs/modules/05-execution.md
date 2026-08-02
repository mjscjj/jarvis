# M5 执行环节

> 隶属总纲 [`docs/00-overview.md`](../00-overview.md)。宽松语义契约见 [`docs/design-loose-semantic-contract.md`](../design-loose-semantic-contract.md)。审批尺度见 `conf/prompts/m5-approval-policy.md`。
>
> 本文讲 **执行环节**。M5 内部分两步：判断环节（read-only 定 disposition，见 `04-decision.md`）与执行环节（工具全开落地，本文）。两步共用工作队列与 worker 池，不是两个流水线阶段。
>
> 消费物：`Task`（`status=pending` 等可执行态）。长期事实由离线引擎沉淀；执行环节**不手工记事实、不回写记忆 sidecar**。

---

## 0. 边界

### 0.1 上游（判断环节 → 执行环节）

判断环节 `ready` 后生成 `Task`。执行环节读到的关键内容：

- `background`：M3 冻结的 `context_snapshot`（含项目、消息、事实等），全程复用
- `plan` / `decision_payload`：当时最好的理解，**可变**——执行中可改并 bump `version` + 写 `task_event`
- `source_clue`、`action_type`、`project_id`、关联 Resource

判断已回答「值不值得做」；执行不再重开，但持有对任务内容的修改权。

### 0.2 下游

- 回写 `Task` 状态与 `execution_run` / `task_event`
- 对外副作用原样记入 `effects`（不校验未知类型）
- 可选飞书通知本人
- **不**回写 mem0 / 任何记忆向量库；结论写进 `summary` / `progress_summary`，由 factengine 之后从原料蒸馏

### 0.3 不属于执行环节

- 抽取 Todo（M3）、Todo→Task（判断环节）
- 数据采集（M2）、事实蒸馏（factengine）
- 按 `action_type` 硬编码审批分流（审批归模型，载体是状态机）

---

## 1. 架构

```text
pending Task
    │
    ▼
AgentExecutor（traex / gpt-5.6-sol）
    │  工具：lark-cli / bytedcli / git / jarvis-tools / …
    │
    ├─ completed
    ├─ waiting          → yield-until / scheduled_task 续跑 Session
    ├─ awaiting_approval → 等人批准高风险副作用
    ├─ needs_human       → 只有 principal 能答的问题
    └─ failed
```

实时推进靠 `pipeline.Coordinator`；`execute.schedule` 补偿遗漏与超时 `executing`。

| 文件 | 作用 |
|---|---|
| `internal/execute/agent_executor.go` | 执行 / 挂起 / 恢复 |
| `codex_runner.go` | Codex/traex Session |
| `store.go` | Task 状态机 |
| `decision_*.go` | 判断环节（同包） |

---

## 2. 修改权与审批

对齐 `AGENTS.md` §4：

1. **修改权归模型。** `background` / `plan` / `decision_payload` 可变；每次改必须留痕。
2. **要不要请示由模型判断。** 依据只在 `m5-approval-policy.md`。代码提供 `awaiting_approval`、批准/驳回入口、事件流与 `effects`，不按类型名拦截。
3. **后果必须记录。** 对外写操作进 `effects[]`，不重写、不拒绝未知 `kind`。

提示词（`conf/prompts/m5-system-prompt.md`）要求：`progress_summary` 写成完整结论（供日后阅读与事实蒸馏），**不要**调用 `append-fact`。查已有事实用 `list-facts`。

---

## 3. 等待与续跑

长等待：`jarvis-tools yield-until` → Task `waiting`，持久化 Codex Session；到期由 `scheduled_task` `exec resume` 续跑。独立新动作才建新 scheduled task。

采集类任务把观察到的事实交回流水线：`append-clue`（给 M2），不是记 fact。

调查后发现不需要任何人动手：`set-todo-status observing`，线索留在视野里。

---

## 4. 运维

```bash
./bin/jarvis-server -config conf/config.yaml -decide-once   # 判断环节一轮
# 执行由流水线实时触发；管理后台可手工执行 / 批准 / 重跑
```

配置：`execute.*`（concurrency、timeout、stale_executing_minute、repo_root、runs_dir）。产物落 `runs_dir`。

---

## 5. 开放问题

1. 审批政策正文是否仍贴合实跑风险——据实校准 `m5-approval-policy.md`。
2. Session resume 与输出格式契约在 CLI 能力变化时的适配。
3. 自动 git commit/push 默认关，是否开放由用户定。
