# HTTP API 分组

> Status: current
> Authority: reference; `internal/api/router.go` is source of truth
> Last verified: 2026-08-02 @ `89fa24b`

本文只按能力分组，不复制 handler 的完整请求/响应结构。新增或删除接口时先改 `internal/api/router.go`，再更新本页。

## 健康与工作项

- `GET /healthz`：存活探针，只 ping SQLite。`scripts/rebuild-server.sh` 靠它判断重启成功，所以外部依赖不进它的状态码。
- `GET /readyz`：依赖全景，逐项报告 SQLite、Qdrant、`lark-cli`、agent CLI。只有 SQLite 不通才返 503；外部依赖不通返 200 且 `status=degraded`，主动关掉的报 `disabled`。
  - `lark_cli` 同时校验二进制可解析和飞书 user 登录可用（`auth status --verify`），正常时附带 `user`、`user_open_id`、`token_status`。飞书 refresh token 会自行过期，过期后二进制仍在原地，只校验路径会一直报绿；所有 `--as user` 能力（群消息采集、建文档、人员搜索）都依赖这条。
- Todo：`GET /api/todos`、`GET /api/todos/:todo_id`、`PATCH /api/todos/:todo_id/status`
- Task：`GET/POST /api/tasks`、`GET /api/tasks/:task_id/runs|events|output`
- Task 控制：`finish`、`supplement`、`execute`、`interrupt`、`rerun`、`reapply`、`resume`、`approve`、`reject`
- 外部效果：`POST /api/tasks/:task_id/effects/recall-message`

`output` 和执行控制接口只有在 Executor 注入时注册。

## 背景与世界状态

- Projects：`GET/POST /api/projects`、`GET/PUT/DELETE /api/projects/:project_id`
- Key matters：`GET/POST /api/key-matters`、`GET/PUT/DELETE /api/key-matters/:key_matter_id`、`POST .../touch`
- Persons：`GET/POST /api/persons`、`POST /api/persons/resolve`、`GET/PUT/DELETE /api/persons/:person_id`
- Groups：`GET /api/groups`、`PUT /api/groups/:group_id`
- Principal：`GET/PUT /api/profile`
- Managed resources：`GET/POST /api/resources`、`GET/PUT/DELETE /api/resources/:resource_id`
- Facts：`GET/POST /api/facts`
- Entity relations：`GET/POST /api/relations`、`DELETE /api/relations/:relation_id`，保存带证据的通用跨模块实体映射
- Relation facts：`GET/POST /api/relation-facts`、`PUT/DELETE /api/relation-facts/:fact_id`

`DELETE /api/key-matters/:key_matter_id` 的业务语义是闭环，`DELETE /api/projects/:project_id` 的业务语义是归档；都不是物理删除。

来源证据使用通用 Message/Clue 查询、Fact 和 Page CAS；核心层不提供 OKR 专用证据队列或合并接口。

## Agent 配置面

