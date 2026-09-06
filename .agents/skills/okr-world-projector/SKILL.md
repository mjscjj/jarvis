---
name: okr-world-projector
description: 将已经填写好的 OKR 在 Jarvis 世界模型中运行起来：读取 O、KR、Point、指标和负责人，按证据识别或补充 Project、KeyMatter、Person、Group、Resource 与外部工作项，建立跨模块强关系，并为后续进展巡检准备稳定锚点。用于“把 OKR 跑起来”“把 OKR 拆到或关联到世界模型”“初始化或刷新 OKR 世界映射”；不复制 OKR 真源、不修改正式周进展、不按标题相似度硬绑，也不为每个 OKR 自动创建 Task 或世界实体。
module: okr
---

# 让填写好的 OKR 在现实世界中运行

把 OKR 看成目标层，把 Jarvis 看成现实层：OKR 回答“承诺实现什么”，Project、KeyMatter、Person、Group、Resource 和外部工作项回答“现实中由谁、通过什么、在哪里推进”。本 Skill 负责建立和刷新两层之间的稳定连接。

一次执行的完成状态不是“生成了多少节点”，而是：目标结构可解析，重要目标有现实承接，所有强关系有直接证据，未确认项被明确保留，后续进展巡检可以沿关系找到证据。

## 硬边界

- OKR 模块是 Objective、KR、Metric、Point、Owner 和正式 Progress 的唯一真源。只通过 `scripts/okr-module-tools` 读取，不直接查询或修改 `okr_workspace_*` 表。
- 不把 O、KR、Point 复制为 Project、KeyMatter 或 Task；它们直接以 `okr_objective:<id>`、`okr_kr:<id>`、`okr_point:<id>` 参与世界图。
- 本 Skill 不读取或修改正式周进展，也不生成 WorldProgress；启用 Biz OKR 时可把进展闭环交给 `weekly-report-progress-sync`。
- 不为了填满图而创建实体。标题相似、同一 Owner、在同一个群出现都只能形成候选，不能单独构成强关系。
- 不把所有 Owner 批量创建成 Person。OKR 原生 Owner 通过 `open_id` 动态解析；只有对 principal 的长期世界确实重要的人才进入 Person。
- 不为 OKR 创建常驻 Task。Task 只表示本次 Agent 执行或真实待办。
- 不向外部系统写入，不发送消息，不改 Meego；本 Skill 只读取证据并维护 Jarvis 内部实体、Page 和关系。
- 所有写入逐项回读。中途失败立即停止；重跑前先查询现状，不依靠事务、回滚或重复创建。

## 关系放在哪里

先使用已有真源，只有跨模块强关系才写 `entity_relation`：

| 关系 | 唯一真源 | 本 Skill 的处理 |
|---|---|---|
| Objective 包含 KR，KR 包含 Metric/Point | OKR 模块 | 读取时派生，不复制 |
| KR/Point 的正式 Owner | OKR 模块的结构化 `open_id` | 解析为 Principal/Person 展示，不复制 |
| KeyMatter 属于 Project | `KeyMatter.ProjectID` | 使用实体字段，不复制 |
| Group 属于 Project | `Group.ProjectID` | 使用 `update-group` 绑定，不复制 |
| Resource 属于 Project/Person | ManagedResource 字段 | 使用资源字段，不复制 |
| Point 明确绑定 Meego WorkItem | 绑定来源自己的稳定 ID | 读取时派生；只有跨来源查询确有需要时才写映射 |
| OKR 与现实实体的对应、贡献、依赖 | 无共同外键 | 写入 `entity_relation` |
| Page 正文提到另一个核心世界实体 | Markdown `[名称](type:id)` | 仅作为弱引用，不提升为强关系 |

关系词只从以下七组中选择；只保存事实成立的一个方向，读取层负责反向显示：

```text
belongs_to / contains
owned_by / owns
participates_in / has_participant
depends_on / required_by
advances / advanced_by
maps_to / mapped_from
derived_from / produces
```

优先使用以下几条主桥：

```text
okr_kr    --maps_to-->  project       # KR 与现实项目有稳定的一一或主承接对应
okr_point --maps_to-->  key_matter    # Point 与长期现实事项明确对应
project   --advances--> okr_kr        # 项目确实对 KR 产生贡献，不要求一一对应
key_matter--advances--> okr_kr        # 事项确实推进 KR
```

不要默认建立 `okr_kr belongs_to project`。`belongs_to` 只表示稳定结构归属；“这个项目帮助实现 KR”应使用 `advances`，“这两者是目标层与现实层的同一承接对象”应使用 `maps_to`。

## 1. 确定运行范围

读取当前周期和完整定义：

```bash
scripts/okr-module-tools scope
scripts/okr-module-tools board --quarter '<quarter>'
```

