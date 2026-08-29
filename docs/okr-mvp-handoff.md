# OKR 模块解耦验收

> Status: current
> Authority: non-normative verification snapshot
> Last verified: 2026-08-29 @ uncommitted worktree

本轮目标是把 Emily 拆成 Jarvis 的 `okr` 与 `weekly-report` 两个内置模块，同时把拆解、同步和催办语义移到工具与 Skill。当前改动未提交、未推送，也未访问真实外部系统。

## 已落地边界

- 模块配置：`conf/modules.yaml` 分别控制 `okr` 和 `weekly-report`；周报显式依赖 OKR，业务配置独立在 `conf/okr-module.yaml`。
- 生命周期：重启后按模块执行迁移、注册 `/api/okr/*` 或 `/api/weekly-report/*`、暴露对应 Skill；关闭模块不会删除 SQLite 数据。
- 存储：`MigrateCore` 与 `MigrateWeeklyReport` 分开拥有 schema 集合，同时保留既有表名和多人负责人旧表主键，避免数据重写。
- 产品：单一 OKR 入口内包含稳定定义、可选周报和 Agent 流程。OKR 负责 KR CRUD、负责人、优先级、标签，以及绑定在一起的 Agent 行动与业务 Prompt；行动明确做什么、何时做、范围和结果给谁，Prompt 决定怎么调查、判断和执行。周报负责填写、会议、评论、历史、图片、Meego 和催填事实。
- 写入：OKR PUT 不改周进展；周报 PUT 只改所选周进展，不改稳定定义；CAS 仍在 KR 版本上防止并发覆盖。
- 世界关系：模块不保存 `world_*_id`，也不在保存时同步世界模型；跨模块映射使用通用 `entity_relation` 和 `/api/relations`。
- 编排：启动时不投影世界模型、不安装默认定时任务。用户在 Agent 流程页创建行动并绑定 Prompt；通用 ScheduledTask 只保证一次、每天、每周或间隔触发，`okr-agent-orchestrator` 实时读取业务 Prompt 并动态组合原子工具。`okr-world-projector` 只拥有稳定实体投影；`weekly-report-progress-sync`、`weekly-report-reminder` 分别拥有同步和催办操作边界；后端不直接调用 Meego CLI。
- 证据：已移除 OKR 专用 evidence API/CLI；同步使用通用 `append-clue`、`append-fact`、`get-page`、`update-page` 和关系工具。
- 执行边界：Task 仍只表示一次执行；任何模块保存、关系投影或证据写回都不会隐式创建 Task。

## 自动验收矩阵

| 要求 | 实现 | 验证 |
|---|---|---|
| 模块开关与依赖 | `internal/appmodule` | appmodule 依赖校验、API strict JSON 单测 |
| 分层迁移与旧数据兼容 | `MigrateCore`, `MigrateWeeklyReport` | 旧 KROwner schema 回归测试 |
| 写入所有权 | `ReplaceKRCore`, `ReplaceWeeklyProgress` | core/weekly 隔离回归测试 |
| 路由受启用状态保护 | `internal/api/okr_module_routes.go` | API 测试与全量编译 |
| Skill 不泄漏 | `skill.WithModuleGate` + 各 Skill 的 `module` frontmatter | catalog/content 单测 |
| 调度关闭后不派发 | `scheduledtask.SetModuleGate` | disabled module dispatch 单测 |
| 通用关系 | `domain.EntityRelation`, `background.RelationService`, `/api/relations` | service 与 CLI endpoint 单测 |
| 通用证据写回 | clue + Fact + Page CAS | HTTP/真实本地 CLI 产品路径测试 |
| 行动与 Prompt 结合 | ScheduledTask + `prompt_key` + 自然语言目标/范围/对象 | weekly 调度单测、前端 typecheck 与 API CRUD |
| 前端仍可构建 | `web/src/okr/**`, module registry | typecheck、Vitest、Vite build |

## 人工验收

1. 同时启用两个模块后重启，确认顶层 OKR、周报页面和两组 API 可用。
2. 关闭 `weekly-report` 后重启，确认 OKR 仍可用，周报导航、路由和两个周报 Skill 不可用；已有 SQLite 数据仍存在。
3. 关闭 `okr` 时若周报仍启用，配置更新必须被拒绝；先关闭周报后可关闭 OKR。
4. 读取 `okr-world-projector` Skill，为一个 KR 建立 Project/Person 关系，再用 `list-relations` 回读证据。
5. 创建带 `{"module":"weekly-report","skill":"weekly-report-progress-sync"}` context 的 ScheduledTask；关闭周报后触发应记录空跑且 Task 数不增加。

## 暂留兼容面

早期 `/api/okrs`、`domain.OKR` 与 `Project.OKRID` 尚未物理删除。新模块完全不依赖它们；后续应在独立迁移中清理，避免本次同时承担存量数据迁移风险。

## 验证命令

```bash
bash -n scripts/jarvis-tools scripts/okr-agent-tools scripts/okr-module-tools scripts/weekly-report-tools
go test ./...
npm --prefix web run typecheck
npm --prefix web test -- --run
npm --prefix web run build
git diff --check
```
