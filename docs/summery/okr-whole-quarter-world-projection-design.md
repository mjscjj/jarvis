# 整季度 OKR 到 Jarvis 业务 Ontology 的完整投影

> Status: implementation-ready
> Authority: normative for whole-quarter projection
> Last verified: 2026-09-07, current uncommitted worktree
> Scope: 通用 OKR 插件、`okr-world-projector` Skill、普通 Task/ExecutionRun、世界地图 OKR Lens

## 1. 完整的定义

整季度投影不是“逐项看过”，也不是在二维图里临时画出 OKR 原生结构。完成后，季度内每个目标节点和每次负责人出现都必须有可查询的现实承接：

```text
Objective --maps_to--> Project
KR        --maps_to--> KeyMatter --ProjectID--> Project
Point     --advances--> 父 KR KeyMatter
KR/Point  --owned_by--> Principal 或 Person
```

- 每个 Objective 至少映射一个 Project。多个 Objective 确实属于同一现实工作流时可以复用 Project。
- 每个 KR 恰有一个主 KeyMatter，并归属父 Objective 承接的 Project。
- 每个 Point 默认推进父 KR 的 KeyMatter；只有它本身需要独立、跨周维护现实状态时才创建额外 KeyMatter。
- 每个带 `open_id` 的结构化 Owner 都必须解析为 Principal 或 Person；同一个 Owner 在不同 KR/Point 的每次出现都必须有 `owned_by`。
- Metric 不单独建世界实体，完整写入对应 KR KeyMatter Page。

因此，完整投影不等于把 OKR 表逐行复制成同构世界表；但也绝不允许用 `evidence_insufficient`、`no_independent_world_entity`、`dynamic_owner_only` 或虚拟图节点替代现实 Project、KeyMatter、Person 和强关系。

## 2. 所有权和边界

- Objective、KR、Metric、Point、Owner 和正式 Progress 的唯一真源是通用 OKR 插件。
- Project、KeyMatter、Principal、Person、Page 和 EntityRelation 的唯一真源是 Jarvis 世界模型。
- Biz OKR 是依赖通用 OKR 的业务应用，不拥有第二份 OKR 或世界投影。
- OKR 定义本身就是目标、拆解和责任归属存在的权威证据。外部证据用于丰富现实状态，不是拒绝建立定义投影的前置条件。
- 投影不读取或修改正式 Progress，不生成 WorldProgress，不发送消息，不写 Meego 或其它外部系统。
- 不创建常驻 Task 表示 O/KR/Point；普通 Task 只记录本次有始有终的投影执行。

## 3. 全量输入

执行必须先冻结季度 manifest，再按 Objective 分片读取，不能只看当前页面或一次大 Board 响应：

```bash
scripts/okr-module-tools scope
scripts/okr-module-tools list-objectives --quarter '<YYYY-Qn>'
scripts/okr-module-tools projection-audit --quarter '<YYYY-Qn>'
scripts/okr-module-tools get-objective --id '<objective_id>'
```

manifest 必须包含 Objective、KR、Metric、Point、KR Owner occurrence 和 Point Owner occurrence 总数。模块关闭、季度不存在、分片缺失或 ID 不稳定时 fail-fast。

## 4. 实体投影规则

### 4.1 Objective 到 Project

先查询已有 `maps_to project` 和现有 Project。已有 Project 确实承接同一现实工作流时复用；否则依据 Objective 定义创建 Project。Objective 标题是创建现实工作容器的直接业务定义证据，不得以“标题不是外部证据”为由跳过。

Project Page 至少记录季度、Objective 稳定引用、业务范围和下属 KeyMatter。状态只按已知事实使用 `planning`、`active` 或 `done`，不得把目标措辞当成完成事实。

### 4.2 KR 到 KeyMatter

每个 KR 创建或复用一个现实 KeyMatter。`ProjectID` 必须指向父 Objective 的 Project。KeyMatter Page 至少记录：

- 季度和 KR 稳定引用；
- KR 当前定义；
- 全部 Metric 衡量口径；
- 全部 Point 的稳定引用和标题；
- 全部结构化 Owner 的人物引用。

世界模型完整保存所有仍成立的 KeyMatter，不存在“全库最多 10 个”的容量限制。主动巡检可每轮优先最近 10 个，那是注意力窗口，不是 Ontology 存储边界。

### 4.3 Point 到 KeyMatter

默认写 `okr_point --advances--> 父 KR KeyMatter`。若 Point 本身是需要独立维护状态、聚合多条证据的长期现实事项，创建或复用同 Project 下的独立 KeyMatter，并写 `maps_to`。不能把 Point 只留为前端虚拟节点。

### 4.4 Owner 到人物

按 `open_id` 对季度内 Owner 去重建实体，但按出现项建关系：

1. 与 Principal 身份相同则复用 Principal。
2. 已有相同 `open_id` 的 Person 则复用。
3. 其余 Owner 全部创建 Person；显示名来自结构化 OKR Owner，资料不足时 department/title 可空。
4. 每个 KR/Point Owner 出现项分别写 `owned_by`。evidence 必须包含 `owner_open_id`，供 occurrence 级审计。

