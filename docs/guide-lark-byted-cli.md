# Lark CLI 与 ByteD CLI 使用手册

> Status: current guide
> Authority: operational reference; CLI `--help` is source of truth
> Last verified: 2026-08-02 @ `89fa24b`
> Verified versions: lark-cli 1.0.72, bytedcli 0.110.0

Jarvis 会直接或通过 Agent 调用 `lark-cli`、`bytedcli lark` 和 `bytedcli codebase`。CLI 更新较快；命令报错或父命令只打印 help 时，先以本机 `--help` 重新核对，不要从本文猜兼容写法。

## 1. 怎么选

| CLI | 适合场景 | 重要边界 |
|---|---|---|
| `lark-cli` | 飞书消息、文档、日历、通讯录、任务、妙记等完整 Lark 能力 | 新 shortcut 通常最先出现，Jarvis M2/M3/M5 直接使用 |
| `bytedcli lark` | 从 ByteD CLI 入口调用已经包装的 Lark 命令 | 不是与 lark-cli 完全等价；当前缺部分 `drive search`、`chat-list` shortcut |
| `bytedcli codebase` | Codebase 仓库、MR、commit、Issue | 使用 ByteCloud/Codebase 登录态，不是 Lark OAuth |

`bytedcli lark` 最终使用 Lark profile；`bytedcli auth login` 是 ByteCloud/Codebase 认证，不能代替 Lark 用户授权。

## 2. 初始化与认证

直用 lark-cli：

```bash
lark-cli config init
lark-cli auth login
lark-cli auth status
```

通过 bytedcli 管理 Lark profile：

```bash
bytedcli lark config
bytedcli lark login
bytedcli lark auth status
```

Codebase 等 ByteCloud 能力：

```bash
bytedcli auth login
bytedcli auth status
```

不要再使用已失效的 `lark-cli login`；首次配置也不是裸 `lark-cli config`。

## 3. 身份、风险与输出

- `--as user`：使用用户 OAuth，读取本人可见消息、文档和妙记时通常需要。
- `--as bot`：使用应用身份，能力受 bot scope 和可见范围限制。
- `--format json` 或 bytedcli 的 `--json`：给程序稳定解析。
- `--dry-run`：命令支持时先查看真实 OpenAPI 请求。

不要假设所有写操作都需要 `--yes`。当前消息发送、创建日程和创建文档是 `Risk: write`，没有统一 `--yes` 参数；只有 help 明确标为 `high-risk-write` 的命令才按该命令要求确认。

## 4. 常用命令

### 4.1 群消息

```bash
lark-cli im +chat-messages-list \
  --chat-id oc_xxx \
  --as user \
  --format json

bytedcli --json lark im chat-messages-list \
  --chat-id oc_xxx \
  --as user
```

发送消息：

```bash
lark-cli im messages send \
  --chat-id oc_xxx \
  --text "hello" \
  --as bot

bytedcli --json lark im messages-send \
  --chat-id oc_xxx \
  --text "hello" \
  --as bot
```

具体参数以 `lark-cli im --help` 和对应子命令 help 为准。旧写法 `bytedcli lark im message list/send` 不存在；部分版本会只打印父级 help 但 exit 0，调用方必须校验输出结构。

### 4.2 联系人

```bash
lark-cli contact +search-user \
  --query "张三" \
  --as user \
  --format json
```

### 4.3 妙记详情与逐字稿

```bash
lark-cli minutes +detail \
  --minute-tokens <token> \
  --transcript \
  --as user \
  --format json
```

旧命令 `minutes +get-transcript` 已不存在。

### 4.4 文档搜索

当前精确的“我编辑过”优先直用 lark-cli 的 drive search：

```bash
lark-cli drive +search \
  --edited-since 2026-08-02 \
  --edited-until 2026-08-03 \
  --sort edit_time \
  --as user \
  --format json
```

还可按需使用：

- `--opened-since` / `--opened-until`
- `--created-by-me`
- `--mine`

`--mine` 表示“我拥有”，不等于“我编辑过”。`--edited-*` 使用服务端 `my_edit_time`，不再需要宽搜后按 `edit_user_id` 本地过滤。

`docs +search` 在当前版本可以省略 `--query`，不要为了兼容旧文档硬塞空格 query。`bytedcli lark` 当前未完整暴露 drive search，因此这类查询直接使用 `lark-cli`。

### 4.5 Codebase MR

跨仓库查询我的 MR：

```bash
bytedcli --json codebase search mr \
  --author @me \
  --sort-by UpdatedAt \
  --sort-order Desc \
  --updated-since 2026-08-02T00:00:00+08:00 \
  --updated-until 2026-08-03T00:00:00+08:00 \
  --page-size 100
```

`mr list` 通常要求 `-R <repo>`，跨仓使用 `search mr`。

## 5. Jarvis 内部使用边界

### M2

`internal/larkcli` 封装 lark-cli 的限流、并发和超时。仓库基线 `lark_cli.concurrent=2`、rate=5、burst=10；实际运行值还可能被 runtime overlay 覆盖。

### Agent

M3/M5 可以直接调用 lark-cli/bytedcli 查证上下文。工具目录只描述稳定入口；具体子命令变化时应先查看本机 help。

### 进度页文档统计

当前 `internal/insight/worklog.go` 的文档查询语义仍是“owner=@me 且当天 update_time”，不是严格的“我当天编辑”。CLI 已支持 `my_edit_time`，但产品代码尚未切换；文档不能把 CLI 能力误写成页面已实现能力。

### jarvis-tools

`scripts/jarvis-tools` 不是 Lark CLI，也不直连数据库。它用 `curl + jq` 调在线 jarvis-server，供 Agent 查询 projects/persons/groups/todos/tasks/facts/scheduled tasks 等，或调用 `append-clue`、`yield-until`。服务不在线时会失败。

## 6. 常见故障

| 现象 | 检查 |
|---|---|
| `unknown command login` | 使用 `lark-cli auth login` |
| bytedcli 子命令 exit 0 但只有 help | 命令路径不存在；改用当前 shortcut 并校验 JSON |
| 用户可见数据为空 | 确认 `--as user`、Lark auth status 和业务 scope |
| bot 发送失败 | 检查 bot 是否在群、应用 scope 和消息目标 |
| bytedcli 找不到 drive search | 当前 wrapper 未暴露；直用 lark-cli |
| 文档“我编辑过”结果不准 | 页面当前是 owner+update_time；CLI 精确查询用 `--edited-*` |
| 参数是否需要 `--yes` 不确定 | 查看该子命令 help 的 Risk 标注，不套用全局规则 |

## 7. 维护要求

更新本文时至少实跑：

```bash
lark-cli --version
bytedcli version
lark-cli auth status
lark-cli <resource> <command> --help
bytedcli lark <resource> <command> --help
```

对于 bytedcli，不能只看 exit code；还要确认返回的是业务 JSON，而不是父级 help。
