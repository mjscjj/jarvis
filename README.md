# Jarvis · 主动式任务数字分身

Jarvis 是运行在本地 Mac 可信环境中的个人任务 Agent。它从飞书消息和外部线索中保留原始证据，抽取 Todo，并由执行 Agent 判断是否值得推进、调用工具完成工作、处理等待、需要时在飞书上问你一句，并留下结果。

系统另有两类后台 Agent：factengine 以持续世界建模为主要任务；主动巡视默认每小时读取世界模型、看护未闭环工作，并可在调查过程中顺手维护明确变化。任何需要改变外部世界的动作都只创建 Task，交给强 M5 执行。

先读：

1. [项目目标](goal.md)
2. [Agent 开发规范](AGENTS.md)
3. [当前架构](docs/00-overview.md)
4. [文档导航与状态](docs/README.md)

## 当前链路

```text
飞书 Bot WebSocket ─> CC Connect
                       ├─ 接受的私聊/@消息 ─> route claim ─> CC 原生 Agent/session
                       └─ 未接受的普通群消息（等待 M2 轮询）

飞书 IM 轮询补偿 ───────────┐
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
                                                         ├─ needs_human -> 问题卡 -> 回答续跑同一 Session
                                                         └─ failed
```

`extracted` Todo 不再经过模型判断，固化步骤只负责按 Todo ID/version 幂等创建 Task。M5 执行 Agent 持有全部语义判断权：调查真实状态、判断是否值得推进、选择动作并完成工作，或把来源 Todo 置回 `observing`。

要不要先问 principal 不是固定流水线阶段，也不由 `action_type` 决定。M5 根据即将发生的具体副作用和 [`conf/prompts/m5-approval-policy.md`](conf/prompts/m5-approval-policy.md) 判断：不用问就直接完成，要问就返回 `needs_human` 加一份 `question`，停在 `needs_human` 等回答。代码修改也没有类型级豁免。

请示副作用和补充信息用的是同一个机制：`question` 自带按钮、下拉、多选、输入框和链接，runtime 只负责把它渲染成飞书卡片、把回答原样交回同一个 Codex Session。答案怎么理解由那个 Session 判断，Jarvis 不解释、也没有单独的批准/驳回接口。

## 核心边界

| 模块 | 当前职责 | 代码入口 |
|---|---|---|
| M1 背景 | principal、项目、关键事项、人物、会话背景、人工资源 | `internal/background/` |
| M2 采集 | 飞书消息事件、会话发现、增量轮询补偿、principal activity、通用 clue 落库 | `internal/capture/` |
| M3 提取 | 证据校验、Todo 抽取/合并、上下文快照、语义去重 | `internal/extract/` |
| Todo 固化 | extracted Todo 按 ID/version 幂等创建 Task，不调用模型 | `internal/execute/materializer.go` |
| M5 执行 | 调查、执行、提问、等待/续跑、人工回答、结果留痕 | `internal/execute/`, `internal/cardask/` |
| 事实引擎 | 在关键路径外从 `message`、Todo、Task 通用蒸馏长期事实，并通过通用工具按需维护当前实体、关系和资料 | `internal/factengine/` |
| 主动巡视 | 周期读取世界模型、看护未闭环工作、按需维护内部认知、为外部行动创建普通 Task | `internal/proactive/` |
| 会议巡扫 | 采集已结束会议和未来 24 小时日程，分别触发会后整理与逐场处理判断 | `internal/meetingsweep/` |
| 晨间简报 | 工作日开工对齐：Skill 取证写稿，定时/手动触发，产物在本地 Markdown | `internal/morningbrief/` |
| 定时任务 | 周期/单次 Task，以及等待 Session 的未来唤醒 | `internal/scheduledtask/`, `internal/taskcreate/` |
| 插件 | 按需启用外部来源或阶段能力；采集插件通过定时任务 + Skill 投递 clue，能力插件复用现有 M3/M5 与 Task 链路 | `internal/plugin/`, [`docs/plugin-system.md`](docs/plugin-system.md) |
| 实时协调 | 按持久化 ID/version 推进 M3→M5，cron 负责补偿 | `internal/pipeline/` |
| 背景事实 | 实体长期事实页 `summary`（整体读写、有上限、页内引用）与自然语言 Fact | `internal/background/`, `internal/progress/`, `internal/knowledge/` |
| 后台与观测 | Overview、日报、worklog、运行状态、日志 | `internal/insight/`, `internal/dailydigest/`, `internal/observability/` |
| Agent 配置面 | prompts、rules、Skills、shared memory、工具目录 | `internal/textstore/`, `internal/workrule/`, `internal/skill/`, `internal/sharedmem/`, `internal/toolcatalog/` |

