# 整季度 OKR 世界地图与稀疏现实映射

> Status: implemented
> Authority: normative for OKR world-map projection
> Last verified: 2026-09-07, current worktree
> Scope: 通用 OKR 插件、`okr-world-projector` Skill、世界地图 OKR Lens

## 1. 完整的是什么

世界地图需要完整展示所选季度的 OKR 原生结构：

```text
Objective
└── KR
    └── Point
```

这套骨架直接读取通用 OKR Board，在前端按原生层级生成。Objective、KR、Point 即使没有任何现实映射，也必须出现在 OKR Lens 中。它们不需要先复制成 Jarvis 的 Project、KeyMatter、Person 或常驻 Task。

现实关系是另一层信息，并且天然可以稀疏：

```text
okr_kr    --maps_to--> Project
okr_point --maps_to--> KeyMatter
Project   --advances--> okr_kr
KeyMatter --advances--> okr_kr
```

只有已经确认、有证据且目标实体可解析的关系才进入地图。未关联表示“目前没有可靠的现实关系”，不是投影失败，也不是需要自动补齐的数据缺陷。

## 2. 所有权与边界

- Objective、KR、Metric、Point、Owner 和正式 Progress 的唯一真源是通用 OKR 插件。
- Project、KeyMatter、Principal、Person、Group、Resource、Page 和 EntityRelation 的唯一真源是 Jarvis 世界模型。
- Biz OKR 是依赖通用 OKR 的业务应用，不拥有第二份 OKR 或世界映射。
- OKR 原生层级和 Owner 在读取时展示，不复制进 `entity_relation`。
- OKR 定义证明目标和责任定义存在，但不能单独证明一个现实实体或跨模块关系存在。
- 投影不读取或修改正式 Progress，不生成 WorldProgress，不发送消息，不写 Meego 或其它外部系统。
- 世界地图是只读组合视图，不成为第三份事实真源。

## 3. 调查范围

`okr-world-projector` 通过原子工具读取 OKR：

```bash
scripts/okr-module-tools scope
scripts/okr-module-tools board --quarter '<YYYY-Qn>'
```

默认只调查用户指定的 Objective、KR、Point 或当前相关范围。用户明确要求整个季度时才遍历全量；“整个季度”只表示调查范围完整，不表示每个节点必须有现实关系。模块关闭、季度不存在或 Board 为空时，直接报告真实状态。

## 4. 现实实体准入

投影需求不能降低世界模型的实体准入标准：

- **Project**：现实中确有跨周持续、边界明确的项目时才创建或复用。Objective/KR 标题本身不是项目证明。
- **KeyMatter**：现实事项需要跨多次动作持续记忆、检查状态或聚合多条证据时才创建。Point 是目标拆解，不自动等于 KeyMatter。
- **Person**：具备稳定身份且后续确需按人维护长期事实时才创建。仅出现在 Owner 列表中不够。
- **Group**：只复用采集层已经发现的群；投影不创建群。
- **Resource**：只有后续会反复使用的权威文档、仓库、看板或链接才创建。

如果去掉 OKR 映射需求后，一个实体就没有独立存在价值，则不创建。创建前先查询去重，创建后立即回读真实 ID；Page 只写已核实的当前事实，不能把目标措辞写成已经发生的进展。

## 5. 关系与证据

关系使用现有开放 relation token，方向按事实语义选择，不为图形布局强制统一。常见关系包括 `maps_to`、`advances`、`depends_on` 和在确有跨层查询需求时的 `owned_by`。

每条新关系至少记录：

- 关系的直接事实依据；
- 可追溯的稳定证据引用；
- 观察时间；
- 与证据质量相称的 confidence；
- 已确认关系的 `confirmed_at`。

没有稳定证据时不写低置信度占位边。写入后从任一端读取双向一跳邻域，确认关系可见。相同五元组幂等刷新，不产生重复边；暂时找不到新依据时不自动删除旧关系，只有直接证据确认失效且任务范围包含刷新时才删除。

## 6. 世界地图读取

当 `okr` 插件启用时，2D 世界地图提供 OKR Lens：

1. 读取所选季度的通用 OKR Board，生成全部 Objective、KR、Point 和原生层级边。
2. 查询这些 OKR 节点作为 source 或 target 的 EntityRelation。
3. 只加载关系指向且能够解析的现实 Page。
4. 把已确认的稀疏现实关系叠加到 OKR 骨架。
5. 某个现实 Page 删除、损坏或暂时无法读取时，跳过该关系目标并提示，不影响 OKR 骨架。

地图不为坏引用合成虚假的现实节点，也不为缺少关系合成 `okr_owner`、Project 或 KeyMatter。用户可切回现实世界 Lens；插件未启用时不请求 OKR 接口，也不展示 OKR Lens。

## 7. OKR 插件页面

“OKR 插件 → 世界关联”是只读关系浏览页：

- 展示当前季度已经确认的跨模块关系；
- 同时读取 OKR 节点作为 source 和 target 的边；
- 明确提示未关联是正常状态；
- 提供进入世界地图的入口；
- 不显示覆盖率，不创建投影 Task，不隐含批量写世界模型。

需要新增或复核关系时，由用户或 Agent 明确调用 `okr-world-projector`，由 Skill 按证据逐项调查。

## 8. 完成标准

一次调查完成应满足：

- 指定范围内的 OKR 结构已通过模块工具读取；
- OKR 原生层级与 Owner 没有为了图展示被复制；
- 每个新建现实实体都有独立现实意义；
- 每条新增或刷新的强关系有直接证据且已回读；
- 未确认或无需映射的节点被如实保留为未关联；
- 没有修改正式 Progress、WorldProgress 或外部系统。

完成结果说明调查范围、确认关系、实际创建的现实实体及独立理由、未确认候选和证据缺口，不承诺机械覆盖率。
