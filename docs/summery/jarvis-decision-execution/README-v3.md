# Agent 决策与执行 · 技术全景图

这套图用于独立飞书文档《Agent 决策与执行：技术架构全景》，表达第二章的稳定概念与当前 Jarvis 实现映射。

## 图表

| 文件 | 回答的问题 |
|---|---|
| `00-project-execution-panorama.svg/png` | 从现实变化、线索准入到 M5 调查、判断、执行、验证的项目执行主链路 |
| `01-technical-panorama.svg/png` | 整个决策与执行闭环由哪些层组成 |
| `02-trigger-mechanisms.svg/png` | 什么会启动或恢复一次思考 |
| `03-fast-slow-brain.svg/png` | 快脑和慢脑如何属于同一个 M5 Agent |
| `04-decision-agent-subagents.svg/png` | 决策 Agent 与执行子 Agent 如何分权 |
| `05-context-rules-memory.svg/png` | 世界上下文、规则和记忆如何加载与更新 |

`document.xml` 是飞书文档正文源文件。每张图保留可编辑 SVG，并提供 1920 × 1080 PNG 预览。

## 关键边界

- M3 是低成本准入，不是快脑；快慢脑都发生在 M5 内部。
- M5 是唯一的语义决策核心，子 Agent 默认只读，只负责有边界的素材调查。
- `execution_context` 是准入时冻结的审计背景；`current_world`、实体 Summary、Fact 和外部调查负责回答当前现实。
- System Prompt、工作规则、审批策略和 Shared Memory 属于可信控制平面；`source_payload` 等业务材料属于证据平面；状态、幂等、审批入口和 effects 属于机器边界。
- 执行结果进入 Task/ExecutionRun/Message 等事实载体，再沉淀为 Summary 与 Fact，成为下一轮判断依据。
