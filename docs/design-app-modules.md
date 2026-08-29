# Jarvis 内置功能模块

> Status: current
> Authority: normative
> Last verified: 2026-08-28 @ uncommitted worktree

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

- `okr` 拥有 Objective、KR、核心指标、结构化多人负责人、优先级、标签、季度材料和世界关系投影。接口位于 `/api/okr/*`，Skill 为 `okr-world-projector`，工具入口为 `scripts/okr-module-tools`。
- `weekly-report` 拥有按周进展、历史对比、填写/会议视图、评论、催填、Meego 观察与周报材料。接口位于 `/api/weekly-report/*`，Skills 为 `weekly-report-progress-sync`、`weekly-report-reminder`，工具入口为 `scripts/weekly-report-tools`。

`weekly-report` 显式依赖 `okr`：可以只启用 OKR，不能在关闭 OKR 时单独启用周报。两者暂时复用 `internal/okrworkspace/**` 兼容适配层和既有 `okr_workspace_*` SQLite 表名，避免为拆分重写历史数据；迁移、HTTP 写入、前端入口、Skill 和工具已经按所有权分开。

Jarvis 核心拥有通用机制：

- Person、Project、KeyMatter、Page、Fact、Message/Clue、Task、ScheduledTask；
- `entity_relation` 和 `/api/relations`；
- 通用 `jarvis-tools`。

核心提示词和工具目录不包含 OKR 的拆解、同步或催办语义。模块语义只在启用后的 Skill 中出现。

## 3. 产品数据与世界状态

OKR 定义和周报产品数据保留在模块表中。OKR 写接口不修改任何周进展；周报写接口只替换所选周的进展，不修改 KR 标题、负责人、核心指标、标签或拆解定义。历史周数据不会因本周保存被覆盖。

需要把某个 KR 映射到 Project、KeyMatter 或 Person 时，Agent 读取 `okr-world-projector` Skill，并通过通用关系写入：

```text
okr_kr:<module id>
  --projects_to--> project:<world id>
  --owned_by-----> person:<world id>
```

关系带证据且可单独审阅、替换或删除。之后进度同步用通用 clue、Fact 和 Page 工具更新世界状态。Task 始终只表示一次执行，不成为 OKR 层级的一部分。

仓库仍保留早期世界模型 `/api/okrs` 作为兼容面，但新的 OKR 模块不依赖它，也不写入它；后续可在独立迁移中移除，不能再新增依赖。

## 4. 自动化

启动过程不导入 Emily 数据、不自动投影世界模型，也不自动创建定时任务。管理员需要自动化时，显式读取模块 Skill 并创建 ScheduledTask：

- `okr-world-projector`：建立或复核跨模块实体关系。
- `weekly-report-reminder`：生成催填批次并在授权后发送；
- `weekly-report-progress-sync`：只读检查 Meego/消息证据并写回通用 Fact/Page。
- `weekly-report-materials`：生成会议/汇报草稿；对外提交必须有已保存草稿、明确目标和审批。

周报的周期流程和消息/材料模板使用受控 Markdown 文件作为唯一真源，注册在 `internal/textstore/defaults.go`，正文位于 `conf/prompts/weekly-report-*.md`。动作管理页面通过通用 `/api/text-files` 读写这些文件；Skills 使用 `scripts/weekly-report-tools text-config --key ...` 实时读取。调度参数继续由 `ScheduledTask` 持有，不把 Markdown 正文复制进数据库或前端常量。

前端在 Jarvis 的单一 `OKR` 一级入口内提供“动作管理”子页。它把上述 Skill 映射为催填、进展巡检、会议材料和对外提交四种业务动作，并复用 `ScheduledTask` 做启停与时间触发、复用 `Task` 展示真实执行状态。这里不创建第二张动作表；Jarvis 的通用“自动化”页面继续承担高级技术配置。

Meego 页面只读取 Agent 通过 `record-meego-observation` 保存的快照；后端不直接调用 `bytedcli`。这使“工具 + 提示词”成为业务编排层。只有身份会话、图片存储、结构化 CRUD、快照持久化、CAS、模块生命周期等必须稳定执行的约束留在 Go 代码中。
