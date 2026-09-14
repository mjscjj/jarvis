# 运行与部署

> Status: current
> Authority: operational reference; scripts and service templates are source of truth
> Last verified: 2026-09-14

## 1. 两种运行形态

### 源码安装

源码安装使用 launchd（macOS）或 user systemd（Linux）管理 Jarvis Server、Qdrant 和 CC Connect。完整 checkout 的唯一安装入口是 `$install-jarvis`：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

安装 Skill 负责依赖门、已有实例归属、飞书身份、CC 绑定、服务注册、世界模型和端到端验收。fresh clone 不直接运行底层服务脚本，也不使用日常 rebuild 代替首次安装。

### macOS App

DMG 安装不注册源码服务。Tauri 启动 `jarvis-app-service`，由它在应用生命周期内：

- 同步包内配置、Skills、脚本和 Web 资源到 Application Support；
- 启动和停止 Qdrant、Jarvis Server 与已配置的 CC Connect；
- 等待健康检查并把本地 URL 交给桌面窗口；
- 配置变化后按 `restart.requested` 重启本地服务；
- 退出应用时清理子进程组。

用户状态位于 `~/Library/Application Support/Jarvis`，不放在 `.app` 内。安装与升级见 [macOS 安装与更新](macos-install-and-update.md)。Agent 打包、验收和发布使用 [桌面发布 Skill](../../.agents/skills/release-jarvis-desktop/SKILL.md)，底层构建及托管参数见 [macOS 打包指引](../../packaging/macos/README.md)。

## 2. 有效配置

运行配置是：

```text
conf/config.yaml
  + conf/config.runtime.yaml 按叶子 key 覆盖
```

- 基线默认值只改 `conf/config.yaml`。
- 本机身份、密钥、端口、模型和调度通过后台设置或 `conf/config.runtime.yaml` 修改。
- 随包发布的基线配置拒绝未知字段；跨版本保留的 runtime overlay 忽略并记录未知键，已知字段的类型错误仍会报错。overlay 不得覆盖基线专有的 `sqlite` 段。
- runtime settings 保存后需要重启；prompts、rules 和大部分 Skills 按各自 reader 实时读取。
- 文档中的示例值不代表当前进程值。实际监听地址和依赖状态读取有效配置与 `/readyz`。

明文凭证只保存在本机配置，文件权限保持 `0600`。不要把 runtime overlay 提交到 Git。

## 3. 日常重建

后端、配置或生产前端变更后统一执行：

```bash
./scripts/rebuild-server.sh
```

脚本会：

1. 从当前 API 查询执行中的 Task；
2. 有 Task 执行时拒绝重启；
3. 构建生产前端和临时后端 binary；
4. macOS 使用稳定签名，Linux 替换 binary；
5. 重启现有服务，服务定义丢失时恢复当前 checkout 的注册；
6. 等待 `/healthz` 成功。

只有明确接受中断当前 Task 时使用：

```bash
./scripts/rebuild-server.sh --force-interrupt-running-tasks
```

不要裸 `go build` 覆盖运行中的 `bin/jarvis-server`。macOS 会破坏稳定签名，所有平台都会绕过执行中 Task 检查和健康验收。

仅修改前端且当前 Server 直接从 checkout 的 `web/dist` 提供静态资源时，可先执行：

```bash
npm --prefix web ci
npm --prefix web run build
```

是否需要重启取决于部署形态；完整重建仍以标准脚本为准。

## 4. 健康与日志

- `/healthz` 只验证服务和 SQLite，供重启脚本判断进程可用。
- `/readyz` 展示 SQLite、Qdrant、lark-cli、Agent CLI 等依赖；外部依赖失败可以返回 HTTP 200 和 `degraded`。
- launchd 使用 `launchctl print` 查看状态；Linux 使用 `systemctl --user status` 和 `journalctl --user`。
- 源码运行日志默认在 `var/log/` 或服务管理器日志中。
- macOS App 日志位于 `~/Library/Application Support/Jarvis/logs`。

服务 label、模板和日志路径以 `deploy/`、`scripts/install-*.sh` 及当前服务定义为准。

## 5. 完整退出

源码安装使用：

```bash
./scripts/stop-jarvis.sh
```

脚本停止当前 checkout 的 Jarvis Server、Qdrant、CC Connect 和可选开发 Web 服务，不删除配置、数据库、日志或业务数据。

macOS App 退出时由桌面 supervisor 停止它管理的本地子进程，同样不删除用户数据。

## 6. 自动更新托管

发布机可以通过 `JARVIS_UPDATE_ROOT` 让 Jarvis Server 托管 `latest.json`、更新包和 DMG：

- 未配置时不注册更新路由；
- 目录不存在时服务启动失败；
- `latest.json` 禁止长期缓存；
- 版本化产物使用 immutable cache；
- 大文件按流传输，不经过全量 gzip 缓冲；
- symlink 产物拒绝提供。

Agent 按 [桌面发布 Skill](../../.agents/skills/release-jarvis-desktop/SKILL.md) 组织固定提交、候选包验收及发布；构建、签名、版本同步和上传参数见 [macOS 打包与发布指引](../../packaging/macos/README.md)。`--candidate` 发布已封存的包，不在上传前重新构建；候选摘要核对不等于真实旧版升级通过。外部网关配置由部署环境维护，不复制进本仓库文档。

## 7. 故障恢复

服务已注册但 API 不可达时：

1. 查看服务状态、当前子进程和错误日志；
2. 确认是否仍有 Agent 子进程或执行中 Task；
3. 修正配置、迁移、依赖或签名问题；
4. 使用 `./scripts/rebuild-server.sh` 恢复当前 checkout；
5. 验证 `/healthz`、`/readyz`、首页和关键 API。

数据库迁移 fail-fast 时保留原数据和错误，不自动清库。无法安全推断的旧 schema 由人工决定迁移或重建。