- Shared memory：`GET/PUT /api/shared-memory`、`POST /api/shared-memory/append`
- Runtime settings：`GET/PUT /api/runtime-settings`
- Work rules：`GET /api/work-rules`、`GET/PUT /api/work-rules/:work_rule_key`
- Text files：`GET /api/text-files`、`GET/PUT /api/text-files/:text_file_key`
- Skills：`GET /api/skills`、`POST /api/skills/scan`、`PUT /api/skills/:skill_name`、`GET /api/skills/:skill_name/content`
- App modules：`GET /api/app-modules`、`PUT /api/app-modules/:module_key`
- OKR 定义：`GET /api/okr/scope|enums|board`，身份、人员、图片、Objective/KR 创建与 KR 编辑位于 `/api/okr/*`。核心 board 和写接口不读取周报表。
- 存在周报历史的 KR 或稳定拆解不允许删除，避免留下孤儿历史。
- 周报：`GET /api/weekly-report/scope|board|weeks|comments|reminder-preview|reminder-batches|meego-preview`；`POST /api/weekly-report/weeks` 开启空周，`DELETE /api/weekly-report/weeks/:week?quarter=...` 删除该季度下整周的周报事实但保留 O/KR 稳定定义。评论、所选周进展、Meego 观察和催办批次的其它写接口位于 `/api/weekly-report/*`。产品固定提供催填、进展巡检、会议材料和对外提交四个 Agent 行动，并固定绑定可编辑 Prompt；季度草稿、区域对齐和其它 Report Prompt 可由普通 Agent Task 使用。执行统一由 `okr-agent-orchestrator` 动态组合原子工具，不提供固定生成 API。
- Meego observation：`POST /api/weekly-report/meego-observations` 只保存 Agent 已通过 `bytedcli` 读取的结构化快照；HTTP handler 不查询 Meego，外部读取和匹配规则归 `weekly-report-progress-sync` Skill。
- OKR identity：`GET /api/okr/me`；启用 `conf/okr-module.yaml` 的 `identity` 后，经 `POST /api/okr/auth/feishu/device` 发起飞书设备授权、`POST /api/okr/auth/feishu/device/:login_id/poll` 轮询并建立 HttpOnly session。流程不需要 OAuth 回调 URL。用户的 access/refresh token 不落库，而是按 open_id 写到 `identity.token_dir` 下的 `<open_id>.json`（含人名、邮箱、scope 和两个 token 的到期时间），同一人再登录一次即覆盖。申请的 scope 由 `identity.scopes_file`（`conf/okr-feishu-scopes.txt`）逐行列出，只覆盖云文档、云空间、知识库和多维表格，`offline_access` 换取 refresh token；文件缺失或没有有效行时启动即失败。`GET /api/okr/feishu-identity?open_id=` 在需要时用 refresh token 续期该用户的 access token、回写同一文件，并返回 `open_id`、`name`、`app_id`、`token_path` 和到期时间——只给位置不给 token，因为只有主服务持有 app secret。凭证不存在或已无法续期返回 404。前端只在 OKR 模块入口做一次全局门禁，并按服务端返回的到期时间统一退出；内部页面不传递身份状态。新建评论由后端读取 session 记录真人作者，其余 OKR/周报写操作按 `Jarvis` 记账，不做行级权限或可见性过滤。
- OKR people：`GET /api/okr/people/search?q=` 走 `contact +search-user`，供人员选择器解析 open_id；`GET /api/okr/people/avatars?names=a,b,c` 批量取头像，供已在看板上的负责人、评论作者展示。飞书只有 `/open-apis/search/v1/user` 在当前授权范围内返回头像，且只能按姓名或邮箱查，所以调用方按 open_id 自行认领结果，同名且无 open_id 时前端退回首字母。已解析的头像按姓名写入 SQLite 同目录的 `feishu-avatars.json` 并跨重启复用——一块看板有几十个负责人，而 lark-cli 只允许两个并发调用。
- OKR images：`POST /api/okr/images` 上传 PNG/JPEG/GIF/WebP，返回可持久化的 `/okr-assets/<sha256>.<ext>`；图片落在 `conf/okr-module.yaml` 的 `upload_dir`。
- 文档导出：`POST /api/weekly-report/feishu-documents`，由用户按钮触发，通过当前 Jarvis `lark-cli --as user` 创建 Markdown 飞书文档。新建文档继承的租户默认密级不允许组织内链接分享，飞书会以 91012 拒绝，所以创建后先按 `lark_cli.export_secure_label` 的标签名（在 `drive +secure-label-list` 里查 id）打一次密级，再设 `link_share_entity=tenant_editable` 并读回校验。标签没配、租户里查不到这个名字或密级写入失败都直接报错，不退回一篇不可分享的文档。

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
- 带 `user_open_id` 时，sidecar 向主服务的 `/api/okr/feishu-identity` 取该登录用户的飞书凭证位置，并把「用谁的身份 + token 文件路径 + 单条命令注入用法」写进本轮 prompt，让 Agent 用用户自己的权限读他扔进来的文档；token 本身不进 prompt。凭证不可用时把原因写进同一段落，不中断对话。

对话只在 `chat.enabled=true` 且依赖构造成功时注册。
