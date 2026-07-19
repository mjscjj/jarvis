# Jarvis · 本地飞书 AI 管家

字节研发工程师在本地 Mac 可信环境运行的私人 AI 管家：自动从飞书消息识别 leader 交办 / 项目讨论，提取行动线索（Todo），经确认固化为明确任务（Task）并执行。

完整设计见 [`docs/00-overview.md`](docs/00-overview.md)（技术栈 / 7 实体 / Todo·Task 拆分 / 端到端链路 / 开放问题）与 `docs/modules/01~05.md`。

## 技术栈

Go 1.26 + Hertz + GORM + codex CLI（M4 决策 / M5 代码执行）+ model API（M2/M3 抽取）+ mem0（Python sidecar）+ Qdrant + lark-cli。

## 当前进度

- M0.2 已完成：统一 `lark-cli` 子进程层、无历史回溯的增量扫描、线程回复拍平、Resource 元数据沉淀和分层 cron 调度。消息扫描只处理数据库中动态标记的 `related_group`。
- M0.3 核心链路已实现：Go 侧 mem0 HTTP client、消息窗口化 worker、每 10 分钟记忆化任务、Python FastAPI sidecar、Qdrant v1.18.2 原生 launchd 服务与锁定依赖。
- 本机 Qdrant 已运行；mem0 sidecar 的真实模型验收等待在 `conf/config.yaml` 填入同时支持 chat 与 embedding 的 OpenAI 兼容端点。

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

## launchd 托管

安装脚本会构建 `bin/jarvis-server`、校验 plist，并注册/重启当前用户的 `com.bytedance.jarvis.server` 服务：

```bash
./scripts/install-launchd.sh
```

日志写入 `var/log/jarvis-server.log` 和 `var/log/jarvis-server.error.log`。

## mem0 与 Qdrant

先在 `conf/config.yaml` 的 `model` 段填写明文 `base_url`、`api_key`、`model`；该端点还必须支持 `mem0.embedding_model`。随后安装两个独立服务：

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

sidecar 依赖由 `sidecar/mem0/uv.lock` 固定；Qdrant 数据、mem0 history 和日志都落在被 Git 忽略的 `var/`。

## 测试

```bash
go test ./...
```

真实 MySQL 迁移集成测试要求一个全新的空测试库：

```bash
JARVIS_TEST_MYSQL_DSN='root:password@tcp(127.0.0.1:3306)/jarvis_migration_test?charset=utf8mb4&parseTime=true&loc=Local' \
  go test ./internal/store -run '^TestMigrateMySQL$' -v
```

## 目录结构

```
jarvis/
├── cmd/jarvis-server/   # 主入口（Hertz 启动）
├── internal/
│   ├── api/             # 路由 + handler（/healthz）
│   ├── config/          # 配置加载与校验
│   ├── capture/         # M2 会话发现、增量扫描与调度
│   ├── domain/          # 7 个核心实体 GORM model
│   ├── larkcli/         # lark-cli 子进程、限流、并发和超时
│   ├── memory/          # 消息窗口化与 mem0 sidecar client
│   └── store/           # MySQL 连接与迁移
├── sidecar/mem0/        # FastAPI + mem0 Python sidecar
├── conf/config.yaml     # 本地配置（本地可信环境，含明文 DSN）
├── deploy/              # launchd plist
├── scripts/             # 安装/运维脚本
└── docs/                # 方案文档
```

M0.3 完成真实模型验收后，下一里程碑是 M0.4：M3 Todo 提取和后台 Todo 看板。
