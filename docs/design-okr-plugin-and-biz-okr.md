# 通用 OKR 插件与 Biz OKR 拆分设计

> Status: phase-1-implemented; phases 2-4 planned
> Authority: normative
> Last verified: 2026-09-06, working tree based on d917713
> Current implementation: [OKR 模块当前实现](modules/06-okr.md)

本文保留已实施拆分的设计理由和阶段二至四的后续方向。日常维护、排障和判断当前系统已经具备什么能力时，以“OKR 模块当前实现”及对应代码真源为准。

## 1. 结论

当前混合在一个实现中的 OKR 能力拆成两个内置业务模块：

```text
Jarvis 核心与世界模型
        ↑ 只通过工具、Skill 和通用 EntityRelation / WorldProgress 协作
通用 OKR 插件（module: okr）
        ↑ 提供稳定的 OKR 领域能力
Biz OKR（module: biz-okr，requires: okr）
        ↑ 提供当前组织使用的打标、Plan、Review、周报和自动化体验
```

- `okr` 是可复用的通用 OKR 插件：拥有 O、KR、Metric、Point、周次、正式 Progress 和周期指标。
- `biz-okr` 是当前业务包装：拥有标签、OKR Plan、Preview/Review、评论、评分、Follow-up、催填、Meego 关联、飞书业务身份和业务 Prompt。
- Jarvis 世界模型不复制 OKR 数据，也不导入 OKR 领域 DTO；模块启用后，由原子工具和 Skill 把有证据的语义投影到通用关系、事实页和世界进展中。
- 当前 `data/okr/okr.db`、既有表名、主键和历史数据全部原地复用。本次拆分不是数据库搬家。
- 当前 Biz OKR 中的 OKR、每周进展、历史记录和页面能力一个都不能丢。

这里的“插件”是产品语义。第一阶段使用现有 `internal/appmodule` 承载业务数据库、API 和 UI 生命周期，不把 OKR 塞进 `internal/plugin`。后者当前是 Codebase、Meego、Oncall 一类“授权 + 调度 + Skill + Clue”的采集器运行时，不适合拥有 OKR 业务表和交互页面。

## 2. 目标与非目标

### 2.1 目标

1. 让通用 OKR 在不启用 Biz 业务时也能独立保存、查询和维护完整 OKR 与正式进展。
2. 保留 Biz OKR 当前全部产品能力和历史数据，只调整语义所有权与模块门禁。
3. 让 Jarvis 在启用 OKR 后获得 OKR 原子工具与 Skills，而不是在核心代码中写 OKR 专用流程。
4. 允许 Agent 基于世界证据形成独立进展判断，并在未来把候选正式进展交给人确认后回填 OKR。
5. 让 Biz OKR 的业务 Prompt、页面和自动化可以继续演化，而不污染通用 OKR 领域。
6. 保持扩展数据对模型友好：机器硬边界结构化，语义内容继续使用文本或宽松 JSON。

### 2.2 非目标

- 不设计动态加载 Go 代码的第三方插件 SDK。
- 不新建第二套 OKR 表、万能 `entity` 表或第二个 OKR 数据库。
- 不把 Objective、KR 或人工 Progress 物化为 Jarvis Task。
- 不让 Jarvis 自动覆盖人工填写的正式 OKR Progress。
- 不按标题相似度自动合并 OKR、Project 或 KeyMatter。
- 不在 Go 中写死“如何拆 OKR、如何判断进展、何时回填”的业务工作流。
- 不为了目录整齐而在第一阶段大规模搬表、改表名或清理历史字段。

## 3. 为什么是两层而不是一个模块

通用 OKR 和 Biz OKR 的生命周期、复用范围与变化原因不同：

| 维度 | 通用 OKR 插件 | Biz OKR |
|---|---|---|
| 服务对象 | 任意使用 OKR 的组织 | 当前 Biz 业务 |
| 稳定语义 | O、KR、指标、拆解、正式进展 | 标签口径、Plan、Review、周报展示、催填、Meego |
| 数据真源 | OKR 领域表 | Biz 业务表及业务字段 |
| Agent 能力 | 通用查询、拆解、进展 CRUD | 业务打标、评审、报告、催填与业务编排 |
| 页面 | 通用 OKR 基础能力 | 当前完整 OKR Tab 与业务工作台 |
| 依赖关系 | 不依赖 Biz | 必须依赖 `okr` |

