# Jarvis · 主动式任务数字分身

Jarvis 是运行在本地 Mac 可信环境中的个人任务 Agent。它从飞书消息和外部线索中保留原始证据，抽取 Todo，并由执行 Agent 判断是否值得推进、调用工具完成工作、处理等待与审批并留下结果。

系统另有两类后台 Agent：factengine 以持续世界建模为主要任务；主动巡视默认每小时读取世界模型、看护未闭环工作，并可在调查过程中顺手维护明确变化。任何需要改变外部世界的动作都只创建 Task，交给强 M5 执行。

先读：

1. [项目目标](goal.md)
2. [Agent 开发规范](AGENTS.md)
3. [当前架构](docs/00-overview.md)
4. [文档导航与状态](docs/README.md)

## 当前链路

```text
飞书 IM 事件 ───────────────┐
飞书 IM 轮询补偿 ───────────┤
                           ├─> M2 capture ─> message ─> M3 extract
外部 Skill / 定时任务 ─> /api/clues ────────┘             │
                                                            ├─ observing：保留观察，不创建 Task
                                                            └─ extracted
                                                                 │
                                               机械固化：Todo=materialized + Task
                                                                 │
                                                               M5 执行 Agent
                                                         ├─ completed -> Task=done
                                                         ├─ observing -> Task/Todo=observing
                                                         ├─ waiting   -> 到期续跑同一 Session
                                                         ├─ needs_human
                                                         ├─ needs_approval -> awaiting_approval
                                                         └─ failed
```

`extracted` Todo 不再经过模型判断，固化步骤只负责按 Todo ID/version 幂等创建 Task。M5 执行 Agent 持有全部语义判断权：调查真实状态、判断是否值得推进、选择动作并完成工作，或把来源 Todo 置回 `observing`。

审批不是固定流水线阶段，也不由 `action_type` 决定。M5 根据即将发生的具体副作用和 [`conf/prompts/m5-approval-policy.md`](conf/prompts/m5-approval-policy.md) 判断：无需审批就直接完成，需要审批才返回完整 proposal 并停在 `awaiting_approval`。代码修改也没有类型级豁免。

## 核心边界

| 模块 | 当前职责 | 代码入口 |
|---|---|---|
| M1 背景 | principal、项目、关键事项、人物、会话背景、人工资源 | `internal/background/` |
| M2 采集 | 飞书消息事件、会话发现、增量轮询补偿、principal activity、通用 clue 落库 | `internal/capture/` |
| M3 提取 | 证据校验、Todo 抽取/合并、上下文快照、语义去重 | `internal/extract/` |
| Todo 固化 | extracted Todo 按 ID/version 幂等创建 Task，不调用模型 | `internal/execute/materializer.go` |
| M5 执行 | 调查、执行、审批、等待/续跑、人工回复、结果留痕 | `internal/execute/` |
| 事实引擎 | 在关键路径外从 `message`、Todo、Task 通用蒸馏长期事实，并通过通用工具按需维护当前实体、关系和资料 | `internal/factengine/` |
| 主动巡视 | 周期读取世界模型、看护未闭环工作、按需维护内部认知、为外部行动创建普通 Task | `internal/proactive/` |
| 会议巡扫 | 采集已结束会议和未来 24 小时日程，分别触发会后整理与逐场处理判断 | `internal/meetingsweep/` |
| 晨间简报 | 工作日开工对齐：Skill 取证写稿，定时/手动触发，产物在本地 Markdown | `internal/morningbrief/` |
| 定时任务 | 周期/单次 Task，以及等待 Session 的未来唤醒 | `internal/scheduledtask/`, `internal/taskcreate/` |
| 实时协调 | 按持久化 ID/version 推进 M3→M5，cron 负责补偿 | `internal/pipeline/` |
| 背景事实 | 自然语言 Fact、实体间自然语言 RelationFact | `internal/progress/`, `internal/knowledge/` |
| 后台与观测 | Overview、日报、worklog、运行状态、日志 | `internal/insight/`, `internal/dailydigest/`, `internal/observability/` |
| Agent 配置面 | prompts、rules、Skills、shared memory、工具目录 | `internal/textstore/`, `internal/workrule/`, `internal/skill/`, `internal/sharedmem/`, `internal/toolcatalog/` |

