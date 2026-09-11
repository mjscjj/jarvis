# 外部 CLI 使用边界

> Status: current
> Authority: operational reference; each CLI `--help` is source of truth
> Last verified: 2026-09-11

Jarvis 和执行 Agent 主要使用 `lark-cli`、`bytedcli`、Agent CLI、`git` 与 `scripts/jarvis-tools`。CLI 更新频繁，本文只定义能力归属，不维护完整命令手册或补丁版本。

## 能力选择

| 工具 | 使用场景 |
|---|---|
| `lark-cli` | 飞书消息、文档、日历、会议、通讯录、任务和其它 Lark 能力 |
| `bytedcli` | ByteCloud/Codebase 等内部工程系统；已有 Lark wrapper 只在功能满足时使用 |
| `traex` / `codex` | Agent 执行；实际选择由有效配置和具体会话决定 |
| `git` | 当前仓库和外部工作区的版本控制事实 |
| `scripts/jarvis-tools` | Jarvis 内部状态和 API 的稳定 JSON 入口 |

## 身份边界

- lark-cli 当前默认 App 与用户 OAuth 是 Jarvis 的飞书身份真源。
- Bot/App 权限、事件订阅和用户 OAuth 是不同主体，不能互相冒充已配置。
- bytedcli 的 ByteCloud 登录不能代替飞书用户授权。
- Agent CLI 的登录状态彼此独立。
- 桌面安装使用包内 CLI；源码安装使用 PATH 中的 CLI。

## 使用原则

1. 先运行目标命令的 `--help` 或对应 schema，再构造参数。
2. JSON 命令只解析 stdout；stderr 保留为诊断，不混入 payload。
3. 不在文档、prompt 或 Go 代码复制第三方 CLI 的完整手册。
4. 领域操作步骤写入对应 Skill；通用能力入口写入 `internal/toolcatalog/`。
5. 版本、Skills 或协议不满足安装要求时，由 `$install-jarvis` 的依赖门处理。
6. 命令失败时保留原始错误，不用字符串猜测业务语义。

## 查找入口

```bash
lark-cli --help
lark-cli <domain> --help
bytedcli --help
traex --help
codex --help
./scripts/jarvis-tools help
./scripts/jarvis-install help
```

飞书具体工作流优先读取当前环境安装的 `lark-*` Skill；源码安装和授权使用 `.agents/skills/install-jarvis/`。