如果继续放在一个模块中，任何组织专属标签、评审模板或外部系统都会成为通用 OKR 的隐式前提；如果把所有东西都重新实现成一套“纯插件”，又会复制现有表、API 和成熟页面。两层结构保留已有投入，同时把未来复用边界建立起来。

## 4. 语义所有权

### 4.1 通用 OKR 插件拥有的对象

```text
Objective
└── Key Result
    ├── Metric
    ├── Point
    └── Progress (按周、可版本控制)

Week
└── Weekly KR Core (周期指标快照)
```

对应现有存储优先原地复用：

- `okr_workspace_objective`
- `okr_workspace_kr`
- `okr_workspace_metric`
- `okr_workspace_point`
- `okr_workspace_progress`
- `okr_workspace_week`
- `okr_workspace_weekly_kr_core`
- 现有同步游标、同步状态及通用成员身份映射

正式 Progress 属于通用 OKR，因为“一个 Point 在某周的正式状态、文字、附件和版本”是 OKR 领域本身的能力，并不依赖 Biz 的评审与催填方式。

### 4.2 Biz OKR 拥有的对象

- KR / Point 的 Biz 标签与打标流程
- OKR Plan
- OKR Preview AI Review
- 周报业务布局和组织口径
- 评论、评分、Follow-up
- 催填批次、缺失人员快照和提醒策略
- Meego 关联与 Biz 特有的数据聚合
- 对外飞书登录身份、业务范围与用户授权
- Report A/B/C、研发对齐等业务 Prompt 与 Agent 编排

Biz 可以读取和调用通用 OKR，但不能成为 Objective、KR 或正式 Progress 的第二真源。

### 4.3 Jarvis 世界模型拥有的对象

- Principal、Person、Project、KeyMatter、Group、Resource 等通用实体
- `EntityRelation`：有证据的跨模块强关系
- Page / Summary：实体当前长期认知
- Fact / TaskEvent：发生过的事实和执行历史
- `WorldProgress`：Jarvis 对某个开放主体、某一观察时间的独立进展判断

Jarvis 世界模型可以依赖已启用 OKR 插件提供的工具能力，但核心存储和核心流水线不能依赖 OKR DTO、Biz 标签或固定业务路由。

## 5. 两种 Progress 必须同时存在

正式 OKR Progress 与 WorldProgress 表面都叫“进展”，但回答的问题不同，不能互相替代。

| 维度 | 正式 OKR Progress | Jarvis WorldProgress |
|---|---|---|
| 回答 | 人在 OKR 产品里正式填了什么 | 根据现实证据，现在实际上进展如何 |
| 所有者 | 通用 OKR 插件 | Jarvis 世界模型 |
| 当前写入者 | 人 | Agent 或世界模型维护流程 |
| 状态 | 组织认可、可展示的正式口径 | 可演化的独立判断 |
| 主体 | OKR Point | 开放 EntityRef，首个适配器为 `okr_point` |
| 证据 | 文本、文档、图片、填写人、版本 | 消息、Meego、Fact、Task、资料等证据引用 |
| 自动同步 | 禁止自动覆盖 | 可以持续刷新自身判断 |

两条时间线可以这样表达：

```text
正式轴：KRProgress(week=W36, source=human, version=3)
现实轴：WorldProgress(subject=okr_point:18, observed_at=..., evidence=[...])
```

短期流程：

1. 人继续在 Biz OKR 当前页面填写正式 Progress。
2. Jarvis 通过 Meego、消息、Fact 等现实证据维护 WorldProgress。
3. 页面可以并排展示“正式进展”和“Jarvis 观察”，但不把后者冒充为正式提交。

未来 AI 回填流程：

1. Agent 读取正式 Progress、WorldProgress 和证据。
2. Agent 生成一份自然语言候选内容，说明依据和仍不确定的部分。
3. 人确认后，Agent 调用通用 OKR 的 Progress 原子写工具。
4. 正式表记录 `source`、`created_by`、`updated_by`、`version` 与 `needs_review`。
5. 写入成功后回读验证；失败则 fail-fast，不反向修改 WorldProgress。

