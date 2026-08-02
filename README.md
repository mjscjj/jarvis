# Jarvis · 本地飞书 AI 管家

字节研发工程师在本地 Mac 可信环境运行的私人 AI 管家：自动从飞书消息识别 leader 交办 / 项目讨论，提取行动线索（Todo），经确认固化为明确任务（Task）并执行。

完整设计见 [`docs/00-overview.md`](docs/00-overview.md)（技术栈 / 核心实体 / Todo·Task 拆分 / 端到端链路 / 开放问题）与 `docs/modules/01~05.md`。写代码前建议先读下方「架构导航」。

## 技术栈

Go 1.26 + Hertz + GORM + `traex`（M3 抽取 / M5 判断的 agent CLI，模型 `gpt-5.5`）+ `traex`（M5 执行 / 右侧对话的 agent CLI，模型 `gpt-5.6-sol`）+ 阿里云百炼 DashScope（`qwen-plus` 抽取 LLM、`text-embedding-v3` 1024 维 embedding）+ `traex`（离线事实抽取，模型 `DeepSeek-V4-Flash`）+ Qdrant + lark-cli。

## 架构导航（写代码前先看这里）

单体 Go 进程，一条流水线 4 个模块。M2 的消息扫描仍由 cron 发起；扫描落库后由进程内协调器按持久化 ID/version 实时推进 `M3 → M5`。MySQL 状态仍是 source of truth，M3/M5 的原 schedule 只做漏通知、崩溃恢复和存量数据补偿。

M5 内部分两步：**判断环节**（read-only，给 Todo 定 disposition，决定值不值得做）和**执行环节**（工具全开的 agent，真正落地）。两步共用一个工作队列和一个 worker 池，不是两个流水线阶段。

线索层没有人工闸门：判断只有 ready（建 Task）和 drop（终结）两个出路。需要 principal 拍板或补信息的事，由执行环节带着完整调查结果在 Task 上提出（`awaiting_approval` / `needs_human`）。

### 数据流

```
飞书 ──lark-cli──▶ [M2 capture] ──▶ message 表
                                     │
                       ┌─────────────┴──────────────┐
                       ▼                             ▼
                [离线事实引擎]                  [M3 extract]
                traex/DeepSeek 蒸馏事实        codex/traex agent 工具循环（默认 engine=codex）
                       │                             │  （自跑 lark-cli/bytedcli/git/jarvis-tools 推算项目/仓库，冻结 context_snapshot）
                  fact 表 ────背景────────────▶ todo 表 + Qdrant todo_semantic
                                                     │
                                    ┌────────────────┴────────────────┐
                                    │  M5 判断环节（read-only）        │
                                    │  codex/traex 判 disposition      │
                                    └────────────────┬────────────────┘
                                                     │
                            ┌────────────────────────┴───────────┐
                            ▼                                     ▼
                          ready                                 drop
                     （直接建 Task）                      （todo=dropped，终结）
                            ▼
                          task 表
                            │
                  M5 执行环节  traex agent
                            │
        code_change：MR review 作为闸门，直接跑完（含 push/MR）
        其余 action_type：propose 判 needs_approval
              ├─ 低风险 → 直接 apply
              └─ 高风险 → 停在 awaiting_approval，用户 Approve 后 apply
                            │
                            ▼
                     对外写入 + diff/产物落 runs_dir
```

### 核心职责边界

| 模块 | 负责 | 不负责 |
|---|---|---|
| M2 采集 | 读取外部事实，记录成功、无权限、暂不可用等采集结果；按自己的可靠性策略重试 | 不判断要不要做事，不创建 Todo，不申请权限，不联系主持人 |
| M3 Todo | 结合采集证据与项目、人物、会话上下文，决定创建、合并或忽略 Todo，并明确建议动作 | 不执行外部写操作 |
| M5 判断环节 | 判断 Todo 是否应固化为 Task，以及缺信息还是需人工决策 | `ready` 只表示自动建 Task，不代表授权外部写入；不形成执行方案 |
| M5 执行环节 | 非 `code_change` Task 先形成可审阅方案；申请权限、发消息等业务外部写操作经明确批准后执行 | 不反过来替采集模块改写事实 |

三条硬规则：

