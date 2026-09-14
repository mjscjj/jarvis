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

Biz OKR 页面底部对话按用户身份选择：既有白名单用户保留普通 Chat，其余已完成 Biz OKR 飞书登录的访客使用受限 `/api/okr-chat/*`。该接口在服务端验证飞书会话，所有访客共用一份 Chat 服务和 `var/okr-chat/chat.db`，共享会话列表、历史、草稿与附件；不按用户分库或建立运行实例。普通 `/api/chat/*` 和 M2/M3/M5 保持原有可信运行方式。

向普通 Biz OKR 访客开放域名时，部署本机的 `auth.enabled` 必须开启，并在 `auth.principals` 中只列允许进入普通 Jarvis 的用户；Biz OKR 和 `/api/okr-chat/*` 仍使用飞书登录。白名单用户若只有飞书会话，页面会提示再完成字节身份登录后使用普通 Chat。Docker 工具隔离限制的是对话 Agent 的数据出口，不能代替浏览器 API 的外层登录。

普通 Jarvis 的网页登录确认浏览器访客自己的字节身份，不复用宿主机的 bytedcli 登录。进入普通页面会自动开始验证；网络中断自动重试并保留当前授权，服务重启丢失流程或链接过期时自动生成并展示新链接，无需手动刷新或重新生成。用户仍需在 SSO 授权页确认身份，明确取消授权或不在白名单时停止重试。OKR 访客页面不会自动发起这层登录。

“正在验证字节身份”长时间不结束时，先按 [网页登录授权域名与超时排查](../summery/sso-web-login.md#当前-cli-授权域名与超时排查2026-09-14) 核对实际 API 地址。2026-09-14 已实测 CN 上游握手频繁超时、i18n BD 地址创建授权约 0.8 秒；具体数据、环境变量及尚未完成的线上切换统一记录在该文档，不把延长等待当作域名问题的修复。

`internal/okrchat/` 拥有容器执行、共享 OKR 聊天库装配和一份专用 Unix socket 出口；`internal/chat/` 复用会话、附件及流式协议。每轮 `docker run --network none`，只挂载共享 OKR 附件和当前会话的 native/work 状态、项目完整 scripts/Skills（只读）、现有模型登录文件（只读）。不挂主库、普通 Chat、完整用户目录、宿主 MCP 配置或 Docker socket。脚本可以自由执行，但不能直接联网。

`internal/toolcatalog/okr_chat.go` 是受限 method/path 和工具说明的共同真源，默认拒绝未列出的请求。出口只将这些 OKR CRUD 请求转给固定主服务地址；模型通过精确域名 `:443` CONNECT 隧道维持原登录和 TLS，拒绝私网/回环目标。宿主 Agent 评审、Task、Todo、消息、普通会话、发通知、身份令牌及通用 HTTP 转发不开放。已授权用户自己上传或写进 OKR 的消息摘录仍是可读材料，不做语义脱敏。

`conf/prompts/okr-chat-system-prompt.md` 定义业务职责与停止边界，经 textstore 注册；不叠加普通 Chat 提示词或共享记忆。`conf/okr-module.yaml` 的 `chat` 控制启动，MVP 固定使用 Codex 0.154.0 + 标准 ChatGPT 登录，未支持自定义 provider、TRAE/Cursor 或直接访问飞书/Meego；Meego 已存快照可读。只复制 auth.json，不复制宿主 config.toml。认证文件刷新保存在 OKR 自己的 native 状态，宿主文件不被容器改写。

部署统一执行 `./scripts/jarvis-deploy --skip-pull`，开启 chat 时会先构建 `deploy/okr-chat/` 镜像。需要可用的 Docker daemon 和宿主已有 Codex 登录。停止/超时回收当轮容器；重启只清理带本实例标记的遗留容器，不碰其它 Docker 工作负载。关闭 chat 不会回退到普通 Agent，也不删除已有独立会话。

验证：`go test ./cmd/... ./internal/...`；`OKR_DOCKER_TEST=1 go test ./internal/okrchat -run TestDockerNetworkAndFilesystem -v`。设置 `OKR_CHAT_AUTH_FILE` 后 `TestDockerModelAndResume` 会进行真实模型调用，验证工具查询、续聊、持久化和取消回收。前端 `web/test/okrChat.browser.mjs` 验证独立 API、历史弹窗和附件地址。

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
- Biz 组合视图读取通用 OKR 和正式 Progress，再叠加标签、评分、评论与 Meego 信息；它不是第二份 OKR 真源。评论复用同一张讨论表，但生命周期明确分为 `(quarter, week)` 周页面和 `plan_id` Plan 页面两种作用域；Plan 评论不会借用或污染任一周次。评论中的 `@` 同时保存可见原文和经人员选择器解析的主应用 `open_id`。只有创建评论时的显式 `@` 会触发通知；页面和 Owner 不再隐式扩大收件人，编辑只更新评论与 mention 数据。通知边界先用 principal 的只读人员查询将该 `open_id` 精确归一为企业邮箱，再固定由“Jarvis通知机器人”发送紧凑 Card 2.0：主体只展示“原文”和“评论”，页面、周期/Plan、O、KR 与具体 KR 收进默认折叠的上下文，并提供 Emily“查看并回复”深链；发送后回读消息确认。不使用默认 Jarvis Bot，也不在身份解析失败时按姓名猜测。投递失败作为本次响应告警返回，不回滚评论。
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

Objective→KR、KR→Metric/Point 和 Owner 等 OKR 内部关系由 OKR 原生结构派生，不复制进 `EntityRelation`。OKR 全景把当前 Principal 直接连到季度顶层 Objective，并按 `open_id` 将 KR/Point Owner 连到已经存在的 Principal/Person；不会为未建模的 Owner 创建人物。只有 OKR 到 Project、KeyMatter、Resource 等跨模块、有证据的强关系才进入通用关系存储。

## 配置与启动

- 模块开关：`conf/modules.yaml`。
- OKR 数据库、图片、Biz 身份和 AI Review 运行配置：`conf/okr-module.yaml`。
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
