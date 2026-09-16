# ProjectRisk / ProjectChange 最小语义设计

## 结论

项目管理首版新增 `ProjectRisk` 与 `ProjectChange` 两个世界实体，不新增 `ProjectProgress` 或 `Deliverable`，也不修改 `KeyMatter` 的字段。

- 周期进展继续以 `WorldProgress(subject_type=project)` 为唯一真源。
- 交付物继续使用关联到 Project 的 `ManagedResource`。
- 项目详情原“里程碑”统一称为“关键事项”。
- 风险从 WorldProgress 的 Markdown 风险段迁出；旧快照仍可读取，但保存新快照时不再写回该段。

## 实体边界

`ProjectRisk` 只结构化程序需要查询的控制字段：所属项目、标题、概率、影响、触发时间和关闭时间。风险描述、触发条件、缓解方案、Owner、支持方与证据写在实体 Markdown Page。

`ProjectChange` 只结构化所属项目、标题、生效时间和关闭时间。变更前后、原因、影响与证据写在实体 Markdown Page。

`KeyMatter` 继续表示通用、长期值得跟进的现实事项。风险触发后若产生一个或多个承接事项，使用 EntityRelation：

```text
project_risk:<id> --handled_by--> key_matter:<id>
```

因此不需要 `KeyMatter.matter_type`，也不借用自由文本 `status` 区分类型。

## 生命周期与接口

- Risk 创建后处于监控中；首次写入 `triggered_at` 表示已经触发，该时间不可清除或改写；DELETE 只写入 `closed_at`。
- Change 以 `changed_at` 作为生效时间；DELETE 只写入 `closed_at`。
- 已关闭的 Risk / Change 保留用于历史查询，不再接受普通字段更新。
- 两者创建和关键生命周期变化写入 Fact；长期内容通过通用 Page CAS 接口读写。
- HTTP 与 `jarvis-tools` 都提供 list/get/create/update/close；列表可按 `project_id` 过滤，并默认隐藏已关闭记录。
