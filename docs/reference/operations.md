# 运行与部署

> Status: current
> Authority: operational reference; scripts and service templates are source of truth
> Last verified: 2026-09-14

## 1. 两种运行形态

### 源码安装

源码安装使用 launchd（macOS）或 user systemd（Linux）管理服务。完整 checkout 的唯一安装入口是 `$install-jarvis`：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

安装 Skill 负责依赖门、已有实例归属、飞书身份、CC 绑定、服务注册、世界模型和端到端验收。fresh clone 不直接运行底层服务脚本，也不使用日常部署代替首次安装；安装进度记录在 `var/install/<run-id>/INSTALL_CHECKLIST.md`。

| 服务 | 地址来源 | 管理方式 |
|---|---|---|
| `com.bytedance.jarvis.server.<配置路径摘要>` | `server.addr` | macOS launchd / Linux user systemd |
| `<实例服务名>.web` | 后端相邻端口 | 可选 Vite 开发服务 |
| `com.bytedance.jarvis.qdrant` | Qdrant 配置 | 依赖服务 |
| CC Connect | CC 配置 | CC daemon / `install-cc-systemd.sh` |

`server.addr` 同时托管 API、生产 `web/dist` 和 `/api/chat/*`。服务名按配置文件绝对路径派生，改端口不改服务名。通过 `scripts/jarvis-instance [CONFIG_PATH]` 读取服务名、地址和日志，通过 `scripts/jarvis-api-base` 获取当前 Agent 使用的 API 地址。

首次启用 macOS 实例的 Vite 服务时，用 `scripts/render-launchd-plist.sh com.bytedance.jarvis.web [CONFIG_PATH]` 渲染，再对返回的 plist 执行 `launchctl bootstrap`。Linux unit 由部署脚本生成；自定义环境变量放同名 `.d/` 目录中的 drop-in，不手改生成的 unit。路径限制以脚本校验为准。

### macOS App

DMG 安装不注册源码服务。Tauri 启动 `jarvis-app-service`，由它在应用生命周期内：

- 同步包内配置、Skills、脚本和 Web 资源到 Application Support；
- 启动和停止 Qdrant、Jarvis Server 与已配置的 CC Connect；
- 等待健康检查并把本地 URL 交给桌面窗口；
- 配置变化后按 `restart.requested` 重启本地服务；
- 退出应用时清理子进程组。

用户状态位于 `~/Library/Application Support/Jarvis`，不放在 `.app` 内。安装与升级见 [macOS 安装与更新](macos-install-and-update.md)，打包发布见 [macOS 打包与发布指引](../../packaging/macos/README.md)。

## 2. 有效配置

```text
conf/config.yaml
  + conf/config.runtime.yaml 按叶子 key 覆盖
```

- 基线默认值只改 `conf/config.yaml`。
- 本机身份、密钥、端口、模型和调度通过后台设置或 `conf/config.runtime.yaml` 修改；runtime overlay 不进 Git，权限保持 `0600`。
- 两份配置都拒绝未知字段；未被 runtime 指定的 key 保留基线值。
- runtime settings 和功能模块配置保存后需要重启；prompts、rules 和 Skills 按各自 reader 实时读取。
- 文档示例不代表当前进程值，实际地址与依赖状态读取有效配置和 `/readyz`。

OKR 模块另从 `conf/okr-module.yaml` 读取公共默认值，并以本机 `conf/okr-module.runtime.yaml` 覆盖指定字段。当前 Emily 的开发容器、应用绑定与模型登录文件属于该本机文件。完整研发环境的开发实例配置由 `scripts/emily-dev` 生成，详见[Emily 完整研发环境](../summery/emily-development-environment.md)。

主进程向 Agent 继承 `JARVIS_API_BASE`、`JARVIS_CONFIG` 和工具 PATH；切换工作目录不会切换实例。多实例只隔离寻址与服务管理，数据库、上传和执行产物需分别配置；外部账号、Qdrant collection 和 Bot 长连接不会自动隔离。

## 3. 日常部署

后端、配置或生产前端变更后统一执行：

```bash
./scripts/jarvis-deploy --skip-pull
```

脚本安装并构建前端、编译主服务，按当前系统只更新选定实例，再验证首页、`/healthz` 和 `/readyz`。macOS 复用稳定签名与 launchd 链路；Linux 注册并重启 user systemd unit。Chat 已由主服务承载，不再构建或启动独立 Chat sidecar。

不带 `--skip-pull` 时，脚本先要求工作树干净并 fast-forward pull 当前 upstream。运行可能修改 Git 跟踪的 `data/okr/okr.db`；数据库及新增/修改的 OKR 产品资源必须保留并提交。只有明确决定放弃本机数据库改动、采用 Git 版本时才使用 `--remote-okr-db`。

Emily 开发容器运行时挂载的就是线上 `data/okr/`；数据修改立即生效。Git worktree 的索引彼此独立，提交开发代码或向 main 合并时，要从共享目录取得 OKR 数据库的一致快照，并纳入同批产品资源。私有 Task、Message 主库和登录 token 不随代码提交。具体边界和当前实例启动命令见[研发环境文档](../summery/emily-development-environment.md)。

