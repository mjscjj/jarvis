# HTTP API 分组

> Status: current
> Authority: reference; `internal/api/router.go` is source of truth
> Last verified: 2026-09-06 @ uncommitted worktree

本文只按能力分组，不复制 handler 的完整请求/响应结构。新增或删除接口时先改 `internal/api/router.go`，再更新本页。

## 项目分享、复制与仓库

- `GET /api/projects/:project_id/export`：导出 `schema_version=1` 项目包。
- `POST /api/projects/import/preview`：校验项目包，并检查代号和资源容量冲突。
- `POST /api/projects/import`：从通过预检的项目包创建项目。
- `POST /api/projects/:project_id/duplicate`：以新名称和代号复制项目、长期摘要及关联资源。
- `POST /api/projects/:project_id/repositories/resolve`：扫描 `execute.repo_root`，按规范化 Git Remote 唯一匹配并保存本机仓库绑定。

项目包不会包含本机路径、身份、聊天记录或凭证。仓库为零匹配或多匹配时不绑定；创建 Task 时也只有项目存在唯一有效本地绑定才自动设置 `repo_path`。

## 字节身份

- `GET /api/auth/status`：读取当前 Jarvis 浏览器会话。
- `POST /api/auth/login`：复用本机 BytedCLI 字节身份；未登录时启动非阻塞 SSO Device Flow。
- `POST /api/auth/login/complete`：轮询并完成 SSO 登录。
- `POST /api/auth/logout`：只清除 Jarvis 浏览器会话，不清除全机 BytedCLI 授权。

浏览器发起的管理 API 请求需要 Jarvis 会话；健康检查、认证接口、`POST /api/clues`、卡片回调和无浏览器 Fetch Metadata 的本机 Agent/CLI 请求不经过该门禁。

## 健康与工作项

- `GET /healthz`：存活探针，只 ping SQLite。`scripts/rebuild-server.sh` 靠它判断重启成功，所以外部依赖不进它的状态码。
- `GET /readyz`：依赖全景，逐项报告 SQLite、Qdrant、`lark-cli`、agent CLI。只有 SQLite 不通才返 503；外部依赖不通返 200 且 `status=degraded`，主动关掉的报 `disabled`。
  - `lark_cli` 同时校验二进制可解析和飞书 user 登录可用（`auth status --verify`），正常时附带 `user`、`user_open_id`、`token_status`。飞书 refresh token 会自行过期，过期后二进制仍在原地，只校验路径会一直报绿；所有 `--as user` 能力（群消息采集、建文档、人员搜索）都依赖这条。
- Todo：`GET /api/todos`、`GET /api/todos/:todo_id`、`PATCH /api/todos/:todo_id/status`
- Task：`GET/POST /api/tasks`、`GET /api/tasks/:task_id/runs|events|output`
- Task 控制：`finish`、`supplement`、`execute`、`interrupt`、`rerun`、`resume`
- 外部效果：`POST /api/tasks/:task_id/effects/recall-message`

`output` 和执行控制接口只有在 Executor 注入时注册。

## 背景与世界状态

- Projects：`GET/POST /api/projects`、`GET/PUT/DELETE /api/projects/:project_id`
- Key matters：`GET/POST /api/key-matters`、`GET/PUT/DELETE /api/key-matters/:key_matter_id`、`POST .../touch`
- Persons：`GET/POST /api/persons`、`GET/PUT/DELETE /api/persons/:person_id`
- Feishu people：`GET /api/people/search?q=` 是全产品统一的飞书人员搜索入口。
- 人员搜索兼容接口（Deprecated）：`POST /api/persons/resolve` 是旧 World 页面接口，body 为 `{query}`，返回 `data.candidates`；`GET /api/biz-okr/people/search?q=` 是旧 Biz OKR 页面接口，返回 `data.users`。两者只服务已加载或缓存的旧前端，内部均委托给 `/api/people/search` 使用的同一飞书解析服务；新代码不得调用。
- 兼容接口删除条件：所有已部署前端均已切换到 `/api/people/search`，且请求日志在一个完整的前端缓存保留周期内没有旧接口调用。删除时同时移除两条路由、两个适配 handler 和对应兼容测试；不要只删路由。
- Groups：`GET /api/groups`、`PUT /api/groups/:group_id`
- Principal：`GET/PUT /api/profile`
- Managed resources：`GET/POST /api/resources`、`GET/PUT/DELETE /api/resources/:resource_id`
- Facts：`GET/POST /api/facts`
- World progress：`GET /api/world-progress` 按 `subject_type + subject_id + period_key` 精确读取，`GET /api/world-progress/period/:period_key` 批量读取一个周期，`GET /api/world-progress/:id` 按 ID 读取，`POST /api/world-progress` 创建，`PUT /api/world-progress/:id` 带 `expected_version` 做 CAS 更新。当前允许为已启用 OKR 模块中真实存在的 `okr_objective`、`okr_kr`、`okr_point` 写入；模块关闭后历史记录仍可读取。它是 Jarvis 的证据化周期判断，不是人工正式周报。
- Entity relations：`GET/POST /api/relations`、`DELETE /api/relations/:relation_id`，保存带证据的通用跨模块实体映射
- 实体长期事实页：`GET /api/pages`、`GET/PUT /api/pages/:type/:id`、`GET /api/pages/:type/:id/backlinks`。`PUT` 需带 `if_unchanged_since` 做 CAS，不匹配返回 409 并回带当前全文。

