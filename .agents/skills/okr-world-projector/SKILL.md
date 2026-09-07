---
name: okr-world-projector
description: 调查已填写 OKR 与 Jarvis 现实世界之间有证据的对应关系：读取 Objective、KR、Point、Metric 与 Owner，复用确实存在的 Project、KeyMatter、Person、Group、Resource 或外部对象，并只在语义明确时建立跨模块关系。用于“关联 OKR 到世界模型”“检查 OKR 现实承接”“刷新 OKR 世界映射”；不要求全覆盖，不为补齐图谱而创建实体，不修改正式 Progress 或 WorldProgress，不发送消息或写外部系统。
module: okr
---

# 调查 OKR 与现实世界的关系

OKR 是目标层真源，Jarvis 世界模型是现实层真源。Objective、KR、Point 无论是否存在现实映射，都直接作为 OKR 原生节点出现在世界地图中。本 Skill 只调查并维护两层之间已经能够被证据确认的稀疏关系。

一次执行完成不等于覆盖率达到 100%。完成标准是：按用户指定范围完成调查；确认的关系有证据且已回读；没有证据的节点保持未关联；没有为了图形完整而制造 Project、KeyMatter 或 Person。

## 硬边界

- OKR 模块是 Objective、KR、Metric、Point、Owner 和正式 Progress 的唯一真源。只通过 `scripts/okr-module-tools` 读取，不直接访问或修改 `okr_workspace_*` 表。
- 不复制 Objective、KR、Point，也不为它们创建常驻 Task。2D 图中的完整 `Objective → KR → Point` 骨架由 OKR 原生结构在读取时生成。
- 未关联是合法状态，不是失败。不得以覆盖率、节点数量或视觉完整为理由创建任何现实实体或强关系。
- Objective/KR 标题、标题相似、同一 Owner、同群出现或 OKR 定义本身，都不能单独证明它与某个现实对象相同。
- 创建 Project、KeyMatter、Person 或 Resource 时，必须分别满足该现实实体自身的准入标准；如果去掉 OKR 投影需求后该实体就没有独立存在价值，则不创建。
- 不批量把 Owner 创建为 Person。Owner 原生字段可直接用于 OKR 展示；只有人物对 principal 的长期世界确有意义且稳定身份可核实时，才创建或复用 Person。
- 不读取或修改正式 Progress，不生成 WorldProgress，不调用 `weekly-report-progress-sync`。
- 不发送消息，不写 Meego 或其它外部系统。所有 Jarvis 内部写入逐项回读；中途失败直接停止，不做事务回滚。

## 关系所有权

先使用已有真源，只有没有共同外键、又确实需要查询的跨模块强关系才写 `entity_relation`：

| 关系 | 唯一真源 | 本 Skill 的处理 |
|---|---|---|
| Objective 包含 KR，KR 包含 Metric/Point | OKR 模块 | 读取时派生，不复制 |
| KR/Point 的正式 Owner | OKR 模块结构化 Owner | 直接展示；仅在现实人物已经确认时建立跨层关系 |
| KeyMatter 属于 Project | `KeyMatter.ProjectID` | 使用实体字段，不复制 |
| Group 属于 Project | `Group.ProjectID` | 使用实体字段，不复制 |
| Resource 属于 Project/Person | ManagedResource 字段 | 使用实体字段，不复制 |
| OKR 与现实实体的对应、推进或依赖 | 无共同外键 | 有直接证据时写 `entity_relation` |
| Page 正文提到实体 | Markdown 引用 | 保持弱引用，不提升为强关系 |

关系词使用现有通用关系语义。常见的稀疏桥包括：

```text
okr_kr    --maps_to--> project
okr_point --maps_to--> key_matter
project   --advances--> okr_kr
key_matter--advances--> okr_kr
```

关系方向按事实语义选择，不为了统一图形强制改成同一方向。OKR Owner 已经是正式责任真源，不要求复制 `owned_by`；如果确有跨层查询需要且现实人物已经存在，可以写一条带证据的关系。

## 1. 确定范围并读取 OKR

```bash
scripts/okr-module-tools scope
scripts/okr-module-tools board --quarter '<quarter>'
```

默认只处理用户指定的 Objective、KR、Point 或当前相关范围。用户明确要求调查整个季度时才遍历全量；“整个季度”只表示调查范围完整，不表示每个节点必须产生现实关系。模块关闭、季度不存在或 OKR 为空时，停止并报告真实原因。

## 2. 先读已有世界

按 OKR 中出现的明确项目代号、稳定 URL/ID、已有关系和 Page 引用查找候选：

```bash
jarvis-tools get-principal
jarvis-tools list-projects --keyword '<明确线索>' --limit 100
jarvis-tools list-key-matters --keyword '<明确线索>' --limit 100
jarvis-tools get-person --open-id '<owner_open_id>'
jarvis-tools list-groups --keyword '<明确线索>' --limit 100
jarvis-tools query-resources --keyword '<明确线索>' --limit 100
jarvis-tools list-relations --node-type '<okr_objective|okr_kr|okr_point>' --node-id '<id>' --limit 100
```

需要确认候选时，再读取 Page、Fact、消息、文档或外部工作项。不要从全租户宽泛搜索开始。对每个节点允许四种正常结果：

- 复用已有现实实体并建立或刷新关系；
- 创建一个本身就值得长期维护的现实实体，再建立关系；
- 保留为未确认候选；
- 判断当前无需映射。

后两种不是失败，也不需要向用户索要一个虚构映射。

## 3. 现实实体准入

- **Project**：只有现实中存在跨周持续、有明确边界的项目时才创建或复用。Objective/KR 标题本身不是项目证明。
- **KeyMatter**：只有事项需要跨多次动作持续记忆、检查状态或聚合多条证据时才创建。Point 是目标拆解，不自动等于 KeyMatter；短期动作属于 Task。
- **Person**：只有具备稳定身份且后续确需按人维护长期事实时才创建。单纯出现在 Owner 列表中不够。
- **Group**：只复用采集层已经发现的群；本 Skill 不创建群。
- **Resource**：只有后续会反复使用的权威文档、仓库、看板或链接才创建。

创建前先查询去重，创建后立即回读真实 ID。Page 只写已核实的当前稳定事实，不把 OKR 目标措辞写成已经发生的进展。

## 4. 建立有证据的关系

每条新关系至少记录来源、直接依据、稳定证据引用和观察时间。`confidence` 由证据质量决定；只有已经确认的关系才设置 `confirmed_at`。没有稳定证据时不写低置信度占位边。

写入后从任一端读取双向一跳邻域，确认关系可见。相同五元组应幂等刷新，不产生重复边。不能仅因本轮没有重新找到依据就删除旧关系；只有直接证据确认关系失效且任务明确包含刷新时才删除。

## 5. 完成检查

- 指定范围内的 OKR 结构已通过模块工具读取，未直查模块数据库。
- OKR 原生层级和 Owner 没有为了图展示被复制。
- 新建的每个现实实体都有独立现实意义，而不是为某个 OKR 节点凑数。
- 每条新增或刷新的强关系都有直接证据并已回读。
- 未确认和无需映射的节点被如实报告，没有被算作失败或强行补齐。
- 没有修改正式 Progress、WorldProgress 或外部系统，也没有发送消息。

最终输出自然语言说明处理范围、确认关系、实际创建的现实实体及其独立理由、未确认候选和证据缺口；不要输出机械的 100% 覆盖承诺。