三条不可破坏的规则：

- M2 只记录事实。错误原文也是事实，错误语义和下一步交给模型判断。
- 新来源通过 `source + Skill/定时任务 + POST /api/clues` 接入，不在 Go 中新增来源专用流水线。
- M3 冻结 `Todo.content`（原始来源、冻结事实和模型说明），固化到 `Task.source_payload`；M5 默认读触发原文与现场摘要，其余按需下钻；下游可补证据，但不重建一份“看起来等价”的背景。
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
- 新增插件或拆分插件耦合：[插件扩展与解耦规范](docs/summery/plugin-extension-spec.md)（含当前接入步骤与尚未实施的改造提案）

## 安装方式

macOS 14+ Apple Silicon 用户优先使用 DMG：

- [macOS 安装、覆盖安装与自动更新](docs/reference/macos-install-and-update.md)

0.1.1 及后续版本会从 `jarvisx.bytedance.net` 检查签名更新并自动安装；0.1.0
及更早版本需要先手动覆盖安装 0.1.1。应用位于 `/Applications/Jarvis.app`，用户数据
独立保存在 `~/Library/Application Support/Jarvis`，覆盖应用不会删除数据。

### 源码安装

日常打开页面先通过 `/api/setup/bootstrap` 读取本机配置，已安装用户无需等待外部 CLI 验证；`/api/setup/status` 在后台完整检查连接，失败时以顶部提示和授权抽屉处理，不卸载当前页面。首次安装及安装重启恢复仍等待完整检查；此优化不改变网页登录校验，也不缓存授权结果。

当前远端是需要权限的 Code 仓库。使用者先 clone **完整仓库**，再在仓库根目录启动支持 repo-local `.agents/skills/` 的 Agent：

```bash
git clone git@code.byted.org:chujiejie.1/jarvis_bot.git
cd jarvis_bot
```

然后直接告诉 Agent：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

`$install-jarvis` 是用户唯一需要触发的整个项目安装流程：先创建或恢复 `var/install/<run-id>/INSTALL_CHECKLIST.md`，再报告机器、配置、旧实例和服务事实，由用户的 Agent 选择依赖安装方式和旧实例处理方式。它先安装全部依赖并通过 `validate-dependencies`，然后把 lark-cli 当前默认 App 绑定到 CC Connect，启动并验收运行底座；服务就绪后内部调用 `$bootstrap-jarvis-world-model` 建立人物、项目、资料、重点事项和监听群，最后完成消息与 CC 对话的真实端到端验收。独立重建世界模型时才单独使用后者。

repo-local Skill 会让 Agent 安装并验收 lark-cli、Lark Agent Skills、bytedcli、Codex、仓库配置使用的 Agent CLI、补丁版 CC Connect 和 Qdrant：

```bash
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-bytedcli
./scripts/jarvis-install install-codex
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies
```