`DELETE /api/key-matters/:key_matter_id` 的业务语义是闭环，`DELETE /api/projects/:project_id` 的业务语义是归档；都不是物理删除。

来源证据使用通用 Message/Clue 查询、Fact 和 Page CAS；核心层不提供 OKR 专用证据队列或合并接口。

## Agent 配置面

- Shared memory：`GET/PUT /api/shared-memory`、`POST /api/shared-memory/append`；整段内容最多 2000 字
- Runtime settings：`GET/PUT /api/runtime-settings`
- Work rules：`GET /api/work-rules`、`GET/PUT /api/work-rules/:work_rule_key`
- Text files：`GET /api/text-files`、`GET/PUT /api/text-files/:text_file_key`
- Skills：`GET /api/skills`、`POST /api/skills/scan`、`PUT /api/skills/:skill_name`、`GET /api/skills/:skill_name/content`
- App modules：`GET /api/app-modules`、`PUT /api/app-modules/:module_key`
- 通用 OKR：`GET /api/okr/scope|enums|board` 读取 Objective、KR、Metric、Point 与负责人；图片、Objective/KR 定义维护也位于 `/api/okr/*`。这些接口不读取 Biz 标签、评分、评论、Meego 或身份表。
- 定义的窄接口：`PUT /api/okr/krs/:kr_id/definition` 只接受 KR 与已有要点的标题和负责人，收不到指标、灯、标签，也不能增删要点。Biz 周报和 Review 页面手里的指标值属于当周副本，靠这个接口的字段边界保证它们到不了主干定义；改完由前端重新取一次 Biz 组合视图当基线。Biz 完整编辑器走 `PUT /api/biz-okr/krs/:kr_id`，标签走 `/api/biz-okr/.../tags`。
- 排序：`PUT /api/okr/objectives/order`（带 `quarter`）和 `PUT /api/okr/objectives/:objective_id/kr-order` 接收该范围内**全量**兄弟 id，写成 `sort_order` 0..n-1；少给、多给、重复或跨范围都拒绝。位置不是内容，所以不撞 `KR.version`，别处打开的页面不会因为有人调顺序而在下次保存时冲突。
- 存在周报历史的 KR 或稳定拆解不允许删除，避免留下孤儿历史。
- 正式 OKR Progress：`GET /api/okr/progress/scope|board`，`GET/POST /api/okr/weeks`，`GET /api/okr/krs/:kr_id/weekly`，`PUT /api/okr/krs/:kr_id/weekly-core`，以及 `/api/okr/points/:point_id/progress`、`/api/okr/progress/:progress_id` 的单条进展 CRUD。通用侧可以开周、读周和逐条改进展，但**没有删整周的入口**：评论、评分、Follow-up 和催填批次都按 `(quarter, week)` 存且没有指向周记录的外键，只删正式时间线会让它们在下次开同名周时复活，所以整周删除只由 `DELETE /api/biz-okr/weeks/:week` 一次性清干净。
- Biz OKR：`GET /api/biz-okr/scope|board|core-board` 提供当前完整业务组合视图；Plan、标签、Review、评论、评分、Follow-up、催填、Meego 和文档导出均位于 `/api/biz-okr/*`。`DELETE /api/biz-okr/weeks/:week?quarter=...` 保留原有“删除整个业务周次”的完整清理语义。固定四个 Agent 行动仍由 `okr-agent-orchestrator` 动态组合原子工具，不提供固定生成 API。
- OKR Preview AI 评审：`POST /api/biz-okr/preview-review`，body 为 `{quarter, week, kind: all|kr|point, kr_id, point_id}`，同步返回 `{content}` Markdown。评审只出判断和建议、不写任何东西，所以不建 Task、不进审批链路、没有运行历史可轮询。Agent 按需使用 `okr-module-tools` 与 `biz-okr-tools` 回查。
- Meego observation：`POST /api/biz-okr/meego-observations` 只保存 Agent 已通过 `bytedcli` 读取的结构化快照；HTTP handler 不查询 Meego，外部读取和匹配规则归 `weekly-report-progress-sync` Skill。
- Biz OKR identity：`GET /api/biz-okr/me`；启用 `conf/okr-module.yaml` 的 `identity` 后，经 `POST /api/biz-okr/auth/feishu/device` 发起飞书设备授权、`POST /api/biz-okr/auth/feishu/device/:login_id/poll` 轮询并建立 HttpOnly session。用户 token 仍按 open_id 写到 `identity.token_dir`，`GET /api/biz-okr/feishu-identity?open_id=` 返回与 token 配对的 App ID 和文件位置。身份只服务 Biz 页面、评论署名和用户态文档读取，不进入通用 OKR 插件。
- Biz OKR people：`GET /api/biz-okr/people/avatars?names=...` 服务 Biz 人员头像展示；新页面的人员搜索复用全局 `/api/people/search?q=`。旧搜索接口及下线条件见上面的“人员搜索兼容接口”。
- OKR images：`POST /api/okr/images` 上传 PNG/JPEG/GIF/WebP，返回可持久化的 `/okr-assets/<sha256>.<ext>`；图片落在 `conf/okr-module.yaml` 的 `upload_dir`。
- 文档导出：`POST /api/biz-okr/feishu-documents`，由用户按钮触发，通过当前 Jarvis `lark-cli --as user` 创建 Markdown 飞书文档。新建文档继承的租户默认密级不允许组织内链接分享，飞书会以 91012 拒绝，所以创建后先按 `lark_cli.export_secure_label` 的标签名（在 `drive +secure-label-list` 里查 id）打一次密级，再设 `link_share_entity=tenant_editable` 并读回校验。标签没配、租户里查不到这个名字或密级写入失败都直接报错，不退回一篇不可分享的文档。

