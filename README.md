# Jarvis · 本地飞书 AI 管家

字节研发工程师在本地 Mac 可信环境运行的私人 AI 管家：自动从飞书消息识别 leader 交办 / 项目讨论，提取行动线索（Todo），经确认固化为明确任务（Task）并执行。

完整设计见 [`docs/00-overview.md`](docs/00-overview.md)（技术栈 / 核心实体 / Todo·Task 拆分 / 端到端链路 / 开放问题）与 `docs/modules/01~05.md`。写代码前建议先读下方「架构导航」。

## 技术栈

Go 1.26 + Hertz + GORM + codex CLI（M4 决策 / M5 代码执行）+ model API（M2/M3 抽取）+ mem0（Python sidecar）+ Qdrant + lark-cli。

## 架构导航（写代码前先看这里）

单体 Go 进程，一条流水线 5 个模块，全部由 cron 驱动，模块之间**不直接调用**，靠数据库标志位解耦（`related_group` / `mem0_processed` / `todo.status` / `task.status`）。

### 数据流

```
飞书 ──lark-cli──▶ [M2 capture] ──▶ message 表
                                     │
                       ┌─────────────┴──────────────┐
                       ▼                             ▼
                [M2.5 memory]                  [M3 extract]
                mem0 sidecar                   model API + 工具循环
                       │                             │
                Qdrant jarvis_memories        todo 表 + Qdrant todo_semantic
                                                     │
                                               [M4 decide]  codex CLI / manual_mvp
                                                     │
                              todo=need_decision ──▶ 用户 Approve ──▶ task 表
                                                     │
                                               [M5 execute]  codex CLI ──▶ diff 落盘
```

### 模块职责与关键文件

| 模块 | 目录 | 干什么 | 核心文件 | LLM/外部依赖 |
|---|---|---|---|---|
| M2 采集 | `internal/capture/` | 发现会话、增量扫描 `related_group` 群的消息 | `service.go`（发现/扫描主逻辑）、`scheduler.go`（cron）、`resources.go`（资源引用提取） | lark-cli（`im +chat-list`、`im +chat-messages-list`） |
| M2.5 记忆 | `internal/memory/` | 消息切窗 → mem0 抽事实 → 向量入库 | `worker.go`（窗口化编排）、`store.go`（pending 查询/标记）、`client.go`（sidecar HTTP） | mem0 sidecar → Qdrant `jarvis_memories` |
| M3 抽取 | `internal/extract/` | 从新消息抽 Todo（function-calling 工具循环 + 语义去重） | `worker.go`（编排）、`pipeline_store.go`（加载/组批）、`prompt.go`（提示词）、`persist.go`（落库）、`dedup.go`（去重）、`provider/`（model API）、`tools/`（2 个工具） | model API + Qdrant `todo_semantic` + mem0（检索） |
| M4 决策 | `internal/decide/` | 给 Todo 定路由：`manual_mvp` 全走人工确认；`codex` 用 codex 判 disposition | `worker.go`（批处理）、`manual_gate.go` / `codex_evaluator.go`（两种评估器）、`codex.go`（调 codex CLI）、`evaluation.go`（落库）、`service.go`（Approve/Reject 建 Task）、`background.go`（快照）、`constants.go`（共享常量） | codex CLI（read-only sandbox） |
| M5 执行 | `internal/execute/` | 执行确认后的 Task（默认只手动触发） | `agent_executor.go`（执行主流程）、`codex_runner.go`（调 codex CLI）、`policy.go`（各 action_type 的 sandbox/是否需批准）、`git.go`（分支/commit/diff）、`store.go`（Task 状态机） | codex CLI（code_change 用 workspace-write）→ diff 落 `runs_dir` |
| 背景管理 | `internal/background/` | 后台可编辑的项目/人物/群/决策主体；种子数据 | `project.go` / `person.go` / `group.go` / `profile.go`（各实体 service）、`resolve.go`（lark-cli 按名字查 open_id）、`seed.go` / `seed_persons.go`（`-seed` / `-seed-persons`） | lark-cli（resolve/拉群成员） |
| API | `internal/api/` | 所有 HTTP handler + 路由注册 | `router.go`（**所有路由在这里注册**）、各资源一个文件 | — |
| 领域模型 | `internal/domain/` | 8 个核心实体的 GORM model | `models.go`（8 实体）、`capture.go`（message/checkpoint）、`extract.go` / `decide.go` / `execute.go`（各模块附属表） | — |
| 存储 | `internal/store/` | MySQL 连接与迁移 | `mysql.go` | MySQL |

### 主要存储（MySQL 是 source of truth）