这不是 Go 自动双写，也不需要新增固定“AI 草稿状态机”。候选内容和判断由 Agent/Skill 负责，数据库只保证 CAS、版本、主体、身份和审计字段。

## 6. OKR 与世界模型如何连接

### 6.1 统一引用

跨模块引用使用稳定 EntityRef：

```text
okr_objective:<stable-id>
okr_kr:<stable-id>
okr_metric:<stable-id>
okr_point:<stable-id>
project:<id>
key_matter:<id>
person:<id>
principal:<id>
```

名称只用于显示，不能作为身份。

### 6.2 不复制 OKR 内部关系

Objective 包含 KR、KR 包含 Metric/Point、Owner 属于哪个 KR，这些已经由 OKR 原生字段或外键表达。World Graph 查询时可以从 OKR API 派生边，但不重复写进 `entity_relation`。

### 6.3 EntityRelation 只保存跨模块强关系

例如：

```text
okr_kr:kr1       --maps_to / mapped_from------> project:7
okr_point:p1     --maps_to / mapped_from-----> key_matter:31
project:7        --advances / advanced_by----> okr_kr:kr1
key_matter:31    --depends_on / required_by--> resource:19
```

关系只保存一个方向；查询和 UI 根据以下关系词典生成反向显示名：`belongs_to / contains`、`owned_by / owns`、`participates_in / has_participant`、`depends_on / required_by`、`advances / advanced_by`、`maps_to / mapped_from`、`derived_from / produces`。`maps_to` 表示跨领域对象的明确对应，`advances` 表示现实对象对目标产生了实际推进。数据库仍允许开放 token，未知关系按原始方向显示。每条关系应保留来源、依据、观察时间、置信度和确认时间。

`okr-world-projector` 是 OKR 到世界模型的语义桥，它必须：

- 通过 OKR 原子工具读取数据，不直接查询模块数据库；
- 只建立有明确证据的跨模块关系；
- 不按标题相似度硬绑；
- 不复制 OKR 内部层级和 Owner；
- 不读取或同步周报 Progress；
- 幂等写入并在写后回读。

正式进展与世界进展的调查、对照和候选回填由独立 Skill 负责，不能塞进关系投影 Skill。当前该适配器是属于 `biz-okr` 的 `weekly-report-progress-sync`；只启用通用 `okr` 时关系投影仍可独立完成，但不会自动获得 Biz 周报与 Meego 的证据巡检能力。

### 6.4 Page 引用仍是弱关系

Markdown 中的：

```markdown
本周围绕 [Bax AM 助手](project:48) 推进。
```

只表示正文引用。它可以被图谱作为淡色弱边展示，但不能自动解释为 Owner、依赖或交付关系。强关系使用 `EntityRelation`。

当前 Page 引用解析器只支持核心世界实体和正整数 ID；`okr_objective:<stable-id>`、`okr_kr:<stable-id>`、`okr_point:<stable-id>` 现阶段只能作为普通文本标识。OKR 到现实实体的机器可查询连接必须使用 `EntityRelation`；等统一 Entity Resolver 落地后再扩展 Page 引用，不在投影 Skill 中假装已经支持。

## 7. 模块生命周期与开关

第一阶段沿用 `internal/appmodule`：

```yaml
modules:
  - key: okr
    enabled: true
  - key: biz-okr
    enabled: true
```

依赖是：

```text
biz-okr requires okr
```

开关语义：

| `okr` | `biz-okr` | 效果 |
|---|---|---|
| off | off | 不迁移、不注册 OKR 业务 API/页面/Skill |
| on | off | 提供通用 OKR、周次、正式 Progress、周期指标和世界投影能力 |
| on | on | 在通用 OKR 之上提供完整 Biz OKR 产品 |
| off | on | 配置错误，启动时 fail-fast |

关闭模块只停止能力暴露和后台行为，不删除表、不清理数据。重新开启后继续使用原数据。

## 8. API、工具与 Skill 边界

### 8.1 API 所有权

目标路由：

```text
/api/okr/*                 通用 OKR
/api/biz-okr/*          Biz 业务能力
```

通用 OKR 应包含：

- Objective / KR / Metric / Point 查询和维护
- Week 查询和维护
- Point Progress CRUD、CAS 和排序
- Weekly KR Core 读写

Biz OKR 应包含：