- **错误是证据，不是决策。** 采集失败时，M2 只把原始错误原样写成证据；是否生成“申请权限”“联系对方”或其他 Todo，由 M3 结合上下文判断。
- **流水线通用，来源差异靠提示词。** M2→M5 只有一套协议，不为某个来源（会议、邮件、日程……）在 Go 里开专用链路。详见 `AGENTS.md` §0.5。
- **自动流转不等于外部授权。** 判断环节自动建 Task 后，申请权限、发消息、修改飞书等业务外部写操作仍须在执行环节等待明确批准。

#### 线索投递：新来源接入 M2 的唯一入口

飞书 IM 之外的来源不写 Go 采集模块，走通用线索投递：一个定时任务驱动 agent 按 Skill 去外部世界取事实，再用 `jarvis-tools append-clue`（即 `POST /api/clues`）把事实原样交回 M2。M2 只做三件机械动作——存进 `message`、按 `(source, external_id)` 幂等、唤醒 M3；解读线索是 M3 的活。

每个 `source` 自动获得一个线索频道（`chat_mode=clue` 的伪会话，如 `clue:feishu_meeting`），首次投递时创建。落地示例见 `.agents/skills/feishu-meeting-clue/`：定时任务只报「哪场会开完了」，妙记有没有、录没录、生成好没有，由 M3 拿到线索后自己查（判断要点写在 `conf/rules/m3.md`）。

### 模块职责与关键文件

| 模块 | 目录 | 干什么 | 核心文件 | LLM/外部依赖 |
|---|---|---|---|---|
| M2 采集 | `internal/capture/` | 采集群聊和内部单聊消息，把原始内容和中立采集结果写成下游证据；另开放通用线索投递入口，让任意 agent 把外部事实交回流水线 | `capture/service.go`（会话发现/扫描）、`scheduler.go`、`clue.go`（线索投递） | lark-cli（IM 只读接口） |
| 离线事实引擎 | `internal/factengine/` | 跑在关键路径之外，把流水线已有原料（先接 `message`）切窗后交给便宜快的模型蒸馏成长期事实，写进 `fact` 表；按 source 维护水位，一轮失败不影响采集和执行 | `worker.go`（编排 + 水位）、`store.go`（原料投影/切窗/主体解析）、`extractor.go`（traex 调用与解码）、`scheduler.go`（cron） | traex agent（`DeepSeek-V4-Flash`） |
| M3 抽取 | `internal/extract/` | 从新消息抽 Todo（默认 `engine=codex`：traex agent 自跑 lark-cli/bytedcli/git/jarvis-tools 推算项目/仓库并冻结 `context_snapshot`；备用 `model_api` function-calling 循环）+ 语义去重 + source_quote 证据重抽 | `worker.go`（编排）、`pipeline_store.go`（加载/组批）、`prompt.go`（提示词）、`persist.go`（落库）、`dedup.go`（去重）、`codexengine/`（traex agent 引擎）、`provider/`（百炼 model API）、`tools/`（工具） | traex agent（`gpt-5.5`）/ 百炼 `qwen-plus` + Qdrant `todo_semantic` + `fact` 表（背景注入） |
| M5 判断环节 | `internal/execute/decision_*.go` | 给 Todo 定 disposition：codex/traex 输出最小外壳 `disposition + plan + payload`，plan/payload 内部保持宽松；判 ready 时建 Task 并原样固化 `decision_payload` 供执行环节使用，判 drop 时终结线索 | `decision_worker.go`（批处理）、`decision_evaluator.go`（评估器）、`decision_codex.go`（调 agent CLI）、`decision_apply.go`（落库建 Task）、`decision_helpers.go`（锁 Todo、写事件） | traex agent（`gpt-5.5`，read-only 判定，可自查补信息） |
| M5 执行 | `internal/execute/` | 执行确认后的 Task：支持 `completed/waiting/needs_human/failed` 结果；长等待时持久化 Codex Session，当前 Turn 退出，到期后 `exec resume` 续跑（CLI 不支持 resume 传 `--output-schema` 时，格式契约写进 prompt 并在本地校验，不合格打回重写）；高风险外部写入仍停在 `awaiting_approval` | `agent_executor.go`（执行/挂起/恢复）、`codex_runner.go`（Codex Session）、`store.go`（Task 状态机） | traex agent（`gpt-5.6-sol`） |
| 定时任务 | `internal/scheduledtask/` | 时间触发器：独立计划到期物化新 Task；`yield-until` 创建的续接计划只恢复原 Task 和原 Codex Session | `service.go`（计划/抢占/Task 物化/Session 恢复）、`scheduler.go`（扫描 cron） | MySQL `scheduled_task` + M5 |
| 流水线协调 | `internal/pipeline/` | 接收 M2/M3/M5 状态提交后的轻量通知，按 chat/todo/task 定向推进；内存队列只加速，启动与 cron 均从 MySQL 补偿 | `coordinator.go`（串行编排与 M5 并发）、`queue.go`（按 ID/version 合并）、`scheduler.go`（补偿调度） | — |
| 背景管理 | `internal/background/` | 后台可编辑的项目/人物/群/决策主体；种子数据 | `project.go` / `person.go` / `group.go` / `profile.go`（各实体 service）、`resolve.go`（lark-cli 按名字查 open_id）、`seed.go` / `seed_persons.go`（`-seed` / `-seed-persons`） | lark-cli（resolve/拉群成员） |
| 关系知识 | `internal/knowledge/` | 在现有业务实体之间保存带来源、有效期和置信度的动态关系；不建通用 entity 表 | `predicates.go`（predicate 注册表）、`service.go`（校验、去重、supersede/retract） | — |
| 进度历史 | `internal/progress/` | 保存 Task 状态机和 Project 进度事件，提供显式存量快照回填 | `service.go`（事件写入/查询）、`backfill.go`（一次性 Task 快照） | — |
| API | `internal/api/` | 所有 HTTP handler + 路由注册 | `router.go`（**所有路由在这里注册**）、各资源一个文件 | — |
| 领域模型 | `internal/domain/` | 8 个核心实体的 GORM model | `models.go`（8 实体）、`capture.go`（message/checkpoint）、`extract.go` / `decide.go` / `execute.go`（各模块附属表） | — |
| 存储 | `internal/store/` | MySQL 连接与迁移 | `mysql.go` | MySQL |