| 表 | 谁写 | 谁读 | 说明 |
|---|---|---|---|
| `feishu_group` | capture | 全部 | 会话目录 + `related_group` 白名单 + tier（tier 仅 UI 展示） |
| `chat_checkpoint` | capture | capture | 增量扫描高水位 |
| `message` | capture | memory/extract | 原始消息，含 `mem0_processed` 标志 |
| `resource` | capture | extract | 消息里的文件/文档/妙记引用（只记引用不下载） |
| `scan_record` | capture | — | 采集审计流水 |
| `project` / `person` / `principal_profile` | background | extract/decide | 背景信息（后台可编辑） |
| `todo` / `todo_event` / `todo_extract_watermark` | extract | decide | 抽取出的行动线索 + 事件 + 抽取游标 |
| `task` / `execution_run` | decide(建)/execute | execute | 确认后的可执行快照 + 执行记录 |
| `decision_audit` | decide | 确认页 | 决策审计 |
| Qdrant `jarvis_memories` | mem0 sidecar | memory/extract/decide | 长期记忆向量 |
| Qdrant `todo_semantic` | extract | extract | Todo 去重向量 |

### 常见「我要改 X 该动哪」

- **改抽取提示词** → `internal/extract/prompt.go`
- **改 M3 用哪些背景（principal/项目/人物）** → `internal/extract/pipeline_store.go`（`loadPrincipal` / `loadProjectSummaries` / `buildChatBatch`）
- **改 codex 决策的判定逻辑** → `internal/decide/codex_evaluator.go`（`dispositionFromDecision` / `routeForDisposition`）
- **改 codex CLI 怎么调（命令行/超时/schema）** → 决策 `internal/decide/codex.go`；执行 `internal/execute/codex_runner.go`
- **加/改 action_type 的执行策略（sandbox、是否需人工批准）** → `internal/execute/policy.go`
- **加一个 HTTP 接口** → `internal/api/`（新建 handler + 在 `router.go` 注册）
- **加一张表 / 改字段** → `internal/domain/`（改 model，加进 `CoreModels()`）
- **改采集频率 / cron** → `conf/config.yaml`（各模块 `schedule`）
- **改前端页面** → `web/src/`（各页面一个文件，见下方目录结构）

### HTTP 接口一览（全部注册在 `internal/api/router.go`）

```
GET  /healthz
GET  /api/todos            GET /api/todos/:id
GET  /api/confirmations    GET /api/confirmations/:id
POST /api/confirmations/:id/approve|reject
GET  /api/tasks            POST /api/tasks/:id/finish   POST /api/tasks/:id/execute
GET/POST/PUT/DELETE /api/projects[/:id]
GET/POST/PUT/DELETE /api/persons[/:id]   POST /api/persons/resolve
GET  /api/groups           PUT /api/groups/:id
GET/PUT /api/profile
```

### 一次性 CLI 动作（互斥，跑完退出）

`-migrate-only` 迁移 · `-discover-once` 发现会话 · `-scan-chat <id>` 扫单群 · `-set-related-groups <ids>` 原子替换白名单 · `-memorize-once` 记忆化 · `-extract-once` 抽 Todo · `-decide-once` 决策分流 · `-seed` 种子项目/任务/群 · `-seed-persons` 从关键群导入真实 Person。

## 当前进度

- M0.2 已完成：统一 `lark-cli` 子进程层、无历史回溯的增量扫描、线程回复拍平、Resource 元数据沉淀和分层 cron 调度。消息扫描只处理数据库中动态标记的 `related_group`。
- M0.3 核心链路已实现：Go 侧 mem0 HTTP client、消息窗口化 worker、每 10 分钟记忆化任务、Python FastAPI sidecar、Qdrant v1.18.2 原生 launchd 服务与锁定依赖。
- M0.4 提取 worker 已实现：相关群增量聚合、背景/记忆注入、Structured Outputs、Todo 事务落库与独立水位推进；同时提供只读 Todo API 和 React + Ant Design 看板。
- M0.5 确认已完成：`extracted Todo → need_decision → 用户批准/拒绝`，批准后原子生成 Task。`decide.mode` 可选 `manual_mvp`（全走人工确认）或 `codex`（codex 只读判 disposition，当前观察期仍统一落到人工确认页）。
- M0.6 MVP 执行闭环已完成：管理后台列出 `pending Task`，支持人工执行后回写 `done/failed + result`。确认与执行页面由同一个 Go 服务托管。
- Kimi Code K2.7（`kimi-for-coding`）已完成 Structured Output 实测；mem0 使用同一端点的 `bge_m3_embed`（1024 维），真实 add/search → Qdrant 链路已验收。

## 本地运行

`conf/config.yaml` 使用本机 MySQL 明文 DSN。首次运行前创建数据库：

```bash
mysql -uroot -p -e 'CREATE DATABASE jarvis CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci'
```

只执行迁移：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -migrate-only
```

首次初始化会话。该命令全量枚举会话，但只把 checkpoint 设为发现时刻，不拉取此前历史消息：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -discover-once
```

手动增量扫描一个已发现会话：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -scan-chat oc_xxx
```

该会话必须已动态标记为 `related_group=1`。名单可通过 `-set-related-groups` 原子替换，数量不写死。

启动服务后按 `conf/config.yaml` 里各模块 `schedule` 注册 cron（capture 的 discover/scan、memory、extract、decide、可选 execute）；`related_group` 群统一按 `scan_schedule` 扫描，tier 仅用于 UI 展示。健康检查同时验证 MySQL：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml
# 另开终端
curl http://127.0.0.1:18800/healthz
```