整理 O、KR、Metric、Point、Owner 的稳定 ID；Metric 作为判断 KR 的衡量口径，不默认变成独立世界实体。

默认处理用户指定的 O、KR 或当前活跃范围。用户明确要求“运行整个季度”时才遍历全量；数据很大时按 Objective 分批执行，但同一次交付中汇总完整覆盖情况。

如果模块未启用、没有季度或没有 OKR，停止并报告真实原因。不要绕过模块 API 查数据库。

## 2. 读取已有世界，不先创建

先查 Principal 和现有现实实体：

```bash
jarvis-tools get-principal
jarvis-tools list-projects --keyword '<明确项目代号或名称>' --limit 100
jarvis-tools list-key-matters --keyword '<明确事项名>' --limit 100
jarvis-tools get-person --open-id '<owner_open_id>'
jarvis-tools list-groups --keyword '<明确项目或群名>' --limit 100
jarvis-tools query-resources --keyword '<明确项目、文档或仓库名>' --limit 100
jarvis-tools list-relations --source-type '<okr_objective|okr_kr|okr_point>' --source-id '<id>' --limit 100
jarvis-tools list-relations --target-type '<okr_objective|okr_kr|okr_point>' --target-id '<id>' --limit 100
```

需要判断当前状态时，再按候选实体读取 `get-page`、`list-facts`、`query-messages` 或具体资源。先从已有关系、明确项目代号、稳定 URL/ID 和 Page 引用下钻；不要从全租户宽泛搜索开始。

将每个待处理 KR 整理成一份工作表：

```text
OKR 节点 | 正式 Owner | 候选 Project | 候选 KeyMatter | 群/资源/外部工作项 | 证据 | 决定
```

`决定` 只允许：复用已有实体、创建现实实体、建立/刷新关系、保留未确认、无须映射。

## 3. 识别现实承接对象

### Principal 与 Person

- Owner `open_id` 等于 Principal 时解析为 `principal:<id>`，不要再创建同名 Person。
- 已存在相同 `open_id` 的 Person 时复用；姓名不同不创建第二个人。
- 只有长期协作、后续需要按人查询或其 Page 会持续维护时才创建 Person。单纯出现在 Owner 列表中不够。
- OKR Owner 表示正式责任；现实中的参与使用 `participates_in`。不要因为是 Owner 就自动断言参与了所有关联 Project/KeyMatter。

### Project

仅当材料表明存在跨周持续、有明确边界的现实项目时创建或复用 Project。Objective/KR 标题本身不是项目证明。优先使用明确项目代号、权威文档、已有群绑定、稳定仓库或用户确认作为证据。

### KeyMatter

仅当某件事需要跨多次动作持续记忆、检查状态或承接多个证据时创建 KeyMatter。Point 是目标拆解，KeyMatter 是现实事项；两者即使标题相同也不是同一条记录。短期动作留给 Task，进展判断留给 WorldProgress。

### Group

Group 由采集层发现，本 Skill 不创建群。找到明确项目群后，先用 `get-group --chat-id` 读取完整记录，再使用 `update-group` 将其 `ProjectID` 指向现实 Project。`update-group` 是完整替换而不是 patch，payload 必须显式携带并默认原样保留 `project_id`、`related_group`、`pinned`、`include_in_memory`、`is_key_group` 五个控制字段；只有当前任务和证据明确要求时才改变监听控制位。写完再次 `get-group` 回读。群里讨论过某个 KR 只在 Page 中用普通文本记录 OKR 稳定引用；仅当存在额外强语义且确需查询时才写 `entity_relation`。

### Resource

只有后续会反复使用的权威文档、仓库、看板或链接才创建 ManagedResource，并优先使用其 `ProjectID`、`PersonID` 或 `link_principal` 字段。普通消息附件不升级为长期资源。

### Meego 和其它插件对象

- 若 Point 已带稳定 `meego_work_item_id`，将其作为来源原生绑定读取，不再复制一条同义边。
- 若工作项来自独立插件查询、没有来源原生绑定且确实需要跨来源检索，可使用 `meego_work_item:<stable-id>` 作为虚拟节点，与 Point 或 KeyMatter 建 `maps_to`；只有插件能够解析该引用时才建立。
- Meego WorkItem 不等于 KeyMatter。只有它确实代表值得长期跟踪的现实事项时才创建 KeyMatter。
- 其它插件实体使用相同原则：来源插件拥有对象与状态，Jarvis 只保存必要的世界实体和跨模块强关系。

## 4. 写入最小现实骨架

确认不存在可复用实体后，才调用 `create-project`、`create-key-matter`、`create-person` 或 `create-resource`。具体 payload 以各命令当前 `--help` 和 API 校验为准，不在 Skill 中复制易漂移字段清单。每次创建后立即用对应 `get-*` 回读，拿真实 ID 再建立下一条连接。