- 标签、Plan、Review、评论、评分、Follow-up
- 催填、Meego、Preview Review、业务配置与授权

第一阶段应在同一个变更中更新前端、脚本和测试，不长期维持 `/api/weekly-report` 与新路径两套真源。为了降低迁移风险，底层可以暂时复用同一个 `okrworkspace.Service`；API 依赖必须先收窄成两个接口面，后续再拆 package。

### 8.2 原子工具

通用工具统一由 `scripts/okr-module-tools` 暴露：

```text
objective / kr / metric / point 查询
week 查询
progress list/get/create/update/delete/reorder
weekly-kr-core get/replace
```

Biz 工具应使用独立入口（目标为 `scripts/biz-okr-tools`）：

```text
tag / plan / review / comment / score / follow-up
reminder / meego / biz prompt
```

工具只提供稳定、可组合的原子能力，不在脚本或 Go handler 中固化完整业务流程。所有写工具保留必要的身份、版本和幂等边界，并在写后回读。

### 8.3 Skill 门禁

Skill frontmatter 的 `module` 是 Agent 能力门禁：

| Skill | 模块 | 职责 |
|---|---|---|
| `okr-world-projector` | `okr` | OKR 与世界模型的强关系投影 |
| 通用 OKR 维护/拆解 Skill | `okr` | 使用原子工具维护 O/KR/Point/Progress |
| `okr-agent-orchestrator` | `biz-okr` | Biz 标签、Plan、Review、报告等动态业务 Prompt |
| `weekly-report-progress-sync` | `biz-okr`（第一阶段） | 当前混合 Biz 的 Meego/周报调查，后续按证据再拆 |
| `weekly-report-reminder` | `biz-okr` | Biz 周报催填 |

模块关闭后，对应 Skill 不进入 Agent catalog/content；ScheduledTask 根据 `context_snapshot.module` 安全跳过。已有 `weekly-report` 绑定通过一次性迁移改成 `biz-okr`，不丢失任务历史。历史版本使用的 `agency-okr` 配置键和 ScheduledTask 模块绑定也在启动时一次性迁移到 `biz-okr`；旧名称不再作为现行模块、API、工具或产品文案暴露。

### 8.4 Prompt 所有权

- 通用 OKR 概念和工具说明由 tool catalog 与通用 OKR Skill 维护。
- Biz 的标签口径、评审模板、报告结构和填写偏好由 Biz Skill 或可编辑业务 Prompt 维护。
- Jarvis 核心 prompt 不复制 OKR 手册；只说明启用的业务能力会通过工具目录与 Skills 提供。

## 9. 代码组织目标

最终目标不是一次性搬目录，而是先切断接口所有权，再逐步移动实现：

```text
internal/
  okr/                       # 通用 OKR 领域（目标目录）
    domain/
    service/
  bizokr/                 # Biz 业务层（目标目录）
    review/
    auth/
    service/
  appmodule/                 # okr / biz-okr 生命周期
  worldprogress/             # Jarvis 核心，开放 SubjectValidator
  entityrelation/            # Jarvis 核心

web/src/
  okr/                       # 通用组件与 API client
  biz-okr/                # 当前完整 Biz OKR 页面与业务组件
```

第一阶段可以继续复用 `internal/okrworkspace` 和当前表，以避免在同一变更中同时承担数据迁移、package 搬家、API 改名和页面重构。判断是否真正解耦的标准不是目录名，而是：

1. `okr` 独立开启时能否工作；
2. Biz API 是否只在 `biz-okr` 开启时注册；
3. 通用 OKR 是否不依赖 Biz service/interface；
4. Agent 是否只通过启用模块的工具和 Skill 获得能力；
5. 关闭 Biz 是否不会丢失或修改任何数据。

## 10. 数据与删除边界

当前一个大 Service 中存在跨所有权级联：删除 Week 或 KR 时会同时删除 Progress、周期指标、标签、评论、评分、Meego 和催填数据。为保证当前行为和历史数据安全，分阶段处理：

1. 第一阶段保留现有数据库级和 Service 级行为，不修改真实数据。
2. 先通过窄接口明确哪些调用属于通用 OKR、哪些属于 Biz。
3. 后续把“删除 Biz 周报周期”的编排放在 Biz 层，由它依次调用通用 OKR 删除能力和 Biz 清理能力。
4. 不引入跨模块数据库事务；任一步失败立即返回，下次可幂等重跑。
5. 在拆出清理逻辑前必须用现有数据副本做回归测试，证明没有孤儿记录和额外删除。