Owner 的原生字段仍是责任真源；`owned_by` 是它在现实人物 Ontology 中的可查询投影，不是第二份可编辑 Owner。UI 不得用动态 Owner 虚拟边掩盖缺失的强关系。

## 5. 规范关系和 evidence

只持久化以下方向：

```text
okr_objective --maps_to--> project
okr_kr        --maps_to--> key_matter
okr_point     --advances|maps_to--> key_matter
okr_kr        --owned_by--> principal|person
okr_point     --owned_by--> principal|person
```

Project 与 KeyMatter 的归属由 `KeyMatter.ProjectID` 表达，不重复写 `belongs_to`。OKR 原生的 Objective→KR→Point 层级也不写 EntityRelation。每条定义投影关系使用 `confidence=1`、非空 `confirmed_at`，evidence 至少包含：

```json
{
  "source": "okr-world-projector",
  "basis": "OKR 定义中的目标、拆解或负责人是本次现实承接关系的权威依据",
  "refs": ["okr_kr:<id>", "key_matter:<id>"],
  "quarter": "<YYYY-Qn>",
  "owner_open_id": "<owned_by only>",
  "observed_at": "<RFC3339>"
}
```

## 6. 执行和幂等

产品入口创建普通 `agent_task`，冻结 `quarter=whole_quarter` 和完整验收条件。相同季度已有非终态任务时复用，避免并发写同一范围。Agent 按 manifest 顺序逐个 Objective 执行：

1. 读取 Objective 完整分片。
2. 创建/复用 Project 并回读。
3. 对每个 KR 创建/复用 KeyMatter、更新 Page、写关系并回读。
4. 对每个 Point 写承接关系并回读。
5. 对每个 Owner 创建/复用人物、写 occurrence 关系并回读。
6. 每完成一个 Objective 写 Task 进度。
7. 最后重新运行 `projection-audit` 对账。

EntityRelation 按五元组 upsert。实体创建前先查询，创建后立即读取真实 ID；Page 使用 CAS。中途失败立即停止并保留已成功写入的数据，下一次从真实现状幂等续跑，不做跨步骤事务回滚。

## 7. 机械审计

`scripts/okr-module-tools projection-audit` 必须用关系 API cursor 读取全部页，并只把下列已确认关系算作覆盖：

| 审计项 | 完成条件 |
|---|---|
| Objective | outgoing `maps_to project` |
| KR | outgoing `maps_to key_matter` |
| Point | outgoing `maps_to/advances key_matter` |
| Owner occurrence | 同一 OKR 节点 outgoing `owned_by principal/person`，且 evidence 的 `owner_open_id` 相同 |

任意其它边、未确认边、反向虚拟边或 OKR 原生层级都不能冒充覆盖。审计同时报告孤儿关系、目标类型分布、每个 Objective 的缺口和 Owner 未解析明细。

## 8. UI 和世界地图

“OKR 插件 → 世界关联”显示上述四项严格覆盖。未覆盖就是待完成，不再显示为“无关系也可能正常”。点击“投影整个季度”时，确认文案明确告知会建立完整 Project、KeyMatter、Person 和 Owner 强关系。

世界地图的 Objective/KR/Point 节点仍由 OKR 原生定义读时生成；现实 Project、KeyMatter、Person/Principal 和跨层边必须来自世界实体与 EntityRelation。地图不得合成 `okr_owner` 虚拟人物，也不得从原生 Owner 动态补一条看似已落地的负责人边。

当 `okr` 插件在当前运行时启用时，2D 世界地图默认进入“OKR 全景”，以当前季度完整的 Objective → KR → Point 为骨架，并接入已经持久化确认的 Project、KeyMatter、Person/Principal。整季度概览本身必须包含全部 Point；选择单个 Objective 只用于聚焦，不得作为延迟补齐数据的手段。用户仍可切回原有“现实世界”视图。插件未启用时不请求 OKR 接口，也不展示 OKR Lens。

## 9. 完成协议

成功必须同时满足：

- Objective 覆盖等于 manifest Objective 总数；
- KR 覆盖等于 manifest KR 总数；
- Point 覆盖等于 manifest Point 总数；
- Owner occurrence 覆盖等于 KR + Point Owner occurrence 总数；
- 每个唯一 Owner `open_id` 都能解析为 Principal 或 Person；
- 每个 KR KeyMatter 归属父 Objective Project；
- 每个 KR 的 Metric、Point 和人物引用已经写入 KeyMatter Page；
- 所有新增/复用实体和关系均已回读，不存在孤儿关系；
- 未修改正式 Progress、未生成 WorldProgress、未发送消息或产生外部 effect。

任一项未满足都不能报告“整季度投影完成”。最终输出使用宽松 JSON `schema=okr_world_projection_result.v2`，至少包含季度、manifest totals、实体创建/复用计数、四类关系覆盖、未覆盖稳定 ID、未解析 Owner 和孤儿关系。
