# OKR 模块与世界模型

> Status: current
> Authority: normative
> Last verified: 2026-09-06 @ uncommitted worktree

## 1. 两份状态，各自负责一件事

通用 OKR 插件保存季度、Objective、KR、核心指标、拆解点、负责人、周次和正式 Progress。Biz OKR 在其上保存业务分类与 Focus/P1/P2 标签、Plan、Review、周报展示、评论、评分、Meego 观察和催填批次，并显式依赖通用 OKR。页面按“业务分类标签 → 优先级标签 → Objective 方向 → KR”动态组织，不复制优先级字段，也不按 Objective 标题推断分类。固定材料流程由 Agent Prompt 组装，不另存专用草稿状态机。

负责人是 KR 和具体 Point（策略/产品）各自的独立关系，分别存在 `okr_workspace_kr_owner` 与 `okr_workspace_point_owner`。通用定义窄写走 `PUT /api/okr/krs/:kr_id/definition`；Biz 完整编辑器走 `PUT /api/biz-okr/krs/:kr_id` 组合写入。同一个人可以只出现在其中一级，负责人不用标签表达。

Agent 通过 `biz-okr-tools replace-kr-tags` 调用 `PUT /api/biz-okr/krs/:kr_id/tags` 维护标签，使用 KR 当前版本和完整标签列表；接口仅更新标签及 KR 版本/编辑者，不改其它定义或正式 Progress。标签的增删、分类映射和批量编排由 Agent 判断，沿用现有标签表及结构标签校验。O 不独立存标签，需要按子 KR 表达。

Jarvis 世界模型保存跨来源的认知状态：Person、Project、KeyMatter、当前 Page、历史 Fact、跨模块关系，以及按主体和周期持久化的 WorldProgress。Task 只是一轮执行单元，不属于任何 OKR 层级。

两者不共享业务表、不互存外键，也不在保存页面时同步写入。需要连接时使用通用 `entity_relation`：模块实体 ID 和世界实体 ID 都以字符串保存，关系类型由 Skill 解释。

## 2. 投影由 Agent 完成

`okr-world-projector` Skill 负责把模块层级映射到世界实体：

1. 用 `scripts/okr-module-tools list-objectives/get-objective` 冻结并逐项读取完整季度；
2. 为每个 Objective 创建或复用 Project，为每个 KR 创建或复用归属于该 Project 的 KeyMatter；
3. 将每个 Point 连接到现实 KeyMatter，并把 Metric 写入 KR KeyMatter Page；
4. 按 `open_id` 把每个结构化 Owner 解析为 Principal/Person，对每次 Owner 出现写 `owned_by`；
5. 用 `create-relation` 保存已确认定义投影，并从两端回读；
6. 用 `projection-audit` 验证 Objective、KR、Point、Owner occurrence 均为全覆盖。

OKR 定义本身是目标、拆解和责任归属存在的权威证据；不能以缺少第二份外部材料为由拒绝投影。标题相似仍不能用来把 OKR 误并到一个已有但语义不同的现实对象；若无正确承接实体，则创建新的 Project/KeyMatter。投影只读取稳定的 OKR 定义，不读取周报内容。投影失败不会阻塞周报填写，世界模型失败也不能回滚模块表写入。

## 3. 周进展闭环

`weekly-report-progress-sync` Skill 执行一次只读巡检：

1. 读取 `/api/biz-okr/board` 与已确认关系；
2. 通过 `bytedcli` 或本地已采集消息读取证据；
3. 原始证据用 `append-clue` 幂等进入统一证据流；
4. 客观变化用 `append-fact` 写入最小世界实体；
5. 当前结论用 `get-page` + `update-page` CAS 更新。
6. 证据足够时，用 `get-world-progress` 读取 O、KR 或 Point 与周次的现有判断，再用 `create-world-progress` 或 `update-world-progress` 持久化 Jarvis 的独立判断。

该 Skill 当前属于可选的 `biz-okr` 证据适配器。只启用通用 `okr` 时，`okr-world-projector` 仍能独立建立关系，但不会自动执行 Biz 周报和 Meego 巡检；不能把这种可选能力缺失当成关系投影失败。关系只落一个事实方向，巡检读取 O、KR、Point 的关系时必须同时查询 source 和 target。

`WorldProgress` 不替代人工维护的正式 `KRProgress`，也不自动回填 OKR。当前可为 `okr_objective`、`okr_kr`、`okr_point` 写入，写操作要求通用 `okr` 模块已启用且主体确实存在；模块关闭后仍可读取历史判断。三层判断由 Agent 根据下级进展、指标和现实证据形成，不在 Go 中写死汇总公式。不存在 OKR 专用 evidence API，也不为某个来源增加 Go 流水线。新增来源通常只需要工具和 Skill；只有必须机器强制的可靠性约束才进入代码。

## 4. 单一产品入口

OKR 只由可选产品模块拥有。Jarvis 核心没有第二套 OKR 表、领域模型、Project 外键或 Page 类型；WorldProgress 只保存对模块稳定引用的世界侧评估，不复制 Objective、KR、Point 或人工进展。模块 Skills 统一使用通用关系、Fact、Page 和 WorldProgress 连接世界状态。
