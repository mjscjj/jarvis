# 运行与部署

> Status: current
> Authority: reference; scripts and plist files are source of truth
> Last verified: 2026-08-07

## 服务

| Label | 端口 | 安装方式 | 日志 |
|---|---:|---|---|
| `com.bytedance.jarvis.server` | 18800 | `./scripts/install-launchd.sh` | `var/log/jarvis-server.log`, `var/log/jarvis-server.error.log` |
| `com.bytedance.jarvis.web` | 18801 | 手工 link + `launchctl bootstrap` | `var/log/vite.log`, `var/log/vite.error.log` |
| `com.bytedance.jarvis.qdrant` | 6333/6334 | `./scripts/install-qdrant.sh` | `var/log/jarvis-qdrant.log`, `var/log/jarvis-qdrant.error.log` |
| `com.cc-connect.service` | 9810/9820 | `./bin/cc-connect-jarvis daemon install` | `~/.cc-connect/logs/cc-connect.log` |

18800 同时托管生产 `web/dist`；18801 只用于 Vite 开发热更。

三份 plist 由 `deploy/*.plist.template` 渲染，`conf/qdrant.yaml` 用相对 `WorkingDirectory` 的路径。移动仓库或换用户后重新渲染即可，不必改仓库文件。

`conf/config.yaml` 与 `conf/config.runtime.yaml` 都存明文密钥，权限保持 `600`。`config.yaml` 由 git 跟踪，而 git 只记录可执行位，重新 clone 后要再 `chmod 600`。

## 首次安装

推荐在完整仓库根目录让用户的 Agent 执行 `$install-jarvis`。它拥有从 checkout 到最终可用的整体安装状态，先创建统一清单，再用 doctor 暴露事实，由 Agent 决定依赖安装方式、飞书 App/Profile 和旧实例处置。所有依赖必须先安装并通过独立验收门，然后绑定同一个 App、启动 CC Connect/Jarvis、完成世界模型和真实端到端验收。

```bash
./scripts/jarvis-install start
./scripts/jarvis-install doctor
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies

# 依赖门返回 ok=true 后，选择并登录一个 lark-cli Profile，写 identity 并绑定 CC：
./scripts/jarvis-install configure-identity --open-id <open_id> --profile <profile> --git-author <author>
./scripts/jarvis-install bind-cc --profile <profile>
./scripts/jarvis-install validate-binding --profile <profile>

# 先启动补丁版 CC Connect，再启动 Jarvis：
./bin/cc-connect-jarvis daemon install --config "$HOME/.cc-connect/config.toml"
./scripts/jarvis-install install-server
./scripts/jarvis-install validate

# 把同一个 run_dir 交给 $bootstrap-jarvis-world-model 完成 INSTALL_CHECKLIST.md 的世界模型 E 区
# 完成监听群新消息和绑定 Bot 对话的真实端到端验收后读回总状态
./scripts/jarvis-install status --run-dir <run_dir>

curl --fail http://127.0.0.1:18800/healthz
curl --fail http://127.0.0.1:6333/healthz

# 逐项确认外部依赖；status=degraded 时看 dependencies 里哪一项是 error
curl -s http://127.0.0.1:18800/readyz | jq
```

顺序是硬边界：创建整体安装清单 → 基础工具链、lark-cli/Lark Skills、traex 登录、补丁版 CC Connect binary 和 Qdrant → `validate-dependencies` → 选择一个 App/Profile 并完成飞书用户登录 → 写 Jarvis runtime identity 与 CC Connect `jarvis-codex` → `validate-binding` → 启动补丁版 CC Connect → 主服务注册 → `$bootstrap-jarvis-world-model` → 真实端到端验收 → `status`。Qdrant 是依赖服务，可以在依赖阶段启动；CC Connect/Jarvis 不能在依赖门前启动。`install-server` 会再次强制通过依赖门和一体化绑定门。

当前内置 Qdrant 安装器只支持 macOS arm64。doctor 会按 `go.mod`、Vite engines、CGO/Xcode 工具链、Lark Skills、已有数据库与 launchd program 报告当前状态。若 label 属于其他 checkout 或发现旧业务数据，Agent 必须先请用户决定复用、迁移或替换。

launchd 不接受相对路径，所以 `deploy/` 里只有占位符模板；`scripts/render-launchd-plist.sh <label>` 按当前仓库位置和 `$HOME` 展开成 `~/Library/LaunchAgents/<label>.plist` 实体文件。改了模板要重新渲染才生效。

首次启用 18801：

```bash
uid=$(id -u)
plist=$(./scripts/render-launchd-plist.sh com.bytedance.jarvis.web)
launchctl bootstrap "gui/$uid" "$plist"
```

