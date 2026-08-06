# Jarvis 文档导航

> Status: current
> Authority: normative index
> Last verified: 2026-08-05 @ `b91146d`

本页区分“当前实现”“提案”“研究”和“历史记录”。代码事实仍以对应 source of truth 为准；`last verified` 只表示文档在该 commit 上做过人工核对。

## 先读什么

1. [项目目标](../goal.md)：稳定愿景和成功标准
2. [Agent 开发规范](../AGENTS.md)：实现时必须遵守的约束
3. [仓库 README](../README.md)：启动、验证和常见修改入口
4. [当前架构](00-overview.md)：跨模块数据流、状态与硬边界

## 权威边界

| 主题 | Source of truth |
|---|---|
| 数据模型与迁移 | `internal/domain/*.go`, `internal/store/sqlite.go` |
| HTTP 路由 | `internal/api/router.go` |
| 基线与运行时配置 | `conf/config.yaml`, `conf/config.runtime.yaml` |
| 页面入口 | `web/src/App.tsx` |
| Agent 稳定行为 | `conf/prompts/`, `conf/rules/`, `.agents/skills/`, `AGENTS.md` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools` |

## 当前实现

### 核心流水线

- [M1 背景](modules/01-background.md)
- [M2 消息与线索采集](modules/02-message.md)
- [M3 Todo 提取](modules/03-task-extract.md)
- [M5 执行环节](modules/05-execution.md)

### 横切能力

| 文档 | 状态 | 说明 |
|---|---|---|
| [Codex Session 挂起与恢复](design-codex-session-continuation.md) | current，需按代码持续核对 | `waiting` / `needs_human` 的同 Session 续跑 |
| [每日进度总结](design-daily-digest.md) | current | 个人/群日报及 Skill 取证 |
| [主动巡视 Agent](design-proactive-heartbeat-agent.md) | implemented-history | 实现动机与验收设计；当前边界见总纲 |
| [文件化文本配置](design-file-backed-text-config.md) | current | prompts、rules 与后台编辑边界 |
| [实体关系与进度历史](design-temporal-relations-and-progress.md) | current，部分段落待继续校准 | RelationFact、TaskEvent、Fact |
| [KeyMatter 关键事项实体](design-key-matter.md) | implemented-history | 已落地；当前实体边界见总纲，字段与接口以代码真源为准 |
| [HTTP API](reference/http-api.md) | current | 路由分组；`router.go` 仍为真源 |
| [运行与部署](reference/operations.md) | current | launchd、端口、签名和重建 |
| [Lark / bytedcli 指南](guide-lark-byted-cli.md) | guide | 当前 CLI 用法，版本变化时需复核 help |

## 提案与实现中设计

这些文档不是当前架构真相，不能用现在时把未落地内容当成已有能力。

| 文档 | 状态 | 已实现 / 未实现边界 |
|---|---|---|
| [长任务 Goal Control](design-long-horizon-agent-goal-control.md) | proposal / partial | 目标线索保真已改善；Goal Store、Supervisor、独立 Verifier 未实现 |
| [宽松语义契约](design-loose-semantic-contract.md) | proposal / partial | M3 Candidate、enrichment 等仍有严格结构；Todo→Task 已改为无模型固化 |
| [世界上下文渐进加载](design-world-context-progressive.md) | implementation-in-progress | 本轮审计时 HEAD 未完整落地；不得提前标 completed |
| [晨间作战简报](design-morning-brief.md) | implementation-in-progress | Skill + `internal/morningbrief` 定时器已接线；无表无接口；用 `-morning-brief-once` 手动跑（只写文件）、`-morning-brief-deliver` 连投递一起验；产物在 `data/morning-brief/` |
| [第一阶段产品方案](design-product-stage-1.md) | proposal | 单人本地协作主干的产品主线：飞书触达与就地处置、收件箱对称、项目工作台、阻塞恢复、世界模型卫生；全部未实现，S0 复用晨报投递路径 |
| [CC Connect 支持飞书文档评论](design-cc-connect-feishu-document-comments.md) | proposal | cc-connect 复用现有 Feishu 长连接接收 `@Bot` 评论，并把 Agent 最终答案写回原评论卡片；尚未实现 |

[上下文链路重设计](design-context-pipeline.md) 是已实施的历史设计稿。当前稳定结论已经写入总纲和模块文档，正文里的“待实施”步骤不作为当前实现说明。

## 研究

- [Agent 决策调研](research/agent-decision-survey.md)：历史研究快照；其中 Jarvis M4 对照已过时
- [类人类决策系统](research/human-like-decision-system.md)：概念研究，不代表当前或已批准架构

## 历史与归档

- [`archive/`](archive/)：已被替代、已实施但不再规范当前系统，或明确未采用的方案
- [`superpowers/specs/`](superpowers/specs/) 与 [`superpowers/plans/`](superpowers/plans/)：带日期的实施记录；会议专用链路部分已被通用 clue 流水线推翻

## 文档状态规则

非生成型产品文档顶部使用以下元数据：

```md
> Status: current | proposal | implementation-in-progress | implemented-history | obsolete
> Authority: normative | non-normative
> Last verified: YYYY-MM-DD @ <commit>
> Superseded by: <path>   # 可选
```

- `current` 才能描述当前代码行为；必须链接代码真源，不复制整套 DDL 或 85 条路由。
- `proposal` / `implementation-in-progress` 明确区分已经落地和尚未落地的部分。
- 实施完成后，把稳定结论并入 current 文档，再把原实施稿标成 `implemented-history`。
- `research` 只提供认知材料，不约束实现；若项目对照过时，必须在开头标明。
- 新增、删除或改名文档时同步更新本索引；相对链接必须可解析。

## 已知文档债务

- `design-context-pipeline.md` 尚未物理移入 `archive/`，避免在共享工作区中制造大范围路径冲突；已在本索引降级为历史设计。
- `design-long-horizon-agent-goal-control.md`、`design-loose-semantic-contract.md` 和 `design-world-context-progressive.md` 尚未物理移入 `proposals/`；状态以本索引和各文档顶部声明为准。
- `data/personal-*` 中的日报证据文件属于生成产物，不受产品文档生命周期规则约束，历史附件链接可能失效。