### 主要存储（MySQL 是 source of truth）

| 表 | 谁写 | 谁读 | 说明 |
|---|---|---|---|
| `feishu_group` | capture | 全部 | 会话目录 + `related_group` 白名单 + tier（tier 仅 UI 展示） |
| `chat_checkpoint` | capture | capture | 增量扫描高水位 |
| `message` | capture | factengine/extract | 原始消息 |
| `resource` | capture | extract | 消息里的文件/文档/妙记引用（只记引用不下载） |
| `scan_record` | capture | — | 采集审计流水 |
| `scheduled_task` | scheduledtask | scheduledtask | 一次性/周期触发、下次执行时间，以及 Task/Codex Session 的未来唤醒 |
| `project` / `person` / `principal_profile` | background | extract/decide | 背景信息（后台可编辑） |
| `todo` / `todo_event` / `todo_extract_watermark` | extract | decide | 抽取出的行动线索 + 事件 + 抽取游标 |
| `task` / `task_event` / `execution_run` | decide(建)/execute | execute/insight | 可执行快照 + 业务状态历史 + Codex 执行审计 |
| `fact` | factengine/background/API | extract/contextsnap/dailydigest | 一条条自然语言事实，按 `(subject_type, subject_id)` 绑到项目/群/人等主体上；`subject_type` 不枚举，具体含义由模型结合上下文推断 |
| `fact_source_cursor` | factengine | factengine | 每个原料来源的抽取水位，崩溃后从上次提交处重放 |
| `relation_fact` | knowledge/API | knowledge | 两个现有实体之间的自然语言关联；确定性外键关系不重复写 |
| `decision_audit` | decide | 确认页 | 决策审计 |
| Qdrant `todo_semantic` | extract | extract | Todo 去重向量 |

### 常见「我要改 X 该动哪」