三条不可破坏的规则：

- M2 只记录事实。错误原文也是事实，错误语义和下一步交给模型判断。
- 新来源通过 `source + Skill/定时任务 + POST /api/clues` 接入，不在 Go 中新增来源专用流水线。
- M3 冻结 `context_snapshot`，Todo→Task→执行复用同一份；下游可补证据，但不重建一份“看起来等价”的背景。
- factengine 是持续世界建模的主要 Agent；主动巡视以看护和推进为主，但调查中可直接维护明确、有用的内部认知，也可把原始证据送入统一线索入口。外部行动统一创建 `source_type=proactive` 的 Task 交给 M5。

各模块的当前实现详见 [`docs/modules/`](docs/README.md#当前实现)。

## Source of truth

不要从文档复制容易漂移的字段、路由或数值：

| 主题 | 权威来源 |
|---|---|
| 项目目标 | `goal.md` |
| Agent 行为和开发约束 | `AGENTS.md`, `conf/prompts/`, `conf/rules/`, `.agents/skills/` |
| 数据模型与迁移 | `internal/domain/*.go`, `internal/store/sqlite.go` |
| HTTP 路由 | `internal/api/router.go` |
| 基线配置 | `conf/config.yaml` |
| 本机运行时覆盖 | `conf/config.runtime.yaml` |
| 页面入口 | `web/src/App.tsx` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools` |

有效配置是 `conf/config.yaml` 与同目录 `conf/config.runtime.yaml` 的合并结果：runtime 文件按叶子 key 覆盖基线，未出现的 key 保留基线值，两个文件都拒绝未知字段。

**改本机运行参数改 `conf/config.runtime.yaml`，不要改 `conf/config.yaml`。** 后台设置页保存时会把整份可调参数快照写进 runtime 文件，此后基线里的同名 key 永久失效——这是「改了 `conf/config.yaml` 没有任何效果」的典型原因。基线只负责仓库默认值和 runtime 未覆盖的键（`sqlite.path`、`server.*`、`capture.hot_age_hours` 等）；身份与密钥（`extract.principal_open_id`、`card_approval.relay_secret`）只写 runtime 文件，它不进 Git、权限保持 `600`。飞书身份直接使用 lark-cli 当前默认身份。

后台保存 runtime settings 后需要重启服务；文档中的配置数值只代表仓库基线，不代表当前进程一定正在使用该值。

## 常见修改入口

- 改 M3 抽取口径：`conf/prompts/m3-system-prompt.md`；改上下文组装：`internal/extract/prompt.go`、`internal/extract/snapshot.go`
- 改 M5 执行行为：`conf/prompts/m5-system-prompt.md`、`conf/rules/m5.md`
- 改主动巡视行为：`conf/prompts/proactive-system-prompt.md`；改调度与调用：`internal/proactive/`
- 改审批尺度：`conf/prompts/m5-approval-policy.md`
- 改严格输出协议/状态路由：`internal/execute/prompt.go`、`internal/execute/store.go`
- 改工具说明：`internal/toolcatalog/` 或对应 Skill，不把工具手册复制进系统提示词
- 改调度、并发、超时等运行参数：`conf/config.runtime.yaml` 或后台设置页，改基线 `conf/config.yaml` 对已被 runtime 覆盖的 key 无效
- 加 HTTP 接口：`internal/api/`，并在 `internal/api/router.go` 注册
- 改表或字段：`internal/domain/` 与 `internal/store/sqlite.go`
- 改前端页面：`web/src/`

## 给其他人安装（推荐）

当前远端是需要权限的 Code 仓库。使用者先 clone **完整仓库**，再在仓库根目录启动支持 repo-local `.agents/skills/` 的 Agent：

```bash
git clone git@code.byted.org:chujiejie.1/jarvis_bot.git
cd jarvis_bot
```

然后直接告诉 Agent：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

`$install-jarvis` 是整个项目安装流程：先创建 `var/install/<run-id>/INSTALL_CHECKLIST.md`，再报告机器、配置、旧实例和服务事实，由用户的 Agent 选择依赖安装方式和旧实例处理方式。它先安装全部依赖并通过 `validate-dependencies`，然后把 lark-cli 当前默认 App 绑定到 CC Connect，启动并验收运行底座；服务就绪后转入 `$bootstrap-jarvis-world-model` 建立人物、项目、资料、重点事项和监听群，最后完成消息与 CC 对话的真实端到端验收。

repo-local Skill 会让 Agent 安装并验收 lark-cli、Lark Agent Skills 和仓库基线使用的 traex：

```bash
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies
```

lark-cli 使用 larksuite 官方 npm installer；traex 使用其 updater 公布的 Code 内网 stable installer。两者安装后都要读回版本，traex 还必须完成 SSO 登录。CC Connect 的版本、upstream commit 和补丁位于 `integrations/cc-connect/`，由 `scripts/install-cc-connect.sh` 构建，只安装 binary，不在依赖阶段启动。Qdrant 是可以在此时启动的依赖服务。内置服务安装目前验收 macOS arm64。

## 本地运行

要求 Go 1.26.4 或更高版本、C 编译器（macOS 装 Xcode Command Line Tools）、满足 Vite engines 的 Node（`^20.19.0` 或 `>=22.12.0`）/npm、`jq`、`git`、`lark-cli`、配置中实际选择的 Agent CLI（仓库基线是 `traex`，可改成其他兼容 CLI）和 Qdrant。doctor 会从合并后的配置读取应检查的 binary，不把 `traex` 写死成安装协议。`conf/config.yaml` 使用本机明文密钥，权限保持 `600`；请注意仓库配置可能包含真实凭证。SQLite 文件及父目录会在启动时自动创建，无需单独安装或建库。

C 编译器是硬依赖：持久层用 `gorm.io/driver/sqlite`，它包装 `mattn/go-sqlite3` 走 CGO。缺了它 `go build` 仍会成功并链接一个 stub，直到启动打开数据库才报 `go-sqlite3 requires cgo to work`，所以两个构建脚本都先跑 `scripts/check-build-toolchain.sh` 把问题挡在构建前。`jq` 供 `scripts/jarvis-tools` 和构建脚本解析 API 响应。

构建期只依赖公网：HTTP 框架用开源 `github.com/cloudwego/hertz`，`go.mod` 里没有 `code.byted.org` 模块，不需要内网 GOPROXY；`web/package-lock.json` 里的 `resolved` 全部指向 `registry.npmjs.org`，`npm ci` 不需要内网镜像。运行期仍然需要内部 CLI（`traex`、`lark-cli`），它们靠 PATH 查找。

只执行迁移或一次性动作：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -migrate-only
go run ./cmd/jarvis-server -config conf/config.yaml -discover-once
go run ./cmd/jarvis-server -config conf/config.yaml -scan-chat oc_xxx
go run ./cmd/jarvis-server -config conf/config.yaml -extract-facts-once
go run ./cmd/jarvis-server -config conf/config.yaml -extract-once
```

全部一次性 flags 以 `go run ./cmd/jarvis-server -h` 为准。

### 安装与重建

```bash
# clone 后的第一个项目动作：建立整个安装过程的状态页
./scripts/jarvis-install start

# 推荐让 Agent 先取得事实；fresh clone 的 identity 配置不完整是正常状态
./scripts/jarvis-install doctor

# 按 doctor 结果安装运行 CLI；已有且可用时是无修改的验证
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect

# 安装/确认 Qdrant，然后通过全部依赖验收门
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies

# 依赖门通过后登录 lark-cli 当前默认身份，再写本机 identity、绑定 CC：
./scripts/jarvis-install configure-identity --open-id <open_id> --git-author <author>
./scripts/jarvis-install bind-cc
./scripts/jarvis-install validate-binding

# 启动补丁版 CC Connect 后，fresh clone 安装主服务：
./bin/cc-connect-jarvis daemon install --config "$HOME/.cc-connect/config.toml"
./scripts/jarvis-install install-server

# 系统级验收
./scripts/jarvis-install validate

# 把同一个 run_dir 交给 $bootstrap-jarvis-world-model 完成安装清单 E 区
# 再完成监听群新消息和绑定 Bot 对话的真实端到端验收，最后读回总状态
./scripts/jarvis-install status --run-dir <run_dir>

# 日常后端修改后重建、稳定签名并重启
./scripts/rebuild-server.sh

curl http://127.0.0.1:18800/healthz

# 逐项检查外部依赖（SQLite / Qdrant / lark-cli / agent CLI）
curl -s http://127.0.0.1:18800/readyz | jq
```

不要在 fresh clone 上提前运行 `install-launchd.sh`、`rebuild-server.sh` 或 `install-server`：必须先通过 `validate-dependencies`，再完成 identity 与 CC 绑定。世界模型初始化在服务就绪后执行，不是启动前置条件，但属于整体项目安装的一部分。`var/install/<run-id>/INSTALL_CHECKLIST.md` 从 checkout 一直记录到端到端验收，逐项标记完成、未做、阻塞或不适用及其原因。

不要裸 `go build` 覆盖 `bin/jarvis-server` 后直接重启，否则会破坏 macOS TCC 的稳定签名。

`rebuild-server.sh` 会先查询正在执行的 Task；服务已注册但 API 不可达时会 fail-fast，`--force-interrupt-running-tasks` 也不会绕过这项检查。先确认没有活跃执行，再做故障恢复。

### 服务与端口

| 服务 | 端口 | 用途 |
|---|---:|---|
| `com.bytedance.jarvis.server` | 18800 | Hertz API + 生产 `web/dist` + 流水线与 cron |
| `com.bytedance.jarvis.web` | 18801 | Vite 开发热更；生产不依赖 |
| `com.bytedance.jarvis.qdrant` | 6333/6334 | HTTP / gRPC，当前只用于 Todo 语义去重 |
| `com.cc-connect.service` | 9810/9820 | 独占同一 Jarvis Bot WebSocket，承载 Agent 入口、文档评论与审批 relay |

仓库没有 Web launchd 安装脚本。首次启用 18801 时先 `./scripts/render-launchd-plist.sh com.bytedance.jarvis.web`，再对渲染出的 plist 执行 `launchctl bootstrap`。

launchd 不接受相对路径，所以 `deploy/` 只存 `*.plist.template`，安装脚本用 `scripts/render-launchd-plist.sh` 把 `__JARVIS_ROOT__` 和 `__HOME__` 展开到 `~/Library/LaunchAgents/`。仓库换目录或换用户后重新渲染即可，不需要改仓库文件。

详细运维说明见 [docs/reference/operations.md](docs/reference/operations.md)。

## 测试

```bash
go test ./...
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
git diff --check
```

依赖真实模型、Agent CLI 或完整配置的测试使用 `integration` tag；缺少依赖时应 fail-fast，不用 `t.Skip` 伪装通过。

## HTTP 与 Agent 工具

完整 HTTP 分组见 [docs/reference/http-api.md](docs/reference/http-api.md)，注册真源仍是 `internal/api/router.go`。

`scripts/jarvis-tools` 不直连数据库，而是用 `curl + jq` 调用正在运行的 jarvis-server。它默认从 `conf/config.yaml` 读取 `server.addr`，也可用 `JARVIS_API_BASE` 覆盖；服务不在线时命令会失败。

## 管理后台

生产访问 `http://127.0.0.1:18800/`。当前 9 个主入口：

- Overview
- 任务
- 定时任务
- 待办
- 背景
- Agent 设置
- 设置
- 进度
- 运行状态

Agent 设置和自动化都是最外层入口：Agent 设置按「任务执行」「线索发现」集中维护系统提示词、阶段工作规则、审批策略及生效预览，二级配置使用横向 Tab 切换，其他 Agent 提示词也保留在该页；自动化集中管理周期与单次定时任务。设置页包含运行配置、系统任务、Skills 和共享记忆。右侧流式对话通过条件注册的 `POST /api/chat` 使用执行模型。

## 目录

```text
cmd/jarvis-server/   主入口与一次性 CLI
internal/            后端模块
web/                 React + Vite 管理后台
conf/                基线配置、prompts、rules、Skills 配置
deploy/              launchd plist
scripts/             安装、签名、重建、jarvis-tools
docs/                当前架构、模块文档、提案、研究和历史索引
data/                生成的日报/周报等本地数据
runs/                Agent 执行产物
var/                 日志、数据库侧车数据和运行时文件
```
