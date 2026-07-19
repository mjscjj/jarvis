# Jarvis · 本地飞书 AI 管家

字节研发工程师在本地 Mac 可信环境运行的私人 AI 管家：自动从飞书消息识别 leader 交办 / 项目讨论，提取行动线索（Todo），经确认固化为明确任务（Task）并执行。

完整设计见 [`docs/00-overview.md`](docs/00-overview.md)（技术栈 / 7 实体 / Todo·Task 拆分 / 端到端链路 / 开放问题）与 `docs/modules/01~05.md`。

## 技术栈

Go 1.26 + Hertz + GORM + codex CLI（M4 决策 / M5 代码执行）+ model API（M2/M3 抽取）+ mem0（Python sidecar）+ Qdrant + lark-cli。

## 运行（骨架阶段）

当前仅骨架：加载配置 + 起 Hertz + `/healthz`。

```bash
go run ./cmd/jarvis-server -config conf/config.yaml
# 另开终端
curl http://127.0.0.1:18800/healthz
```

## 目录结构

```
jarvis/
├── cmd/jarvis-server/   # 主入口（Hertz 启动）
├── internal/
│   ├── api/             # 路由 + handler（/healthz）
│   └── config/          # 配置加载
├── conf/config.yaml     # 本地配置（本地可信环境，明文）
└── docs/                # 方案文档
```

后续里程碑（见 `docs/00-overview.md` §9）逐步加入：7 实体 GORM model + MySQL 迁移、lark-cli 采集、mem0 记忆化、Todo 提取、codex 确认、Task 执行。