`KRPoint.MeegoWorkItemID` / `MeegoURL` 是已知的 Biz 字段泄漏。第一阶段保留列和数据，通过 API/接口所有权隔离；等存在第二种通用外部工作项来源时，再以证据引用或开放 relation 重构，不为假想扩展提前建抽象。

## 11. 分阶段实施

### 阶段一：建立模块和接口边界

- 新增 `biz-okr` 模块并声明 `requires: [okr]`。
- 将原 `weekly-report` 模块配置、Skill 和 ScheduledTask 绑定迁移到 `biz-okr`。
- 让 `okr` 独立拥有 Week、正式 Progress 和 Weekly KR Core 的迁移与 API。
- 将 Biz API 注册归 `biz-okr`，保持当前完整页面与数据。
- 把 Progress 原子命令迁入 `okr-module-tools`，Biz 命令保留独立入口。
- 更新前端门禁、API client 和边界测试。

### 阶段二：收窄 Service 与前端组件

- 从大 `okrworkspace.Service` 提取通用 OKR 与 Biz 两组小接口。
- 将 handlers 按所有权拆文件/package，避免 Biz handler 直接调用无关通用内部实现。
- 把通用 OKR 组件与 Biz 页面壳分开；Biz 页面组合通用组件。
- 将 `internal/okrreview`、Biz auth 和业务 Prompt 明确收进 Biz 目录。

### 阶段三：Agent 维护正式进展

- 为通用 Progress 提供完备的读、创建、CAS 更新、附件与回读工具。
- 增加“WorldProgress + 现实证据 → 正式 Progress 候选”的 Skill。
- 初期所有候选由人确认；稳定后再由 Prompt/规则决定哪些低风险内容可直接写入。
- 所有写入保留 `source`、操作者、版本、证据摘要和审阅标记。

### 阶段四：统一 World Graph 读取

- 聚合 OKR 原生结构、Jarvis 实体、EntityRelation、Page 引用和两种进展。
- 图谱是可重建的只读投影，不增加第二套 `world_node` 真源。
- 3D 图、搜索和 Agent 共用同一读取 DTO；写操作仍路由到真正所有者。

## 12. 验收标准

### 数据安全

- 升级前后现有表数量、关键记录数量和主键保持一致。
- 所有历史 Progress 文本、附件、图片、版本、来源、审阅状态均可读取。
- 关闭或重新开启 Biz 不执行 delete、truncate 或数据复制。

### 模块边界

- `okr=on, biz-okr=off` 可以访问 O/KR/Point、Week、Progress 和周期指标。
- Biz API、Skill、ScheduledTask 和页面只在 `biz-okr=on` 时暴露。
- `okr=off, biz-okr=on` 启动失败并明确指出缺少依赖。
- 世界模型核心测试不需要构造 Biz 业务对象。

### 产品完整性

- 当前 OKR Tab 中的 OKR、标签、Plan、Review、周报、评论、评分、Follow-up、催填和 Meego 能力都仍可使用。
- 正式 Progress 的增删改查、排序、图片和文档附件行为与拆分前一致。
- 原有 ScheduledTask 历史仍保留，新运行使用 `biz-okr` 门禁。

### Agent-first

- OKR 拆解、关系判断、进展归纳和是否回填由 Skill/Prompt 决定。
- Go 代码只负责模块依赖、API 注册、权限、版本、幂等、调度和参数校验。
- 新增业务口径不要求修改 Go 枚举或在核心流水线增加来源分支。

## 13. 最终原则

这次拆分不是把同一坨代码改两个目录名，而是建立三条长期稳定的语义边界：

> 通用 OKR 保存组织认可的目标结构和正式进展；Biz OKR 提供当前业务的操作与展示方式；Jarvis 世界模型保存现实世界的实体、关系、事实和独立进展判断。

三者通过稳定引用、原子工具、Skills 和有证据的关系协作。各自保留唯一真源，既能完整承接今天的业务，也为以后由 Agent 调查、生成候选并回填正式 OKR 留出自然演进路径。
