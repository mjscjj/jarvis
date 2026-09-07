---
name: okr-world-projector
description: 将已填写的 OKR 完整投影为 Jarvis 业务 Ontology：逐一读取 Objective、KR、Point、Metric 与结构化 Owner，为每个 Objective 建立或复用现实 Project，为每个 KR 建立或复用归属该 Project 的 KeyMatter，让每个 Point 连接到现实 KeyMatter，并把每个 Owner 解析为 Principal/Person 后建立可查询关系。用于“把 OKR 拆到现实模型”“建立完整业务 Ontology”“初始化或刷新整个季度的 OKR 世界投影”；不修改 OKR 真源、正式 Progress 或 WorldProgress，不发送消息或写外部系统。
module: okr
---

# 把 OKR 完整投影为业务 Ontology

OKR 是目标层真源，Jarvis 世界模型是现实层真源。本 Skill 不把 OKR 表复制一遍，而是保证每一个目标节点都能落到现实中的“人、项目、关键事项”上，并能从两端查询：

```text
Objective --maps_to--> Project
KR        --maps_to--> KeyMatter --belongs_to--> Project（由 ProjectID 表达）
Point     --advances--> KeyMatter
KR/Point  --owned_by--> Principal 或 Person
```

同一现实 Project 可以承接多个 Objective；一个 Point 默认推进父 KR 的 KeyMatter，只有它本身确实需要独立长期跟踪时才新建 KeyMatter。因此完整投影不等于按 OKR 行数机械复制世界实体，但每个 O、KR、Point 和 Owner 都必须有现实承接，不能只留下“已审计但未映射”。

## 硬边界

- Objective、KR、Metric、Point、Owner 和正式 Progress 的唯一真源是 OKR 模块。只通过 `scripts/okr-module-tools` 读取，不直接访问或修改 `okr_workspace_*` 表。
- OKR 定义本身就是“该业务承诺、分解和负责人存在”的权威证据；建立 `maps_to`、`advances`、`owned_by` 投影不需要再找一份外部材料。外部材料只用于丰富现实实体当前状态，不能作为拒绝投影的理由。
- Objective 必须有 Project 承接；KR 必须有 KeyMatter 承接；Point 必须连接到一个 KeyMatter；每个带 `open_id` 的结构化 Owner 必须解析为 Principal 或 Person。
- 不允许把 `evidence_insufficient`、`no_independent_world_entity`、`dynamic_owner_only` 或“仅在图上显示虚拟节点”作为完整季度的成功结果。无法落地的节点必须使本次执行停在失败或 `needs_human`，并列出准确 ID 和原因。
- Metric 不单独创建世界实体；它必须完整写入所属 KR 的 KeyMatter Page，作为衡量口径。
- OKR 原生层级仍由 OKR 模块读取，不写 `contains` 关系；跨层现实投影才写 `entity_relation`。
- 不创建常驻 Task 表示 O/KR/Point。Task 只记录本次投影执行或真实动作。
- 不读取或修改正式 Progress，不生成 WorldProgress，不调用 `weekly-report-progress-sync`，不发送消息，不写 Meego 或其他外部系统。
- 所有创建和关系写入逐项回读。中途失败立即停止；重跑先查询现状并幂等复用，不做事务回滚。

## 关系语义

只写事实成立的一个方向，读取层负责反向显示：

```text
okr_objective --maps_to--> project
okr_kr        --maps_to--> key_matter
okr_point     --advances--> key_matter
okr_kr        --owned_by--> principal|person
okr_point     --owned_by--> principal|person
```

若一个 Point 是独立、跨周持续的现实事项，可为它建立单独 KeyMatter，并使用 `okr_point --maps_to--> key_matter`；否则必须 `advances` 父 KR 的 KeyMatter。Project 与 KeyMatter 的归属使用 `KeyMatter.ProjectID`，不复制为 `entity_relation`。

每条投影关系的 evidence 至少包含：

```json
{
  "source": "okr-world-projector",
  "basis": "OKR 定义中的 Objective/KR/Point/Owner 是本次现实承接关系的权威依据",
  "refs": ["okr_kr:<id>", "key_matter:<id>"],
  "quarter": "<YYYY-Qn>",
  "observed_at": "<RFC3339>"
}
```

定义投影使用 `confidence=1` 和 `confirmed_at`。这只确认结构对应，不断言目标已经完成或当前进展正常。

## 1. 冻结完整季度范围

```bash
scripts/okr-module-tools scope
scripts/okr-module-tools list-objectives --quarter '<quarter>'
scripts/okr-module-tools projection-audit --quarter '<quarter>'
```

整季度模式必须按 manifest 顺序逐个读取：

```bash
scripts/okr-module-tools get-objective --id '<objective_id>'
```

保存 Objective、KR、Point、Metric 和每次 Owner 出现的稳定 ID/`open_id`。`board` 可以浏览，但不能代替 manifest + Objective slice 的全量遍历。模块关闭、季度不存在或 slice 不完整时 fail-fast。

## 2. 建立现实 Project 层

每个 Objective 必须恰有至少一条到 Project 的 `maps_to`：

1. 先查已有关系和 Project。
2. 若已有 Project 确实承接同一业务工作流，直接复用；多个 Objective 可以映射到同一 Project。
3. 若没有承接对象，依据 Objective 定义创建 Project。Objective 标题在这里是创建现实工作容器的直接业务定义证据，不得再以“标题不是证据”为由跳过。
4. 新 Project 使用稳定、可读名称；可选 code 必须唯一。`role` 表达 principal 在现实工作中的角色，无法证明 owner 时使用 `participant`；状态按季度和当前事实使用 `planning`、`active` 或 `done`，不得从目标措辞臆造完成。
5. Project Page 写明承接的季度 Objective 稳定引用、范围、下属 KeyMatter；不复制整段 OKR 正文，不制造进展。
6. 写 `okr_objective --maps_to--> project` 并从两端回读。

