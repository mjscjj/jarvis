# 安装边界与决策表

下表和命令针对完整 checkout 的源码安装。DMG 的安装与服务生命周期归应用内 onboarding / supervisor；不把源码安装的工具链、daemon 注册或清单验收当作桌面后台建模的前置条件。

## 语义所有权

| 事项 | 唯一所有者 | 安装 Agent 的动作 |
|---|---|---|
| 从 checkout 到最终可用的安装运行与清单 | `$install-jarvis` + `var/install/<run-id>/INSTALL_CHECKLIST.md` | 一开始创建，跨依赖、绑定、服务、世界模型和端到端阶段持续更新 |
| 本机依赖、版本、平台、服务与文件权限事实 | `jarvis-install doctor/validate-dependencies/validate` | 读取 JSON，选择处理方式；依赖门通过前不启动主服务 |
| lark-cli 与官方 Agent Skills 安装 | larksuite 官方 npm installer / `lark-cli update` | 首装使用 installer；低于项目最低版本或 Skills/协议不完整时用 `lark-cli update --json` 同步，再读回版本/Skills |
| CC Connect 固定调用的 Codex CLI | OpenAI 官方 `@openai/codex` npm 包 | 通过 `install-codex` 安装，完成 device auth，再由 `doctor` / `validate-dependencies` 验收；不能只检查 runtime 配置中的 Agent CLI |
| traex stable 安装 | TRAE CLI updater 公布的 Code 内网 installer | 通过 `install-traex` 调用并完成 SSO |
| Jarvis 补丁版 CC Connect binary | `integrations/cc-connect/manifest.sh`、`patches/` 与 `scripts/install-cc-connect.sh` | 通过 `install-cc-connect` 构建验收；此动作不配置、不启动 |
| lark-cli 默认 App 与 Jarvis Bot 绑定 | `$install-jarvis` + `cc-connect-binding.md` | 将 lark-cli 当前默认 App 绑定到 CC Connect `jarvis-codex`，再跑 `validate-binding` |
| Jarvis Bot WebSocket | CC Connect | 只启动补丁版 CC Connect；Jarvis 不再连接同一个 App |
| Qdrant 下载版本、校验和、服务安装 | `scripts/install-qdrant.sh` | 通过 `jarvis-install install-qdrant` 调用 |
| 主服务构建与注册 | `scripts/install-launchd.sh` / `scripts/install-systemd.sh` | 配置完成后通过 `install-server` 按平台调用 |
| 既有 checkout 的主服务安全重建或服务恢复 | `scripts/rebuild-server.sh` | 确认复用决定和 checkout 归属后调用；注册缺失时按平台恢复，不重新执行完整安装 |
| 飞书默认身份登录和本机 identity | `$install-jarvis` | 在服务启动前配置并读回 |
| 飞书初始化能力与权限缺口 | `$install-jarvis` + `feishu-capability-audit.md` | 统一使用 `scripts/jarvis-lark-auth begin/check` 申请和检查内置功能所需权限；已有登录也检查。只读审计发现缺口后回到安装授权步骤处理 |
| 近 7 天业务证据与世界模型工作稿 | `$bootstrap-jarvis-world-model` | 服务就绪后转交同一个 install run；世界模型 Skill 只更新清单 E 区 |
| PrincipalProfile、项目、人物、重点事项、群监听 | M1/M2 现有接口 | 只由 `$bootstrap-jarvis-world-model` 编排 |
| 未内置安装动作的缺失依赖 | 用户的 Agent | 按机器选择，不为未知工具在脚本中猜包管理器 |
| lark-cli 默认飞书 App 的初始化和登录 | 用户 | Agent 展示当前身份与证据，未配置时再引导初始化 |
| 旧实例、旧数据库如何处理 | 用户 | 发现后停止，询问复用/迁移/替换 |

## 必须由机器保证的边界

