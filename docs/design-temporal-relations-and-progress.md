# 实体关系与进度历史存储方案

> Status: current
> Authority: normative design
> Last verified: 2026-09-05

## 1. 目标

Jarvis 已用 `project`、`person`、`feishu_group`、`todo`、`task`、`resource` 等业务表保存实体。本方案补充四类信息：

1. `task_event`：Task 的结构化状态变化。
2. `fact`：任意主体（项目、群、人……）的自然语言事实流。
3. `entity_relation`：跨核心实体与可选模块实体的带证据映射。
4. `world_progress`：Jarvis 对外部主体在某个周期内的证据化判断快照。

实体长期事实仍写入 Page 正文并用 Markdown 引用形成 backlinks；`EntityRelation` 不复制这些叙述，而是服务代码和模块确实需要查询、过滤或投影的通用映射。

不创建通用 `entity` 或 `entity_alias` 表，不复制已有实体，也不引入独立知识图谱数据库。SQLite 仍是唯一真相来源。

## 2. 设计原则

- 实体身份结构化，语义尽量自然语言化。
- 模型负责从描述和上下文中推断“具体是什么关系、属于哪类进展”。
- 已有外键能表达的关系继续读原字段，不在 `EntityRelation` 中重复建边。
- Task 有明确状态机，继续用结构化事件；事实的类型不稳定，使用通用描述。
- 关系类型是开放 token；证据保留为宽松 JSON，不为业务关系建立枚举。

## 3. EntityRelation

`entity_relation` 保存 `source_type/source_id/relation_type/target_type/target_id`，以及宽松 `evidence`、可选 `confidence` 和 `confirmed_at`。同一五元组 upsert，类型不使用业务枚举，因此 OKR 等可选模块可以投影到通用世界模型，而核心不需要依赖模块 DTO。

Page 的 Markdown 引用和 EntityRelation 各有所有权：前者是给模型与人读取的实体长期事实正文，后者是程序要查询的跨模块映射。删除一条关系不会改写 Page，维护 Page 也不会暗中创建关系。

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
created execution_started rerun_requested supplemented
execution_succeeded execution_failed execution_observing execution_interrupted
stale_failed stale_requeued updated closed feishu_message_recalled
waiting_scheduled resumed human_input_requested human_response_received
```

上面审批事件只用于解释旧历史数据；当前执行协议统一以 `needs_human + question` 停车，并在用户回答后续跑同一 Session，不再创建独立 approval/apply 阶段。

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

### 6.1 WorldProgress

`world_progress` 使用 `(subject_type, subject_id, period_key)` 唯一键，保存 `signal`、自然语言 `summary`、宽松 `evidence`、`evidence_until`、`assessed_at` 和 CAS `version`。`signal` 只提供 `unknown/green/yellow/red` 四种机器可筛选颜色；具体处境、变化、风险和下一观察点都写在 summary。`assessed_at` 是 Jarvis 形成判断的时间，`evidence_until` 是本次判断所覆盖证据的截止时间，两者不能混用。

它不是 Fact，不冒充客观事件，也不替代外部产品的正式进展。主体适配器接受未归档的 `project`，以及已启用 OKR 模块中真实存在的 `okr_objective`、`okr_kr` 和 `okr_point`：创建和更新时确认对应主体存在；读取历史行不依赖模块当前可用。相同内容更新是 no-op，不递增版本或刷新评估时间。

### 6.2 接口

```text
GET    /api/tasks/:task_id/events

GET    /api/facts?subject_type=project&subject_id=1&from=&until=&limit=
POST   /api/facts

GET    /api/pages
GET    /api/pages/:type/:id
PUT    /api/pages/:type/:id
GET    /api/pages/:type/:id/backlinks

GET    /api/relations
POST   /api/relations
DELETE /api/relations/:relation_id

GET    /api/world-progress?subject_type=okr_point&subject_id=...&period_key=...
GET    /api/world-progress/period/:period_key
GET    /api/world-progress/:world_progress_id
POST   /api/world-progress
PUT    /api/world-progress/:world_progress_id
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

- 人物 / 项目详情：展示该实体的 `summary` 页、`fact` 时间线，以及 `list-backlinks` 反链。
- 任务详情：展示 `task_event` 时间线。

`jarvis-tools get-page` 读长期事实全文；`list-facts` 按下钻历史明细。

## 8. Schema 与历史数据

SQLite 只按当前领域模型建表，不携带旧数据库的版本迁移链，也不在运行时维护双栈兼容。历史数据如需保留，在切换服务前执行一次离线导入并核对表级行数；应用启动后只读取 SQLite。

未来需要图查询时，可以从已有外键和页内引用投影到图数据库，节点 ID 使用 `type:id`；投影是可重建索引，不改变 SQLite 的真相来源地位。
