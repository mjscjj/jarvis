# M1 背景信息模块

> Status: current
> Authority: normative module guide
> Last verified: 2026-08-02 @ `89fa24b`
> Code source: `internal/background/`, `internal/domain/models.go`, `internal/domain/knowledge.go`

M1 维护 principal 的稳定工作背景，供 M3/M5 和日报读取。SQLite 是真源；不向向量库同步背景，也没有 memory sidecar。

## 1. 边界

| M1 负责 | M1 不负责 |
|---|---|
| PrincipalProfile、Project、Person、ManagedResource CRUD | 消息采集、Todo 抽取、M5 执行 |
| Group 的人工背景与 Project 归属 | Group 发现、活跃度和消息落库 |
| lark-cli 姓名解析 | 完整飞书读写封装 |
| 后台背景配置页 | 持续世界建模 |

## 2. 当前关系

```mermaid
erDiagram
    PROJECT ||--o{ GROUP : "project_id"
    PROJECT ||--o{ MANAGED_RESOURCE : "project_id"
    PERSON ||--o{ MANAGED_RESOURCE : "person_id"
    PRINCIPAL_PROFILE ||--o{ MANAGED_RESOURCE : "link_principal"
    PROJECT ||--o{ TODO : "project_id"
    PROJECT ||--o{ TASK : "project_id"
```

当前没有 `project_member` 模型或表。动态实体关系使用自然语言 `RelationFact` 表达；一个 Group 至多直接绑定一个 Project。

## 3. 关键模型

- Project：`code`、`name`、`role(owner|participant)`、`status(planning|active|paused|archived|done)`、`priority`、`repos`、`description`、`tech_stack`、`key_decisions`、`timeline`、`notes`
- Person：Feishu ID、姓名、`role(leader|key|colleague|other)`、权重、关系、沟通风格、P2P chat、启用状态
- PrincipalProfile：本人身份、职责、偏好和 leader
- Group：采集维护会话身份与活跃信息；M1 维护 `project_id` 和 `background_note`。`include_in_memory` 当前只存储/展示，没有 memory sidecar 运行效果
- ManagedResource：人工维护的文档、链接、仓库或备注，可关联 Person、Project 和 principal

字段和 allowlist 以 Go model/service 为准，不在本文复制 DDL。

## 4. Fact 与 RelationFact

- Project 创建、修改、归档会写自然语言 Fact；Fact 也可通过 API 写入。
- factengine 从 message、TodoEvent 和 TaskEvent 持续蒸馏 Fact，并通过通用 CRUD 工具按需维护当前背景、关系和资料。
- RelationFact 保存两个既有实体之间的自然语言关系与有效期。
- M1 不负责从会话批量蒸馏事实。

## 5. API 与初始化

Projects、Persons、Groups、Profile、Managed resources、Facts 和 RelationFacts 的路由见 [HTTP API](../reference/http-api.md)。`DELETE /api/projects/:id` 实际是软归档。

首次身份、项目、人物、重点事项和群监听统一由仓库级 `bootstrap-jarvis-world-model` Skill 依据当前用户证据建立。M1 不保留任何特定用户的 seed 数据，也不从关键群机械批量导入人物。

## 6. 已知边界

- 多仓库 Project 由模型结合 Task 上下文选择 repo。
- Person 停用依赖人工维护。
- Project 与 Person 目前没有结构化成员关系表。
