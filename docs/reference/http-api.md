# HTTP API 分组

> Status: current
> Authority: reference; `internal/api/router.go` is source of truth
> Last verified: 2026-08-02 @ `89fa24b`

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
- Todo：`GET /api/todos`、`GET /api/todos/:todo_id`、`PATCH /api/todos/:todo_id/status`
- Task：`GET/POST /api/tasks`、`GET /api/tasks/:task_id/runs|events|output`
- Task 控制：`finish`、`supplement`、`execute`、`interrupt`、`rerun`、`resume`
- 外部效果：`POST /api/tasks/:task_id/effects/recall-message`

`output` 和执行控制接口只有在 Executor 注入时注册。

## 背景与世界状态

- Projects：`GET/POST /api/projects`、`GET/PUT/DELETE /api/projects/:project_id`
- Persons：`GET/POST /api/persons`、`POST /api/persons/resolve`、`GET/PUT/DELETE /api/persons/:person_id`
- Groups：`GET /api/groups`、`PUT /api/groups/:group_id`
- Principal：`GET/PUT /api/profile`
- Managed resources：`GET/POST /api/resources`、`GET/PUT/DELETE /api/resources/:resource_id`
- Facts：`GET/POST /api/facts`
- 实体长期事实页：`GET /api/pages`、`GET/PUT /api/pages/:type/:id`、`GET /api/pages/:type/:id/backlinks`。`PUT` 需带 `if_unchanged_since` 做 CAS，不匹配返回 409 并回带当前全文。

`DELETE /api/projects/:project_id` 的业务语义是归档，不是物理删除。

## Agent 配置面

- Shared memory：`GET/PUT /api/shared-memory`、`POST /api/shared-memory/append`；整段内容最多 2000 字
- Runtime settings：`GET/PUT /api/runtime-settings`
- Work rules：`GET /api/work-rules`、`GET/PUT /api/work-rules/:work_rule_key`
- Text files：`GET /api/text-files`、`GET/PUT /api/text-files/:text_file_key`
- Skills：`GET /api/skills`、`POST /api/skills/scan`、`PUT /api/skills/:skill_name`、`GET /api/skills/:skill_name/content`

Runtime settings 写入后需要重启进程生效；prompts/rules/Skills 按各自 reader 的行为读取。

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
- 对话：`POST /api/chat`（SSE）

对话只在 `chat.enabled=true` 且依赖构造成功时注册。

### Principal 通知卡片

`POST /api/notices/principal`：由 Agent 显式调用，给当前 Principal 发 Bot 卡片。请求包含 `content`、`idempotency_key`，可选 `type`（默认 Notice，开放字符串）、`links`、`details`、`extra`、`task_id`。返回消息凭据和 effect，错误响应也保留部分发送凭据；不改变 Task 状态。CLI：`jarvis-tools notice-principal --payload-file FILE`。详见 [通知卡片说明](../summery/notice-principal-proposal.md)。
