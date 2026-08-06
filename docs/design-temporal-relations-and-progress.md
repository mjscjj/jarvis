# 实体关系与进度历史存储方案

> Status: current
> Authority: normative design
> Last verified: 2026-08-02 @ `89fa24b`

## 1. 目标

Jarvis 已用 `project`、`person`、`feishu_group`、`todo`、`task`、`resource` 等业务表保存实体。本方案只补充三类信息：

1. `relation_fact`：两个现有实体之间的关联。
2. `task_event`：Task 的结构化状态变化。
3. `fact`：任意主体（项目、群、人……）的自然语言事实流。

不创建通用 `entity` 或 `entity_alias` 表，不复制已有实体，也不引入独立知识图谱数据库。SQLite 仍是唯一真相来源。

## 2. 设计原则

- 实体身份结构化，语义尽量自然语言化。
- 模型负责从描述和上下文中推断“具体是什么关系、属于哪类进展”。
- 已有外键能表达的关系继续读原字段，不重复写 `relation_fact`。
- Task 有明确状态机，继续用结构化事件；事实的类型不稳定，使用通用描述。
- 早期 MVP 不增加 predicate、置信度、来源、状态或详情 JSON；当前已加入 `valid_from/valid_until` 表达关系有效期。

## 3. RelationFact

### 3.1 字段

```text
id
entity_a_type
entity_a_id
entity_b_type
entity_b_id
description
valid_from
valid_until
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
- 多态实体引用无法用一个普通外键表达，写入服务负责存在性校验。

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
execution_observing
```

## 5. Fact

### 5.1 字段

```text
id
subject_type
subject_id
description
occurred_at
source_kind
source_id
created_at
```

实体表继续保存当前状态；`fact` 只追加发生过的事情。

`subject_type` 故意不是枚举：`project`、`group`、`person`、`task` 是目前会被读回的类型，但模型判断一条事实属于别的东西时可以自己写值，不会被拒绝。只有能对上表的类型才校验主体存在性，认不出的类型照样入库——宁可存一条无法命名主体的事实，也不为一个没人要求的枚举丢掉观察。

没有单独的日期列。按自然日筛选用 `occurred_at` 的半开区间，由调用方决定时区；存一个本地日期反而会在时区配置变化时静默算错。

描述不区分固定事件类型，例如：

```text
完成第一版关系事实接口，下一步接入后台项目详情页。
项目状态从“active”调整为“paused”。
飞书开放平台权限仍未审批，当前阻塞消息回放验收。
```

项目创建、资料更新、状态调整和归档会追加简短描述；Fact 也可通过 API 写入。factengine 从 message、TodoEvent 和 TaskEvent 蒸馏 Fact，并可通过通用工具按需维护当前实体、关系和资料；M3/M5 的变化通过各自生命周期事件被同一事实引擎消费。

`daily_digest` 是面向人的个人/群日报，不是通用 Fact rollup，也不替代 Fact 的时间查询。

## 6. API

```text
POST   /api/relation-facts
GET    /api/relation-facts?entity_type=project&entity_id=1&page=1&page_size=20
PUT    /api/relation-facts/:fact_id
DELETE /api/relation-facts/:fact_id

GET    /api/tasks/:task_id/events

GET    /api/facts?subject_type=project&subject_id=1&from=&until=&limit=
POST   /api/facts
```

创建关系请求：

```json
{
  "entity_a": {"type": "person", "id": 17},
  "entity_b": {"type": "project", "id": 3},
  "description": "张三负责 Jarvis 的飞书接入。"
}
```

记录事实请求：

```json
{
  "subject_type": "project",
  "subject_id": 3,
  "description": "完成关系事实接口，下一步接入后台展示。"
}
```

读取时 `from` / `until` 是 RFC3339 的半开区间；要一个自然日就由调用方传当天和次日的本地零点。

所有写接口严格解码 JSON，未知字段直接返回 400。

## 7. 后台展示

不增加新的导航 Tab：

- 人物详情：展示与该 Person 关联的 `relation_fact`。
- 任务详情：展示 `task_event` 时间线和与该 Task 关联的 `relation_fact`。
- 背景 → 项目详情：展示项目资料、该项目的 `fact` 时间线、与该 Project 关联的 `relation_fact`，并允许输入一段自然语言记录事实。

`jarvis-tools get-project` 同时返回项目资料、最近 50 条项目事实和项目关系；`get-person` 同时返回人物资料和人物关系，供模型直接推断上下文。

## 8. Schema 与历史数据

SQLite 只按当前领域模型建表，不携带旧数据库的版本迁移链，也不在运行时维护双栈兼容。历史数据如需保留，在切换服务前执行一次离线导入并核对表级行数；应用启动后只读取 SQLite。

未来需要图查询时，可以从已有外键和 `relation_fact` 投影到图数据库，节点 ID 使用 `type:id`；投影是可重建索引，不改变 SQLite 的真相来源地位。
