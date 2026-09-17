# 重要架构图与文章

> Status: deliverable index
> Authority: non-normative
> Last reviewed: 2026-09-14

本目录保存正式表达、交付和明确要求保留的原始材料，不是架构事实真源。涉及当前行为时，以 [`goal.md`](../../goal.md)、[`docs/00-overview.md`](../00-overview.md) 和代码为准。

## 当前交付

- [可迭代世界观章节](jarvis-worldview/README.md)：正文、三张最终图及 SVG 源。
- [主动式数字分身总架构图](jarvis-proactive-digital-twin-architecture.md)：绘制说明、SVG 和 PNG。
- [三个评审群 PRD 原始材料](group-prd-comments/raw-comments.md)及[归纳分析](group-prd-comments/analysis.md)：保留用于校准评审 Skill 的精选原文和结论；采集期结构化返回属于运行时证据，本地存放在 `~/tmp/jarvis/prd-review/calibration/group-prd-comments/evidence/`，不进入 Git。
- [本人评论补充样本](prd-review-comments/raw-comments.md)及[初步分析](prd-review-comments/analysis.md)：含非 PRD 样本，使用时保留范围边界。

## 方案与历史材料

- [OKR 入口统一到开发实例：开发方案与问题记录](2026-09-16-okr-development-entry.md)：需求边界、外层切换方案、实施状态、问题台账和验收记录；已部署，真实登录业务验收待补。
- [Emily 完整研发环境](emily-development-environment.md)：当前研发实例的共享数据、配置、部署和分支维护说明；技术行为仍以代码为准。
- [原始 OKR 单轮容器设计](okr-chat-isolation-mvp-design.md)：历史模式，当前完整研发模式以上一文档为准。
- [OKR 与 Jarvis 世界模型整合方案](okr-jarvis-world-model-integration.md)：历史方案，当前边界见 [OKR 模块](../modules/06-okr.md)。
- [网页 SSO 登录接入](sso-web-login.md)：当前 CLI 授权域名与超时对比、配置方法，以及尚未实施的个人 JWT SDK 方案。

## 保留规则

- 每个表达主题保留一个 README、可编辑源和一个最终导出；必要时保留最终 PDF。
- 不提交版本化中间图、contact sheet、视觉测试快照或可重新生成的预览。
- 不提交调查原始返回、生成草稿、文档回读、通知 payload 和回执；这些临时文件建议放在 `~/tmp/jarvis/`。模块持久化真源和正式交付仍使用各自目录。
- 方案、审查记录和实施计划分别进入 `docs/proposals/`、`docs/research/` 或 Git 历史。
- 已采集的原始材料完整保留，归纳与原文分开；稳定评审方法沉淀到对应 Skill，不以本目录分析稿作为运行时规则。
