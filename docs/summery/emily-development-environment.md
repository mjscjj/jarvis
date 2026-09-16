# Emily 开发实例

代码与 OKR-MVP 使用同一套实现。分支只承载尚未合入的功能；环境差异属于
本机配置、容器 HOME 和数据目录，不维护开发版业务逻辑。

本文描述整理后的代码和切换步骤，不代表当前运行容器已经完成迁移。
旧容器在完成下述数据整理、独立授权和重新部署之前仍使用原来的挂载。

## 数据与身份

| 内容 | 归属 |
|---|---|
| `data/okr/okr.db`、SQLite sidecar、`assets/`、`activity/`、Owner 映射和术语表 | 唯一共享的业务数据目录，两边读写同一份 |
| `var/development.db`、普通 Chat、OKR Chat、附件、Task、Message、世界模型、日志 | 各实例私有，不从主环境导入 |
| Lark、ByteDance、Codex 配置及凭证 | 各实例 HOME 内独立授权，不挂载主环境登录文件 |
| OKR 网页用户 Token、通讯录缓存 | 各实例 `var/okr/`，不进入共享目录 |
| CLI 可执行程序、Go 工具链 | 可只读复用程序文件，不复用身份配置 |

同名 CLI、Profile 和容器内路径不会导致身份共享。开发实例 HOME 是
`/dev-state/home`，对应当前 checkout 的 `var/container/home`。
不再使用旧的宿主 `var/emily-development/home`，其中可能遗留复制的模型凭证。

容器使用普通 Docker bridge 网络，不运行出口网关、域名白名单或宿主 CLI 代理。
镜像直接基于 Node，不继承单轮 OKR Chat 镜像的 HTTP(S) 代理变量。
没有宿主 Docker socket，不使用 host 网络。

## 对话与入口

前端 API、静态文件和附件统一使用当前实例的路径前缀；开发页面不再绕回根路径
`/api/okr-chat`。已有通用路径代理、Cookie 路径处理和前端 `appPath` 保留。

`okr-module.runtime.yaml` 的 `chat.runtime` 只有两种执行方式：

- `docker`（公共默认）：保留主环境原有的单轮隔离容器及受限 OKR API。
- `local`：在当前实例运行已安装的 CLI，复用普通 Chat runner、工具目录、
  取消执行和原生 thread 恢复。适用于整个实例都可供开发 Agent 使用的容器。

两者使用各自实例的 `var/okr-chat/chat.db` 和附件目录，并保留按 OKR 用户的
会话归属检查。`local` 不是浏览器用户之间的进程沙箱；该实例的数据和能力属于
开发用途。敏感主环境保持 `docker`，不把它切换为 `local`。
旧的 `chat.development_container` 已删除；不再由主服务跨容器运行开发对话。

开发入口仍可使用主服务现有的路径代理，例如：

```yaml
server:
  development_path: /dev/
  development_socket: /tmp/emily-development-1001/ingress/web.sock
```

这是 HTTP 入口，不共享数据库、登录 Token 或模型会话。路径不声明为浏览器安全边界。
入口由主实例管理员维护；开发启动脚本不再修改或重启主实例。

## 初始化与部署

从开发 checkout 运行。宿主只需 Docker、Python/PyYAML、Go 和已安装的原生
`lark-cli`、`bytedcli` 程序。后两者以只读程序文件挂载进容器；宿主是否登录无关。

首次生成配置：

```bash
./scripts/emily-dev --initialize --public-url https://example.com/dev/ \
  --principal developer@example.com
```

该命令只从仓库公共默认值生成当前实例的 `config.development.yaml`、
`config.runtime.yaml` 和 `okr-module.runtime.yaml`，遇到既有配置会拒绝覆盖。
`sqlite.path` 仍属于基础配置；其余本机调整沿用 runtime overlay。
后台采集和自动任务初始关闭，可以按开发需要启用，不作为工具权限限制。

在本机 `okr-module.runtime.yaml` 配置开发 Lark App、Profile、网页身份和
`identity.app_secret_env`，密钥放在开发专用环境文件中。不得复制主实例的
App Secret、principal、Profile 配置或用户 Token。开发负责人信息按既有配置
入口设置，不能沿用主实例的 open_id。

启动前检查共享目录和挂载：

```bash
./scripts/emily-dev --okr-data /absolute/product-only/okr \
  --env-file /absolute/development.env --dry-run
```