- **改抽取提示词** → `internal/extract/prompt.go`
- **改 M3 用哪些背景（principal/项目/人物）** → `internal/extract/pipeline_store.go`（`loadPrincipal` / `loadProjectSummaries` / `buildChatBatch`）
- **改 codex 决策的判定逻辑** → `internal/execute/decision_evaluator.go`（`dispositionFromDecision` / `routeForDisposition`）
- **改 codex CLI 怎么调（命令行/超时/schema）** → 判断 `internal/execute/decision_codex.go`；执行 `internal/execute/codex_runner.go`
- **加/改 action_type 的执行策略（sandbox、是否需人工批准）** → `internal/execute/policy.go`
- **加一个 HTTP 接口** → `internal/api/`（新建 handler + 在 `router.go` 注册）
- **加一张表 / 改字段** → `internal/domain/`（改 model，加进 `CoreModels()`）
- **改采集频率 / 补偿 cron** → `conf/config.yaml`（各模块 `schedule`）
- **改前端页面** → `web/src/`（各页面一个文件，见下方目录结构）

### HTTP 接口一览（全部注册在 `internal/api/router.go`）

```
GET  /healthz
GET  /api/todos            GET /api/todos/:id
GET  /api/tasks            GET /api/tasks/:id/runs|events
POST /api/tasks
POST /api/tasks/:id/finish|supplement
POST /api/tasks/:id/execute|rerun|reapply|approve|reject  # 需 Executor 已启用（异步）
POST /api/tasks/:id/resume                               # 回复 needs_human 并恢复原 Codex Session
GET/POST/PUT/DELETE /api/projects[/:id]
GET/POST /api/projects/:id/events
GET/POST /api/relation-facts   PUT/DELETE /api/relation-facts/:id
GET/POST/PUT/DELETE /api/persons[/:id]   POST /api/persons/resolve
GET  /api/groups           PUT /api/groups/:id
GET/PUT /api/profile
GET/POST/PUT/DELETE /api/resources[/:id]
GET  /api/overview         GET /api/digests   POST /api/digests/summarize
GET  /api/debug/status|modules|failures|scans|watermarks|todos|tasks|logs
POST /api/debug/capture/discover|scan-related|scan-chat   # 需 Capture 已注入
POST /api/chat                                            # 需 chat.enabled=true（SSE 流式对话）
```

### 一次性 CLI 动作（互斥，跑完退出）

`-migrate-only` 迁移 · `-backfill-progress-events` 为无事件历史的存量 Task 写一次当前状态快照 · `-discover-once` 发现会话 · `-scan-chat <id>` 扫单群 · `-set-related-groups <ids>` 原子替换白名单 · `-extract-facts-once` 离线抽事实 · `-extract-once` 抽 Todo · `-decide-once` 决策分流 · `-seed` 种子项目/任务/群 · `-seed-persons` 从关键群导入真实 Person · `-open-p2p` 把存量内部私聊一次性纳入监听（`related_group=1`）。

## 调度与后台循环

M2 扫描发现新消息后立即通知 `internal/pipeline.Coordinator`。协调器先按 chat 跑 M3，拿到本次提交的 Todo ID/version 后入 M5 队列走判断环节；判断为 ready 自动建 Task 或用户批准建 Task 后，Task 再入同一个 M5 队列走执行环节。通知不承担持久化，进程重启可完全依靠 MySQL 状态恢复。间隔与开关仍在 `conf/config.yaml`：

| Loop | 配置键 | 默认间隔 | enable 开关 | 说明 |
|---|---|---|---|---|
| M2 采集-发现 | `capture.discover_schedule` | `@every 6h` | 常开 | 全量枚举会话；只把 checkpoint 设为发现时刻，不回溯历史 |
| M2 采集-扫描 | `capture.scan_schedule` | `@every 5m` | 常开 | 对 `related_group=1` 群统一增量扫描（tier 仅用于 UI 展示） |
| 离线事实 | `factengine.schedule` | `@every 15m` | 常开 | 消息切窗 → traex/DeepSeek 蒸馏 → `fact` 表 |
| M3 抽取补偿 | `extract.schedule` | `@every 10m` | `extract.enabled=true` | 实时路径按 chat 触发；cron 扫描遗漏的新消息并推进水位 |
| M5 判断补偿 | `decide.schedule` | `@every 1m` | `decide.enabled=true` | 实时路径按 Todo ID/version 触发；cron 扫描遗漏的 `extracted` |
| M5 执行补偿 | `execute.schedule` | `@every 5m` | `execute.enabled=true` | 实时路径按 Task ID/version 入队；cron 恢复遗漏的 `pending` 和僵尸 `executing`，并发 `execute.concurrency`（默认 3） |
| 定时任务 | `scheduled_task.schedule` | `@every 1m` | `scheduled_task.enabled=true` | 扫描已到期且启用的 `active` 计划：普通计划物化新 Task，续跑计划恢复原 Task 的 Codex Session |

