# 宽松语义结构与阶段解耦方案

> Status: partially implemented
> Authority: non-normative
> Last verified: 2026-08-02 @ `89fa24b`
> Warning: M3 Candidate 已收缩为严格小外壳 + 不解析的 payload；ContextSnapshot v1、execution enrichment 和部分 Structured Output 仍是严格结构。本文的 ContextDocument 示例不是当前协议。

Jarvis 的 M3 与 M5 执行不应共享一套庞大的模型语义 DTO。程序只固定硬消费字段，模型语义用自然语言或宽松 JSON 原样传递。这样上游新增一段判断、证据或结果时，下游能直接带给模型，不需要同步改 Go struct、JSON Schema、前端类型和历史数据。

当前落地状态（2026-08-03）：M3 Candidate 已是机器消费小外壳 + 文本 payload，Task 用一个宽松 `source_payload` 保存任意来源的完整原始语义；Todo 来源直接固化原始 extraction result，M5 结合它与冻结上下文判断。`repo_path` 只保存调用方明确选定的执行工作副本，不从项目仓库列表猜测；未指定时 M5 继承 Jarvis 当前工作目录。但 execution enrichment、ContextSnapshot v1 和部分 Structured Output 仍是严格结构。以 current 模块文档和代码为准。

## 目标

1. 阶段之间只共享 ID、状态、版本、关联关系、幂等键、调度、审批、执行参数等硬控制字段。
2. 模型生成或消费的目标、背景、计划、原因、证据、风险、过程和结果，统一放在宽松 `payload`。
3. 保留 `kind + label`。它是语义块的轻量约定，不是封闭类型系统。
4. 每次观察得到的上下文按 revision 冻结。下游可以追加新信息，但不要重建或改写已经作为决策依据的上游语义。

## 开放语义块约定

阶段之间不共享一个必须同步升级的公共大 DTO。需要表达多段语义时，各阶段可以采用下面这个小约定：

```json
{
  "summary": "给人和模型快速读的一段总结",
  "blocks": [
    {
      "kind": "risk",
      "label": "外部发送风险",
      "content": {
        "level": "medium",
        "basis": "需要真实发送飞书消息"
      }
    }
  ]
}
```

- `kind` 是自由字符串，用于 UI 或提示词做轻量分组。可以有推荐值，但不做全局封闭枚举。
- `label` 是给人看的短标题，应稳定保留。
- `content` 是任意非 `null` JSON。可以是字符串、对象、数组、数字或布尔值。
- 未知 `kind` 必须原样展示或传递，不能丢弃。
- 阶段可以直接使用一段自然语言或宽松 JSON；没有多块内容时，不要求为了形式统一而包装成 `blocks`。
- 每个阶段只解释自己硬消费的字段。即使碰巧都使用 `kind + label + content`，也不导出一套跨阶段 Go DTO。

推荐 `kind`：

- `context`：背景、结论、说明。
- `evidence`：证据、链接、消息、文件。
- `risk`：风险与注意事项。
- `clarification`：需要人补充或拍板的问题。
- `plan`：执行方案片段。
- `doc_link` / `code_link` / `commit_digest`：执行产物和引用。

## M3 抽取

硬字段：

- `action_type`：开放的动作标签，并参与去重身份。
- `title`：列表展示需要。
- `target`：去重和 action hash 需要。
- `source_message_ids` / `source_quote`：证据完整性需要。
- `status`：决定是否机械物化为 Task。
- `project_hint`：仅用于项目 code/name 精确解析，无法确定时为 null。

宽松语义：

- 最终结果、当前状态与阻塞；
- 背景、链接、待决问题；
- 交办人、期限、承诺强度和其他推断依据。

代码只会从引用消息的真实发送者机械推导 assigner；模型推断出的“实际交办人”放进 payload，不覆盖来源身份。

目标输出：

```json
{
  "candidates": [
    {
      "action_type": "code_change",
      "status": "extracted",
      "title": "修复工具执行报错",
      "target": "jarvis tool 执行报错",
      "project_hint": "jarvis",
      "source_message_ids": ["om_xxx"],
      "source_quote": "修复工具的执行报错",
      "payload": "用户要求修复 jarvis tool 执行失败，并去掉工具次数上限；完成后相关测试通过。"
    }
  ]
}
```

M3 的 `payload` 当前定义为非空文本，自然语言或 JSON 文本均可；Jarvis 不校验内部 JSON，也不为 payload 建 schema，原样保存并传给 M5。

## ContextDocument

当前 `context_snapshot` 不应长期维持为大公共 DTO。目标是收缩为 ContextDocument：

```json
{
  "snapshot_version": "v2",
  "captured_at": "2026-07-24T10:00:00Z",
  "summary": "这条任务来自 Jarvis 项目群，涉及当前机器上的 Jarvis 仓库。",
  "blocks": [
    {"kind": "principal", "label": "我", "content": {}},
    {"kind": "project", "label": "Jarvis", "content": {}},
    {"kind": "conversation", "label": "相关消息", "content": []},
    {"kind": "memory", "label": "相关记忆", "content": []}
  ]
}
```