为新建或确认的 Project、KeyMatter、Person、Group、Resource 写最小长期 Page：第一行说明它是什么，正文保留当前稳定范围、明确责任和 OKR 引用，例如：

```markdown
这是承接 OKR「提升交付效率」（okr_kr:kr_123）的长期项目。

- 当前范围：……
- 关键事项：[交付链路改造](key_matter:71)
- 主要讨论空间：[项目核心群](group:8)
```

当前 Page 解析器只把 `principal`、`person`、`project`、`key_matter`、`group`、`resource`、`task`、`todo`、`fact` 加正整数 ID 的 Markdown 链接识别为叙述性弱引用；`okr_kr:kr_123` 目前只是供人和 Agent 阅读的普通文本标识，不会形成 backlink。OKR 与现实实体的可查询连接必须写入 `entity_relation`。不要在 Page 中写尚未发生的进展，也不要把 OKR 定义复制成长篇摘要。更新 Page 前先 `get-page`，使用 CAS 写入；冲突时重新读取并合并。

## 5. 建立有证据的强关系

每条新边至少记录：本 Skill 来源、直接依据、证据引用或稳定 ID、观察时间。`confidence` 由证据质量决定；只有已经确认的关系才设置 `confirmed_at`。

```bash
jarvis-tools create-relation --payload - <<'JSON'
{
  "source_type": "okr_point",
  "source_id": "<point_id>",
  "relation_type": "maps_to",
  "target_type": "key_matter",
  "target_id": "<key_matter_id>",
  "evidence": {
    "source": "okr-world-projector",
    "basis": "<为什么可以确认对应>",
    "refs": ["<稳定证据引用>"],
    "observed_at": "<RFC3339>"
  },
  "confidence": 1,
  "confirmed_at": "<RFC3339>"
}
JSON
```

然后分别从 source 和 target 方向回读：

```bash
jarvis-tools list-relations --source-type okr_point --source-id '<point_id>' --limit 100
jarvis-tools list-relations --target-type key_matter --target-id '<key_matter_id>' --limit 100
```

相同五元组会刷新证据，不应产生重复边。没有稳定证据时不要写“低置信度占位边”，在结果中列为未确认候选即可。不能仅因本轮没找到关系就删除旧边；只有直接证据确认关系已失效且当前任务明确包含刷新时，才删除并记录依据。

## 6. 把 OKR 接入持续运行

关系投影完成后，按用户目标选择：

- 只要求映射、拆解或初始化：到此结束，报告后续巡检尚未启动。
- 要求“把 OKR 跑起来”或立即看现实进展：先尝试 `jarvis-tools get-skill --name weekly-report-progress-sync`。它属于可选的 `biz-okr` 模块；能读取时按其正文执行一次，由它读取人工正式 Progress、Meego/消息等证据并维护 Fact、Page 和 O/KR/Point 的 WorldProgress。因模块关闭而不可用时，关系投影仍然成功，只报告当前未启用 Biz 证据适配器，因此没有生成新的 WorldProgress。其它读取错误按真实错误停止，不伪装成模块关闭。
- 要求持续运行：只有 `weekly-report-progress-sync` 可用时，才用 `list-scheduled-tasks` 查找既有的 OKR 进展巡检。用户已给出周期时才创建或更新 ScheduledTask；没有周期时不猜测，报告需要配置执行频率。不要重复创建同名 active 任务。

关系投影与进展巡检必须保持两个独立职责：稳定定义或现实承接变化时重跑本 Skill；日常变化由进展巡检处理。投影失败不回滚 OKR，也不阻塞人填写正式 Progress。

## 完成检查

- 已回读本轮范围内全部 O、KR、Point 和结构化 Owner，且没有直查模块数据库。
- OKR 原生层级和 Owner 没有复制到 `entity_relation`。
- 创建的每个 Project、KeyMatter、Person 和 Resource 都代表独立现实对象，而不是为 OKR 节点凑数。
- Group 使用已有发现记录和 `ProjectID`，没有由本 Skill 创建。
- 每条强关系都有直接依据并从两端回读；未知项没有被强绑。
- Page 只对当前解析器支持的核心实体使用 `[名称](type:id)` 弱引用；OKR 稳定引用使用普通文本，并由 `entity_relation` 提供机器可查询连接。
- 没有修改正式 Progress、外部系统或发送消息，也没有为 O/KR/Point 创建常驻 Task。
- 交付结果包含：处理范围、复用/创建实体、建立/刷新关系、群和资源绑定、外部对象覆盖、未确认候选、证据缺口，以及进展巡检是否已执行或调度。
