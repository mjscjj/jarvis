# M5 执行环节

> Status: current
> Authority: normative module guide
> Last verified: 2026-08-02 @ `89fa24b`
> Code source: `internal/execute/`, `internal/taskcreate/`, `internal/scheduledtask/`

执行环节接管 `pending` Task：调查真实状态、确定目标和动作、判断具体副作用是否要审批，并把事项推进到真实结果、等待或明确阻塞。

## 1. Task 来源与上下文

Task 可来自：

- `todo`：M5 判断 `ready` 后创建；
- `scheduled_task`：到期物化；
- `manual`：通过 API 创建。

`execution_mode=direct` 只是持久化元数据，不绕过审批。

执行 prompt 包含：完整 `source_clue`、判断方向 `plan`、判断上下文 `decision_payload`、冻结 `background`、execution supplements、最近 5 次 runs、shared memory、rules、Skills、工具目录和审批政策。

上游语义是 clue/direction，不是最终契约。当前代码尚无通用接口更新 `background/plan/decision_payload`；能更新的是 supplements、状态、结果和 Task summary。目标设计中的“可变且留痕”仍是实现缺口。

## 2. 执行 phases

| Phase | 作用 |
|---|---|
| `execute` | 首轮调查、决定并执行，或提出审批/等待/人工问题 |
| `apply` | 忠实落地已批准 proposal，不重新拟稿 |
| `resume_waiting` | 等待到期后续跑同一 Session |
| `resume_human` | principal 回复后续跑同一 Session |

apply 阶段如果发现另一个未获批副作用，仍可再次进入审批；不能把一次批准扩大成所有后续动作的授权。

## 3. Outcome 与 Task 状态

```text
pending -> executing
  completed               -> done
  observing               -> observing（有来源 Todo 时同步回 observing）
  waiting                 -> waiting -> resume_waiting -> executing
  needs_human             -> needs_human -> resume_human -> executing
  needs_approval=true     -> awaiting_approval -> apply -> executing
  failed / 运行错误        -> failed
```

`observing` 表示调查后确认事项真实但当前不需要任何人行动，不是完成也不是失败。

## 4. 审批

所有 action_type 走同一执行入口。Agent 根据 [`conf/prompts/m5-approval-policy.md`](../../conf/prompts/m5-approval-policy.md) 判断即将发生的具体副作用：

- 不需要审批：继续执行并核验；
- 需要审批：不执行该副作用，返回 `needs_approval=true` 和完整 proposal；
- principal 批准：启动 fresh apply run，忠实落地已审阅 artifact；
- apply 失败：可以 reapply 同一已批准 proposal。

代码只提供状态、批准/驳回入口、事件和运行审计，不按 `action_type` 决定风险。

## 5. 等待与人工回复

长等待由 Agent 调 `jarvis-tools yield-until` 创建绑定 ScheduledTask。Task 保存等待来源 run 和 Codex Session；到期后调 `exec resume`，不是从头重跑。

`needs_human` 也保存 Session。principal 回复写入 execution supplements 后，从暂停点继续，回答问题不自动等于批准副作用。

## 6. 进度、运行与 effects

- Task `summary` / `last_progress_at`：整个事项目前进展；summary 未变化时不伪造“有新进展”。
- ExecutionRun `summary`：本次运行做了什么。
- ExecutionRun 还记录 stage、sandbox、prompt、session、output、effects、repo、时间与错误。
- effects 使用严格外壳 `kind/title/url/target/preview/extra`，其中 `kind` 开放；未知 kind 保留。它是 Agent 声明，不是独立 verifier 的 receipt。
- 代码分支、commit、push、MR 等交付结果写进 effects，不再有专用 Git 列或 Go 编排。

factengine 当前只从 `message` 蒸馏 Fact，不会自动把 Task summary/ExecutionRun 变成长期 Fact。

## 7. 接口与运维

Task 相关接口包括：runs、events、output、execute、interrupt、rerun、reapply、resume、approve、reject、finish、supplement 和 message recall。完整分组见 [HTTP API](../reference/http-api.md)。

实时推进由 `pipeline.Coordinator` 触发；`execute.schedule` 恢复漏通知和过期 `executing`。配置在 `execute.*`，运行时 overlay 保存后需重启。

重建服务前必须确认没有活跃 Task，见 [运行与部署](../reference/operations.md)。

## 8. 当前缺口

- 没有独立 Verifier；`done` 仍主要来自同一执行 Agent 的 completion claim。
- effects 未对外部系统 receipt 做独立核验。
- Task 三个可变语义字段缺更新 API/tool 与事件留痕。
- Task/ExecutionRun 尚未接入 factengine 自动沉淀。
