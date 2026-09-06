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
| 插件 | 按需启用和授权外部来源，以定时任务 + Skill 向通用 clue 入口投递原始证据 | `internal/plugin/`, [`docs/plugin-system.md`](docs/plugin-system.md) |
| 实时协调 | 按持久化 ID/version 推进 M3→M5，cron 负责补偿 | `internal/pipeline/` |
| 背景事实 | 实体长期事实页 `summary`（整体读写、有上限、页内引用）与自然语言 Fact | `internal/background/`, `internal/progress/`, `internal/knowledge/` |
| 后台与观测 | Overview、日报、worklog、运行状态、日志 | `internal/insight/`, `internal/dailydigest/`, `internal/observability/` |
| Agent 配置面 | prompts、rules、Skills、shared memory、工具目录 | `internal/textstore/`, `internal/workrule/`, `internal/skill/`, `internal/sharedmem/`, `internal/toolcatalog/` |

三条不可破坏的规则：

- M2 只记录事实。错误原文也是事实，错误语义和下一步交给模型判断。
- 新来源通过 `source + Skill/定时任务 + POST /api/clues` 接入，不在 Go 中新增来源专用流水线。
- M3 冻结 `Todo.content`（原始来源、冻结事实和模型说明），固化到 `Task.source_payload`；M5 默认读触发原文与现场摘要，其余按需下钻；下游可补证据，但不重建一份“看起来等价”的背景。
- factengine 是持续世界建模的主要 Agent；主动巡视以看护和推进为主，但调查中可直接维护明确、有用的内部认知，也可把原始证据送入统一线索入口。外部行动统一创建 `source_type=proactive` 的 Task 交给 M5。

