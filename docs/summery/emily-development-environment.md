# Emily 完整研发环境

## 范围

- 完整源码使用独立 worktree，由常驻 Docker 容器执行、构建和运行前后端。
- 直接共享线上 OKR 数据库、图片和业务资源，允许修改数据和表结构。
- 任务、消息、普通会话等使用开发实例自己的 `var/development.db`，不复制生产主库、日志或私人资料。
- 网页沿用现有登录实现，开发会话单独保存。通知机器人和通讯录查询经宿主凭证出口复用；不向容器提供 principal 的个人消息/私有文档授权。
- 容器没有直接网络和 Docker 控制 socket；模型、登录及依赖下载走明确的 HTTPS 目标出口。容器的 `jarvis-tools` 访问自己的完整 API。
- 生产主服务代码更新仍走现有部署流程；开发实例构建不会更新生产前后端。

## 路径是部署配置，不是第二套业务路由

用户已明确接受同域开发页面可能使用生产浏览器登录态，不要求子域名隔离。
路径只用于区分实例，不声明它是浏览器安全边界。

生产入口配置（仅启用开发入口的实例填写）：

```yaml
server:
  development_path: /dev/
  development_socket: /tmp/emily-development-1001/ingress/web.sock
```

开发实例配置：

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

`/dev/` 可以替换成 `/sandbox/emily/` 等规范路径，改配置并重新构建即可，不需要改业务代码。
不能只修改代理路径而保留旧的前端构建产物。

## 初始化与部署

主机使用 `/usr/bin/python3`（需要 PyYAML）、Docker、Go、npm、已配置的 lark-cli：

```bash
./scripts/emily-dev --path /dev/ --secret-file /path/to/existing-okr-app.env --activate
```

初始化脚本从现有公开域名和 `--path` 生成开发实例的 `public_base_url`，
`--activate` 同步生成生产代理配置并调用标准部署脚本启动开发与生产服务。
更换前缀时仍使用这一条命令，不需要分别手改两处配置。主机重启后可重跑该命令恢复临时 socket。
不修改公司的域名网关。源码位于旁边的 `emily-development` worktree，分支 `codex/emily-development`。
入口通过生产服务中的可选代理连接容器 Unix socket；生产网关原有根路径转发即可覆盖它。

`conf/okr-module.yaml` 的 `chat.development_container: emily-development` 将生产 OKR 对话的执行环境切到该容器，
对话列表仍保存在现有共享的 `var/okr-chat/chat.db`。新会话使用完整研发工具说明；旧会话不会被自动改写。

容器内重新构建使用 `./scripts/jarvis-deploy --skip-pull`。没有 systemd 或宿主部署能力，
它只管理开发实例 PID。Git worktree 的正式提交与合并由宿主开发流程完成。

## 凭证与数据细节

- 整个 `data/okr/` 共享，以保持 SQLite 和资源路径一致；其中 `feishu-tokens/` 用开发 token 目录覆盖，既有网页登录用户的 token 不进入容器。
- 模型只挂载登录文件，不挂宿主 Codex 配置、MCP、普通会话或记忆。
- 开发环境保留通知应用的网页登录 secret，宿主 lark-cli 出口仅接受身份检查、通讯录查询以及固定通知机器人操作，不开放个人消息、任意命令或本地文件参数。
- `capture.enabled` 省略时保持现有行为；显式 `false` 时不注册自动发现/采集作业。该开关本身不是数据安全边界。
- 开发实例不启动生产 CC Connect 长连接；本地回调使用新生成的 relay secret。

## 验证

已加入任意前缀（根路径、`/dev/`、嵌套路径）的前端路径测试，
代理请求、Cookie 和重定向测试，以及凭证出口拒绝个人消息和参数注入的测试。
2026-09-14 已部署到 `https://emily.bytedance.net/dev/#/biz-okr?tab=okr-plan`：

- 全量 Go 测试、181 项前端测试、类型检查通过。
- 真实模型通过 Docker 读取完整源码与共享 OKR 库；开发 Task、Message 表均为 0 条。
- 容器不能读取生产主库、Docker socket，也不能直接连接生产 API。
- 浏览器显示飞书登录入口，无页面异常；全部开发 API 请求均携带 `/dev/` 前缀。
- 生产和开发健康检查均通过；真实用户完成授权后的登录尚未端到端实测。
