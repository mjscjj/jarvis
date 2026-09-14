# OKR 模块当前实现

> Status: current
> Authority: normative
> Last verified: 2026-09-14, current working tree

本文只维护 OKR 模块当前已经落地的边界和常用修改入口。长期拆分原则、双 Progress 的设计理由及后续阶段见 [通用 OKR 插件与 Biz OKR 拆分设计](../design-okr-plugin-and-biz-okr.md)。字段、路由和运行配置仍以代码与配置文件为最终真源。

## 当前结论

OKR 当前是两个内置 App Module，而不是 `internal/plugin` 采集插件：

```text
okr（通用 OKR）
└── biz-okr（Biz 业务包装，requires: okr）

Jarvis 世界模型
└── 通过原子工具、Skill、EntityRelation 和 WorldProgress 与 OKR 协作
```

- `okr` 拥有 Objective、KR、Metric、Point、结构化负责人、周次、Weekly KR Core 和正式 Progress。
- `biz-okr` 拥有标签、Biz OKR Plan、Plan/进度评审、周报业务展示、评论、评分、Follow-up、催填、Meego、飞书页面身份和业务 Agent 编排。
- 当前完整业务页面显示为 `Biz OKR`，内部模块 key 仍为 `biz-okr`。启用通用 `okr` 后，左侧“插件”下出现独立 `OKR 插件` 页面，展示通用结构、正式进展与 Jarvis 世界进展对照、跨世界关系；关系列表同时读取以 O/KR/Point 为 source 和 target 的边，并只展示当前季度节点。它不承载 Biz 标签、Plan、Review、评论或评分。
- 两个模块继续复用 `internal/okrworkspace/`、`data/okr/okr.db` 和既有 `okr_workspace_*` 表。本次拆分没有搬库、改表名或复制历史数据。
- `internal/plugin` 仍只负责 Codebase、Meego、Oncall 等外部线索采集插件；OKR 不进入这套采集器运行时。

模块注册和依赖的代码真源是 `internal/appmodule/module.go`，仓库默认开关是 `conf/modules.yaml`。插件管理页用一张目录同时展示通用 `OKR 插件` 与 Codebase、Meego、Oncall；启用后都在左侧“插件”下出现自己的页面。底层仍按能力区分：OKR 复用 `appmodule` 的业务生命周期，后三者使用 `internal/plugin` 的授权、调度和 Clue 采集运行时。`biz-okr` 是依赖 OKR 插件的独立业务应用，用户可见名称为 `Biz OKR`，在左侧有自己的入口，并在系统设置中管理启停。`biz-okr=on, okr=off` 是非法组合，会因依赖缺失而失败；关闭模块不会删除数据。旧配置键 `agency-okr` 会在启动时原子迁移为 `biz-okr`。

## OKR 独立对话

当前 Emily 实例启用了[完整源码研发模式](../summery/emily-development-environment.md)：`chat.development_container` 指向常驻开发容器，容器可修改整个系统代码，直接读写同一份线上 `data/okr/`，Task、Message 等使用独立开发主库。入口路径由实例配置生成，不增加第二套业务路由。开发页面的对话框调用主站已有的 `/api/okr-chat/*`，复用同一会话库与飞书登录状态；模型执行仍进入研发容器。

Biz OKR 页面底部对话按用户身份选择：既有白名单用户保留普通 Chat，其余已完成 Biz OKR 飞书登录的访客使用 `/api/okr-chat/*`。所有访客仍共用一份 Chat 服务和 `var/okr-chat/chat.db`，但新会话绑定已验证的飞书 `union_id`；列表、历史、草稿、附件、续聊、取消和删除都按归属校验。旧共享会话没有可验证的创建人，保留在库中但不自动分给任一用户，也不出现在个人列表。OKR 产品数据库 `data/okr/` 仍共享；普通 `/api/chat/*` 和 M2/M3/M5 保持原有行为。

