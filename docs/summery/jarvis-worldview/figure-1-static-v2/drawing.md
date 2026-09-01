# 图 1 绘制说明：什么是 Jarvis 的世界观

## 唯一要传达的一句话

Jarvis 的世界观，是以 Principal 为坐标，把人、事、物及其关系，组织成一份包含当前认知、历史事实和未闭环行动的持续演化世界。

## 主要关系

中心坐标 + 分层结构。不是流程图，也不是世界模型与运行时认知的对比图。

## 一级概念

1. Principal：观察、责任、价值与边界的坐标原点。
2. 实体网络：Person、Project、Key Matter、Group、Resource。
3. 实体认知：Summary、Fact、PageRevision 是每个长期实体共同拥有的认知结构。
4. 行动状态：Todo、Task、ExecutionRun 描述尚未闭环的现实工作。

关系与时间是贯穿维度，不画成实体。图中不出现“Agent 此刻的认知面”、FactEngine、M3、M5、Proactive 或更新流程。

## 视觉语法

- 深蓝圆形：Principal 坐标。
- 蓝色圆角卡：长期实体。
- 绿色：Summary / 当前认知。
- 琥珀色：Fact / 历史事实。
- 紫色：PageRevision / 认知版本。
- 青绿色：Todo、Task、ExecutionRun / 行动状态。
- 无箭头细实线：实体关系。
- 虚线：跨层说明或维度关系。
- 箭头：只用于 Todo → Task → ExecutionRun。
- 大圆角区域：Jarvis 世界的系统边界。

## 验收

- 三秒测试：先看到 Principal 中心、实体网络、实体认知和行动状态。
- 路径测试：视觉从中心实体网络进入认知结构，再落到底部行动状态。
- 删除测试：删除任一一级概念都会让“世界观”的定义不完整；关系图例、时间轴和解释句均为可删除的二级信息。
- 脱离正文测试：单看图仍能回答“Jarvis 的世界由什么构成”。
