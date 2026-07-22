# 时间关系与进度历史存储方案

## 1. 目标

Jarvis 已用 `project`、`person`、`feishu_group`、`todo`、`task`、`resource` 等表保存确定性业务实体。本方案补充两类能力：

1. 保存现有外键无法表达、会随时间变化、必须保留证据的跨实体关系。
2. 保存 Task 和 Project 的完整生命周期，避免用 `updated_at` 猜测完成时间。

本方案不创建通用 `entity` 或 `entity_alias` 表。现有业务表继续是实体和身份的唯一真相。

## 2. 边界

### 2.1 确定性关系继续用现有字段

下列关系已有唯一权威字段，禁止重复写入 `relation_fact`：

| 关系 | 权威字段 |
|---|---|
| Group 属于 Project | `feishu_group.project_id` |
| Todo 来自 Group | `todo.group_id` |
| Todo 属于 Project | `todo.project_id` |
| Task 由 Todo 生成 | `task.todo_id` |
| Task 属于 Project | `task.project_id` |

图投影需要这些边时，直接读取现有外键。

### 2.2 RelationFact 只保存动态知识

适合写入 `relation_fact` 的关系包括：

- Person `responsible_for` Project
- Person `reports_to` Person
- Person `collaborates_with` Person
- Project `depends_on` Project
- Project `blocked_by` Task
- Decision `affects` Task
- Person `prefers` 一个 JSON 值

每条关系必须带来源。模型推断的关系还必须带置信度、模型和 prompt 版本。

### 2.3 任意概念暂不升格为实体

“使用 Go”“偏好异步沟通”这类值先写入 `value_json`。只有当一个概念需要独立属性、生命周期和多跳引用时，才新增明确的业务表。

## 3. 数据模型

### 3.1 relation_fact

`relation_fact` 使用 `(subject_type, subject_id)` 和 `(object_type, object_id)` 引用现有业务记录。MySQL 无法为这种多态引用建立普通外键，因此写入服务必须按类型检查目标是否存在。

核心约束：

- `subject_type` 只允许代码注册的实体类型。
- object 实体与 `value_json` 必须二选一。
- `dedup_key` 唯一，重试不能产生重复事实。
- `status` 只允许 `active`、`superseded`、`retracted`。
- 单值 predicate 产生新事实时，旧事实转为 `superseded`，不删除。
- typed predicate 禁止写入本表。

当前支持的实体类型：

```text
project person principal group todo task resource managed_resource
```

### 3.2 task_event

`task_event` 记录 Task 状态机和人工补充，不替代 `execution_run`：

- `execution_run`：一次 Codex 执行尝试的输入、输出和耗时。
- `task_event`：Task 从一个状态变到另一个状态的业务历史。

`task_id + task_version` 唯一。Task 每次版本增加后必须产生一条对应事件。

主要事件：

```text
created execution_started approval_requested approval_granted
approval_rejected rerun_requested reapply_started supplemented
execution_succeeded execution_failed stale_failed
```

### 3.3 project_event

`project_event` 保存项目进度、决策、里程碑和状态变化：

```text
created profile_updated status_changed progress_reported
milestone_reached decision_recorded blocked unblocked delivered archived
```

Project 的当前状态仍由 `project` 表保存。事件只保存历史和来源。

## 4. 写入规则

### 4.1 RelationFact

写入顺序：

1. 校验 predicate 定义。
2. 校验 subject 存在。
3. 校验 object 或 value 的 XOR 约束。
4. 校验 object 存在。
5. 规范化输入并计算 `dedup_key`。
6. 插入新事实。
7. 对单值 predicate，将冲突的旧 active 事实标记为 `superseded`。

系统不按名称模糊绑定实体。调用者先用现有 Resolver 或工具把外部标识解析成业务主键。

### 4.2 TaskEvent

所有 Task 状态更新继续由 `internal/execute.Store` 负责，并在同一状态写入边界追加 TaskEvent。M4 创建 Task 时追加 `created` 事件。

事件写失败必须向上返回错误，不得静默跳过。

### 4.3 ProjectEvent

项目创建、更新和删除入口由 `background.ProjectService` 负责：

- 创建项目后写 `created`。
- 状态变化写 `status_changed`。
- 其它字段变化写 `profile_updated`，`detail.changed_fields` 只列出真实变化字段。
- 删除接口改为把项目状态设为 `archived` 并写 `archived` 事件；项目不再物理删除。
- 进度、决策、里程碑等业务事件走显式 ProjectEvent API。

## 5. 查询

### 5.1 当前关系

当前事实满足：

```sql
status = 'active'
AND (valid_from IS NULL OR valid_from <= :as_of)
AND (valid_to IS NULL OR valid_to > :as_of)
```

### 5.2 历史关系

历史查询不筛 `status`，按 `created_at, id` 返回全部事实及 supersede/retract 链路。

### 5.3 进度

进度统计按 `task_event.occurred_at` 和 `project_event.occurred_at` 聚合，不再用实体的 `updated_at` 近似完成时间。

## 6. API

```text
POST /api/relation-facts
GET  /api/relation-facts
POST /api/relation-facts/:fact_id/retract

GET  /api/tasks/:task_id/events

GET  /api/projects/:project_id/events
POST /api/projects/:project_id/events
```

所有写接口使用严格 JSON 解码；未知字段直接返回 400。

## 7. 图和向量投影

未来投影到 Neo4j 时，节点 ID 直接使用 `type:id`：

```text
person:17
project:3
task:46
```

投影器从两处生成边：

1. 现有业务外键。
2. `relation_fact` 动态关系。

Qdrant payload 同样保存 `subject_type/subject_id/object_type/object_id`。MySQL 始终是 source of truth，图和向量索引可以重建。

## 8. 历史数据

本次只创建新表，不伪造历史关系或 Task 状态迁移：

- `relation_fact` 和 `project_event` 从上线时刻开始记录。
- 现有 Task 各写一条 `snapshot_imported` 事件，只表示切换时的当前状态；不根据 `updated_at` 或 `execution_run` 猜测过去的迁移时间。
- 已有 `todo_event`、`decision_audit`、`execution_run` 保持不变。

一次性回填脚本与在线写入逻辑分离，不保留长期兼容分支。

代码提供显式的一次性命令，但不会在普通启动时自动修改存量数据：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -backfill-progress-events
```