lark-cli 首装使用 larksuite 官方 npm installer，已有版本低于 `1.0.93` 或 Skills/协议不完整时通过 `lark-cli update --json` 同步；Codex 通过官方 `@openai/codex` npm 包安装；traex 使用其 updater 公布的 Code 内网 stable installer。安装后都要读回版本，Codex 与 traex 还必须分别完成登录。CC Connect 的版本、upstream commit 和补丁位于 `integrations/cc-connect/`，由 `scripts/install-cc-connect.sh` 构建，只安装 binary，不在依赖阶段启动。Qdrant 是可以在此时启动的依赖服务。内置服务安装支持 macOS arm64 与 Linux x86_64。

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
# clone 后的第一个项目动作：恢复最近的安装状态页；不存在时新建
./scripts/jarvis-install start --resume-latest

# 推荐让 Agent 先取得事实；fresh clone 的 identity 配置不完整是正常状态
./scripts/jarvis-install doctor

# 按 doctor 结果安装运行 CLI；已有且可用时是无修改的验证
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-bytedcli
./scripts/jarvis-install install-codex
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect

# 安装/确认 Qdrant，然后通过全部依赖验收门
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies

# 依赖门通过后登录 lark-cli 当前默认身份，再写本机 identity、绑定 CC：
./scripts/jarvis-install configure-identity --agent-name <name> --open-id <open_id> --git-author <author>
printf '%s\n' '<App Secret>' | ./scripts/jarvis-install bind-cc
./scripts/jarvis-install validate-binding

`bind-cc` 会立即验证 App ID/Secret。已有 CC Connect Feishu `allow_from` 不是 Principal 本人时命令会停止；用户确认替换后才可加 `--replace-allow-from`。`validate-binding` 会拒绝缺失或通配的访问白名单。需要临时开放给其他人时，应作为当前机器的显式运行决策处理。

# 启动补丁版 CC Connect 后，fresh clone 安装主服务（Linux 可用
# scripts/install-cc-systemd.sh <独立配置路径> 避免覆盖别的 CC 项目）：
./bin/cc-connect-jarvis daemon install --config "$HOME/.cc-connect/config.toml"
# Linux 改用：./scripts/install-cc-systemd.sh "$HOME/.cc-connect/config.toml"
./scripts/jarvis-install install-server

# 系统级验收
./scripts/jarvis-install validate

# 把同一个 run_dir 交给 $bootstrap-jarvis-world-model 完成安装清单 E 区
# 再完成监听群新消息和绑定 Bot 对话的真实端到端验收，最后读回总状态
./scripts/jarvis-install status --run-dir <run_dir>

# 日常后端修改后重建并重启；macOS 保持稳定签名，Linux 使用 systemd
./scripts/rebuild-server.sh

curl http://127.0.0.1:18800/healthz

