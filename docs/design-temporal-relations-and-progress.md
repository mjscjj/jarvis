# 实体关系与进度历史存储方案

## 1. 目标

Jarvis 已用 `project`、`person`、`feishu_group`、`todo`、`task`、`resource` 等业务表保存实体。本方案只补充三类信息：

1. `relation_fact`：两个现有实体之间的关联。
2. `task_event`：Task 的结构化状态变化。
3. `project_event`：Project 的自然语言进度历史。

不创建通用 `entity` 或 `entity_alias` 表，不复制已有实体，也不引入独立知识图谱数据库。MySQL 仍是唯一真相来源。

## 2. 设计原则

- 实体身份结构化，语义尽量自然语言化。
- 模型负责从描述和上下文中推断“具体是什么关系、属于哪类进展”。
- 已有外键能表达的关系继续读原字段，不重复写 `relation_fact`。
- Task 有明确状态机，继续用结构化事件；Project 的进度类型不稳定，使用通用描述。
- 早期 MVP 不增加 predicate、置信度、来源、有效期、状态、详情 JSON 等字段。

## 3. RelationFact

### 3.1 字段

```text
id
entity_a_type
entity_a_id
entity_b_type
entity_b_id
description
created_at
updated_at
```

`entity_a` 和 `entity_b` 都引用已有业务表。当前支持：

```text
project person principal group todo task resource managed_resource
```

`description` 用自然语言完整描述关联，例如：

```text
张三负责 Jarvis 的飞书接入，日常技术方案需要先和李四对齐。
任务“补充项目背景”是 Jarvis 当前进度阻塞项，完成后才能开始自动执行验收。
```

### 3.2 约束

- 两端实体必须存在，且不能是同一个实体。
- 实体对按 `type:id` 排序后存储，因此 A-B 和 B-A 是同一条记录。
- 一个实体对只保留一条事实；重复写入会更新 `description`。
- 删除使用物理删除，不维护撤回、失效或 supersede 状态。
- MySQL 无法给多态实体引用建立普通外键，写入服务负责存在性校验。

以下确定性关系继续使用已有字段：

| 关系 | 权威字段 |
|---|---|
| Group 属于 Project | `feishu_group.project_id` |
| Todo 来自 Group | `todo.group_id` |
| Todo 属于 Project | `todo.project_id` |
| Task 由 Todo 生成 | `task.todo_id` |
| Task 属于 Project | `task.project_id` |

## 4. TaskEvent

`task_event` 保持结构化设计，用来记录 Task 状态机和人工补充，不替代 `execution_run`：

- `execution_run`：一次 Codex 执行尝试的输入、输出和耗时。
- `task_event`：Task 从一个状态变到另一个状态的业务历史。

Task 每次版本变化追加一条事件，保留事件类型、前后状态、操作者、关联执行和详情。查询按 `occurred_at DESC, id DESC` 返回。

主要事件：

```text
created execution_started approval_requested approval_granted
approval_rejected rerun_requested reapply_started supplemented
execution_succeeded execution_failed stale_failed snapshot_imported
```

## 5. ProjectEvent

### 5.1 字段

```text
id
project_id
description
occurred_at
created_at
```

`project` 表继续保存当前状态；`project_event` 只追加发生过的事情。描述不区分固定事件类型，例如：

```text
完成第一版关系事实接口，下一步接入后台项目详情页。
项目状态从“active”调整为“paused”。
飞书开放平台权限仍未审批，当前阻塞消息回放验收。
```

项目创建、资料更新、状态调整和归档会自动追加简短描述；模型或用户也可通过 API 直接记录任意自然语言进展。

## 6. API

```text
POST   /api/relation-facts
GET    /api/relation-facts?entity_type=project&entity_id=1&page=1&page_size=20
PUT    /api/relation-facts/:fact_id
DELETE /api/relation-facts/:fact_id

GET    /api/tasks/:task_id/events

GET    /api/projects/:project_id/events
POST   /api/projects/:project_id/events
```

创建关系请求：

```json
{
  "entity_a": {"type": "person", "id": 17},
  "entity_b": {"type": "project", "id": 3},
  "description": "张三负责 Jarvis 的飞书接入。"
}
```

记录项目进展请求：

```json
{
  "description": "完成关系事实接口，下一步接入后台展示。"
}
```

所有写接口严格解码 JSON，未知字段直接返回 400。

## 7. 后台展示

不增加新的导航 Tab：

- 人物详情：展示与该 Person 关联的 `relation_fact`。
- 任务详情：展示 `task_event` 时间线和与该 Task 关联的 `relation_fact`。
- 背景 → 项目详情：展示项目资料、`project_event` 时间线、与该 Project 关联的 `relation_fact`，并允许输入一段自然语言记录进展。

`jarvis-tools get-project` 同时返回项目资料、最近 50 条项目事件和项目关系；`get-person` 同时返回人物资料和人物关系，供模型直接推断上下文。

## 8. 迁移和历史数据

旧版 `relation_fact` 和 `project_event` 结构字段过多，本次不保留兼容逻辑：

- 如果旧表为空，迁移会删除旧表并按新模型重建。
- 如果旧表存在数据，启动会 fail-fast，要求先明确历史数据处理方式，不自动猜测转换。
- 当前本机旧表已确认为空，可以直接重建。
- `task_event` 结构不变，已有 Task 历史继续保留。

未来需要图查询时，可以从已有外键和 `relation_fact` 投影到图数据库，节点 ID 使用 `type:id`；投影是可重建索引，不改变 MySQL 的真相来源地位。
