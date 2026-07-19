# Jarvis · 本地飞书 AI 管家

字节研发工程师在本地 Mac 可信环境运行的私人 AI 管家：自动从飞书消息识别 leader 交办 / 项目讨论，提取行动线索（Todo），经确认固化为明确任务（Task）并执行。

完整设计见 [`docs/00-overview.md`](docs/00-overview.md)（技术栈 / 7 实体 / Todo·Task 拆分 / 端到端链路 / 开放问题）与 `docs/modules/01~05.md`。

## 技术栈

Go 1.26 + Hertz + GORM + codex CLI（M4 决策 / M5 代码执行）+ model API（M2/M3 抽取）+ mem0（Python sidecar）+ Qdrant + lark-cli。

## 当前进度

已完成 M0.2：在 M0.1 的 Go/Hertz、7 个核心实体和 MySQL 基础上，加入统一 `lark-cli` 子进程层、`message` / `chat_checkpoint` 支撑表、会话发现、无历史回溯的增量扫描、线程回复拍平、Resource 元数据沉淀和分层 cron 调度。

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
│   └── store/           # MySQL 连接与迁移
├── conf/config.yaml     # 本地配置（本地可信环境，含明文 DSN）
├── deploy/              # launchd plist
├── scripts/             # 安装/运维脚本
└── docs/                # 方案文档
```

下一里程碑 M0.3：mem0 sidecar、Qdrant 和消息记忆化 job。