M3/M5 的实时通知与补偿任务都只进入同一个协调器队列，不会由两套 scheduler 并行调用 worker。队列按实体 ID/version 合并等待中的重复通知；数据库状态与乐观锁拦截过期通知。一次性 CLI flag（见上）仍用同一批 worker 单跑一轮后退出。

## 当前进度

- M0.2 已完成：统一 `lark-cli` 子进程层、无历史回溯的增量扫描、线程回复拍平、Resource 元数据沉淀和分层 cron 调度。消息扫描只处理数据库中动态标记的 `related_group`。
- 事实沉淀已改为离线引擎：`internal/factengine` 每 15 分钟按水位消费 `message`，切窗后交 `traex`/`DeepSeek-V4-Flash` 蒸馏成 `fact`，M3 抽取时按群和项目注入作背景。mem0 与 `jarvis_memories` 已退役。
- M0.4 提取 worker 已实现：相关群增量聚合、背景/记忆注入、Structured Outputs、Todo 事务落库与独立水位推进；同时提供只读 Todo API 和 React + Ant Design 看板。
- M0.5 判断环节已完成：`extracted Todo → codex/traex 判 disposition → ready 建 Task 或 drop 终结`，并把方案、澄清点、证据和风险保存在宽松 plan/payload 中。
- M0.6 MVP 执行闭环已完成：管理后台列出 Task，支持人工执行后回写 `done/failed + result`。
- 现状：M3 抽取、M5 判断已 codex 化（默认走 traex agent 自跑工具推算项目/仓库并冻结上下文快照）；M5 执行支持自动执行、两阶段人工审批，以及 `needs_human` 挂起/回复后恢复原 Codex Session（不是重跑 Task）；抽取/记忆 LLM 接入阿里云百炼 `qwen-plus`，embedding 用 `text-embedding-v3`（1024 维）。
- M3/M5 已改为状态提交后实时串行推进；两者的 schedule 只承担启动恢复和周期补偿。

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

启动服务后按 `conf/config.yaml` 里各模块 `schedule` 注册 cron（见上「调度与后台循环」）；`related_group` 群统一按 `scan_schedule` 扫描，tier 仅用于 UI 展示。后端监听 `127.0.0.1:18800`（同时用 `StaticFS` 托管 `web/dist` 静态前端），健康检查同时验证 MySQL。

**日常启动 / 重启主进程**（build + 稳定 codesign + launchd，不要裸 `go run` / 裸 `go build`）：

```bash
# 首次安装主服务（前端 npm ci + build、编译并签名 jarvis-server、bootstrap launchd）
./scripts/install-launchd.sh

# 之后改后端代码：重编译 + 签名 + 重启
./scripts/rebuild-server.sh

curl http://127.0.0.1:18800/healthz
```

管理后台：`http://127.0.0.1:18800/`（页面见下「管理后台」）。开发热更前端另开 `com.bytedance.jarvis.web`（`18801`），见下「launchd 托管」。

## launchd 托管

生产环境用 macOS launchd 常驻守护 3 个服务（`deploy/` 下 plist，均 `RunAtLoad` + `KeepAlive`）。launchd 只在登录时扫描 `~/Library/LaunchAgents`，所以安装脚本会把 plist 软链过去，再从软链 bootstrap，开机/重新登录后自动后台拉起：

| 服务 Label | plist | 作用 | 日志 |
|---|---|---|---|
| `com.bytedance.jarvis.server` | `deploy/com.bytedance.jarvis.server.plist` | 主进程 `bin/jarvis-server`（Hertz + 实时流水线 + 补偿 cron + 静态前端，`127.0.0.1:18800`） | `var/log/jarvis-server.{log,error.log}` |
| `com.bytedance.jarvis.web` | `deploy/com.bytedance.jarvis.web.plist` | 前端 Vite dev（`npm run dev`，`127.0.0.1:18801`，仅开发热更用；生产前端由主进程从 `web/dist` 托管） | `var/log/vite.{log,error.log}` |
| `com.bytedance.jarvis.qdrant` | `deploy/com.bytedance.jarvis.qdrant.plist` | Qdrant 向量库（`6333` HTTP / `6334` gRPC） | `var/log/jarvis-qdrant.{log,error.log}` |

