# 长任务 Goal Control

> Status: proposal
> Authority: non-normative
> Last reviewed: 2026-09-11

## 问题

当前 Task 已支持冻结原始目标、跨 Run summary、等待/人工回答后续跑同一 Session，以及按需读取历史。对于持续数天、包含多个独立里程碑的任务，仍缺少一份机器可读取的“当前目标版本和剩余工作”。

## 已有基础

- 原始目标保存在 `Task.source_payload`，不由下游改写。
- M5 可以更新 Task 的当前目标、执行指示、summary 和 supplements。
- ExecutionRun、TaskEvent 和 effects 保存过程与后果。
- `waiting` / `needs_human` 恢复同一个 Agent Session。

这些能力继续保留，不为 Goal Control 新建第二套执行流水线。

## 待验证的最小增量

只有真实运行证明 Task summary 无法稳定承载长任务进度时，再考虑增加：

```text
Goal
  task_id
  objective       # 当前目标，自然语言
  status          # 最小控制状态
  version
  content         # 当前计划、子目标、调整依据的宽松 JSON
```

候选能力：

1. M5 在执行中显式修订当前 objective；
2. 每次修订按 version 留痕；
3. 新 Run 默认读取当前 Goal 与原始 Task 来源；
4. Verifier 仅在发现稳定质量问题后再单独论证，不随 Goal Store 一起建设。

## 非目标

- 不建设多 Agent 编排平台、Goal Tree DSL 或复杂状态机；
- 不把计划步骤变成 runtime 强制流程；
- 不用 Goal 覆盖原始委托证据；
- 不为每个 Task 默认创建 Goal；
- 不把审批做成 Goal 阶段。

## 启动条件

实施前必须先提供至少三类失败样本，证明现有 `source_payload + summary + supplements + run history` 无法恢复目标，并说明新增字段的具体程序消费点。没有这组证据时维持现状。