# 逐项检查外部依赖（SQLite / Qdrant / lark-cli / agent CLI）
curl -s http://127.0.0.1:18800/readyz | jq
```

不要在 fresh clone 上提前运行底层服务安装脚本、`rebuild-server.sh` 或 `install-server`：必须先通过 `validate-dependencies`，再完成 identity 与 CC 绑定。世界模型初始化在服务就绪后执行，不是启动前置条件，但属于整体项目安装的一部分。`var/install/<run-id>/INSTALL_CHECKLIST.md` 从 checkout 一直记录到端到端验收，逐项标记完成、未做、阻塞或不适用及其原因。

不要裸 `go build` 覆盖 `bin/jarvis-server` 后直接重启，否则会破坏 macOS TCC 的稳定签名。

`rebuild-server.sh` 在服务已注册时先查询正在执行的 Task，再开始构建；API 不可达时会 fail-fast，`--force-interrupt-running-tasks` 也不会绕过这项检查。对于已经确认复用的 checkout，若服务注册丢失，它会按当前平台恢复生产前端、后端和服务注册，不重新执行完整安装，也不升级 CC Connect。fresh clone 仍必须走上面的 `install-server` 流程。

### 服务与端口

| 服务 | 端口 | 用途 |
|---|---:|---|
| `com.bytedance.jarvis.server` | 18800 | Hertz API + 生产 `web/dist` + 流水线与 cron |
| `com.bytedance.jarvis.web` | 18801 | Vite 开发热更；生产不依赖 |
| `com.bytedance.jarvis.qdrant` | 6333/6334 | HTTP / gRPC，当前只用于 Todo 语义去重 |
| `com.cc-connect.service`（macOS）/ `com.bytedance.jarvis.cc-connect`（Linux） | 9810/9820 | 独占同一 Jarvis Bot WebSocket，承载 Agent 入口、文档评论与问题卡 relay |

仓库没有 Web launchd 安装脚本。首次启用 18801 时先 `./scripts/render-launchd-plist.sh com.bytedance.jarvis.web`，再对渲染出的 plist 执行 `launchctl bootstrap`。

`deploy/` 同时保存 launchd 与 systemd 模板；渲染脚本把当前仓库和用户目录写入本机服务定义。仓库换目录或换用户后重新安装服务即可，不需要改仓库文件。

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
- 工作设定
- 设置
- 进度
- 运行状态

「工作设定」顶部提供全局主动程度：安静 / 普通 / 活跃，默认普通。当前选择保存在 `conf/prompts/initiative-level.md`，保存后 M3 后续批次、M5 新执行及等待/人工回答恢复、主动巡视下一轮实时读取，无需重启。M3/M5 三档行为在各自「工作规则」编辑；主动巡视规则与生效预览位于「其他 Agent」。生效预览与运行时共用组装逻辑，包含当前档位、对应规则和 M5 审批策略。

档位控制后台准入、可选扩展与通知尺度；明确交办、明确订阅和已有承诺继续履行，不取消已建任务或撤回问题卡。普通档沿用原有行为；安静档优先明确介入，活跃档增加有依据的提前准备。审批、采集和 CC Connect 直接交互继续使用原机制。详细行为见[三档设计](docs/summery/initiative-level-design.md)。

工作设定和自动化都是最外层入口：工作设定按「任务执行」「线索发现」集中维护系统提示词、阶段工作规则、审批策略及生效预览，二级配置使用横向 Tab 切换，其他 Agent 提示词也保留在该页；自动化集中管理周期与单次定时任务。设置页包含运行配置、系统任务、Skills 和共享记忆。首个「对话」Tab 使用持久会话 API，并可按会话选择 Codex、TRAE 或 Cursor 及其动态模型目录。

左侧「对话」保留完整历史页；其他页面底部提供紧凑快捷对话，默认可输入，最新回复显示一行，点击可查看全文。会话、模型、Agent、推理强度、上下文引用与附件复用同一套会话 API。`Chat` 在应用内持续挂载，完整页与 `components/ChatDock.tsx` 只切换展示，因此切页不丢草稿、不打断流式回复，也不会覆盖当前工作台的 URL 筛选条件。刷新后的会话通过详情中的 `running` 恢复停止入口，每 2 秒检查后台回复是否结束；不重连旧流，也不自动重发。后端持久化消息和附件后发送 SSE `accepted`，前端收到确认才清空输入，拒绝发送则保留草稿与附件。浏览器交互回归位于 `web/test/chatDock.browser.mjs`：启动 Vite 后执行；可通过 `CHAT_TEST_URL`、`PLAYWRIGHT_MODULE`、`CHROME_EXECUTABLE` 指定测试环境，所有 API 使用隔离测试数据，不调用真实 Agent。

## 目录

```text
cmd/jarvis-server/   主入口与一次性 CLI
internal/            后端模块
web/                 React + Vite 管理后台
conf/                基线配置、prompts、rules、Skills 配置
deploy/              launchd / systemd 服务模板
scripts/             安装、签名、重建、jarvis-tools
docs/                当前架构、模块文档、提案、研究和历史索引
data/                生成的日报/周报等本地数据
runs/                Agent 执行产物
var/                 日志、数据库侧车数据和运行时文件
```
