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

`install-launchd.sh` 会执行前端 `npm ci + build`、编译后端、稳定签名、建立主服务的 LaunchAgent 软链并 bootstrap。它不会安装 Qdrant，也不会安装 Web 开发服务。

首次启用 18801：

```bash
uid=$(id -u)
mkdir -p "$HOME/Library/LaunchAgents"
ln -sfn \
  /Users/bytedance/workspace-local/jarvis/deploy/com.bytedance.jarvis.web.plist \
  "$HOME/Library/LaunchAgents/com.bytedance.jarvis.web.plist"
launchctl bootstrap "gui/$uid" \
  "$HOME/Library/LaunchAgents/com.bytedance.jarvis.web.plist"
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

## 故障恢复

若主服务已注册但 API 不可达：

1. 先用进程、日志和 `launchctl` 确认没有仍在执行的 Agent 子进程；
2. 查看 `var/log/jarvis-server.error.log`，确认配置/迁移/签名失败原因；
3. 必要时 `launchctl bootout` 旧服务；
4. 运行 `./scripts/rebuild-server.sh` 构建和签名；服务未注册时脚本只替换二进制；
5. 从 `~/Library/LaunchAgents/com.bytedance.jarvis.server.plist` 重新 bootstrap；
6. 验证 `/healthz`、任务 API 和首页。

不要在不知道是否有活跃执行时强制重启；它会终止 Agent 子进程。
