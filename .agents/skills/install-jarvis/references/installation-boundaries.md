# 安装边界与决策表

## 语义所有权

| 事项 | 唯一所有者 | 安装 Agent 的动作 |
|---|---|---|
| 从 checkout 到最终可用的安装运行与清单 | `$install-jarvis` + `var/install/<run-id>/INSTALL_CHECKLIST.md` | 一开始创建，跨依赖、绑定、服务、世界模型和端到端阶段持续更新 |
| 本机依赖、版本、平台、服务与文件权限事实 | `jarvis-install doctor/validate-dependencies/validate` | 读取 JSON，选择处理方式；依赖门通过前不启动主服务 |
| lark-cli 与官方 Agent Skills 安装 | larksuite 官方 npm installer | 通过 `install-lark-cli` 调用并读回版本/Skills |
| traex stable 安装 | TRAE CLI updater 公布的 Code 内网 installer | 通过 `install-traex` 调用并完成 SSO |
| Jarvis 补丁版 CC Connect binary | `integrations/cc-connect/manifest.sh`、`patches/` 与 `scripts/install-cc-connect.sh` | 通过 `install-cc-connect` 构建验收；此动作不配置、不启动 |
| lark-cli Profile 与 Jarvis Bot 绑定 | `$install-jarvis` + `cc-connect-binding.md` | 同一 App/Profile 投影到 Jarvis runtime config 和 CC Connect `jarvis-codex`，再跑 `validate-binding` |
| Jarvis Bot WebSocket | CC Connect | 只启动补丁版 CC Connect；Jarvis 不再连接同一个 App |
| Qdrant 下载版本、校验和、launchd 安装 | `scripts/install-qdrant.sh` | 通过 `jarvis-install install-qdrant` 调用 |
| 主服务构建、签名、launchd 注册 | `scripts/install-launchd.sh` | 配置完成后通过 `install-server` 调用 |
| 已注册主服务的安全重建 | `scripts/rebuild-server.sh` | 确认属于当前 checkout 后调用 |
| 飞书 App/Profile 登录和本机 identity | `$install-jarvis` | 在服务启动前配置并读回 |
| 飞书初始化能力与权限缺口 | `$install-jarvis` + `feishu-capability-audit.md` | 对选定 Profile 做只读探针并分为核心、可选增强、条件能力和不使用；不发起权限申请 |
| 近 7 天业务证据与世界模型工作稿 | `$bootstrap-jarvis-world-model` | 服务就绪后转交同一个 install run；世界模型 Skill 只更新清单 E 区 |
| PrincipalProfile、项目、人物、重点事项、群监听 | M1/M2 现有接口 | 只由 `$bootstrap-jarvis-world-model` 编排 |
| 缺失依赖的具体安装方式 | 用户的 Agent | 按机器选择，不在脚本写死包管理器 |
| 使用哪个飞书 app/profile | 用户 | Agent 展示候选与证据，不猜 |
| 旧实例、旧数据库如何处理 | 用户 | 发现后停止，询问复用/迁移/替换 |

## 必须由机器保证的边界

- 当前内置 Qdrant 安装器只支持 Darwin arm64；其他平台 fail-fast。
- Go 版本不得低于 `go.mod`，Node 满足当前 Vite engines，CGO 和 C toolchain 可用。
- 配置引用的 runtime binary 必须存在；引用 `traex` 时必须已登录。
- `bin/cc-connect-jarvis` 必须报告固定 Jarvis patch commit；官方未打补丁 binary 不满足依赖门。
- `conf/config.runtime.yaml` 被 Git 忽略；服务安装前 base/runtime 配置都收紧到 `0600`。
- identity、模型和 embedding 机器配置不完整时不得注册主服务。
- `lark_cli.profile`、`card_approval.profile`、principal open_id、CC Connect `jarvis-codex` App 和 relay secret 必须来自同一个绑定；不一致时不得注册主服务。
- Qdrant 不健康时不得注册主服务。
- 飞书 App/Profile 绑定和首次启动主服务前必须先运行 `validate-dependencies`；它只验收依赖，不启动 Jarvis。
- `install-server` 必须在调用 `install-launchd.sh` 前再次通过依赖门，不能依赖 Agent 口头声明或之前的旧结果。
- 同一 launchd label 已被其他 checkout 占用时不得自动替换。
- 即使 program 属于当前 checkout，只要 CC Connect/Jarvis 已在运行，也属于已有实例事实；fresh-install Agent 先报告现状并让用户确认复用或重建，不能把“路径一致”当成重启授权。
- 系统验收以真实 `/healthz`、`/readyz` 和 Qdrant health 为准，不以进程存在或构建成功代替。
- 系统验收还要确认 launchd 实际运行当前 checkout 的补丁版 CC Connect；另一条同 App WebSocket 不得并存。

## 应保留给 Agent 的灵活度

- 依赖可能已由 Homebrew、npm、公司环境管理器或手工安装提供；只验证能力，不强制来源。
- 一个用户可能有多个 lark-cli app/profile；Agent 应读取现状、最小授权并在歧义时请用户选择。
- 安装初始化只审计当前飞书能力，不自动申请缺失 scope；核心能力缺失如实阻塞相关阶段，可选高级通讯录字段缺失则记录未知并继续。
- 选定 Profile 后，CC Connect 的 model、display、allow/admin 和群回复策略仍由 Agent 与用户按场景决定；App 身份和 relay 绑定不可自由漂移。
- 初始化证据不足时可继续调查或保留未知，不为填满字段编造内容。
- 项目、关键人物、重点事项和监听群数量没有固定下限；由证据决定，高影响歧义再交给用户。
- 系统已有健康 Qdrant 时可以复用，不要求重新安装。
- Qdrant 是可在依赖阶段启动的依赖服务；Jarvis 主服务在依赖门、identity 和 CC 绑定完成后启动，不等待世界模型工作稿。
- fresh clone 和已有 checkout 共享同一安装 Skill，但分别走 install-server 或 rebuild-server。
- 整体安装清单固定安装阶段和硬验收点，但每一阶段的调查路径、依赖安装方式、证据深度和实体数量由用户的 Agent 根据现状决定。

## 分发边界

仓库远端当前是私有 Code 仓库，不应宣称可被 GitHub-only 的 Skill installer 直接安装。正确入口是让有权限的使用者 clone 完整仓库，在根目录启动能读取 repo-local `.agents/skills/` 的 Agent，然后触发 `$install-jarvis`。若未来要开放 GitHub 分发，应另行补齐公开远端、访问策略和发布验收，而不是在本 Skill 中伪造 fallback。
