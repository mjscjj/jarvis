---
name: okr-agent-orchestrator
description: 根据 OKR 模块中可编辑的业务 Prompt 和绑定的 Agent 行动，动态组合 Jarvis、飞书、Meego 与 OKR/周报原子工具完成一次目标。用于季度 OKR 草稿、区域或研发对齐、Report A/B/C、周报催填、进展巡检，以及配置行动的时间、范围和对象后由通用 ScheduledTask 触发执行。
module: okr
---

# OKR Agent 动态流程

不要把 Prompt 当成固定步骤清单。每次执行都先基于实时事实理解目标，再选择必要工具、调整调查深度并决定停止位置。

## 读取本次目标

先读取共用原则：

```bash
scripts/okr-agent-tools prompt --key okr_agent_principles
```

从 Task `context_snapshot.prompt_key` 读取本次业务 Prompt：

```bash
scripts/okr-agent-tools prompt --key '<okr_agent_*>'
```

没有 `prompt_key` 时，只能使用用户或 Task 目标中明确指定的 Prompt key；不得按标题猜一个流程。Prompt 缺失或为空时 fail-fast，不改走数据库或旧生成接口。

ScheduledTask 触发时，把 Task instruction 作为本次行动目标，并读取冻结上下文里的 `action_scope` 和 `action_recipient`。它们是自然语言约束，不是固定 workflow 参数：结合实时事实解析实际范围和对象，无法确认时保留缺口，不自行猜测 ID。

## 选择原子工具

按本次目标选择最小工具集合，不要求固定顺序：

- OKR 稳定定义：`scripts/okr-module-tools scope|board`；
- 周报事实：`scripts/weekly-report-tools scope|board`；
- 已确认关系、Message、Clue、Fact、Page 和调度：`jarvis-tools`；
- 飞书文档、表格、消息：先读对应 `lark-*` Skill，再用 `lark-cli`；
- Meego：先用 `bytedcli --json --all-help` 发现当前版本的查询或写入命令。

工具只提供事实或单个副作用。语义匹配、去重、归类、摘要、优先级、on track 判断和下一步由你结合上下文完成。

## 执行边界

- 读取 API 响应后验证成功字段；失败时保留原始错误并停止依赖该事实的动作。
- 先回读再写入；关系和外部对象使用稳定来源 ID，重复执行不得制造副本。
- 候选匹配必须说明证据和不确定性。标题相似不能单独建立关系或创建 Meego。
- 草稿默认留在 Task 结果中。创建文档、发送消息、修改 Meego 或正式 OKR 属于具体副作用，由 M5 按统一审批策略判断，不读取业务 Prompt 中的 `approval` 字段替代判断。
- 不调用固定报告生成器来替代 Agent 判断；只读取原子 board、证据和已保存事实。
- 不创建 OKR 专用 Task 状态机。定时触发只承载本次目标，完成后结束。
- 不根据日期文字决定本次是否应该运行；一次 Task 已经代表调度器确认到点，weekly/daily/interval 由通用 ScheduledTask 保证。

## 完成回执

输出本次 Prompt key、事实范围、实际使用工具、关键判断及依据、产生的副作用与回执、跳过项、待确认项和覆盖缺口。不要用“Task 成功”替代外部送达或写入成功。