安装（首次）：

```bash
./scripts/install-launchd.sh        # 主进程（含前端 build + codesign）
./scripts/install-qdrant.sh
```

常用运维（`UID_=$(id -u)`）：

```bash
# 改后端：重编译 + 稳定 codesign + 重启主进程
./scripts/rebuild-server.sh

# 重启前端 / 向量库
launchctl kickstart -k gui/$UID_/com.bytedance.jarvis.web
launchctl kickstart -k gui/$UID_/com.bytedance.jarvis.qdrant

# 查状态 / 看日志
launchctl print gui/$UID_/com.bytedance.jarvis.server
tail -f var/log/jarvis-server.log var/log/jarvis-server.error.log
```

主进程用证书 **`Jarvis Local`** + identifier **`com.bytedance.jarvis.server`** 签名，避免每次 rebuild 因 adhoc 指纹变化反复弹出「完全磁盘访问」。`install-launchd.sh` / `rebuild-server.sh` 会先构建并签名临时文件，验证通过后才原子替换正式二进制；launchd 入口也会复验签名，未签名或签名身份错误时拒绝启动。首次签好后到 **系统设置 → 隐私与安全性 → 完全磁盘访问权限** 确认勾选 `bin/jarvis-server`（旧 adhoc 条目可删掉重加一次）。

| 脚本 | 作用 |
|------|------|
| `scripts/install-launchd.sh` | 前端 build + `go build` + codesign + 软链 `~/Library/LaunchAgents` + bootstrap 主服务（开机自启） |
| `scripts/rebuild-server.sh` | `go build` + codesign + `kickstart` 主服务 |
| `scripts/ensure-codesign-identity.sh` | 若无「Jarvis Local」则创建并导入登录钥匙串 |
| `scripts/sign-jarvis-server.sh` | 对 `bin/jarvis-server` 签名 |
| `scripts/verify-server-signature.sh` | 校验固定 identifier 与签名证书，错误时 fail-fast |
| `scripts/run-signed-server.sh` | launchd 启动入口，拒绝运行未稳定签名的主进程 |

## 模型与 Qdrant

`conf/config.yaml` 当前使用阿里云百炼 DashScope（OpenAI 兼容端点）：LLM 为 `qwen-plus`，embedding 为 `text-embedding-v3`（1024 维），embedding 供 Todo 语义去重使用。离线事实抽取不共用这个端点，走 `traex` 的 `DeepSeek-V4-Flash`。密钥明文保存在本机配置中。Qdrant 单独装成服务：

```bash
./scripts/install-qdrant.sh
curl http://127.0.0.1:6333/healthz
```

手工执行一轮离线事实抽取；正常服务模式下由 `factengine.schedule` 定时驱动：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -extract-facts-once
```

手工执行一次 Todo 提取；正常服务模式下由 M2 扫描完成事件实时触发，`extract.schedule` 负责补偿：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -extract-once
```

手工执行一次 MVP 人工确认分流；正常服务模式下由 M3 提交事件实时触发，`decide.schedule` 负责补偿：

```bash
go run ./cmd/jarvis-server -config conf/config.yaml -decide-once
```

Qdrant 数据和日志落在被 Git 忽略的 `var/`。

## 测试

```bash
go test ./...
npm --prefix web run typecheck
npm --prefix web run build
```

默认测试只包含可在本机稳定、无外部依赖运行的单元/组件测试。依赖真实
MySQL、模型或完整配置的用例统一使用 `integration` 构建标签；显式启用后缺少
所需环境变量会直接失败，不再用 `t.Skip` 产生“看似通过、实际没跑”的结果。

真实 MySQL 迁移集成测试要求一个全新的空测试库：

```bash
JARVIS_TEST_MYSQL_DSN='root:password@tcp(127.0.0.1:3306)/jarvis_migration_test?charset=utf8mb4&parseTime=true&loc=Local' \
  go test -tags=integration ./internal/store -run '^TestMigrateMySQL$' -v
```

用当前配置做真实 Structured Output 和可回滚的 M3 全链路验收：

