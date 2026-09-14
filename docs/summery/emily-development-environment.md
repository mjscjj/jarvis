# Emily 完整研发环境

> Status: current · 本文是完整研发模式的操作与维护真源；原始单轮 OKR 容器方案见 [历史设计](okr-chat-isolation-mvp-design.md)。

## 范围

- 完整源码使用独立 worktree，由常驻 Docker 容器执行、构建和运行前后端。
- 直接把线上 `data/okr/` 挂入开发容器，OKR 数据库、图片和业务资源是同一份物理文件，允许修改数据和表结构。
- 任务、消息、普通会话等使用开发实例自己的 `var/development.db`，不复制生产主库、日志或私人资料。
- 网页沿用现有登录实现，开发会话单独保存。通知机器人和通讯录查询经宿主凭证出口复用；不向容器提供 principal 的个人消息/私有文档授权。
- 容器没有直接网络和 Docker 控制 socket；模型、登录及依赖下载走明确的 HTTPS 目标出口。容器的 `jarvis-tools` 访问自己的完整 API。
- 生产主服务代码更新仍走现有部署流程；开发实例构建不会更新生产前后端。

## 可配置的访问路径

用户已明确接受同域开发页面可能使用生产浏览器登录态，不要求子域名隔离。
路径只用于区分实例，不声明它是浏览器安全边界。

生产入口的本机配置示例（仅启用开发入口的实例填写）：

```yaml
server:
  development_path: /dev/
  development_socket: /tmp/emily-development-1001/ingress/web.sock
```

开发实例生成配置的关键字段示例：

```yaml
server:
  addr: 127.0.0.1:18812
  public_base_url: https://emily.bytedance.net/dev/
sqlite:
  path: var/development.db
capture:
  enabled: false
```

`public_base_url` 是开发实例公开地址的真源。`jarvis-instance` 派生 `web_base_path`；
标准部署脚本将该值交给 Vite 构建，前端从生成 HTML 的 meta 中读取统一前缀，
用于 API、静态资源和附件。不要在业务组件里增加 `/dev/api/...` 分支。
代理统一去掉配置前缀，后端始终使用原来的 `/api/...` 路由。
Cookie 的名称空间和 Path、相对重定向也随代理前缀变化。
不配置代理时生产实例没有额外开发入口；公开地址没有路径时前端仍从 `/` 构建。

`/dev/` 可以替换成 `/sandbox/emily/` 等规范路径，通过下文的 `--path` 重新生成配置并构建即可，不需要改业务代码。
不能只修改代理路径而保留旧的前端构建产物。

## 初始化与部署

主机使用 `/usr/bin/python3`（需要 PyYAML）、Docker、Go、npm、已配置的 lark-cli：

```bash
./scripts/emily-dev --path /dev/ --secret-file /path/to/existing-okr-app.env \
  --directory-profile YOUR_DIRECTORY_PROFILE \
  --notification-profile YOUR_NOTIFICATION_PROFILE --activate
```

初始化脚本从现有公开域名和 `--path` 生成开发实例的 `public_base_url`，
`--activate` 同步生成生产代理配置并调用标准部署脚本启动开发与生产服务。
更换前缀时仍使用这一条命令，不需要分别手改两处配置。主机重启后可重跑该命令恢复临时 socket。
不修改公司的域名网关。源码位于旁边的 `emily-development` worktree，分支 `codex/emily-development`。
入口通过生产服务中的可选代理连接容器 Unix socket；生产网关原有根路径转发即可覆盖它。

生产实例的 `conf/okr-module.runtime.yaml` 设置 `chat.development_container: emily-development`，将 OKR 对话的执行环境切到该容器；
对话列表仍保存在现有共享的 `var/okr-chat/chat.db`。新会话使用完整研发工具说明；旧会话不会被自动改写。

Codex 原生 thread 丢失时，主站自动以同一网页会话中已保存的历史重建底层 thread，并记录原始 CLI 错误；不会删除共享历史或要求手动新开会话。授权及网络错误不会被当作 thread 丢失，恢复后可再次发送消息。真实容器回归可运行 `EMILY_DEVELOPMENT_CHAT_ROOT=$PWD/var/okr-chat go test ./internal/okrchat -run TestDevelopmentServiceRecoversUnavailableNativeThread -v`，测试会使用临时会话并清理。

开发页面的 Biz OKR 对话框直接调用同域主站的 `/api/okr-chat/*`，复用这份会话库和主站已有的 OKR 飞书登录状态；执行仍进入同一个研发容器。开发实例自己的 OKR Chat 服务保持关闭，避免在容器里递归启动 Docker。普通开发页面 API 继续走配置前缀，只有这一个已存在的 OKR 对话接口使用根路径。

容器内重新构建使用 `./scripts/jarvis-deploy --skip-pull`。没有 systemd 或宿主部署能力，
它只管理开发实例 PID。Git worktree 的正式提交与合并由宿主开发流程完成。

## 凭证与数据细节

