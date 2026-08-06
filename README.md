# Jarvis · 主动式任务数字分身

Jarvis 是运行在本地 Mac 可信环境中的个人任务 Agent。它从飞书消息和外部线索中保留原始证据，抽取 Todo，并由执行 Agent 判断是否值得推进、调用工具完成工作、处理等待与审批并留下结果。

系统另有两类低成本 Agent：factengine 以持续世界建模为主要任务；主动巡视默认每小时读取世界模型、看护未闭环工作，并可在调查过程中顺手维护明确变化。任何需要改变外部世界的动作都只创建 Task，交给强 M5 执行。

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

有效配置是 `conf/config.yaml` 与同目录 `conf/config.runtime.yaml` 的合并结果。后台保存 runtime settings 后需要重启服务；文档中的配置数值只代表仓库基线，不代表当前进程一定正在使用该值。

## 常见修改入口

- 改 M3 抽取口径：`conf/prompts/m3-system-prompt.md`；改上下文组装：`internal/extract/prompt.go`、`internal/extract/snapshot.go`
- 改 M5 执行行为：`conf/prompts/m5-system-prompt.md`、`conf/rules/m5.md`
- 改主动巡视行为：`conf/prompts/proactive-system-prompt.md`；改调度与调用：`internal/proactive/`
- 改审批尺度：`conf/prompts/m5-approval-policy.md`
- 改严格输出协议/状态路由：`internal/execute/prompt.go`、`internal/execute/store.go`
- 改工具说明：`internal/toolcatalog/` 或对应 Skill，不把工具手册复制进系统提示词
- 加 HTTP 接口：`internal/api/`，并在 `internal/api/router.go` 注册
- 改表或字段：`internal/domain/` 与 `internal/store/sqlite.go`
- 改前端页面：`web/src/`

## 本地运行

要求 Go 1.26.4、Node/npm、`traex`、`lark-cli` 和 Qdrant。`conf/config.yaml` 使用本机明文密钥；请注意仓库配置可能包含真实凭证。SQLite 文件及父目录会在启动时自动创建，无需单独安装或建库。

构建期只依赖公网：HTTP 框架用开源 `github.com/cloudwego/hertz`，`go.mod` 里没有 `code.byted.org` 模块，不需要内网 GOPROXY。运行期仍然需要内部 CLI（`traex`、`lark-cli`），它们靠 PATH 查找。

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
# 主服务：安装依赖、构建前端、编译并签名后端、注册 launchd
./scripts/install-launchd.sh

# Qdrant 单独安装
./scripts/install-qdrant.sh

# 日常后端修改后重建、稳定签名并重启
./scripts/rebuild-server.sh

curl http://127.0.0.1:18800/healthz

# 逐项检查外部依赖（SQLite / Qdrant / lark-cli / agent CLI）
curl -s http://127.0.0.1:18800/readyz | jq
```

不要裸 `go build` 覆盖 `bin/jarvis-server` 后直接重启，否则会破坏 macOS TCC 的稳定签名。

`rebuild-server.sh` 会先查询正在执行的 Task；服务已注册但 API 不可达时会 fail-fast，`--force-interrupt-running-tasks` 也不会绕过这项检查。先确认没有活跃执行，再做故障恢复。

### 服务与端口

| 服务 | 端口 | 用途 |
|---|---:|---|
| `com.bytedance.jarvis.server` | 18800 | Hertz API + 生产 `web/dist` + 流水线与 cron |
| `com.bytedance.jarvis.web` | 18801 | Vite 开发热更；生产不依赖 |
| `com.bytedance.jarvis.qdrant` | 6333/6334 | HTTP / gRPC，当前只用于 Todo 语义去重 |

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

生产访问 `http://127.0.0.1:18800/`。当前 8 个主入口：

- Overview
- 任务
- 定时任务
- 待办
- 背景
- 设置
- 进度
- 运行状态

设置页包含运行配置、系统任务、工作规则、系统提示词、审批规则、Skills 和共享记忆。右侧流式对话通过条件注册的 `POST /api/chat` 使用执行模型。

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
