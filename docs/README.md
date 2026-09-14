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
- [OKR 模块当前实现](modules/06-okr.md)：通用 OKR、Biz OKR、正式 Progress 与 WorldProgress 的维护边界
- [内置功能模块](design-app-modules.md)：模块注册、启停与生命周期
- [双飞书应用身份](design-dual-app-identity.md)：主应用与 OKR 对外登录应用的分工
- [OKR 世界模型](design-okr-world-model.md)：基于证据的实体映射与周期判断
- [OKR 模块解耦验收](okr-mvp-handoff.md)

### 操作参考

- [运行与部署](reference/operations.md)
- [HTTP API 分组](reference/http-api.md)
- [macOS 安装与更新](reference/macos-install-and-update.md)
- [Lark CLI 与 ByteD CLI](reference/cli.md)

## 提案

`proposals/` 只保存尚待决策或仍有明确未实施部分的方案。提案不是当前架构真源。

- [Shell 工具系统优化（代码已实现，macOS 验收待完成）](proposals/tool-system.md)
- [长任务 Goal Control](proposals/goal-control.md)
- [第一阶段产品方向](proposals/product-stage-1.md)
- [Agent 可安装功能源码包](proposals/plugin-extension.md)
- [世界模型 3D 视图](proposals/world-model-3d.md)
- [通用 OKR 与 Biz OKR 拆分](design-okr-plugin-and-biz-okr.md)：第一阶段已实施，后续阶段仍为方案；当前维护边界见 OKR 模块文档
- [网页 SSO 登录接入](summery/sso-web-login.md)：官方方法已查证，域名、代码接入和真实登录验收未完成
- [实体 Summary：持续演化的当前认知页](proposals/entity-summary-pages.md)

实现提案时，把稳定结论合并进对应 current 模块；原提案随即删除，必要的关键取舍提炼进 `decisions/`，不能继续作为平行真源。

## 研究与历史

- `research/`：可复用的研究快照，不约束实现。
- Git 历史承担实施计划、中间方案和已废弃设计的追溯；仓库不为“也许以后会看”保留第二份文件。仍影响当前架构的关键取舍提炼进 `decisions/`。
- [旧版 OKR 与世界模型整合方案](summery/okr-jarvis-world-model-integration.md)：历史设计，不替代当前 OKR 模块文档。

## 正式交付物

[`summery/`](summery/README.md) 保存重要架构图、文章和 Slides 的最终交付及可编辑源，以及明确要求留存的 PRD 评审原始材料。它不是技术真源，不保存多版中间图、视觉测试快照、contact sheet 或可重新生成的缓存。

## Source Of Truth

| 主题 | Source of truth |
|---|---|
| 数据模型与迁移 | `internal/domain/`, `internal/store/sqlite.go` |
| OKR 产品事实与迁移 | `internal/okrworkspace/`, `internal/okrreview/`, `data/okr/` |
| HTTP 路由 | `internal/api/router.go` |
| 配置 | `conf/config.yaml`, `conf/config.runtime.yaml`, `conf/modules.yaml`, `conf/okr-module.yaml` |
| 页面入口 | `web/src/App.tsx` |
| Agent 行为 | `conf/prompts/`, `conf/rules/`, `.agents/skills/` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools`, `scripts/lib/jarvis-tools/` |
| 安装与发布动作 | `scripts/`, `packaging/`, `.agents/skills/install-jarvis/` |

## 维护规则

- current 文档只写稳定边界、所有权、输入输出和已知缺口，不复制实现清单。
- proposal 必须明确未实现部分，不得使用现在时冒充当前能力。
- 实施完成后先更新 current 真源，再删除实施稿；必要的取舍保存到 decisions。
- 交付目录只保留最终版本和可编辑源；已采集的原始证据完整保留，不以归纳代替原文。
- 新增、删除或改名文档时同步更新索引；相对链接必须可解析。
- 本机路径、运行快照、版本化下载地址和实际配置值不进入长期架构文档。
