# Jarvis 内置功能模块

> Status: current
> Authority: normative
> Last verified: 2026-09-06 @ uncommitted worktree

## 1. MVP 形态

Jarvis 是通用宿主，不是 OKR 产品本身。当前采用随 Jarvis 编译的内置模块，而不是动态加载第三方代码：

- `web/src/modules/registry.tsx` 注册前端入口；
- `conf/modules.yaml` 只保存已知模块的启用状态；
- `internal/appmodule` 是路由、迁移、Skill 和调度共用的唯一启用判断；
- 模块关闭不会删除它已有的业务数据，也不会影响 Person、Project、KeyMatter、Page、Fact、Task 等宿主能力。

开关写入后需要重启服务。重启后，关闭的模块不执行自身迁移、不加载自身配置、不注册业务路由和静态资源；它拥有的 Skill 不进入 Agent catalog，带 `context_snapshot.module` 的 ScheduledTask 只记录安全空跑，不创建 Task。

等第二个真实模块验证同一生命周期后，再考虑独立进程、动态包或插件市场；当前不引入第二套插件运行时。

## 2. 所有权与依赖

当前有两个内置模块：

- `okr` 是通用 OKR 插件，拥有季度、Objective、KR、核心指标、拆解点、结构化多人负责人、周次、周期指标快照和正式 Progress。接口位于 `/api/okr/*`，世界投影 Skill 为 `okr-world-projector`，原子工具入口为 `scripts/okr-module-tools`。
- `agency-okr` 是当前组织的业务包装，拥有结构标签、OKR Plan、Preview/Review、周报展示、评论、评分、Follow-up、催填、Meego 观察、业务身份和可编辑业务 Prompt。接口位于 `/api/agency-okr/*`，Skills 包括 `okr-agent-orchestrator`、`weekly-report-progress-sync`、`weekly-report-reminder`，工具入口为 `scripts/okr-agent-tools` 和 `scripts/agency-okr-tools`。

`agency-okr` 显式依赖 `okr`：可以只启用通用 OKR，不能在关闭 OKR 时单独启用 Agency OKR。两者第一阶段继续复用 `internal/okrworkspace/**` 包、`data/okr/okr.db` 和既有 `okr_workspace_*` 表名，但迁移集合、HTTP 路由、Skill、工具与前端门禁按语义所有权分开。关闭模块不删除或搬迁数据。完整拆分边界见 [通用 OKR 插件与 Agency OKR 拆分](design-okr-plugin-and-agency-okr.md)。

Jarvis 核心拥有通用机制：

- Person、Project、KeyMatter、Page、Fact、Message/Clue、Task、ScheduledTask；
- `entity_relation` 和 `/api/relations`；
- 通用 `jarvis-tools`。

核心提示词和工具目录不包含 OKR 的拆解、同步或催办语义。模块语义只在启用后的 Skill 中出现。

## 3. 产品数据与世界状态

OKR 定义、正式 Progress 和 Agency 业务事实保存在仓库内独立数据库 `data/okr/okr.db`，图片保存在 `data/okr/assets/`，两者随代码提交。该数据库使用 DELETE journal，成功写入直接落主文件，不依赖未提交的 WAL。Prompt、策略和模板正文仍以 `conf/prompts/*.md` 为唯一真源，不复制进数据库。

Jarvis 通用 Task、Fact、Page、ScheduledTask 以及 Agency OKR OAuth/session 等机器运行态仍保存在 `var/` 下的本机运行库，不进入 Git。模块库只包含结构化产品事实：通用 OKR 的季度、Objective、KR、指标、拆解点、负责人、周次和正式进展，以及 Agency 的标签、Plan、评论、评分、Meego 快照和催填批次。Agency 页面从同一份事实动态组装“业务分类标签 → 优先级标签 → Objective 方向 → KR”，不按标题匹配或保存第二套层级。

定义写接口不修改任何正式 Progress；通用 OKR 使用单条 Progress 的 create/update/delete 原子接口，每条进展持有自己的 `version`，不修改 KR 定义版本。定义接口拒绝夹带周进展。存在进展历史的拆解点或 KR 不允许删除，避免把历史变成孤儿数据。Agency 对组合视图的写入分别调用通用定义/Progress API 和 Agency 业务 API，不维护第二份进展。

需要把某个 KR 映射到 Project、KeyMatter 或 Person 时，Agent 读取 `okr-world-projector` Skill，并通过通用关系写入：

```text
okr_kr:<module id>
  --delivered_by--> project:<world id>
```

OKR 内部层级和负责人由 OKR 原生字段表达，不复制到 `entity_relation`。只有跨模块强关系才带证据写入并可单独审阅、替换或删除。之后进度同步用通用 clue、Fact、Page 和 WorldProgress 工具更新世界状态。Task 始终只表示一次执行，不成为 OKR 层级的一部分。

OKR 只有产品模块这一份领域模型；Jarvis 核心和 Project 不再保存另一套 OKR 结构。产品模块与世界模型之间只通过 `entity_relation` 连接。

## 4. 自动化

启动过程不导入 Emily 数据、不自动投影世界模型，也不自动创建定时任务。业务流程不在 Go 或前端组装，统一由 Agent 读取 Prompt 后动态选择原子工具：

- `okr-world-projector`（`module: okr`）：建立或复核跨模块实体关系；
- `okr-agent-orchestrator`（`module: agency-okr`）：读取行动绑定的业务 Prompt，完成季度草稿、区域/研发对齐、Report A/B/C、周报催填或进展巡检；
- `weekly-report-reminder`（`module: agency-okr`）：生成催填批次并在授权后发送；
- `weekly-report-progress-sync`（`module: agency-okr`）：只读检查 Meego/消息证据并写回通用 Fact/Page/WorldProgress。

共用原则和八份业务 Prompt 使用受控 Markdown 文件作为唯一真源，注册在 `internal/textstore/defaults.go`，正文位于 `conf/prompts/okr-agent-*.md`。OKR 的“Agent 流程”页面把固定行动管理与 Prompt 编辑放在一起。产品固定提供周报催填、进展自动巡检、会议材料生成和对外材料提交四个行动，并固定绑定对应 Prompt；用户只配置启停和执行时间，范围、对象、产出与验收全部在 Prompt 中维护。Prompt 通过通用 `/api/text-files` 读写，Skill 使用 `scripts/okr-agent-tools prompt --key ...` 实时读取。催填消息仍使用独立的 `weekly-report-reminder-template.md`。

行动不另建 OKR 专用表，而是复用 ScheduledTask。前端固定行动目录只把 `action_key`、Skill、Prompt key 和执行时间投影到 ScheduledTask，不保存自由目标、范围、结果对象、业务审批策略或步骤状态。到点后只创建一个普通 Task，由 `okr-agent-orchestrator` 读取绑定 Prompt 和实时事实决定怎么行动；审批由 M5 按统一策略结合具体副作用判断，Task/effects 负责留痕。Jarvis 的通用“自动化”页面仍可管理全部调度，OKR 页面提供四个业务行动的聚焦入口。

Meego 页面只读取 Agent 通过 `record-meego-observation` 保存的快照；后端不直接调用 `bytedcli`。固定的季度草稿、区域同步、风险聚合和 Report 草稿 API/DTO/页面已移除；迁入仓库的模块数据库不包含已废弃的草稿表。只有身份会话、图片存储、结构化 CRUD、快照持久化、CAS、模块生命周期等必须稳定执行的约束留在 Go 代码中。
