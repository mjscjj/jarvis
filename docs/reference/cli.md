# CLI 使用边界

> Status: current
> Authority: operational reference; each CLI `--help` is source of truth
> Last verified: 2026-09-13

Jarvis 和执行 Agent 主要使用 `lark-cli`、`bytedcli`、Agent CLI、`git` 与 `scripts/jarvis-tools`。CLI 更新频繁，本文只定义能力归属，不维护完整命令手册或补丁版本。

## 能力选择

| 工具 | 使用场景 |
|---|---|
| `lark-cli` | 飞书消息、文档、日历、会议、通讯录、任务和其它 Lark 能力 |
| `bytedcli` | ByteCloud/Codebase 等内部工程系统；已有 Lark wrapper 只在功能满足时使用 |
| `traex` / `codex` | Agent 执行；实际选择由有效配置和具体会话决定 |
| `git` | 当前仓库和外部工作区的版本控制事实 |
| `scripts/jarvis-tools` | Jarvis 内部状态和 API 的稳定 JSON 入口 |

## Jarvis 工具契约

- 保留扁平命令，已知命令可直接调用。`--help` 展示能力组，`help world|evidence|task|schedule|memory|skill|notify` 按组发现，`help all` 展示完整命令索引；`<command> --help` 离线提供输入、返回与机器约束。
- 公共工具目录只负责能力发现，不复制命令参数、Skill 操作步骤或阶段职责。各阶段能力相同，M3/M5 的调查深度与停止条件仍由各自 Prompt/rules 决定。
- Bash 入口加载 `scripts/lib/jarvis-tools/`。HTTP 错误正文写 stderr 并非零退出，不自动重试写请求；原始 JSON 由 Node helper 无损提取，保留大整数、小数和未知语义字段。
- 地址按 `--api-base URL` → `JARVIS_API_BASE` → 工作区有效配置选择。配置路径按 `--config PATH` → `JARVIS_CONFIG_PATH` → 工具所属工作区 `conf/config.yaml` 选择，包含同目录的 runtime overlay；相对路径以工具工作区为基准。自然日 `--date` 使用 `JARVIS_TIMEZONE`，缺失时读取配置时区。
- 无显式连接时复用 `jarvis-config show-connection`：源码执行现有 `go run` 入口，桌面执行资源目录中的二进制。有完整连接环境的 Agent 不再加载配置；选定地址失败即报错，不探测或切换端口。源码独立调用可能有 Go 启动开销。
- 服务用临时 `-addr` 启动时，独立工具传 `--api-base` 或环境变量，CC 的 `bind/validate` 同步传 `--addr`。长期地址变更应更新配置并重新绑定、重启相关进程。
- 世界实体列表由服务端搜索并分页；CLI 默认读取一页，通过 `--page` / `--limit` 继续读取，不再全量抓取后本地过滤。`get-agent-identity` 返回有效 Agent 名称及配置中的 Principal open ID，不暴露凭据。
- 桌面包的 `scripts/`、JSON helper 与 `web/dist/` 随版本更新，即使本地修改过也会被替换；可编辑配置、Prompt、rules、Skills 仍保留本地修改。
- Skill 的 `enabled` / `stages` 只控制自动加入阶段目录，不构成读取权限；插件停用控制其 Skill 可用性。`inline` 仍保留短小且必要的阶段行为，领域长流程按需读取。
- M3 的 `model_api` 仍受支持；只有该引擎构造 function-calling 工具箱，Codex 直接调用 CLI。不同 Agent runner 的生命周期统一不在本轮改动范围。

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
./scripts/jarvis-tools --help
./scripts/jarvis-tools help world
./scripts/jarvis-tools get-project --help
./scripts/jarvis-install help
```

飞书具体工作流优先读取当前环境安装的 `lark-*` Skill；源码安装和授权使用 `.agents/skills/install-jarvis/`。