只有拿到一个 Objective 的 Project ID 后，才能处理它的 KR。

## 3. 建立现实 KeyMatter 层

每个 KR 必须恰有一个主 KeyMatter 承接：

1. 先查该 KR 已有关系和现有 KeyMatter。
2. 已有 KeyMatter 表达同一持续事项时复用；否则以 KR 定义创建新的 KeyMatter。
3. `ProjectID` 必须指向承接父 Objective 的 Project。若复用 KeyMatter 但 ProjectID 不一致，先核对真实归属；不能静默跨项目。
4. KeyMatter Page 至少写入：季度与 KR 稳定引用、KR 当前定义、全部 Metric 衡量口径、全部 Point 稳定引用及标题、结构化 Owner 的人物引用。Page 表达定义和当前稳定范围，不把计划写成已完成事实。
5. 写 `okr_kr --maps_to--> key_matter` 并从两端回读。

世界模型完整保存所有仍成立的 KeyMatter，没有“全库最多 10 个”的容量限制。主动巡视可只关注最近活跃的 10 个，那是注意力窗口，不是 Ontology 存储边界。

## 4. 投影每个 Point

每个 Point 都必须有一条现实承接关系：

- 默认写 `okr_point --advances--> <父 KR 的 KeyMatter>`。
- 若 Point 明确是一件需要独立长期维护状态、聚合多条证据的现实事项，则创建/复用独立 KeyMatter，归属同一 Project，并写 `maps_to`。
- Point 标题、稳定 ID 和 Owner 必须出现在承接 KeyMatter Page 中；不能把 131 个 Point 只保留为前端虚拟节点。
- Point 已有 Meego ID 时仍保留来源原生绑定；它不替代 Point 到现实 KeyMatter 的连接。

## 5. 投影每个 Owner 为现实人物

按 `open_id` 去重处理整个季度全部结构化 Owner：

1. `open_id` 等于 Principal 时复用 `principal:<id>`。
2. 已有相同 `open_id` 的 Person 时复用，禁止因中英文姓名不同重复建人。
3. 其他 Owner 全部创建 Person；`open_id` 是稳定业务键，OKR 中的姓名是创建时可用的权威显示名。没有更多资料时允许 department/title 为空，role 使用 `colleague`，不得因为资料不全只留虚拟 Owner。
4. Person Page 写明其是该季度的结构化 OKR Owner，并列出其负责的 KR/Point 稳定引用；关系本身以 `entity_relation` 为可查询真源。
5. 对每个 Owner 出现项分别写：`okr_kr|okr_point --owned_by--> principal|person`。

Owner 关系是 OKR 原生责任人在现实人物层的投影，不是第二份可编辑的负责人真源。下次运行若 OKR Owner 改变，必须报告旧投影边；只有当前任务明确包含刷新且新定义直接证明旧边失效时才删除。

## 6. 读取、写入与回读顺序

常用查询：

```bash
jarvis-tools get-principal
jarvis-tools list-projects --limit 100
jarvis-tools list-key-matters --limit 100
jarvis-tools list-persons --limit 100
jarvis-tools get-person --open-id '<owner_open_id>'
jarvis-tools resolve-world-node --type '<okr_objective|okr_kr|okr_point>' --id '<id>'
jarvis-tools list-relations --node-type '<okr_objective|okr_kr|okr_point>' --node-id '<id>' --limit 200
```

`resolve-world-node` 只从所属模块读取节点，不复制节点或推断关系；`list-relations --node-type/--node-id` 一次返回该节点的双向一跳邻域。写入使用 `create-project`、`create-key-matter`、`create-person`、`update-page` 和 `create-relation`。payload 以各命令 `--help` 与 API 校验为准。每次创建后立即读取真实 ID；Page 使用 CAS；关系按五元组幂等 upsert，并从节点的一跳邻域回读。

Group 和 Resource 不是全量完成门槛。只有已有证据能把群、文档、仓库或外部工作项稳定绑定到 Project/KeyMatter 时才补充；不得因缺少这些可选实体阻塞人、项目、关键事项三层完整性。

## 7. 整季度完成协议

每完成一个 Objective，记录其 Project、全部 KR KeyMatter、Point 关系和 Owner 关系计数。最终必须再次运行：

```bash
scripts/okr-module-tools projection-audit --quarter '<quarter>'
```

成功必须同时满足：

- Objective 关系覆盖 = manifest Objective 总数；
- KR 关系覆盖 = manifest KR 总数；
- Point 关系覆盖 = manifest Point 总数；
- 每个带 `open_id` 的 KR/Point Owner 出现项都有 `owned_by` 到 Principal/Person；
- 每个唯一 Owner `open_id` 都能从世界模型解析；
- 每个 KR 的 KeyMatter 归属父 Objective 的 Project；
- 每个 KR 的全部 Metric 和 Point 已写入承接 KeyMatter Page；
- 新增/复用实体和关系均已回读；不存在孤儿关系；
- 没有修改正式 Progress、生成 WorldProgress、发送消息或产生外部 effect。

任何一项未满足都不能报告“整季度投影完成”。失败结果必须列出未覆盖的稳定 ID，供同一任务续跑。

最终输出使用宽松 JSON `schema=okr_world_projection_result.v2`，至少包含 quarter、manifest totals、Project/KR KeyMatter/Person 的复用与创建、Objective/KR/Point/Owner 关系计数、未覆盖 ID、孤儿关系和最终机械覆盖；不能再输出 `no_independent_world_entity`、`evidence_insufficient` 或 `dynamic_owner_only` 作为成功处置。
