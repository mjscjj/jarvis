# 图 1 绘制说明：Jarvis 核心实体关系

## 唯一要传达的一句话

人通过协作场推进事，事依赖资源；Jarvis 从 Principal 的责任与目标出发理解整张实体网络。

## 主要关系类型

关系网络。它不是流程图、时间图或数据表 ER 图，重点是长期实体之间如何组成一个可理解的工作世界。

## 一级概念

1. 人：Principal 与 Person。Principal 是观察坐标，Person 是关键协作者。
2. 事：Project 与 Key Matter。Project 汇聚长期目标和进展，Key Matter 承载需要持续看护的事项。
3. 场：Group。人围绕项目协作和沟通的上下文。
4. 物：Resource。支撑人和事的文档、仓库、系统入口等资源。

## 图中关系

- Principal — Project：负责 / 参与。
- Principal — Person：协作 / 决策。
- Person — Project：共同推进。
- Person — Group：沟通发生在。
- Group — Project：围绕 / 归属。
- Project — Key Matter：承载。
- Project — Resource：关联资源；具体依赖关系由实体语义补充。
- Resource 可直接关联 Principal、Person 与 Project。

## 视觉语法

- 深蓝胶囊：Principal，强调它是世界的坐标，不是普通人物节点。
- 蓝色圆：Person。
- 琥珀色中心圆与胶囊：Project、Key Matter，表示“事”。
- 紫色胶囊：Group，表示协作场。
- 绿色折页卡：Resource，表示“物”。
- 灰色实线：当前模型中的直接归属或绑定。
- 蓝灰虚线：通过 Summary 实体引用表达、可随认知变化更新的语义关系。

## 明确删除的内容

本图不出现 Summary、Fact、PageRevision、Todo、Task、ExecutionRun、FactEngine、M3、M5、Proactive 和更新时间线。它们分别属于认知记录、行动状态或维护流程，不属于本图的实体关系层。

## 验收

- 三秒测试：先看到中心 Project，再看到 Principal、Person、Group、Key Matter、Resource 五类关联实体。
- 路径测试：沿关系线可以回答“谁在什么场中推进什么事，并依赖什么资源”。
- 删除测试：删除关系说明或实现备注，不影响六类实体和主关系；删除任一核心实体会损坏模型含义。
- 脱离正文测试：单看图可以说清实体类别、核心关系以及直接绑定与动态语义关系的区别。
