# Jarvis · 本地飞书 AI 管家

字节研发工程师在本地 Mac 可信环境运行的私人 AI 管家：自动从飞书消息识别 leader 交办 / 项目讨论，提取行动线索（Todo），经确认固化为明确任务（Task）并执行。

完整设计见 [`docs/00-overview.md`](docs/00-overview.md)（技术栈 / 7 实体 / Todo·Task 拆分 / 端到端链路 / 开放问题）与 `docs/modules/01~05.md`。

## 技术栈

Go 1.26 + Hertz + GORM + codex CLI（M4 决策 / M5 代码执行）+ model API（M2/M3 抽取）+ mem0（Python sidecar）+ Qdrant + lark-cli。

## 当前进度

- M0.2 已完成：统一 `lark-cli` 子进程层、无历史回溯的增量扫描、线程回复拍平、Resource 元数据沉淀和分层 cron 调度。消息扫描只处理数据库中动态标记的 `related_group`。
- M0.3 核心链路已实现：Go 侧 mem0 HTTP client、消息窗口化 worker、每 10 分钟记忆化任务、Python FastAPI sidecar、Qdrant v1.18.2 原生 launchd 服务与锁定依赖。
- M0.4 提取 worker 已实现：相关群增量聚合、背景/记忆注入、Structured Outputs、Todo 事务落库与独立水位推进；同时提供只读 Todo API 和 React + Ant Design 看板。
- M0.5 MVP 确认已完成：`extracted Todo → need_decision → 用户批准/拒绝`，批准后原子生成 Task；MVP 不打分、不调用 codex、不自动确认。
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

启动服务后会注册 discover/hot/warm/cold 四类定时任务；健康检查同时验证 MySQL：

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

当前后台提供：

- Todo 查询与详情；
- 待确认详情、批准生成 Task、拒绝 Todo；
- Task 查询、人工完成或失败回写。

## 目录结构

```
jarvis/
├── cmd/jarvis-server/   # 主入口（Hertz 启动）
├── internal/
│   ├── api/             # 路由 + handler（/healthz）
│   ├── config/          # 配置加载与校验
│   ├── capture/         # M2 会话发现、增量扫描与调度
│   ├── decide/          # M4 人工确认闸门
│   ├── domain/          # 7 个核心实体 GORM model
│   ├── execute/         # M5 人工 Task 执行闭环
│   ├── extract/         # M3 聚合、prompt、模型抽取、Todo 事务与水位
│   ├── larkcli/         # lark-cli 子进程、限流、并发和超时
│   ├── memory/          # 消息窗口化与 mem0 sidecar client
│   └── store/           # MySQL 连接与迁移
├── sidecar/mem0/        # FastAPI + mem0 Python sidecar
├── web/                  # React + Vite + Ant Design Todo 看板
├── conf/config.yaml     # 本地配置（本地可信环境，含明文 DSN）
├── deploy/              # launchd plist
├── scripts/             # 安装/运维脚本
└── docs/                # 方案文档
```

MVP 已覆盖采集、提取、人工确认、Task 生成与人工完成回写。高级语义去重、智能记忆、自动决策和自动外部执行均作为后续增强，不阻塞当前人工闭环。