OKR 飞书登录会话已持久化在 Jarvis 私有运行主库的 `okr_workspace_auth_session` 表，浏览器通过 HttpOnly 的 `jarvis_okr_session` Cookie 恢复身份；服务重启不需要再次授权。`conf/okr-module.yaml` 的 `identity.session_ttl_hours` 设为 `8760`（365 天），同时决定新会话与 Cookie 的有效期。已签发的旧会话保留原到期时间，下一次正常登录使用一年有效期；主动退出立即删除对应会话。此设置只延长网页登录，不改变飞书 API access/refresh token 自身的期限。

向普通 Biz OKR 访客开放域名时，部署本机的 `auth.enabled` 必须开启，并在 `auth.principals` 中只列允许进入普通 Jarvis 的用户；Biz OKR 和 `/api/okr-chat/*` 仍使用飞书登录。白名单用户若只有飞书会话，页面会提示再完成字节身份登录后使用普通 Chat。Docker 工具隔离限制的是对话 Agent 的数据出口，不能代替浏览器 API 的外层登录。

普通 Jarvis 的网页登录确认浏览器访客自己的字节身份，不复用宿主机的 bytedcli 登录。进入普通页面会自动开始验证；网络中断自动重试并保留当前授权，服务重启丢失流程或链接过期时自动生成并展示新链接，无需手动刷新或重新生成。用户仍需在 SSO 授权页确认身份，明确取消授权或不在白名单时停止重试。OKR 访客页面不会自动发起这层登录。

