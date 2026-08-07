# 安装边界与决策表

## 语义所有权

| 事项 | 唯一所有者 | 安装 Agent 的动作 |
|---|---|---|
| 本机依赖、版本、平台、服务与文件权限事实 | `jarvis-install doctor/validate` | 读取 JSON，选择处理方式 |
| lark-cli 与官方 Agent Skills 安装 | larksuite 官方 npm installer | 通过 `install-lark-cli` 调用并读回版本/Skills |
| traex stable 安装 | TRAE CLI updater 公布的 Code 内网 installer | 通过 `install-traex` 调用并完成 SSO |
| Qdrant 下载版本、校验和、launchd 安装 | `scripts/install-qdrant.sh` | 通过 `jarvis-install install-qdrant` 调用 |
| 主服务构建、签名、launchd 注册 | `scripts/install-launchd.sh` | 配置完成后通过 `install-server` 调用 |
| 已注册主服务的安全重建 | `scripts/rebuild-server.sh` | 确认属于当前 checkout 后调用 |
| 飞书身份、近 7 天证据、初始化草案 | `$initialize-jarvis` | 转交，不在安装 Skill 复制 |
| PrincipalProfile、项目、人物、重点事项、群监听 | M1/M2 现有接口 | 只由 `$initialize-jarvis` 编排 |
| 缺失依赖的具体安装方式 | 用户的 Agent | 按机器选择，不在脚本写死包管理器 |
| 使用哪个飞书 app/profile | 用户 | Agent 展示候选与证据，不猜 |
| 旧实例、旧数据库如何处理 | 用户 | 发现后停止，询问复用/迁移/替换 |

## 必须由机器保证的边界

- 当前内置 Qdrant 安装器只支持 Darwin arm64；其他平台 fail-fast。
- Go 版本不得低于 `go.mod`，Node 满足当前 Vite engines，CGO 和 C toolchain 可用。
- 配置引用的 runtime binary 必须存在；引用 `traex` 时必须已登录。
- `conf/config.runtime.yaml` 被 Git 忽略；服务安装前 base/runtime 配置都收紧到 `0600`。
- identity、模型和 embedding 机器配置不完整时不得注册主服务。
- Qdrant 不健康时不得注册主服务。
- 同一 launchd label 已被其他 checkout 占用时不得自动替换。
- 系统验收以真实 `/healthz`、`/readyz` 和 Qdrant health 为准，不以进程存在或构建成功代替。

## 应保留给 Agent 的灵活度

- 依赖可能已由 Homebrew、npm、公司环境管理器或手工安装提供；只验证能力，不强制来源。
- 一个用户可能有多个 lark-cli app/profile；Agent 应读取现状、最小授权并在歧义时请用户选择。
- 初始化证据不足时可继续调查或保留未知，不为填满字段编造内容。
- 项目、关键人物、重点事项和监听群数量没有固定下限；由证据和用户审阅决定。
- 系统已有健康 Qdrant 时可以复用，不要求重新安装。
- fresh clone 和已有 checkout 共享同一初始化 Skill，但分别走 install-server 或 rebuild-server。

## 分发边界

仓库远端当前是私有 Code 仓库，不应宣称可被 GitHub-only 的 Skill installer 直接安装。正确入口是让有权限的使用者 clone 完整仓库，在根目录启动能读取 repo-local `.agents/skills/` 的 Agent，然后触发 `$install-jarvis`。若未来要开放 GitHub 分发，应另行补齐公开远端、访问策略和发布验收，而不是在本 Skill 中伪造 fallback。