- 整个 `data/okr/` 共享，以保持 SQLite 和资源路径一致；其中 `feishu-tokens/` 用开发 token 目录覆盖，既有网页登录用户的 token 不进入容器。
- 模型只挂载登录文件，不挂宿主 Codex 配置、MCP、普通会话或记忆。
- 开发环境保留通知应用的网页登录 secret，宿主 lark-cli 出口仅接受身份检查、通讯录查询以及固定通知机器人操作，不开放个人消息、任意命令或本地文件参数。
- `capture.enabled` 省略时保持现有行为；显式 `false` 时不注册自动发现/采集作业。该开关本身不是数据安全边界。
- 开发实例不启动生产 CC Connect 长连接；本地回调使用新生成的 relay secret。

## 共享 OKR 数据的提交方式

开发容器运行时挂载生产 worktree 的 `data/okr/`。它直接修改这份产品数据，修改立即对线上 OKR 生效；开发实例的 Task、Message、网页登录会话和模型会话仍保存在各自的运行库里。

Git worktree 各有自己的索引和检出文件。开发 worktree 里看到的 `data/okr/okr.db` 路径，不等于容器实际挂载的生产目录。**只从正在使用的共享目录取得一个 SQLite 一致快照，并把它和同批新增或修改的 OKR 产品资源带入要合并的分支。** 不让两个 worktree 各自维护、提交不同的数据库版本。活跃库用 SQLite backup 生成快照，再写入目标分支的 Git 索引；也可以把已有的数据提交合入目标分支。两个分支若都需要记录这次产品变化，应使用同一快照。合入 main 后，main 保留这份产品数据的版本历史；部署不会创建第二套 OKR 库。

`git commit` 只记录快照，不会改动线上文件。`git checkout`、`git restore`、`git reset --hard`、带 `--remote-okr-db` 的部署以及可能替换数据库文件的合并/切换，才可能把旧 Git 版本写回正在使用的线上目录；操作前确认目标路径不是共享库。开发 worktree 中未挂载的旧检出文件也不能直接拿来提交。审核 PR 时确认数据库快照和图片属于同一批产品改动。

这个规则也适用于只改 OKR 产品数据、没有改代码的提交。私有主库、登录 token 和运行日志不进入 Git。

## 验证

已加入任意前缀（根路径、`/dev/`、嵌套路径）的前端路径测试，
代理请求、Cookie 和重定向测试，以及凭证出口拒绝个人消息和参数注入的测试。
2026-09-14 已部署到 `https://emily.bytedance.net/dev/#/biz-okr?tab=okr-plan`：

- 全量 Go 测试、181 项前端测试、类型检查通过。
- 真实模型通过 Docker 读取完整源码与共享 OKR 库；开发 Task、Message 表均为 0 条。
- 容器不能读取生产主库、Docker socket，也不能直接连接生产 API。
- 浏览器显示飞书登录入口，无页面异常；全部开发 API 请求均携带 `/dev/` 前缀。
- 生产和开发健康检查均通过；真实用户完成授权后的登录尚未端到端实测。

## 代码、实例配置与 main 的归属

| 内容 | 真源与维护方式 | 是否进入 main |
|---|---|---|
| Docker、路径代理、配置加载、对话适配器、部署脚本和测试 | 当前仓库源代码；功能分支评审合入 | 是，作为可选公共能力 |
| 模块公共默认值 | `conf/okr-module.yaml`；Chat 和应用登录默认关闭，容器名、应用 ID、个人登录文件为空 | 是 |
| 生产路径前缀、socket、公开地址 | 本机 `conf/config.runtime.yaml` | 否，已忽略 |
| 当前 Chat 启用、容器名、Codex 登录文件、飞书应用绑定 | 本机 `conf/okr-module.runtime.yaml`，覆盖公共默认值后执行同一套严格校验 | 否，已忽略 |
| 开发实例端口、空主库、后台开关、token 目录 | 开发 worktree 的 `config.development.yaml` 与两个 `*.runtime.yaml`，由初始化脚本生成；SQLite 路径仍由专用基础配置负责 | 否，不再改写公共 YAML |
| 通讯录与通知 Bot profile | 初始化参数，生成到本机 systemd unit | 否；公共代码不内置当前 profile |
| secret、用户 token、缓存、运行日志 | 系统环境文件或 `var/` 等本机状态目录 | 否 |
| 架构、操作方法、维护约定 | 本文；模块文档只链接本文 | 是，随代码同一提交维护 |
| 当前 Emily 地址、实测结果 | 本文交付记录，明确是实例示例 | 可进入说明文档，不作为默认配置 |
| `data/okr/okr.db` 和产品图片 | 从运行中的共享目录取得同一份一致快照，带入目标分支 | 是，有数据改动就提交，并合入 main |

合入 main 时以完整开发环境能力为独立 PR，不直接合并整个 OKR MVP 分支的历史。
PR 纳入本表的公共能力、测试、文档，以及共享 OKR 产品数据库和资源；
排除个人凭证、私有运行数据库、实例配置和其他无关业务修改。
共享 OKR 数据仍按当前实例约定直接修改。现阶段代码提交在 OKR MVP 分支，尚未合入 main。

今后修改公开行为，同一提交更新本文和相应测试；更换本机路径、授权或容器绑定，
只修改运行时配置并重新部署。`--activate` 写实例覆盖文件，不再污染公共默认配置。

开发和线上运行时没有两套 OKR 库；Git 分支可以记录同一次共享数据快照，不能把独立检出的旧库当成另一份运行数据。