“正在验证字节身份”长时间不结束时，先按 [网页登录授权域名与超时排查](../summery/sso-web-login.md#当前-cli-授权域名与超时排查2026-09-14) 核对实际 API 地址。2026-09-14 已实测 CN 上游握手频繁超时、i18n BD 地址创建授权约 0.8 秒；具体数据、`auth.login_api_base_url` 部署配置与验证范围统一记录在该文档，不把延长等待当作域名问题的修复。

`internal/okrchat/` 拥有容器执行和共享 OKR 聊天库装配；`internal/chat/` 复用会话、附件及流式协议。配置了 `chat.development_container` 时，对话进入常驻开发容器，完整源码可写，线上 `data/okr/` 可读写，开发实例拥有自己的 API 和空主库。没有配置该字段时，使用原始单轮容器：挂载 OKR 附件、会话状态和可写前端目录，通过受限工具出口调用 OKR 接口。两种模式均不挂载生产主库、普通 Chat、私人资料或 Docker socket，也没有直接网络。

网页会话 ID 和历史以共享 Chat 库为准，Codex thread ID 只是可替换的底层状态。升级或更换授权后若 Codex 明确返回旧 thread 不存在，服务记录实际 stderr、清除旧 ID，并在同一轮用已保存的可见历史自动新建底层 thread；成功后写回新 ID，不要求用户新开网页会话。只重试一次；授权、网络等其他错误保留原始原因，待环境恢复后下一轮仍可继续。

完整研发模式修改的是开发 worktree；容器内用 `./scripts/jarvis-deploy --skip-pull` 构建、重启开发实例，生产前后端仍走宿主正常部署。开发目录不是线上源码副本的自动发布机制；OKR 产品数据是例外，它直接共享并立即影响线上。源码合并、产品数据快照及配置归属见[研发环境文档](../summery/emily-development-environment.md)。

原始单轮容器的 `internal/toolcatalog/okr_chat.go` 以 method/path 清单限制工具请求。完整研发模式在开发容器内使用自己的完整 API，通过文件和进程边界隔离生产 Task、Todo、消息与普通会话；通知机器人和查人能力通过限定出口复用。已授权用户自己上传或写进 OKR 的消息摘录仍是可读材料，不做语义脱敏。

`conf/prompts/okr-chat-system-prompt.md` 定义业务职责与停止边界，经 textstore 注册；不叠加普通 Chat 提示词或共享记忆。`conf/okr-module.yaml` 提供公共默认值；当前实例的 Chat 启用、容器名、模型登录文件以及飞书应用绑定在不入 Git 的 `conf/okr-module.runtime.yaml`。模型只使用所需登录文件，不复制宿主全量配置。

生产主服务部署统一执行 `./scripts/jarvis-deploy --skip-pull`。完整研发模式的首次配置与启动使用 `scripts/emily-dev --activate`；该命令同时构建开发容器、配置路径并按统一脚本部署两个实例。原始单轮模式启用时才构建 `deploy/okr-chat/` 镜像。关闭 OKR Chat 不删除已有独立会话。

验证：`go test ./cmd/... ./internal/...`；原始单轮容器使用 `OKR_DOCKER_TEST=1 go test ./internal/okrchat -run TestDockerNetworkAndFilesystem -v`。在生产 worktree 根目录，完整研发模式使用 `EMILY_DEVELOPMENT_CHAT_ROOT=$PWD/var/okr-chat go test ./internal/okrchat -run TestDevelopmentModelCanInspectFullSource -v`。前端 `web/test/okrChat.browser.mjs` 验证独立 API、历史弹窗和附件地址。

## 数据所有权

| 语义 | 当前所有者 | 当前存储 |
|---|---|---|
| O、KR、Metric、Point、Owner | `okr` | 既有 OKR 领域表 |
| 周次、周期指标、人工正式进展 | `okr` | `okr_workspace_week`、`okr_workspace_weekly_kr_core`、`okr_workspace_progress` |
| 标签、Plan、评论、评分、Follow-up | `biz-okr` | 既有 Biz 业务表 |
| Meego 快照、催填批次 | `biz-okr` | 既有 Meego / reminder 表 |
| Biz 页面登录和用户授权 | `biz-okr` | Jarvis 运行库 session + 本地 token 文件 |
| OKR 与项目、关键事项等跨模块强关系 | Jarvis 世界模型 | `EntityRelation` |
| Jarvis 基于现实证据形成的独立进展判断 | Jarvis 世界模型 | `WorldProgress` |

正式 OKR Progress 与 `WorldProgress` 必须并存：前者是人在 OKR 中维护、组织认可的正式口径；后者是 Jarvis 根据消息、Meego、Fact、Task 等证据形成的现实判断。当前没有 Go 自动双写，也不能用 `WorldProgress` 自动覆盖人工正式进展。

## 当前读写边界

### 通用 OKR

- API 前缀：`/api/okr/*`。实际注册见 `internal/api/okr_module_routes.go`。
- 原子工具：`scripts/okr-module-tools`。
- 已支持 Objective/KR 维护、Metric/Point/Owner 完整拆解、周次、Weekly KR Core、正式 Progress CRUD、图片上传和乐观版本控制。`插件 → OKR` 以只读方式展示结构和分层进展；人工填写仍在 OKR 业务页面。
- 通用侧可以开周和逐条改进展，但没有删整周的入口。整周删除只归 Biz：仅软删除周记录，保留所有关联数据；列表和周页面不再展示已删除周，同一 `(quarter, week)` 不允许重新新建。
- `PUT /api/okr/krs/:kr_id` 与 `okr-module-tools replace-kr` 只接受通用拆解数据，不能夹带 Biz 标签、Meego 或周进展。
- 只启用 `okr` 时，不校验或初始化 Biz SSO、飞书 Secret 和 OKR AI Review。

### Biz OKR

- API 前缀：`/api/biz-okr/*`。
- 原子工具：`scripts/biz-okr-tools`。
- 页面入口：`web/src/modules/registry.tsx` 注册的 `Biz OKR`（内部 key 为 `biz-okr`），复用当前 `web/src/okr/` 页面实现。
- Biz 组合视图读取通用 OKR 和正式 Progress，再叠加标签、评分、评论与 Meego 信息；它不是第二份 OKR 真源。评论复用同一张讨论表，但生命周期明确分为 `(quarter, week)` 周页面和 `plan_id` Plan 页面两种作用域；Plan 评论不会借用或污染任一周次。评论中的 `@` 同时保存可见原文和经固定人员目录核验的完整企业邮箱。只有创建评论时的显式 `@` 会触发通知；页面和 Owner 不再隐式扩大收件人，编辑只更新评论与 mention 数据。通知边界直接使用已核验完整企业邮箱，通过 `feishu.app_id / cli_profile` 固定的通知机器人发送并回读 Card 2.0。通知意图与评论同事务保存，回执单独持久化；失败不回滚评论，已成功的不重发，有消息 ID 的未知结果只回读核验。原始 CLI 错误保留服务日志，页面显示简洁状态。
- Review 的结构化待跟进事项支持 `not_started`、`in_progress`、`done`、`abandoned` 四种状态；Review 会议页只开放状态编辑，其余字段保持只读。
- 正式 Progress 的写入仍调用 `/api/okr/*`，写完再回读 Biz 组合视图，防止页面本地状态丢失 Biz 字段。
- 多人填写使用细粒度乐观并发：已有实体按 `version` 做 CAS，首次周核心数据与首次评分从版本 1 开始；页面以服务端最新结果为基线重放保存期间的新草稿，评分和 Meego 确认只合并自己负责的字段。Objective/KR 排序使用范围顺序快照，评论编辑使用版本；级联删除以及从 KR 中移除 Metric/Point 都在同一写入临界区校验覆盖子项/回复的删除快照。冲突返回 409 并保留可继续处理的本地输入，不允许旧页面静默覆盖或删除协作者刚保存的数据。
- `OKR Agent` 页面按“通知与跟进 / 材料生成 / Prompt”组织：四个可调度行动在各自任务详情中维护执行时间和绑定 Prompt；第三个 Tab 集中编辑没有绑定定时行动的 Markdown Prompt。
- OKR AI Review 是只读同步能力。Plan 页面使用 `okr-agent-plan-review.md` 评审执行前的目标、成功标准、取舍和路径；Review 周使用 `okr-agent-progress-review.md` 评审结果、数据、归因、风险和下一步。两份文件是各自唯一语义真源。

旧 `/api/weekly-report/*` 和 `scripts/weekly-report-tools` 不再保留。`weekly-report` 仍可能出现在前端页面 surface、分享 URL、Skill 名，以及 ScheduledTask 旧绑定迁移中；这些不再表示模块键。

## Agent 与世界模型

模块能力通过原子工具和 Skill 暴露，不在 Jarvis 核心流水线增加 OKR 专用分支：

| Skill | 门禁模块 | 当前职责 |
|---|---|---|
| `okr-world-projector` | `okr` | 读取稳定 OKR 结构，建立有证据的跨模块关系；不读取周进展 |
| `okr-agent-orchestrator` | `biz-okr` | 根据可编辑业务 Prompt 编排标签、Plan、Review 和报告动作 |
| `weekly-report-progress-sync` | `biz-okr` | 调查 Meego/飞书证据并维护世界模型，不自动提交正式进展 |
| `weekly-report-reminder` | `biz-okr` | 生成催填快照并提醒真实负责人 |

周报催填的收件人判断和个性化文案仍由 Biz OKR Prompt/Skill 所有；实际外发统一调用无模块门禁的 `feishu-broadcast` Skill，由“Jarvis通知机器人”直接私聊负责人并返回逐人送达回执，不再创建或维护 OKR 催填助手群。

Objective→KR、KR→Metric/Point 和 Owner 等 OKR 内部关系由 OKR 原生结构派生，不复制进 `EntityRelation`。OKR 全景把当前 Principal 直接连到季度顶层 Objective，并按 可选的已核验 `union_id` 将 KR/Point Owner 连到已经存在的 Principal/Person；不会为未建模的 Owner 创建人物。只有 OKR 到 Project、KeyMatter、Resource 等跨模块、有证据的强关系才进入通用关系存储。

## 配置与启动

- 模块开关：`conf/modules.yaml`。
- OKR 数据库、图片、Biz 身份和 AI Review 的公共默认值：`conf/okr-module.yaml`；当前实例的应用身份、模型登录文件和开发容器绑定：不入 Git 的 `conf/okr-module.runtime.yaml`。
- 启动装配、迁移和旧 ScheduledTask 绑定迁移：`cmd/jarvis-server/main.go`。
- 历史 `agency-okr` 配置键和 ScheduledTask 模块绑定会在启动时幂等迁移为 `biz-okr`。
- 通用迁移集合：`internal/okrworkspace/domain/models.go` 的 `CoreModels()`。
- Biz 迁移集合：同文件的 `BizModels()`。
- 完整 UI 需要两个模块都开启；只启用 `okr` 时提供 API、数据和 Agent 能力。

配置修改后按仓库统一方式重启或部署，不自行拼接构建命令。关闭模块只停止路由、页面、Skill 和后台能力，不清理表或历史记录。

## 当前已知边界与后续工作

以下内容尚未完成，不应描述成当前已有能力：

1. `internal/okrworkspace.Service` 和 `web/src/okr/` 仍是共享实现目录，当前解耦发生在迁移集合、API、DTO、工具、Skill 和模块门禁层；尚未物理拆成 `internal/okr`、`internal/bizokr` 和两套前端目录。
2. Agent 尚无完整的“现实证据 + WorldProgress → 正式 Progress 候选 → 人确认 → 回填”产品闭环；当前仅具备所需的独立进展存储和正式 Progress 原子写工具。
3. 统一的后端 World Graph 聚合层尚未落地；OKR 插件页当前直接聚合 OKR API、WorldProgress 和双向 EntityRelation，主世界地图的 OKR Lens 也由前端组合完整 Objective/KR/Point 骨架与稀疏现实关系。
4. `KRPoint` 中仍保留 Meego 字段以兼容现有数据，API 已隔离其所有权，但字段尚未迁出通用模型。

这些后续项的设计依据统一维护在拆分设计文档，不在本文展开实施计划。

## 常见修改入口

| 要修改的内容 | 入口 |
|---|---|
| 模块名称、依赖、启停规则 | `internal/appmodule/`, `conf/modules.yaml` |
| 通用/Biz 数据归属与迁移 | `internal/okrworkspace/domain/models.go`, `internal/okrworkspace/migrate.go` |
| 通用/Biz 读取与写入边界 | `internal/okrworkspace/service.go` 及同目录领域文件 |
| API 所有权 | `internal/api/okr_module_routes.go`, `internal/api/okr_workspace.go` |
| Biz 登录身份 | `internal/okrworkspace/auth/`, `internal/chat/feishuidentity.go` |
| 通用/Biz Agent 工具 | `scripts/okr-module-tools`, `scripts/biz-okr-tools`, `internal/toolcatalog/` |
| Agent 语义流程 | `.agents/skills/okr-*`, `.agents/skills/weekly-report-*`, 对应业务 Prompt |
| Biz 页面与 API client | `web/src/okr/`, `web/src/modules/registry.tsx` |
| HTTP 路由速查 | `docs/reference/http-api.md`；最终以注册代码为准 |

修改时先判断语义属于通用 OKR、Biz 业务包装还是 Jarvis 世界模型，再改对应所有者；不要因为当前共用一个 Service 或目录，就把三类语义重新混回去。

### 稳定飞书人员身份（2026-09-14）

业务 Owner、Follow-up 与评论 mentions 统一使用 `email,name,union_id?`，邮箱必须完整；邮箱前缀只用于搜索。`open_id` 只在飞书接口内部及作者认证留痕中使用。`owner_key/person_id` 从邮箱派生，姓名不改变身份。旧客户端提交 `open_id` 会被拒绝，需刷新。

通知与登录共用 `feishu.app_id`；每条通知显式选择 `feishu.cli_profile`，启动校验实际 App ID。目录默认使用同应用 Bot，需通讯录基本资料、邮箱权限及相应数据范围。权限未开通时，仅允许显式配置 `directory_identity: user`、`directory_app_id`、`directory_profile` 的过渡目录；不继承 CLI 默认身份。头像按同目录的精确邮箱匹配，缺图显示姓名占位。

`POST /api/biz-okr/comments/:comment_id/notifications/retry` 只处理既有收件意图；已投递不再发，未知但有回执只核验，未知且无回执不盲目重试。编辑评论不产生新通知；历史评论迁移不补发。

历史数据必须在停写后经 `scripts/okr-email-migrate` 的 `resolve → dry-run → apply --backup` 显式迁移。脚本保留原始备份，核验所有映射，事务更新所有业务引用，拒绝冲突；触发器阻止旧进程重新写入裸 ID。查不到的历史 Owner 可在证据文件中明确声明为 `display_only_owners`，保留姓名、邮箱为空且不可通知；评论/跟进事项的未解析收件人仍中止迁移。
