# OKR 模块与世界模型

> Status: current
> Authority: normative
> Last verified: 2026-08-30 @ uncommitted worktree

## 1. 两份状态，各自负责一件事

OKR 模块保存稳定定义：季度、Objective 方向、KR、核心指标、负责人和标签。业务分类与 Focus/P1/P2 优先级都是 KR 的结构标签；页面按“业务分类标签 → 优先级标签 → Objective 方向 → KR”动态组织，不复制优先级字段，也不按 Objective 标题推断分类。周报模块保存按周变化的进展、评论、Meego 观察和催填批次，并显式依赖 OKR。固定材料流程由 Agent Prompt 组装，不另存专用草稿状态机。

负责人是 KR 和具体 KR（策略/产品）各自的独立关系，分别存在 `okr_workspace_kr_owner` 与 `okr_workspace_point_owner`，两级都通过 `PUT /api/okr/krs/:kr_id` 的完整定义写入，同一个人可以只出现在其中一级。负责人不用标签表达。

Agent 通过 `okr-module-tools replace-kr-tags` 调用 `PUT /api/okr/krs/:kr_id/tags` 维护标签，使用 KR 当前版本和完整标签列表；接口仅更新标签及 KR 版本/编辑者，不改其它定义或周报事实。标签的增删、分类映射和批量编排由 Agent 判断，沿用现有标签表及结构标签校验。O 不独立存标签，需要按子 KR 表达。

Jarvis 世界模型保存跨来源的认知状态：Person、Project、KeyMatter、当前 Page、历史 Fact 和它们之间的关系。Task 只是一轮执行单元，不属于任何 OKR 层级。

两者不共享业务表、不互存外键，也不在保存页面时同步写入。需要连接时使用通用 `entity_relation`：模块实体 ID 和世界实体 ID 都以字符串保存，关系类型由 Skill 解释。

## 2. 投影由 Agent 完成

`okr-world-projector` Skill 负责把模块层级映射到世界实体：

1. 用 `scripts/okr-module-tools board` 读取模块真源；
2. 用 `jarvis-tools` 查找或创建 Person、Project、KeyMatter；
3. 用 `create-relation` 保存已确认映射及证据；
4. 用 `list-relations` 回读验证。

没有证据时不建立关系；标题相似不能单独作为自动关联依据。投影只读取稳定的 OKR 定义，不读取周报内容。投影失败不会阻塞周报填写，世界模型失败也不能回滚模块表写入。

## 3. 周进展闭环

`weekly-report-progress-sync` Skill 执行一次只读巡检：

1. 读取 `/api/weekly-report/board` 与已确认关系；
2. 通过 `bytedcli` 或本地已采集消息读取证据；
3. 原始证据用 `append-clue` 幂等进入统一证据流；
4. 客观变化用 `append-fact` 写入最小世界实体；
5. 当前结论用 `get-page` + `update-page` CAS 更新。

不存在 OKR 专用 evidence API，也不为某个来源增加 Go 流水线。新增来源通常只需要工具和 Skill；只有必须机器强制的可靠性约束才进入代码。

## 4. 单一产品入口

OKR 只由可选产品模块拥有。Jarvis 核心没有第二套 OKR 表、领域模型、Project 外键、Page 类型或 CRUD API；模块 Skills 统一使用通用关系、Fact 和 Page 连接世界状态。
