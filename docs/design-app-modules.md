# Jarvis 内置功能模块

> Status: current
> Authority: normative
> Last verified: 2026-08-30 @ uncommitted worktree

## 1. MVP 形态

Jarvis 是通用宿主，不是 OKR 产品本身。当前采用随 Jarvis 编译的内置模块，而不是动态加载第三方代码：

- `web/src/modules/registry.tsx` 注册前端入口；
- `conf/modules.yaml` 只保存已知模块的启用状态；
- `internal/appmodule` 是路由、迁移、Skill 和调度共用的唯一启用判断；
- 模块关闭不会删除它已有的业务数据，也不会影响 Person、Project、KeyMatter、Page、Fact、Task 等宿主能力。

开关写入后需要重启服务。重启后，关闭的模块不执行自身迁移、不加载自身配置、不注册业务路由和静态资源；它拥有的 Skill 不进入 Agent catalog，带 `context_snapshot.module` 的 ScheduledTask 只记录安全空跑，不创建 Task。

等第二个真实模块验证同一生命周期后，再考虑独立进程、动态包或插件市场；当前不引入第二套插件运行时。

## 2. 所有权与依赖

当前有两个可独立展示的内置模块：

- `okr` 拥有 Objective、KR、核心指标、结构化多人负责人、优先级、标签、可编辑业务 Prompt 和世界关系投影。接口位于 `/api/okr/*`，Skills 为 `okr-agent-orchestrator`、`okr-world-projector`，工具入口为 `scripts/okr-agent-tools`、`scripts/okr-module-tools`。
- `weekly-report` 拥有按周进展、历史对比、填写/会议视图、评论、催填和 Meego 观察。接口位于 `/api/weekly-report/*`，Skills 为 `weekly-report-progress-sync`、`weekly-report-reminder`，工具入口为 `scripts/weekly-report-tools`。

`weekly-report` 显式依赖 `okr`：可以只启用 OKR，不能在关闭 OKR 时单独启用周报。两者复用 `internal/okrworkspace/**` 包和 `okr_workspace_*` 表名前缀，但迁移集合、HTTP 写入、前端入口、Skill 和工具按所有权分开。

Jarvis 核心拥有通用机制：

- Person、Project、KeyMatter、Page、Fact、Message/Clue、Task、ScheduledTask；
- `entity_relation` 和 `/api/relations`；
- 通用 `jarvis-tools`。

核心提示词和工具目录不包含 OKR 的拆解、同步或催办语义。模块语义只在启用后的 Skill 中出现。

## 3. 产品数据与世界状态

OKR 定义和周报产品事实保存在仓库内独立数据库 `data/okr/okr.db`，图片保存在 `data/okr/assets/`，两者随代码提交。该数据库使用 DELETE journal，成功写入直接落主文件，不依赖未提交的 WAL。Prompt、策略和模板正文仍以 `conf/prompts/*.md` 为唯一真源，不复制进数据库。

Jarvis 通用 Task、Fact、Page、ScheduledTask 以及 OKR OAuth/session 等机器运行态仍保存在 `var/` 下的本机运行库，不进入 Git。模块库只包含结构化产品事实：Objective、KR、指标、拆解点、结构化负责人、标签，以及启用周报后产生的周次、进展、评论、Meego 快照和催填批次。

OKR 写接口不修改任何周进展；周报使用单条进展的 create/update/delete 原子接口，每条进展持有自己的 `version`，不修改 KR 定义版本。核心接口拒绝夹带周进展。存在周报历史的拆解点或 KR 不允许删除，避免把历史变成孤儿数据。

需要把某个 KR 映射到 Project、KeyMatter 或 Person 时，Agent 读取 `okr-world-projector` Skill，并通过通用关系写入：

```text
okr_kr:<module id>
  --projects_to--> project:<world id>
  --owned_by-----> person:<world id>
```

关系带证据且可单独审阅、替换或删除。之后进度同步用通用 clue、Fact 和 Page 工具更新世界状态。Task 始终只表示一次执行，不成为 OKR 层级的一部分。

仓库仍保留早期世界模型 `/api/okrs` 作为兼容面，但新的 OKR 模块不依赖它，也不写入它；后续可在独立迁移中移除，不能再新增依赖。

## 4. 自动化

启动过程不导入 Emily 数据、不自动投影世界模型，也不自动创建定时任务。业务流程不在 Go 或前端组装，统一由 Agent 读取 Prompt 后动态选择原子工具：

- `okr-agent-orchestrator`：读取行动绑定的业务 Prompt，完成季度草稿、区域/研发对齐、Report A/B/C、周报催填或进展巡检；
- `okr-world-projector`：建立或复核跨模块实体关系。
- `weekly-report-reminder`：生成催填批次并在授权后发送；
- `weekly-report-progress-sync`：只读检查 Meego/消息证据并写回通用 Fact/Page。

共用原则和八份业务 Prompt 使用受控 Markdown 文件作为唯一真源，注册在 `internal/textstore/defaults.go`，正文位于 `conf/prompts/okr-agent-*.md`。OKR 的“Agent 流程”页面把固定行动管理与 Prompt 编辑放在一起。产品固定提供周报催填、进展自动巡检、会议材料生成和对外材料提交四个行动，并固定绑定对应 Prompt；用户只配置启停和执行时间，范围、对象、产出与验收全部在 Prompt 中维护。Prompt 通过通用 `/api/text-files` 读写，Skill 使用 `scripts/okr-agent-tools prompt --key ...` 实时读取。催填消息仍使用独立的 `weekly-report-reminder-template.md`。

行动不另建 OKR 专用表，而是复用 ScheduledTask。前端固定行动目录只把 `action_key`、Skill、Prompt key 和执行时间投影到 ScheduledTask，不保存自由目标、范围、结果对象、业务审批策略或步骤状态。到点后只创建一个普通 Task，由 `okr-agent-orchestrator` 读取绑定 Prompt 和实时事实决定怎么行动；审批由 M5 按统一策略结合具体副作用判断，Task/effects 负责留痕。Jarvis 的通用“自动化”页面仍可管理全部调度，OKR 页面提供四个业务行动的聚焦入口。

Meego 页面只读取 Agent 通过 `record-meego-observation` 保存的快照；后端不直接调用 `bytedcli`。固定的季度草稿、区域同步、风险聚合和 Report 草稿 API/DTO/页面已移除；迁入仓库的模块数据库不包含已废弃的草稿表。只有身份会话、图片存储、结构化 CRUD、快照持久化、CAS、模块生命周期等必须稳定执行的约束留在 Go 代码中。
