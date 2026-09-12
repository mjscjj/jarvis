# Jarvis · 主动式任务数字分身

Jarvis 是运行在本地可信环境中的个人任务 Agent。它持续接收工作事实，维护世界状态，判断哪些事情值得推进，调用工具完成工作，并把结果沉淀回来。

## 阅读入口

1. [项目目标](goal.md)：稳定愿景与成功标准
2. [Agent 开发规范](AGENTS.md)：语义所有权、MVP 和实现约束
3. [当前架构](docs/00-overview.md)：跨模块数据流与硬边界
4. [文档导航](docs/README.md)：当前文档、提案、研究与交付物
5. [OKR 模块当前实现](docs/modules/06-okr.md)：通用 OKR、Biz OKR 与世界模型的维护边界

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
- **世界模型**：实体页回答“现在是什么”，Fact 指向“发生过什么”的原始证据；EntityRelation 保存可查询的明确映射，WorldProgress 保存证据化周期判断。
- **主动巡视**：看护未闭环工作；需要改变外部世界时创建普通 Task 交给 M5。

请示副作用不是固定流程阶段。M5 根据即将发生的具体动作判断是否需要询问 principal；需要时统一返回 `needs_human + question`，回答恢复同一个 Agent Session。

## Source Of Truth

| 主题 | 权威来源 |
|---|---|
| 项目目标 | `goal.md` |
| 开发和架构约束 | `AGENTS.md` |
| 当前跨模块架构 | `docs/00-overview.md` |
| 数据模型与迁移 | `internal/domain/`, `internal/store/sqlite.go` |
| OKR 产品模块 | `internal/okrworkspace/`, `internal/okrreview/`, `conf/okr-module.yaml`, `data/okr/` |
| HTTP 路由 | `internal/api/router.go` |
| 基线与本机配置 | `conf/config.yaml`, `conf/config.runtime.yaml` |
| Agent 行为 | `conf/prompts/`, `conf/rules/`, `.agents/skills/` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools` |
| 飞书应用与登录身份 | [双飞书应用身份](docs/design-dual-app-identity.md)，`conf/okr-module.yaml` 与 `conf/okr-feishu-scopes.txt` |
| 页面入口 | `web/src/App.tsx` |
| 安装动作 | `scripts/jarvis-install`, `.agents/skills/install-jarvis/` |

文档不复制完整 DDL、路由、CLI help 或本机有效配置。

有效配置是 `conf/config.yaml` 与同目录 `conf/config.runtime.yaml` 的合并结果：runtime 按叶子 key 覆盖基线，未出现的 key 保留基线值，两个文件都拒绝未知字段。**本机参数、身份和密钥写 runtime 文件，不改仓库基线。** runtime 文件不进 Git，权限保持 `600`。后台保存后需要重启；prompts、rules 和 Skills 按各自 reader 实时读取。

Jarvis 本体使用 lark-cli 当前默认身份，只服务 principal；OKR 页面登录使用独立低敏应用。网页登录与白名单接入见 [网页 SSO 登录接入](docs/summery/sso-web-login.md)：个人 JWT SDK 方案已查证，域名接入、代码实现与真实登录验收尚未完成。

`server.addr` 是实例后端监听地址的配置真源。主进程向 Agent 子进程导出 `JARVIS_API_BASE`、`JARVIS_CONFIG` 和仓库工具 PATH；切换工作目录不会切换实例。通用、世界模型、OKR/周报工具统一用 `scripts/jarvis-api-base`，优先采用继承地址，否则读取选定配置；配置错误直接失败，不扫描端口。模块工具的显式 `--base-url` 可指定其它实例。

服务名由配置文件绝对路径生成；改端口不改服务名，不同配置不共用服务。多实例仍需分开配置数据库与产物路径，外部账号、Qdrant collection 和 Bot 长连接不会自动隔离。给用户的页面与卡片链接优先使用 `server.public_base_url`。

## 常见修改入口

- M3 行为：`conf/prompts/m3-system-prompt.md`, `conf/rules/m3.md`, `internal/extract/`
- M5 行为：`conf/prompts/m5-system-prompt.md`, `conf/rules/m5.md`, `internal/execute/`
- 请示尺度：`conf/prompts/m5-approval-policy.md`
- CC 前台交互：`conf/prompts/cc-system-prompt.md`, `integrations/cc-connect/`
- 世界模型：`internal/background/`, `internal/progress/`, `internal/factengine/`
- 主动巡视：`conf/prompts/proactive-system-prompt.md`, `internal/proactive/`
- 插件：`internal/plugin/`, `conf/skills.yaml`, 对应 Skill
- OKR：`internal/okrworkspace/`, `internal/okrreview/`, `web/src/okr/`；生命周期由 `internal/appmodule/` 管理
- 运行配置：后台“系统设置”或 `conf/config.runtime.yaml`
- Web：`web/src/`

各模块的稳定契约见 [docs/modules](docs/README.md#当前实现)。

## 安装与运行

macOS 14+ Apple Silicon 用户优先使用 [DMG 安装与更新](docs/reference/macos-install-and-update.md)。

源码安装从完整 checkout 开始，在仓库根目录让 Agent 执行：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

该 Skill 负责依赖、飞书身份、CC Connect、服务、世界模型和真实端到端验收。不要绕过依赖门、身份和 CC 绑定，在 fresh clone 上直接注册服务。安装清单记录在 `var/install/<run-id>/INSTALL_CHECKLIST.md`。

要求 Go 1.26.4 或更高版本、C 编译器、满足 Vite engines 的 Node（`^20.19.0` 或 `>=22.12.0`）/npm、jq、git、lark-cli、有效配置选定的 Agent CLI 和 Qdrant。SQLite 驱动依赖 CGO，构建脚本通过 `scripts/check-build-toolchain.sh` 检查。通用运行数据库在本机创建；可选 OKR 模块的产品数据库及资源随仓库保存在 `data/okr/`。

`bind-cc` 会立即验证 App ID/Secret。已有 Feishu `allow_from` 不是 Principal 本人时会停止，明确确认替换后才可使用 `--replace-allow-from`；`validate-binding` 拒绝缺失或通配的白名单。首次安装动作和 CLI 参数以安装 Skill、脚本 help 为准。

### 改完代码怎么生效

两个系统统一执行：

```bash
./scripts/jarvis-deploy --skip-pull
```

它安装并构建前端、编译主服务、按当前系统重启选定实例，最后验证首页、`/healthz` 与 `/readyz`。macOS 走稳定签名与 launchd，Linux 走 user systemd。`scripts/rebuild-server.sh` 是 macOS 底层脚本，不能在 Linux 使用，也不要裸 `go build` 覆盖运行二进制后重启。

`--skip-pull` 部署当前工作树；不带时先要求工作树干净并 fast-forward pull。`--remote-okr-db` 只用于明确放弃本机 OKR 数据库改动、使用 Git 版本的场景；通常必须保留并提交 OKR 产品数据。

重启前查询 `/api/tasks?status=executing`。macOS 底层脚本会拒绝中断执行中 Task，API 不可达也会 fail-fast；Linux 没有这道检查，重启会打断当前执行子进程。Chat 与主服务共用进程和 `/api/chat/*`，重启会中断当前轮次，已保存的历史保留。

```bash
./scripts/jarvis-instance conf/config.yaml
./scripts/jarvis-api-base
curl --fail "$(./scripts/jarvis-api-base)/healthz"
curl --fail "$(./scripts/jarvis-api-base)/readyz" | jq
```

服务名、地址、日志从配置派生，不手写固定值。Linux unit 会被部署脚本重新生成，实例额外环境变量（如 OKR 登录应用密钥）放同名 `.d/` 目录的 drop-in。完整服务管理、前端开发、退出和恢复见 [运行与部署](docs/reference/operations.md)。

## 开发验证

```bash
go test ./cmd/... ./internal/...
npm --prefix web test
npm --prefix web run typecheck
git diff --check
```

需要真实外部服务或凭证的测试单独运行：

```bash
go test -tags=integration ./internal/...
```

前端改动还需启动开发服务并执行浏览器回归。一次性迁移、扫描和抽取 flags 以 `go run ./cmd/jarvis-server -h` 为准。

## 管理后台

主导航围绕对话、工作台、任务、已启用业务模块（如 OKR）、世界、插件、工作设定和系统管理组织。导航分组默认收起，之后在当前浏览器记住选择；工作台合并当日任务态势与历史回顾，自动化收进任务的二级页。

工作设定按任务执行、线索发现维护系统提示词、阶段规则、请示策略和生效预览。系统设置包含运行、调度、Skills、共享记忆、功能模块和应用版本。对话拥有独立持久会话与默认 Agent、模型、推理档位和执行边界，不复用 M3/M5 阶段语义；每个会话可单独选择。

完整 HTTP 能力分组见 [HTTP API](docs/reference/http-api.md)。

## 目录

```text
cmd/                 进程入口
internal/            后端模块
web/                 React + Vite 管理后台
conf/                基线配置、prompts、rules、Skills 和模块配置
.agents/skills/      Jarvis 领域 Skills
integrations/        外部集成与补丁
deploy/              服务模板
scripts/             安装、部署和 Agent 工具
docs/                当前架构、参考、提案、研究和正式交付物
data/                生成报告及随仓库提交的 OKR 产品数据库与资源
runs/                Agent 执行产物
var/                 本机日志、数据库与运行状态
```