- 当前内置安装器只支持 Darwin arm64 与 Linux x86_64；其他平台 fail-fast。
- Go 版本不得低于 `go.mod`，Node 满足当前 Vite engines，CGO 和 C toolchain 可用。
- 配置引用的 runtime binary 必须存在；引用 `traex` 时必须已登录。
- `bin/cc-connect-jarvis` 必须报告固定 Jarvis patch commit；官方未打补丁 binary 不满足依赖门。
- lark-cli 必须支持绑定验收使用的 `card.action.trigger --dry-run` 协议，世界模型所需五个 Lark Skills 必须全部存在。
- `codex` 必须存在，因为 CC Connect `jarvis-codex` 固定使用它；仓库 runtime 选择的 Agent CLI 是另一项独立依赖。
- `conf/config.runtime.yaml` 被 Git 忽略；服务安装前 base/runtime 配置都收紧到 `0600`。
- identity、模型和 embedding 机器配置不完整时不得注册主服务。
- principal open_id、lark-cli 当前默认 App、CC Connect `jarvis-codex` App 和 relay secret 必须形成可用绑定；不完整时不得注册主服务。
- Qdrant 不健康时不得注册主服务。
- 飞书默认 App 绑定和首次启动主服务前必须先运行 `validate-dependencies`；它只验收依赖，不启动 Jarvis。
- `install-server` 必须在调用平台服务安装脚本前再次通过依赖门，不能依赖 Agent 口头声明或之前的旧结果。
- `rebuild-server.sh` 只服务已经确认复用的 checkout；label 缺失时直接恢复当前主服务，不把 CC Connect 构建、飞书能力审计或世界模型初始化变成恢复前置条件。
- 同一服务 label 已被其他 checkout 占用时不得自动替换。
- 即使 program 属于当前 checkout，只要 CC Connect/Jarvis 已在运行，也属于已有实例事实；fresh-install Agent 先报告现状并让用户确认复用或重建，不能把“路径一致”当成重启授权。
- 系统验收以真实 `/healthz`、`/readyz` 和 Qdrant health 为准，不以进程存在或构建成功代替。
- 系统验收还要确认服务管理器实际运行当前 checkout 的补丁版 CC Connect；另一条同 App WebSocket 不得并存。
- 本机只能机器校验当前 service 和端口，不能证明其他机器没有消费同一 App；外部唯一性必须保留为人工确认。

## 应保留给 Agent 的灵活度

- 依赖可能已由 Homebrew、npm、公司环境管理器或手工安装提供；只验证能力，不强制来源。
- lark-cli 可以存在多个 profile，但 Jarvis 只消费当前默认身份，不在项目内建立第二套 profile 选择。
- 首次安装统一申请内置功能所需权限，源码与桌面复用 `scripts/jarvis-lark-auth`；已有 token 有效也不能跳过权限检查。只读审计发现 OAuth 缺口后由安装流程补齐；应用侧或资源侧无权保留具体阻塞，不反复发起无效 OAuth。可选高级通讯录字段缺失记录未知并继续。
- CC Connect 的 model、display、admin 和群回复策略仍由 Agent 与用户按场景决定；`allow_from` 必须只允许 Principal 本人，App 身份和 relay 绑定不可自由漂移。
- 初始化证据不足时可继续调查或保留未知，不为填满字段编造内容。
- 项目、关键人物、重点事项和监听群数量没有固定下限；由证据决定，高影响歧义再交给用户。
- 系统已有健康 Qdrant 时可以复用，不要求重新安装。
- Qdrant 是可在依赖阶段启动的依赖服务；Jarvis 主服务在依赖门、identity 和 CC 绑定完成后启动，不等待世界模型工作稿。
- fresh clone 和已有 checkout 共享同一安装 Skill，但分别走 install-server 或 rebuild-server。
- 整体安装清单固定安装阶段和硬验收点，但每一阶段的调查路径、依赖安装方式、证据深度和实体数量由用户的 Agent 根据现状决定。

## 分发边界

源码安装的仓库远端当前是私有 Code 仓库，不应宣称可被 GitHub-only 的 Skill installer 直接安装。有权限的使用者 clone 完整仓库后，在根目录触发 `$install-jarvis`。DMG 分发则使用已有应用内安装入口，不要求用户取得源码；打包与签名要求以 `packaging/macos/README.md` 为准。若未来要开放 GitHub 源码分发，再补齐公开远端、访问策略和发布验收。