各模块的当前实现详见 [`docs/modules/`](docs/README.md#当前实现)；OKR 维护入口见 [OKR 模块当前实现](docs/modules/06-okr.md)。

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
| 飞书应用与登录身份 | [docs/design-dual-app-identity.md](docs/design-dual-app-identity.md)，配置真源是 `conf/okr-module.yaml` 与 `conf/okr-feishu-scopes.txt` |

有效配置是 `conf/config.yaml` 与同目录 `conf/config.runtime.yaml` 的合并结果：runtime 文件按叶子 key 覆盖基线，未出现的 key 保留基线值，两个文件都拒绝未知字段。

**改本机运行参数改 `conf/config.runtime.yaml`，不要改 `conf/config.yaml`。** 后台设置页保存时会把整份可调参数快照写进 runtime 文件，此后基线里的同名 key 永久失效——这是「改了 `conf/config.yaml` 没有任何效果」的典型原因。基线只负责仓库默认值和 runtime 未覆盖的键（`sqlite.path`、`server.*`、`capture.hot_age_hours` 等）；身份与密钥（`extract.principal_open_id`、`card_approval.relay_secret`）只写 runtime 文件，它不进 Git、权限保持 `600`。Jarvis 本体的飞书身份直接使用 lark-cli 当前默认身份，也就是权限最大、只服务 principal 一人的主应用。OKR 模块的页面登录是**另一个**低敏应用，见 [双飞书应用身份设计](docs/design-dual-app-identity.md)。

后台保存 runtime settings 后需要重启服务；文档中的配置数值只代表仓库基线，不代表当前进程一定正在使用该值。

`server.addr` 是本实例后端地址的唯一配置。`jarvis-server` 启动时把实际地址、绝对配置路径和本仓库工具目录导出为 `JARVIS_API_BASE`、`JARVIS_CONFIG`、`PATH`，所有 Agent 子进程继承；切换工作目录不会切换实例。通用工具、世界模型工具、OKR/周报工具统一使用 `scripts/jarvis-api-base`：先用继承的 API 地址，否则通过 `scripts/jarvis-instance` 读取选定配置（默认本仓库 `conf/config.yaml` 加运行时覆盖）。配置错误直接失败，不扫描端口。工具需要 Go 和 jq；模块工具的显式 `--base-url` 可以指定其它实例。环境变量统一为 `JARVIS_API_BASE`，不再使用 `JARVIS_BASE_URL`。

启动、重建、健康检查和开发代理也读取同一配置。`scripts/jarvis-instance [CONFIG_PATH]` 输出地址与服务名；服务名按配置文件绝对路径生成，改端口不改服务名，不同配置不会共用 launchd job。Vite 默认从后端相邻端口开始，并在它与 `chat.addr` 相同时再顺延一个端口；端口占用直接报错。两实例仍需分别配置数据库/产物路径，飞书账号、Qdrant collection 等外部资源不会因更换 HTTP 端口自动隔离。

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

lark-cli 使用 larksuite 官方 npm installer；traex 使用其 updater 公布的 Code 内网 stable installer。两者安装后都要读回版本，traex 还必须完成 SSO 登录。CC Connect 的版本、upstream commit 和补丁位于 `integrations/cc-connect/`，由 `scripts/install-cc-connect.sh` 构建，只安装 binary，不在依赖阶段启动。Qdrant 是可以在此时启动的依赖服务。内置服务安装支持 macOS arm64 与 Linux x86_64。

## 本地运行

要求 Go 1.26.4 或更高版本、C 编译器（macOS 装 Xcode Command Line Tools）、满足 Vite engines 的 Node（`^20.19.0` 或 `>=22.12.0`）/npm、`jq`、`git`、`lark-cli`、配置中实际选择的 Agent CLI（仓库基线是 `traex`，可改成其他兼容 CLI）和 Qdrant。doctor 会从合并后的配置读取应检查的 binary，不把 `traex` 写死成安装协议。`conf/config.yaml` 使用本机明文密钥，权限保持 `600`；请注意仓库配置可能包含真实凭证。本机运行 SQLite 会自动创建；可选 OKR 模块的产品数据库随仓库位于 `data/okr/okr.db`。

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

### 改完代码怎么生效（日常回路）

**改完任何代码，跑这一条就够，两个系统通用：**

```bash
./scripts/jarvis-deploy --skip-pull
```

它做完整的一轮：`npm ci` + 构建 `web/dist`、编译 `bin/jarvis-server` 和 `bin/jarvis-chat-server`、按当前系统重启服务，最后验证首页、`/healthz` 和 `/readyz`（要求所有依赖为 `ok` 或 `disabled`），任何一步失败都直接退出。`--skip-pull` 表示部署当前工作树，不拉 upstream——本地开发几乎总是要带上它。

不是所有改动都需要重新部署：

| 改了什么 | 需要做什么 |
|---|---|
| Go 代码 | `./scripts/jarvis-deploy --skip-pull` |
| 只改前端 | `npm --prefix web run build`，刷新页面即可，后端直接读 `web/dist` 目录 |
| `conf/prompts/*.md`、`conf/rules/*.md` | 什么都不用做。`textstore` 和 `workrule` 每次调用都从磁盘读，下一次 Agent 运行就是新内容 |
| `conf/*.yaml`（含 `config.runtime.yaml`、`okr-module.yaml`、`modules.yaml`） | 只需重启服务，不必重新编译，见下面的重启命令 |

不带 `--skip-pull` 时它会先要求工作树干净、拉 `--ff-only`，然后用相同流程部署；`--remote-okr-db` 用于明确以 Git 中的 OKR 数据库覆盖本机改动。

**不要按操作系统各自发挥。** 两个系统的差别 `jarvis-deploy` 已经处理掉了：

| | macOS | Linux（本机） |
|---|---|---|
| 服务管理 | launchd（`launchctl`） | 无需 root 的 user systemd |
| 底层重建脚本 | `scripts/rebuild-server.sh`（zsh + 稳定签名 + `launchctl kickstart`） | 无独立脚本，逻辑在 `jarvis-deploy` 的 `deploy_linux` 里 |
| 单独重启 | `launchctl kickstart -k gui/$UID/<label>` | `systemctl --user restart <label>.service` |

`scripts/rebuild-server.sh` 只在 macOS 可用（它依赖 zsh、`codesign`、`launchctl`、`plutil`），**在 Linux 上跑不通，不要试**。同理 `internal/toolcatalog` 里两个安装用例在 Linux 上必然因缺 `plutil` 失败，属于已知的平台差异，不是回归。

实例的地址、服务名和日志路径都由配置派生，不要手写：

```bash
./scripts/jarvis-instance conf/config.yaml   # api_base / launchd_label / chat_* / log_files
./scripts/jarvis-api-base                    # 只要后端地址
```

当前仓库这份配置解析出的是 `http://127.0.0.1:18802`、服务名 `com.bytedance.jarvis.server.462093b6e0bd71d9`，对话 sidecar 在 `18801`。换配置或换目录这些值都会变，所以脚本读一次比记住可靠。

日常排查（把 `<label>` 换成上面查到的服务名）：

```bash
# 只重启，不重新编译（改了 conf/*.yaml 或 conf/prompts/*.md 之后）
systemctl --user restart <label>.service            # Linux
launchctl kickstart -k "gui/$UID/<label>"           # macOS

systemctl --user status <label>.service --no-pager
tail -f var/log/jarvis-server.error.log

curl "$(./scripts/jarvis-api-base)/healthz"
curl -s "$(./scripts/jarvis-api-base)/readyz" | jq
```

对话 sidecar 是独立服务（`<label>.chat.service`），重建主服务不会打断正在进行的对话；只改对话配置时单独重启它即可。

### 首次安装

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
./scripts/jarvis-install configure-identity --agent-name <name> --open-id <open_id> --git-author <author>
./scripts/jarvis-install bind-cc
./scripts/jarvis-install validate-binding

`bind-cc` 会把 CC Connect Feishu `allow_from` 收紧为 Principal 本人；`validate-binding` 会拒绝缺失或通配的访问白名单。需要临时开放给其他人时，应作为当前机器的显式运行决策处理。

# 启动补丁版 CC Connect 后，fresh clone 安装主服务（Linux 可用
# scripts/install-cc-systemd.sh <独立配置路径> 避免覆盖别的 CC 项目）：
./bin/cc-connect-jarvis daemon install --config "$HOME/.cc-connect/config.toml"
./scripts/jarvis-install install-server

# 系统级验收
./scripts/jarvis-install validate

# 把同一个 run_dir 交给 $bootstrap-jarvis-world-model 完成安装清单 E 区
# 再完成监听群新消息和绑定 Bot 对话的真实端到端验收，最后读回总状态
./scripts/jarvis-install status --run-dir <run_dir>
```

装完之后的日常回路回到上一节的 `./scripts/jarvis-deploy --skip-pull`。

不要在 fresh clone 上提前运行 `install-launchd.sh`、`rebuild-server.sh` 或 `install-server`：必须先通过 `validate-dependencies`，再完成 identity 与 CC 绑定。世界模型初始化在服务就绪后执行，不是启动前置条件，但属于整体项目安装的一部分。`var/install/<run-id>/INSTALL_CHECKLIST.md` 从 checkout 一直记录到端到端验收，逐项标记完成、未做、阻塞或不适用及其原因。

在 macOS 上不要裸 `go build` 覆盖 `bin/jarvis-server` 后直接重启，否则会破坏 TCC 的稳定签名——这正是那里必须走 `rebuild-server.sh` 的原因。Linux 没有签名要求，`jarvis-deploy` 直接构建再换二进制。

macOS 的 `rebuild-server.sh` 会先查询正在执行的 Task；服务已注册但 API 不可达时会 fail-fast，`--force-interrupt-running-tasks` 也不会绕过这项检查。先确认没有活跃执行，再做故障恢复。Linux 路径没有这道 Task 检查，重启会打断正在执行的 Task 子进程，忙的时候先看一眼 `/api/tasks?status=executing`。

### 服务与端口

| 服务 | 端口 | 用途 |
|---|---:|---|
| `com.bytedance.jarvis.server.<配置路径摘要>` | `server.addr`（基线 18800） | Hertz API + 生产 `web/dist` + 流水线与 cron |
| `<实例服务名>.web` | 后端相邻且避开 Chat 的端口 | Vite 开发热更；生产不依赖 |
| `com.bytedance.jarvis.qdrant` | 6333/6334 | HTTP / gRPC，当前只用于 Todo 语义去重 |
| `com.cc-connect.service`（macOS）/ `com.bytedance.jarvis.cc-connect`（Linux） | 9810/9820 | 独占同一 Jarvis Bot WebSocket，承载 Agent 入口、文档评论与问题卡 relay |

服务名按配置文件绝对路径生成，所以同一台机器上多份配置各自独立，改端口不改服务名。

macOS：仓库没有 Web launchd 安装脚本，首次启用当前实例的 Vite 开发服务时先 `./scripts/render-launchd-plist.sh com.bytedance.jarvis.web`，再对渲染出的 plist 执行 `launchctl bootstrap`。launchd 不接受相对路径，所以 `deploy/` 只存 `*.plist.template`，安装脚本用 `scripts/render-launchd-plist.sh` 把 `__JARVIS_ROOT__` 和 `__HOME__` 展开到 `~/Library/LaunchAgents/`。仓库换目录或换用户后重新渲染即可，不需要改仓库文件。

Linux：unit 文件写在 `~/.config/systemd/user/<label>.service`，每次 `jarvis-deploy` 都会按当前仓库路径重新生成，所以不要手改它——要加环境变量（例如 OKR 模块登录应用的 `JARVIS_OKR_EMILY_APP_SECRET`）请放进同名 `.d/` 目录下的 drop-in，它不会被覆盖。仓库路径、配置路径和日志路径都不支持空格或 `%`。

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

生产访问 `scripts/jarvis-api-base` 输出的地址（仓库基线为 `http://127.0.0.1:18800/`）。主导航围绕工作台、任务、已启用业务模块（如 OKR）、世界、插件、工作设定和系统管理组织。工作台合并当日任务态势与历史回顾；自动化收进「任务」的二级页。

工作设定按「任务执行」「线索发现」集中维护系统提示词、阶段工作规则、审批策略及生效预览，二级配置使用横向 Tab 切换，其他 Agent 提示词也保留在该页。设置页包含运行配置、系统任务、Skills 和共享记忆。右侧流式对话由独立 `jarvis-chat-server` 提供，可单独配置 Codex/TraeX、模型和超时；重建主服务不会终止正在执行的对话。

## 目录

```text
cmd/jarvis-server/   主入口与一次性 CLI
internal/            后端模块
web/                 React + Vite 管理后台
conf/                基线配置、prompts、rules、Skills 配置
deploy/              launchd / systemd 服务模板
scripts/             安装、签名、重建、jarvis-tools
docs/                当前架构、模块文档、提案、研究和历史索引
data/                生成的日报/周报，以及随仓库提交的 OKR 模块数据库与资源
runs/                Agent 执行产物
var/                 日志、数据库侧车数据和运行时文件
```
