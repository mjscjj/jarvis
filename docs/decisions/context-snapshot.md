# 冻结上下文与渐进读取

> Status: accepted
> Authority: architecture decision
> Last verified: 2026-09-11

## 决策

上下文在 M3 创建或更新 Todo 时组装一次，保存为：

```text
source + capture + annotation
```

- `source`：原始请求、直接来源消息 ID 和原句；
- `capture`：创建时的完整原文和现场事实；
- `annotation`：M3 的开放自然语言说明。

固化器把 `Todo.content` 原样复制到 `Task.source_payload`。下游可以补充新证据，但不重新查库拼一份“看起来等价”的背景替代冻结快照。

## 理由

1. 原始语义必须可审计，不能被后续状态覆盖；
2. 当前世界会变化，不能把实体最新状态混入创建时证据；
3. 默认 prompt 应保持紧凑，完整背景和历史 Run 只按需读取；
4. 模型语义需要允许扩展，不能提升成跨链路严格 DTO。

## 读取规则

M5 默认看到直接来源、简报、当前 Task 状态、可展开区块和运行概要。完整会话、背景、来源、annotation、相关工作和 Run 正文通过 `jarvis-tools` 按需读取。

实体页与 Fact 提供当前世界和历史证据，不能替代冻结来源。reaction 等机器动作只使用经过校验的来源消息 ID，不从 annotation 猜目标。

## 后果

- `source`、`capture` 和 `annotation` 保持宽松 JSON；
- 同一消息正文只在 capture 中保存一次；
- 未知模型字段完整保留；
- Todo 与 Task 仍是独立生命周期，不能合并；
- 旧上下文格式无法安全推断时 fail-fast，由人工决定迁移或重建。
