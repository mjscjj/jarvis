# 运行与部署

> Status: current
> Authority: reference; scripts and plist files are source of truth
> Last verified: 2026-08-02 @ `89fa24b`

## 服务

| Label | 端口 | 安装方式 | 日志 |
|---|---:|---|---|
| `com.bytedance.jarvis.server` | 18800 | `./scripts/install-launchd.sh` | `var/log/jarvis-server.log`, `var/log/jarvis-server.error.log` |
| `com.bytedance.jarvis.web` | 18801 | 手工 link + `launchctl bootstrap` | `var/log/vite.log`, `var/log/vite.error.log` |
| `com.bytedance.jarvis.qdrant` | 6333/6334 | `./scripts/install-qdrant.sh` | `var/log/jarvis-qdrant.log`, `var/log/jarvis-qdrant.error.log` |

18800 同时托管生产 `web/dist`；18801 只用于 Vite 开发热更。

三份 plist 和 `conf/qdrant.yaml` 使用当前仓库的绝对路径。移动仓库或换用户后，先修改这些路径再 bootstrap。

## 首次安装

```bash
./scripts/install-launchd.sh
./scripts/install-qdrant.sh

curl --fail http://127.0.0.1:18800/healthz
curl --fail http://127.0.0.1:6333/healthz
```

`install-launchd.sh` 会执行前端 `npm ci + build`、编译后端、稳定签名、渲染主服务的 LaunchAgent 并 bootstrap。它不会安装 Qdrant，也不会安装 Web 开发服务。

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

Jarvis Bot 的飞书长连接由 CC Connect 独占。`jarvis-server` 不启动 Feishu event consumer，M2 按 `capture.scan_schedule` 增量轮询工作消息；不要为同一个 app 恢复第二条连接。

飞书卡片内审批使用独立配置，不复用消息采集的事件开关。推荐让 CC Connect 继续持有当前 Jarvis Bot 的唯一长连接：

```yaml
card_approval:
  enabled: true
  profile: "cli_a96a0c8d82b85cb1"
  principal_open_id: "ou_xxx"
  relay_secret: "<与 CC Connect 相同的本机共享密钥>"
```

对应的 `jarvis-codex` Feishu platform 配置：

```toml
jarvis_approval_url = "http://127.0.0.1:18800/internal/card-approval/callback"
jarvis_approval_secret = "<同一个本机共享密钥>"
jarvis_approval_timeout_ms = 2500
```

审批卡由当前 Jarvis Bot 发送；按钮值使用 `action=jarvis_approval` 命名空间。CC Connect 的现有 `OnP2CardActionTrigger` 收到点击后，通过带共享密钥的 localhost HTTP 请求转给 Jarvis，再把 Jarvis 返回的完整卡片同步回飞书。Jarvis 不启动 `card.action.trigger` 连接，因此普通消息、文档评论和既有 CC Connect 卡片链路不会被抢占。

Jarvis 端仍校验 Principal open_id、当前 Task 状态、proposal 的 `source_run_id`、发送卡片的 `message_id` 和 Task version；CC Connect 只负责机械传输，不持有审批状态。URL 必须是 loopback，密钥只写进 Git 忽略的 `conf/config.runtime.yaml` 和本机 `~/.cc-connect/config.toml`。

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
