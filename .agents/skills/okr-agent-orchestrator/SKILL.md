---
name: okr-agent-orchestrator
description: 根据 OKR 模块中可编辑的业务 Prompt，动态组合 Jarvis、飞书、Meego 与 OKR/周报原子工具完成固定 Agent 行动或一次性目标。用于季度 OKR 草稿、区域或研发对齐、Report A/B/C、周报催填和进展巡检。
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

ScheduledTask 触发时，`action_key` 只标识产品里的固定行动，`prompt_key` 指向完整业务语义。范围、对象、产出和验收标准全部从 Prompt 读取，不从行动配置拼装第二份语义；无法确认稳定 ID 时保留缺口，不自行猜测。

## 选择原子工具

按本次目标选择最小工具集合，不要求固定顺序：

- OKR 稳定定义只读：`scripts/okr-module-tools scope|board|get-kr|people-search`；图片材料可用 `upload-image` 保存，但不得调用或绕过工具创建、修改、删除 O/KR；
- 周次与周报事实：`scripts/weekly-report-tools scope|weeks|open-week|board`；
- 单条周进展：`create-progress|update-progress|delete-progress`，写入前必须回读 KR 最新 `version`；
- 评论协作：`comments|create-comment|update-comment|delete-comment`；
- Meego 差异：`meego-preview|point-meego-preview|record-meego-observation|confirm-meego-progress`；
- 催填与材料：`reminder-preview|reminder-batches|create-reminder-batch|create-feishu-document`；
- 已确认关系、Message、Clue、Fact、Page 和调度：`jarvis-tools`；
- 飞书文档、表格、消息：先读对应 `lark-*` Skill，再用 `lark-cli`；
- Meego：先用 `bytedcli --json --all-help` 发现当前版本的查询或写入命令。

工具只提供事实或单个副作用。语义匹配、去重、归类、摘要、优先级、on track 判断和下一步由你结合上下文完成。

## 执行边界

- 读取 API 响应后验证成功字段；失败时保留原始错误并停止依赖该事实的动作。
- 先回读再写入；关系和外部对象使用稳定来源 ID，重复执行不得制造副本。
- O/KR 标题、负责人、优先级、指标、拆解、标签和 Meego 绑定是人的稳定定义。即使本次目标要求优化 OKR，Agent 也只输出候选建议，不直接调用 `/api/okr` 写接口或操作模块数据库。
- 周报工具是可组合能力，不是必须按帮助顺序执行的 workflow。是否开周、填写进展、评论、确认 Meego、催填或生成材料，只服从本次 Prompt 和实时事实。
- 创建周进展时使用稳定、可重跑的进展 ID 和 `expected_version=0`；更新和删除必须使用该条进展自己的最新 `expected_version`，不能使用 KR 定义版本。409 后重新读取并重新判断，不机械覆盖。
- 候选匹配必须说明证据和不确定性。标题相似不能单独建立关系或创建 Meego。
- 草稿默认留在 Task 结果中。删除周报内容、创建文档、发送消息或修改 Meego 属于具体副作用，由 M5 按统一审批策略判断，不读取业务 Prompt 中的 `approval` 字段替代判断。O/KR 稳定定义没有 Agent 写工具，审批不能突破这条边界。
- 不调用固定报告生成器来替代 Agent 判断；只读取原子 board、证据和已保存事实。
- 不创建 OKR 专用 Task 状态机。定时触发只承载本次目标，完成后结束。
- 不根据日期文字决定本次是否应该运行；一次 Task 已经代表调度器确认到点，weekly/daily/interval 由通用 ScheduledTask 保证。

## 完成回执

输出本次 Prompt key、事实范围、实际使用工具、关键判断及依据、产生的副作用与回执、跳过项、待确认项和覆盖缺口。不要用“Task 成功”替代外部送达或写入成功。