升级脚本清理旧 Chat sidecar 的配置和当前实例服务；旧 Markdown 对话不迁入持久会话。新会话历史保留，但重启主服务会打断当前对话轮次。

部署前检查当前实例的执行任务：

```bash
curl --fail "$(./scripts/jarvis-api-base)/api/tasks?status=executing"
```

macOS 底层 `rebuild-server.sh` 会在替换二进制前查询执行中的 Task，API 不可达或存在活跃 Task 时拒绝继续；`--force-interrupt-running-tasks` 只能在明确接受中断时使用，不能绕过查询失败。Linux 路径没有该检查，重启会中断执行子进程。

`rebuild-server.sh` 依赖 zsh、codesign、launchctl 和 plutil，仅适用于 macOS。所有平台都不要自行拼接 `go build` 与重启；macOS 还会因此破坏 TCC 稳定签名。

已有固定名称或手工注册的旧服务不会自动迁移。先核对进程、工作目录、数据库和活跃执行，再停止确认属于当前实例的旧服务；不要同时保留两个指向同一配置的自动重启服务。

## 4. 健康与日志

```bash
./scripts/jarvis-instance conf/config.yaml
curl --fail "$(./scripts/jarvis-api-base)/healthz"
curl --fail "$(./scripts/jarvis-api-base)/readyz" | jq
```

- `/healthz` 验证服务和 SQLite，供部署脚本判断进程可用。
- `/readyz` 展示 SQLite、Qdrant、lark-cli、Agent CLI 等依赖；外部依赖失败可以返回 HTTP 200 和 `degraded`，部署验收仍要求依赖为 `ok` 或 `disabled`。
- macOS 用 `launchctl print`，Linux 用 `systemctl --user status <label>.service` 和 `journalctl --user`。
- 源码日志位于配置的 `server.log_files`，通常在 `var/log/`；macOS App 日志在 `~/Library/Application Support/Jarvis/logs`。

服务 label、模板和日志路径以 `deploy/`、安装脚本和当前服务定义为准。

## 5. 完整退出

源码安装使用：

```bash
./scripts/stop-jarvis.sh --config conf/config.yaml
```

后台“退出”也调用该脚本。它只停止选定实例的 Jarvis Server 和可选开发 Web 服务，不停止共享 Qdrant、CC Connect 或其它配置的 Jarvis。退出不删除配置、数据库、日志或业务数据，下次按正常部署流程启动。

macOS App 退出时由桌面 supervisor 停止它管理的本地子进程，同样不删除用户数据。

## 6. 用户入口与飞书卡片

`server.addr` 控制监听，`server.public_base_url` 是用户打开 Jarvis 的统一入口，用于页面分享、通知、问题和回答后卡片的详情链接。未配置时，macOS 使用本机地址，Linux 使用可解析的局域网地址及实际服务端口；回环链接仅能在有本地服务或端口转发的设备打开。

飞书卡片回答 M5 的提问使用独立的 `card_approval` 配置，不复用消息采集开关。CC Connect 持有 Jarvis Bot 唯一长连接，`card_approval.enabled`、`principal_open_id` 和本机 `relay_secret` 与 CC 绑定配套使用。

绑定工具按实例生成 CC Feishu platform 的 `jarvis_approval_url`、`jarvis_approval_secret` 和 timeout；改端口后重新绑定以刷新回调地址，校验工具会暴露旧地址。绑定只操作确认过的唯一 Bot，不自动复制多个连接。密钥只写本机配置，不提交仓库。

## 7. 自动更新托管

发布机可通过 `JARVIS_UPDATE_ROOT` 让主服务托管 `latest.json`、更新包和 DMG：

- 未配置时不注册 `/jarvis-updates/:filename`；
- 目录不存在或不是目录时启动失败；
- `latest.json` 禁止长期缓存，版本化产物使用 immutable cache；
- 大文件按流传输，不经过全量 gzip 缓冲；
- symlink 产物拒绝提供。

构建、签名、版本同步和上传顺序见 [macOS 打包与发布指引](../../packaging/macos/README.md)。外部网关配置由部署环境维护，不复制到本仓库。

## 8. 故障恢复

服务已注册但 API 不可达时：

1. 查看所选实例的服务状态、子进程和错误日志，确认是否仍有执行中的 Agent。
2. 修正配置、迁移、依赖或签名问题，不停止其它实例。
3. 通过 `./scripts/jarvis-deploy --skip-pull` 恢复；macOS 若因 API 不可达拒绝重建，先确认无活跃执行，再按底层脚本的 build-only 与服务重新注册流程恢复。
4. 验证 `/healthz`、`/readyz`、首页和关键 API。

数据库迁移 fail-fast 时保留原数据和错误，不自动清库。无法安全推断的旧 schema 由人工决定迁移或重建。
