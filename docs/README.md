# Jarvis 文档导航

> Status: current
> Authority: normative index
> Last verified: 2026-09-11

文档按语义所有权物理分层。只有“当前实现”可以描述现行行为；字段、路由、默认值和 CLI 参数仍以代码与命令帮助为准。

## 先读什么

1. [项目目标](../goal.md)
2. [Agent 开发规范](../AGENTS.md)
3. [仓库入口](../README.md)
4. [当前架构](00-overview.md)

## 当前实现

### 架构与模块

- [当前架构与跨模块契约](00-overview.md)
- [冻结上下文决策](decisions/context-snapshot.md)
- [M1 背景与世界模型](modules/01-background.md)
- [M2 消息与线索采集](modules/02-message.md)
- [M3 Task 准入](modules/03-task-extract.md)
- [M5 执行、提问与恢复](modules/05-execution.md)
- [总结与洞察](modules/06-insight.md)
- [插件系统](modules/07-plugins.md)

### 操作参考

- [运行与部署](reference/operations.md)
- [HTTP API 分组](reference/http-api.md)
- [macOS 安装与更新](reference/macos-install-and-update.md)
- [macOS 打包与发布](../packaging/macos/README.md)：Agent 工作流见 [桌面发布 Skill](../.agents/skills/release-jarvis-desktop/SKILL.md)
- [Lark CLI 与 ByteD CLI](reference/cli.md)

## 提案

`proposals/` 只保存尚待决策或仍有明确未实施部分的方案。提案不是当前架构真源。

- [Shell 工具系统优化（代码已实现，macOS 验收待完成）](proposals/tool-system.md)
- [长任务 Goal Control](proposals/goal-control.md)
- [第一阶段产品方向](proposals/product-stage-1.md)
- [Agent 可安装功能源码包](proposals/plugin-extension.md)
- [世界模型 3D 视图](proposals/world-model-3d.md)
- [实体 Summary：持续演化的当前认知页](proposals/entity-summary-pages.md)

实现提案时，把稳定结论合并进对应 current 模块；原提案随即删除，必要的关键取舍提炼进 `decisions/`，不能继续作为平行真源。

## 研究与历史

- `research/`：可复用的研究快照，不约束实现。
- Git 历史承担实施计划、中间方案和已废弃设计的追溯；仓库不为“也许以后会看”保留第二份文件。仍影响当前架构的关键取舍提炼进 `decisions/`。

## 正式交付物

[`summery/`](summery/README.md) 保存重要架构图、文章和 Slides 的最终交付及可编辑源。它不是技术真源，不保存多版中间图、视觉测试快照、contact sheet 或可重新生成的缓存。

## Source Of Truth

| 主题 | Source of truth |
|---|---|
| 数据模型与迁移 | `internal/domain/`, `internal/store/sqlite.go` |
| HTTP 路由 | `internal/api/router.go` |
| 配置 | `conf/config.yaml`, `conf/config.runtime.yaml` |
| 页面入口 | `web/src/App.tsx` |
| Agent 行为 | `conf/prompts/`, `conf/rules/`, `.agents/skills/` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools`, `scripts/lib/jarvis-tools/` |
| 源码安装动作 | `scripts/jarvis-install`, `.agents/skills/install-jarvis/` |
| macOS 打包与发布动作 | `packaging/macos/`, `.agents/skills/release-jarvis-desktop/` |

## 维护规则

- current 文档只写稳定边界、所有权、输入输出和已知缺口，不复制实现清单。
- proposal 必须明确未实现部分；不得使用现在时冒充当前能力。
- 实施完成后先更新 current 真源，再删除或归档实施稿。
- 交付目录只保留最终版本和可编辑源。
- 本机路径、运行快照、版本化下载地址和实际配置值不进入长期架构文档。
