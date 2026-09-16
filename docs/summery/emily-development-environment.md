# Emily 开发实例

Emily development 运行与 OKR-MVP 完全相同的仓库代码。它不是另一套业务实现，
差异只存在于容器、本机运行配置和实例状态。

## 边界

- 主环境的整个 `data/okr/` 直接挂载到开发容器的 `/opt/jarvis/data/okr`。
  目录内现有及以后新增的所有内容均共享，不拆分子目录。
- 开发实例的 Jarvis 主库、Task、Message、普通 Chat、OKR Chat、附件、日志和
  CLI HOME 保存在开发 checkout 的 `var/`，不读取主环境对应目录。
- 开发实例使用自己的 `conf/config.runtime.yaml`、
  `conf/okr-module.runtime.yaml` 和 `var/container/development.env`。
- Lark CLI、ByteDance CLI 和 Codex 的当前登录状态保存在开发实例 HOME；程序文件
  可以从宿主只读挂载。用户明确选择时，可以一次性复制 Codex 授权，运行时不挂载
  主 HOME。
- 容器使用普通 Docker bridge 网络，不使用宿主 Docker socket、出口网关或网络白名单。

## 运行结构

容器把当前开发 checkout 挂到 `/opt/jarvis`，并用第二个 bind mount 将共享
`data/okr/` 覆盖到 `/opt/jarvis/data/okr`。开发 worktree 本身保持普通 Git 检出，
不建立 OKR 软链接，不设置 `skip-worktree`、自定义 exclude 或提交 hook。

开发实例读取仓库的 `conf/config.yaml`，再合并当前 worktree 中不入 Git 的
`conf/config.runtime.yaml`。相对数据库路径因此自然落在开发 checkout 的 `var/`，
不会访问主环境 Jarvis 主库。OKR 模块同理由 `conf/okr-module.runtime.yaml` 覆盖；
开发容器使用 `chat.runtime: local`，避免在容器内再启动 Docker。

主服务可通过本机 runtime 配置把 `/dev/` 代理到开发容器的 Unix socket。该代理只
提供 HTTP 入口，不共享 Jarvis 主库、进程或 HOME，开发启动脚本也不修改或重启主服务。

## 首次配置

在开发 checkout 运行：

```bash
./scripts/emily-dev --initialize \
  --public-url https://emily.bytedance.net/dev/ \
  --principal chujiejie.1
```

命令只生成该 worktree 的两个 runtime overlay。随后填写开发 Lark App 配置和
`var/container/development.env`，并在开发 HOME 中分别完成所需 CLI 登录。

## 启动与部署

```bash
./scripts/emily-dev --deploy \
  --okr-data /data00/home/chujiejie.1/workspace-local/jarvis-okr-mvp/data/okr \
  --env-file var/container/development.env
```

该命令构建并替换 `emily-development` 容器，然后在容器内执行
`./scripts/jarvis-deploy --skip-pull`。日常只需重复同一命令。它只操作开发容器；
主服务继续使用自己的标准部署流程。

检查实际挂载而不修改运行状态：

```bash
./scripts/emily-dev --dry-run \
  --okr-data /data00/home/chujiejie.1/workspace-local/jarvis-okr-mvp/data/okr \
  --env-file var/container/development.env
```

代码在两个分支间按正常 Git 流程同步。共享 OKR 数据由 OKR-MVP worktree 按仓库规则
提交快照；开发 worktree 不需要任何额外 Git 索引规则。