Runtime settings 写入后需要重启进程生效；模块开关保存后也需要重启，下一次启动会统一决定迁移、路由、静态资源、Skill 与调度边界。prompts、rules、Skills 按各自 reader 的行为读取。

## 调度、线索与总结

- Scheduled tasks：`GET/POST /api/scheduled-tasks`、`POST /api/scheduled-tasks/yield`、`PUT/DELETE /api/scheduled-tasks/:scheduled_task_id`、`POST .../trigger`
- 通用线索：`POST /api/clues`
- Overview / digests：`GET /api/overview`、`GET /api/digests`、`POST /api/digests/summarize`
- Daily digests：`GET /api/daily-digests`、`POST /api/daily-digests/generate`
- Worklog：`GET /api/worklog/commits`、`GET /api/worklog/documents`

`/api/clues` 只有在 Capture 注入时注册；daily digest 和 worklog 也按依赖条件注册。

## 调试与对话

- Debug：`modules`、`agent-processes`、`failures`、`scans`、`watermarks`、`logs`
- System task runs：`GET /api/system-tasks/runs`
- 主动巡视运行记录：`GET /api/debug/proactive-runs`、`GET /api/debug/proactive-runs/:run_id`
- 手工采集：`POST /api/debug/capture/discover|scan-related|scan-chat`
- 主服务对话发现：`GET /api/chat-config`
- 前端部署事实：`GET /api/web-config`，返回 `server.public_base_url`。分享链接用它当根地址，作者从 IP 打开页面也能复制出域名链接；配置留空时返回空串，链接沿用当前浏览器地址。
- 独立 Chat sidecar：`POST /api/chat`（multipart + SSE；`message` 必填，`thread_id`、JSON 字符串 `page_context`、单张 PNG/JPEG `image`、`user_open_id` 可选，图片上限 10 MB）、`GET /api/chat/:thread_id`
- 带 `user_open_id` 时，sidecar 向主服务的 `/api/biz-okr/feishu-identity` 取该登录用户的飞书凭证位置，并把「用谁的身份 + token 文件路径 + 单条命令注入用法」写进本轮 prompt，让 Agent 用用户自己的权限读他扔进来的文档；token 本身不进 prompt。凭证不可用时把原因写进同一段落，不中断对话。

对话只在 `chat.enabled=true` 且依赖构造成功时注册。