## 日常重建

```bash
./scripts/rebuild-server.sh
```

该脚本会：

1. 构建临时二进制；
2. 用固定 identity 签名并校验；
3. 若主服务已注册，查询 `/api/tasks?status=executing`；
4. 没有活跃 Task 才替换二进制并 `kickstart`；
5. 等待 `/healthz` 返回 200。

服务已注册但 18800 API 不可达时，脚本会拒绝重启。`--force-interrupt-running-tasks` 只允许明确中断已查到的执行任务，不能绕过 API 查询失败。

不要裸 `go build` 覆盖 `bin/jarvis-server`；否则会改变签名身份，导致完全磁盘访问权限不稳定。

## 状态与日志

```bash
uid=$(id -u)
launchctl print "gui/$uid/com.bytedance.jarvis.server"
launchctl print "gui/$uid/com.bytedance.jarvis.web"
launchctl print "gui/$uid/com.bytedance.jarvis.qdrant"

tail -f var/log/jarvis-server.log var/log/jarvis-server.error.log
```

## 配置

有效配置为：

```text
conf/config.yaml
  + conf/config.runtime.yaml 覆盖
```

`config.runtime.yaml` 由后台运行配置写入且被 Git 忽略。保存后需要重启；不要把其中数值写进 current 文档当成所有机器的默认值。

Jarvis Bot 的飞书长连接由 CC Connect 独占。`jarvis-server` 不启动 Feishu event consumer，M2 按 `capture.scan_schedule` 增量轮询工作消息；不要为同一个 app 恢复第二条连接。`lark_cli.profile`、`card_approval.profile` 和 CC Connect `jarvis-codex` 必须来自安装时选择的同一个 App/Profile；机器校验入口是 `./scripts/jarvis-install validate-binding --profile <profile>`。

飞书卡片内审批使用独立配置，不复用消息采集的事件开关。推荐让 CC Connect 继续持有当前 Jarvis Bot 的唯一长连接：

```yaml
card_approval:
  enabled: true
  profile: "<已确认的 lark-cli profile>"
  principal_open_id: "ou_xxx"
  relay_secret: "<与 CC Connect 相同的本机共享密钥>"
```

对应的 `jarvis-codex` Feishu platform 配置：

```toml
jarvis_approval_url = "http://127.0.0.1:18800/internal/card-approval/callback"
jarvis_approval_secret = "<同一个本机共享密钥>"
jarvis_approval_timeout_ms = 2500
```

M5 返回 proposal 后，Jarvis 先持久化 `awaiting_approval`，再由当前 Jarvis Bot 立即发送审批卡；按钮值使用 `action=jarvis_approval` 命名空间，并携带 `task_id` 和已持久化的 Task `version`。CC Connect 的现有 `OnP2CardActionTrigger` 收到点击后，通过带共享密钥的 localhost HTTP 请求转给 Jarvis，再把处理结果追加到原卡片并移除确认/拒绝按钮。Jarvis 不启动 `card.action.trigger` 连接，因此普通消息、文档评论和既有 CC Connect 卡片链路不会被抢占。

Jarvis 端校验 Principal open_id，并用卡片携带的 Task version 原子认领当前 `awaiting_approval` proposal；旧卡片或重复点击会因 version/status 冲突被拒绝。CC Connect 只负责机械传输和保留原卡片展示，不持有审批状态。URL 必须是 loopback，密钥只写进 Git 忽略的 `conf/config.runtime.yaml` 和本机 `~/.cc-connect/config.toml`。

当前 Jarvis Bot 对应的飞书 app 必须在开发者后台开启机器人、授予消息权限，并在「事件与回调 → 回调配置」中启用 callback。回调落地日志前缀为 `job=card-approval`；只有 Principal 本人的按钮点击会进入审批，版本冲突/状态已变会记为 `skipped=already-handled`，不会重复执行。

## 故障恢复

若主服务已注册但 API 不可达：

1. 先用进程、日志和 `launchctl` 确认没有仍在执行的 Agent 子进程；
2. 查看 `var/log/jarvis-server.error.log`，确认配置/迁移/签名失败原因；
3. 必要时 `launchctl bootout` 旧服务；
4. 运行 `./scripts/rebuild-server.sh` 构建和签名；服务未注册时脚本只替换二进制；
5. 从 `~/Library/LaunchAgents/com.bytedance.jarvis.server.plist` 重新 bootstrap；
6. 验证 `/healthz`、任务 API 和首页。

不要在不知道是否有活跃执行时强制重启；它会终止 Agent 子进程。