管理后台直接打开 `http://127.0.0.1:18800/`，包含 Todo、待确认和 Task 执行三个页面。

## launchd 托管

安装脚本会构建 `bin/jarvis-server`、校验 plist，并注册/重启当前用户的 `com.bytedance.jarvis.server` 服务：

```bash
./scripts/install-launchd.sh
```

日志写入 `var/log/jarvis-server.log` 和 `var/log/jarvis-server.error.log`。

## mem0 与 Qdrant

`conf/config.yaml` 当前使用 Kimi Code API：chat 模型为 `kimi-for-coding`，embedding 模型为 `bge_m3_embed`（1024 维）。密钥明文保存在本机配置中。安装两个独立服务：

```bash
./scripts/install-qdrant.sh
./scripts/install-mem0-sidecar.sh
curl http://127.0.0.1:6333/healthz
curl http://127.0.0.1:18900/health
```

手工执行一次记忆化：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -memorize-once
```

手工执行一次 Todo 提取；正常服务模式下由 `extract.schedule` 非重叠触发：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -extract-once
```

手工执行一次 MVP 人工确认分流；正常服务模式下由 `decide.schedule` 触发：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -decide-once
```

sidecar 依赖由 `sidecar/mem0/uv.lock` 固定；Qdrant 数据、mem0 history 和日志都落在被 Git 忽略的 `var/`。

## 测试

```bash
go test ./...
npm --prefix web run typecheck
npm --prefix web run build
```

真实 MySQL 迁移集成测试要求一个全新的空测试库：

```bash
JARVIS_TEST_MYSQL_DSN='root:password@tcp(127.0.0.1:3306)/jarvis_migration_test?charset=utf8mb4&parseTime=true&loc=Local' \
  go test ./internal/store -run '^TestMigrateMySQL$' -v
```

用当前配置做真实 Structured Output 和可回滚的 M3 全链路验收：

```bash
JARVIS_TEST_MODEL_CONFIG=../../../conf/config.yaml \
  go test ./internal/extract/provider -run '^TestClientLiveStructuredOutput$' -v
JARVIS_TEST_PIPELINE_CONFIG=../../conf/config.yaml \
  go test ./internal/extract -run '^TestPipelineLive$' -v
```

## 管理后台（MVP）

生产构建由 Go 服务从 `web/dist` 直接托管。开发前端时仍可启动 Vite 热更新：

```bash
cd web
npm ci --registry=https://registry.npmjs.org
npm run dev
```

Vite 默认监听 `127.0.0.1:18801`，并把 `/api` 代理到 `jarvis-server` 的 `127.0.0.1:18800`。

当前后台提供三个页面（`web/src/`）：

- **待确认**（`Confirmations.tsx`）：待确认详情、批准生成 Task、拒绝 Todo；
- **任务**（`Tasks.tsx`）：Task 查询、人工完成或失败回写；
- **背景**（`Background.tsx`）：项目 / 人物 / 群 / 决策主体（我）的维护，支持列表内直接编辑。

## 目录结构

```
jarvis/
├── cmd/jarvis-server/   # 主入口（Hertz 启动 + 所有 -xxx-once CLI 动作）
├── internal/
│   ├── api/             # 路由 + 所有 HTTP handler（router.go 注册全部路由）
│   ├── background/      # 项目/人物/群/决策主体的后台 service + 种子数据
│   ├── capture/         # M2 会话发现、增量扫描与调度
│   ├── config/          # 配置加载与校验
│   ├── decide/          # M4 决策：manual_mvp 人工闸门 / codex 判定
│   ├── domain/          # 8 个核心实体 + 各模块附属表 GORM model
│   ├── embedding/       # M3 去重用的 embedding client
│   ├── execute/         # M5 codex 驱动的 Task 执行
│   ├── extract/         # M3 聚合、prompt、工具循环抽取、Todo 事务与水位
│   │   ├── provider/    # OpenAI 兼容 model API 传输层
│   │   └── tools/       # function-calling 工具（查历史 / 查记忆）
│   ├── larkcli/         # lark-cli 子进程、限流、并发和超时
│   ├── memory/          # 消息窗口化与 mem0 sidecar client
│   ├── semantic/        # Qdrant todo_semantic 索引
│   └── store/           # MySQL 连接与迁移
├── sidecar/mem0/        # FastAPI + mem0 Python sidecar
├── web/                  # React + Vite + Ant Design 管理后台
├── conf/config.yaml     # 本地配置（本地可信环境，含明文 DSN）
├── deploy/              # launchd plist
├── scripts/             # 安装/运维脚本
└── docs/                # 方案文档（00-overview + modules/01~05）
```

MVP 已覆盖采集、提取、人工确认、Task 生成与人工完成回写。高级语义去重、智能记忆、自动决策和自动外部执行均作为后续增强，不阻塞当前人工闭环。
