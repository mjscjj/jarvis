# Jarvis · 主动式任务数字分身

Jarvis 是运行在本地可信环境中的个人任务 Agent。它持续接收工作事实，维护世界状态，判断哪些事情值得推进，调用工具完成工作，并把结果沉淀回来。

## 阅读入口

1. [项目目标](goal.md)：稳定愿景与成功标准
2. [Agent 开发规范](AGENTS.md)：语义所有权、MVP 和实现约束
3. [当前架构](docs/00-overview.md)：跨模块数据流与硬边界
4. [文档导航](docs/README.md)：当前文档、提案、研究与交付物

## 核心链路

```text
飞书 Bot WebSocket ─> CC Connect
                       ├─ 当前会话可直接完成 ─> 即时回复
                       └─ 长期、多步或有副作用 ─> manual Task ─┐

飞书轮询 / 外部 Skill ─> M2 原始事实 ─> M3 准入 ─> Todo ────┤
                                                            v
                                                     M5 执行 Agent
                                         调查 ─> 动作 ─> 验证 ─> 沉淀
```

- **M2 机械采集**：保留原始事实，不解释错误、不判断价值。
- **M3 最短准入**：只调查到足以决定 `extracted` 或 `observing`。
- **Todo 固化**：`extracted` Todo 按 ID/version 幂等创建 Task，不再经过模型闸门。
- **M5 完整执行**：调查现实、调整目标、选择工具、处理等待和人工问题，直到得到真实结果。
- **世界模型**：实体页回答“现在是什么”，Fact 指向“发生过什么”的原始证据。
- **主动巡视**：看护未闭环工作；需要改变外部世界时创建普通 Task 交给 M5。

请示副作用不是固定流程阶段。M5 根据即将发生的具体动作判断是否需要询问 principal；需要时统一返回 `needs_human + question`，回答恢复同一个 Agent Session。

## Source Of Truth

| 主题 | 权威来源 |
|---|---|
| 项目目标 | `goal.md` |
| 开发和架构约束 | `AGENTS.md` |
| 当前跨模块架构 | `docs/00-overview.md` |
| 数据模型与迁移 | `internal/domain/`, `internal/store/sqlite.go` |
| HTTP 路由 | `internal/api/router.go` |
| 基线与本机配置 | `conf/config.yaml`, `conf/config.runtime.yaml` |
| Agent 行为 | `conf/prompts/`, `conf/rules/`, `.agents/skills/` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools` |
| 页面入口 | `web/src/App.tsx` |
| 安装动作 | `scripts/jarvis-install`, `.agents/skills/install-jarvis/` |

文档不复制完整 DDL、路由、CLI help 或本机有效配置。运行时值以基线配置和 `conf/config.runtime.yaml` 合并结果为准。

## 常见修改入口

- M3 行为：`conf/prompts/m3-system-prompt.md`, `conf/rules/m3.md`, `internal/extract/`
- M5 行为：`conf/prompts/m5-system-prompt.md`, `conf/rules/m5.md`, `internal/execute/`
- 审批尺度：`conf/prompts/m5-approval-policy.md`
- CC 前台交互：`conf/prompts/cc-system-prompt.md`, `integrations/cc-connect/`
- 世界模型：`internal/background/`, `internal/progress/`, `internal/factengine/`
- 主动巡视：`conf/prompts/proactive-system-prompt.md`, `internal/proactive/`
- 插件：`internal/plugin/`, `conf/skills.yaml`, 对应 Skill
- 运行配置：后台“系统设置”或 `conf/config.runtime.yaml`
- Web：`web/src/`

各模块的稳定契约见 [docs/modules](docs/README.md#当前实现)。

## 安装与运行

macOS 14+ Apple Silicon 用户优先使用 [DMG 安装与更新](docs/reference/macos-install-and-update.md)。

源码安装从完整 checkout 开始，在仓库根目录让 Agent 执行：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

该 Skill 负责依赖、飞书身份、CC Connect、服务、世界模型和真实端到端验收。不要绕过它在 fresh clone 上直接注册服务。

日常后端修改后统一执行：

```bash
./scripts/rebuild-server.sh
```

脚本会在重启前检查执行中的 Task。只有明确接受中断时才使用：

```bash
./scripts/rebuild-server.sh --force-interrupt-running-tasks
```

完整服务、端口、日志和故障恢复见 [运行与部署](docs/reference/operations.md)。

## 开发验证

```bash
go test ./cmd/... ./internal/...
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run build
git diff --check
```

需要真实外部服务或凭证的测试单独运行：

```bash
go test -tags=integration ./internal/...
```

只运行后端开发进程：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml
```

一次性命令以 `go run ./cmd/jarvis-server -h` 为准。

## 管理后台

主导航提供对话、工作台、任务、线索、世界、自动化、插件、工作设定、系统设置、安全保护和运行状态。

对话工作区使用持久会话 API。新会话的默认 Agent、模型、推理档位和执行边界在系统设置中独立维护；每个会话仍可单独选择。对话工作区不是 M3/M5 运行阶段，不复用阶段语义。

完整 HTTP 能力分组见 [HTTP API](docs/reference/http-api.md)。

## 目录

```text
cmd/                  进程入口
internal/             后端模块
web/                  React 管理后台
conf/                 基线配置、prompts 与 rules
.agents/skills/       Jarvis 领域 Skills
integrations/         外部集成与补丁
scripts/              安装、重建和 jarvis-tools
docs/                 当前架构、参考、提案、研究和正式交付物
data/ runs/ var/      本机生成数据、执行产物和运行状态
```