```bash
JARVIS_TEST_MODEL_CONFIG=../../../conf/config.yaml \
  go test -tags=integration ./internal/extract/provider -run '^TestClientLiveStructuredOutput$' -v
JARVIS_TEST_PIPELINE_CONFIG=../../conf/config.yaml \
  go test -tags=integration ./internal/extract -run '^TestPipelineLive$' -v
```

配置齐全时可一次运行全部集成测试：

```bash
go test -tags=integration ./...
```

## 管理后台（MVP）

生产构建由 Go 服务从 `web/dist` 直接托管。开发前端时仍可启动 Vite 热更新：

```bash
cd web
npm ci --registry=https://registry.npmjs.org
npm run dev
```

Vite 监听 `127.0.0.1:18801`（`strictPort`），把 `/api`、`/healthz` 代理到 `jarvis-server` 的 `127.0.0.1:18800`。前端也有 launchd 服务 `com.bytedance.jarvis.web`（跑 `npm run dev`，端口 18801），仅用于开发热更；**生产访问统一走 18800**，由 Go 从 `web/dist` 托管。

当前后台页面（`web/src/`，左侧导航）：

- **Overview**（`Overview.tsx`）：首屏，待我处理、Leader 未闭环、运行异常等抬头指标；
- **任务**（`Tasks.tsx`）：Task 查询、执行/审批/重跑、人工完成或失败回写——唯一的人工闸门在这里；
- **待办**（`Todos.tsx`）：抽取出的 Todo 列表与详情；
- **背景**（`Background.tsx`）：项目 / 人物 / 群 / 决策主体（我）的维护，支持列表内直接编辑；
- 另有 进度（`Progress.tsx`）、调试（`Debug.tsx`）辅助页；
- **右侧流式对话**（`Chat.tsx`）：常驻侧栏，走 SSE `POST /api/chat`（`chat.enabled=true` 才注册），底层用 traex（`gpt-5.6-sol`）。

## 目录结构

```
jarvis/
├── cmd/
│   └── jarvis-server/   # 主入口（Hertz + 实时流水线 + 补偿 cron + 所有 -xxx-once CLI 动作）
├── internal/
│   ├── api/             # 路由 + 所有 HTTP handler（router.go 注册全部路由）
│   ├── background/      # 项目/人物/群/决策主体/资源的后台 service + 种子数据
│   ├── capture/         # M2 会话发现、增量扫描与调度
│   ├── chat/            # 右侧流式对话（SSE），底层调 execute.bin 指定的 agent CLI
│   ├── config/          # 配置加载与校验
│   ├── contextsnap/     # 上下文快照（context_snapshot）组装与解析
│   ├── domain/          # 8 个核心实体 + 各模块附属表 GORM model
│   ├── embedding/       # M3 去重用的百炼 embedding client
│   ├── execute/         # M5：decision_*.go 判断环节，其余为执行环节 + 两阶段审批
│   ├── extract/         # M3 聚合、prompt、工具循环抽取、Todo 事务与水位
│   │   ├── codexengine/ # traex agent 抽取引擎（默认 engine=codex）
│   │   ├── provider/    # 百炼 OpenAI 兼容 model API 传输层（备用 model_api 引擎）
│   │   └── tools/       # function-calling 工具（查历史 / 查记忆 / 查资源）
│   ├── insight/         # 工作台/进度/调试面板的只读聚合
│   ├── larkcli/         # lark-cli 子进程、限流、并发和超时
│   ├── factengine/      # 离线事实引擎：切窗、蒸馏、水位
│   ├── pipeline/        # M3→M5 实时协调、去重队列与补偿 cron
│   ├── semantic/        # Qdrant todo_semantic 索引
│   └── store/           # MySQL 连接与迁移
├── web/                  # React + Vite + Ant Design 管理后台
├── conf/config.yaml     # 本地配置（本地可信环境，含明文 DSN）
├── deploy/              # launchd plist
├── scripts/             # jarvis-tools Agent 工具入口 + 安装/运维脚本
└── docs/                # 方案文档（00-overview + modules/01~05，archive/ 存已废弃方案）
```

当前已覆盖采集、记忆、codex 化的 M3 抽取 / M5 判断、人工确认、Task 生成，以及 M5 的自动执行 + 两阶段人工审批闭环（含对外写入与 push/MR）。人工确认与手动执行始终可用，作为自动链路的兜底。
