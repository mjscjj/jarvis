# Agent 决策与执行完整技术全景

最终交付只保留一张 ImageGen 位图：`agent-decision-execution-full-panorama.png`。

这张图以 M5 决策 Agent 为中心，把以下语义组织在同一个闭环里：

- 左侧：现实变化、时间到达、主动巡视、人工续办四类触发，以及 M2 / M3 / Task 基础设施；
- 顶部：可信控制与业务证据两条装载平面；
- 中央：同一 M5 Agent 内的快脑调查与慢脑行动；
- 右侧：工具、执行子 Agent、外部系统和审批后的外部写入；
- 底部：新证据经 Message / Todo / Task / ExecutionRun 和 FactEngine 沉淀为 Summary / Fact，再被下一轮按需读取。

生成方式：内置 ImageGen，`infographic-diagram`。视觉使用暖白背景、扁平矢量化技术图风格，以及橙色触发、紫色决策、蓝色上下文、绿色执行、灰色机器边界的统一语义。

核心约束：M3 只做准入；快脑与慢脑属于同一 M5；子 Agent 默认只读且不拥有审批和完成判断权；Shared Memory 属于可信控制；Summary / Fact 属于世界上下文和跨轮记忆。