去掉 `--dry-run` 可启动容器。在容器内独立配置/登录 Lark、ByteDance 和模型 CLI；
ByteDance 使用本站适用的 `--site i18n-tt`。新 Profile 可以继续使用默认名称。
未完成应用配置和授权时，Biz OKR 的现有启动检查会报错，不回退到主环境身份。

授权后只部署开发实例：

```bash
docker exec emily-development ./scripts/jarvis-deploy --skip-pull
```

后续重建容器可给 `scripts/emily-dev` 加 `--deploy`，同样调用标准部署入口。
HOME、开发数据库、会话和日志均持久化在开发 checkout 的 `var/` 等实例目录中。

## 从旧环境切换

代码整理与运行迁移分开进行。切换前备份两边本机配置，保留现有功能改动和
运行数据；不要用 Git checkout/restore 把快照写回共享 SQLite。

1. 主环境保留既有 App/授权。把主环境 OKR 用户 Token、`directory-cache*`、
   `agent-session` 和备份移入主环境自己的 `var/okr/`，同步实际消费者的路径。
   将主环境 `identity.token_dir` 改为 `var/okr/feishu-tokens`；通讯录缓存自动
   随该目录定位。`agent-session` 等无仓库消费者的遗留文件先归档，不删除。
   迁移 Token 与修改配置需在停写窗口内完成，避免丢失新授权。
2. 保持业务数据库和资源原地不动。共享目录只允许业务文件及 SQLite sidecar；
   启动脚本发现 Token、缓存、其他未知文件或资源软链接会拒绝启动。
   挂载整个业务目录，不能单独 bind SQLite 文件，避免 WAL 和文件替换问题。
3. 公共代码合入主分支后，主环境解除 `development_container` 绑定，配置 `runtime: docker`，仍使用
   主环境自己的会话库和原有隔离执行方式。部署主服务统一使用
   `./scripts/jarvis-deploy --skip-pull`，先确认没有应继续运行的任务/对话。
4. 备份开发实例原来从主环境生成的三份配置，再初始化独立配置。既有
   `var/development.db` 保留；检查其中的旧身份设置并按开发身份调整。
   使用新的 `var/container/home`，不复制旧 HOME 中的凭证。
5. 完成开发独立授权并重建容器，确认开发页面全部 API 请求走自身前缀。
   切换后停用旧 `emily-development-gateway.service`；历史文件可以归档，
   不把主环境的聊天历史搬进开发环境。

## 共享数据与 Git

OKR 业务数据仍从 OKR-MVP worktree 提交一致快照及同批资源，不在开发分支复制库。
宿主开发 worktree 如需读取同一份文件，可显式建立链接：

```bash
./scripts/emily-share-okr-data --source /absolute/product-only/okr
```

该脚本只管理当前 worktree 的链接与 Git 忽略规则，不移动或恢复源目录的数据。
旧检出副本会保存在开发 `var/okr-checkout-before-share-*`。
容器支持该链接，也支持把业务目录直接挂在正常的 `data/okr` 目录上。
合并代码时保留共享链接，不让 Git 写回实时库。

## 验证

- `python3 -m unittest discover -s deploy/emily-dev -p '*_test.py'`：初始化不继承
  主身份、不覆盖既有配置、共享目录排除私人文件、网络与挂载边界。
- `go test ./internal/okrchat ./internal/chat ./internal/okrworkspace/moduleconfig ./cmd/jarvis-config`：
  本地执行、实例间数据分离、会话/附件归属、取消和历史恢复、配置校验。
- 前端类型检查和业务测试：保留区域分享、评论浏览及提醒功能；浏览器实测在
  新授权和运行迁移后进行，不能用单元测试替代真实登录验证。

2026-09-16 代码整理验证：196 项前端测试、类型检查、4 项部署/验收入口测试通过；
`internal/okrchat`、`internal/chat`、`internal/okrworkspace/moduleconfig`、
`cmd/jarvis-config` 测试通过，服务入口编译检查通过。工具目录相关测试通过；
完整工具目录测试中的两项宿主安装集成测试在旧容器中仍受 Git 元数据隐藏、
systemd/旧 CLI 包装环境限制，不记作全量 Go 测试通过。
新镜像 `emily-development:isolation-check` 构建成功；临时 bridge 容器可运行
原生 Lark CLI 1.0.93、ByteDance CLI 0.144.0，且没有主凭证和旧代理变量。
直连 npm 得到 HTTP 200，飞书站点返回 HTTP 404（仅证明 HTTPS 可达，不代表
授权或 API 功能通过）。未替换运行中的容器，未改主环境配置或实时 OKR 数据。
