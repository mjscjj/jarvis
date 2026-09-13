# Agent 可安装功能源码包

> Status: proposal
> Authority: non-normative
> Last reviewed: 2026-09-13

当前内置插件行为见 [插件系统](../modules/07-plugins.md)。本文只定义未来如何分享和安装一项
Jarvis 功能，不把它设计成运行时插件系统。

## 结论

MVP 分享的是一份**功能源码包**。它可以包含 Go 后端、HTTP API、React 页面、Skills、
定时任务示例和安装说明。用户把源码包交给目标 Jarvis 中的 Agent，由 Agent 理解当前仓库，
把代码合并到正确位置并运行验证。安装完成后，这项功能就是目标 Jarvis 的普通源码，不存在
独立插件进程、动态路由、动态前端模块或插件生命周期。

这不是一个需要宿主在运行时理解的文件协议。功能包只服务安装 Agent，因此不建设 Manifest、
依赖解析、版本协商、授权 provider 注册、配置表单 DSL、沙箱、热加载或自动卸载。若目标仓库
已经变化，由 Agent 根据语义寻找接入点，而不是由安装器逐行套补丁。

## 建议目录

功能包可以是一个目录、压缩包或 Git 仓库。只约定一个给 Agent 阅读的 `INSTALL.md`；其余
目录按实际功能取舍，不要求每个包齐全：

```text
my-delegations/
├── INSTALL.md
├── backend/
│   ├── domain.go
│   ├── service.go
│   ├── api.go
│   └── service_test.go
├── web/
│   ├── Delegations.tsx
│   └── presentation.ts
└── skills/
    ├── my-delegations-extract/SKILL.md
    ├── my-delegations-execute/SKILL.md
    └── my-delegations-review/SKILL.md
```

`INSTALL.md` 用自然语言说明：

- 功能解决什么问题，以及哪些代码属于它；
- 后端模型、Service、HTTP 路由和启动组装需要接入什么语义；
- 页面、导航和 API client 需要接入什么语义；
- Skills 放置位置、适用阶段，以及是否需要创建普通 ScheduledTask；
- 依赖哪些 Jarvis 现有工具或外部 CLI，缺少时如何明确报错；
- 安装后要运行哪些现有测试，以及一条最小人工验收路径。

不要求填写机器可解析字段，也不要求列举整个目标仓库的固定文件和行号。包内可以保留参考
源码；安装 Agent 应优先复用目标仓库现有模型、API 和页面结构，只合并完成该功能所需的最小
语义闭包。

## 两类功能

只需要 Agent 行为的功能可以只分享 Skill 和设置说明。例如 Oncall 采集可由一个 collector
Skill 加一条普通 ScheduledTask 完成，采集结果仍走 `/api/clues`。

需要产品界面的功能可以连源码一起分享。例如“我的交办”可以携带 `delegation_progress`
模型、查询和更新 Service、HTTP API、React 页面及三个阶段 Skill。安装 Agent 负责注册模型、
路由和页面；不需要 Jarvis 先具备动态加载后端和前端代码的能力。

功能包作者可以直接从一个已运行的 Jarvis 实现中抽取这些文件作为参考。代码与目标仓库冲突
时，以目标仓库当前架构和 `AGENTS.md` 为准，Agent 应移植语义，不保留重复真源或兼容层。

## 分享与安装

最初的“市场”只需要展示功能说明并提供功能包下载。安装流程是：

```text
选择功能包 -> 下载或复制到本地 -> 让 Agent 阅读 INSTALL.md
             -> Agent 合并源码和 Skills -> 构建、测试、人工验收
```

升级时再次把新版功能包交给 Agent，结合 Git diff 和当前代码做增量合并。卸载同样由 Agent
根据功能包和 Git 历史识别改动后执行；MVP 不承诺一键升级、自动迁移或无损卸载。

## 不变边界

- 安装后的功能仍复用 Jarvis 的 M2→M3→M5、Task、ScheduledTask 和工具体系，不创建第二套内核；
- 来源差异优先留在 Skill，外部语义继续使用完整文本或宽松 JSON；
- 功能确实需要独立生命周期、查询或 UI 时，才随包提供最小模型、API 和页面；
- 安装行为会修改主仓库源码，必须保留可审查 diff，并在安装后执行与风险相称的验证；
- 功能包来自可信作者并由用户明确交给 Agent 安装。面向不可信公共包的审核、签名和沙箱不属于 MVP。

## 当前代码的处理

现有 `internal/plugin` 仍是 Codebase、Meego、Oncall 和“我的交办”的内置启停页面，本文不把
它描述成可分发平台。下一步只有在实际抽取第一个功能包时，才根据真实移植成本决定哪些当前
内置接线保留、删除或改成普通 Jarvis 功能；不先新增通用安装框架。