冻结与修订原则：

- 一次 M3 observation 产生一个不可变 ContextDocument，并记录 revision / observation id。
- Todo 可以继续接收新证据，但新增证据写入 append-only supplement/event，不修改旧 ContextDocument。
- 每次重新抽取明确引用它实际使用的 observation 与 supplements。
- Task 创建时冻结 M3 使用的 ContextDocument，并把完整 extraction result 写入 `source_payload`；M5 只消费 Task 自带背景，不回查并重建 Todo 当前状态。
- M5 执行默认把这些语义整体传给模型，不解析内部业务字段。

## Todo 到 Task 固化

固化步骤不调用模型，也不生成新的语义内容，只保证机器边界：

- `status=extracted` 的 Todo 通过 version CAS 更新为 `materialized`。
- 同一事务写入 append-only `TodoEvent` 并调用 Task Factory。
- Task 继续受 `todo_id` 唯一键约束；同一旧通知重复到达时返回已有 Task。
- `context_snapshot` 原样固化为 `background`，`extraction_result` 原样固化为 `source_payload`。
- 不生成中间 `plan` 或来源专用字段，交给 M5 执行自行判断。

## M5 执行

硬字段：

- `outcome`: `completed | observing | waiting | needs_human | failed`
- `waiting`: 只有等待唤醒时使用，必须严格。
- `proposal`: 只有外部写入审批时使用，`action/target/artifact` 必须严格。

`awaiting_approval` 是 Task 状态，不是 Codex `outcome`；它由执行结果中的 `needs_approval=true` 和合法 proposal 推导。

宽松字段：

- `summary`
- `failure_reason`
- `needs_followup`
- `enrichments`
- 执行证据、链接、产物、风险、补充说明

第一步落地先保持现有外壳，只把 `enrichments` 改为开放语义块：

```json
{
  "outcome": "completed",
  "summary": "已完成修改并通过测试。",
  "failure_reason": "",
  "needs_followup": "",
  "enrichments": [
    {
      "kind": "code_link",
      "label": "核心修改",
      "content": {
        "path": "internal/execute/prompt.go",
        "note": "enrichment content 支持任意 JSON"
      }
    }
  ],
  "waiting": null
}
```

## 存储边界

Todo 保留：

- `id/title/action_type/target`
- `group_id/project_id`
- `status`
- `dedup_fingerprint/version/timestamps`
- `source evidence`
- `source_payload/background/execution_result` 宽松 JSON

Task 保留：

- `id/todo_id/title/action_type/target`
- `source_type/source_id/occurrence_key`
- `status/version`
- `project_id`
- `repo_path`
- `background/source_payload/execution_result` 宽松 JSON

所有来源统一提供非 `null` 的 `source_payload`；执行提示词完整透传，不假设内部固定字段，也不人为生成占位计划。

`repo_path` 是可选的明确执行参数；运行时只校验已指定目录是否为 Git working copy，不反解析 background，也不从项目仓库列表增加默认值。

TodoEvent / TaskEvent：

- 事件只记录状态变化、actor、时间和宽松 detail。
- 状态事件不重复复制完整 context snapshot。
- Observation、Proposal 等模型产物必须落在不可变 artifact/event 中并保存该次完整 payload。
- 工具调用 trace 单独保存为运行观测数据，不混入语义 payload；需要给后续模型时，以 `evidence` block 投影必要结论和引用。

## 实施顺序

1. M5 `enrichments`: `detail string` 改为 `content` 任意非 `null` JSON，前端通用渲染未知内容。
2. M3 Candidate 改为硬字段 + payload；严格 Structured Output 阶段先用 JSON 文本承载开放 payload。
3. ContextSnapshot v2 改为按 observation/revision 冻结的 ContextDocument，supplements 只追加。
4. ~~Task 增加 `repo_path`，移除 M5 对 `contextsnap` 的依赖。~~ 已完成。
5. 清理普通 TodoEvent 的完整 snapshot 复制，保留不可变模型 artifact。历史数据是否迁移另行确认。

## 验收点

- 未知 `kind` 能从 M5 输出落库并在前端展示。
- `content` 为字符串、对象、数组时都能展示。
- 未知 `kind` 和 payload 新增字段不会导致解析失败。
- 只有硬控制字段失败才 fail-fast；语义字段新增不需要改 Go struct。
- M5 不因为新增 enrichment 字段而拒绝模型输出。
- 同一个 Todo 只能创建一个 Task；任一步失败不会留下 Todo 已 materialized 但 Task 不存在的提交状态。
- 有新 supplement 时旧物化通知不会覆盖新 Todo revision。
- 后续 M3 改造时，新增 payload block 不要求 Task 或执行环节同步改类型。
