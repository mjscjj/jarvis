# HTTP API 分组

> Status: current
> Authority: reference; `internal/api/router.go` is source of truth
> Last verified: 2026-08-02 @ `89fa24b`

本文只按能力分组，不复制 handler 的完整请求/响应结构。新增或删除接口时先改 `internal/api/router.go`，再更新本页。

## 健康与工作项

- `GET /healthz`：存活探针，只 ping SQLite。`scripts/rebuild-server.sh` 靠它判断重启成功，所以外部依赖不进它的状态码。
- `GET /readyz`：依赖全景，逐项报告 SQLite、Qdrant、`lark-cli`、agent CLI。只有 SQLite 不通才返 503；外部依赖不通返 200 且 `status=degraded`，主动关掉的报 `disabled`。
- Todo：`GET /api/todos`、`GET /api/todos/:todo_id`、`PATCH /api/todos/:todo_id/status`
- Task：`GET/POST /api/tasks`、`GET /api/tasks/:task_id/runs|events|output`
- Task 控制：`finish`、`supplement`、`execute`、`interrupt`、`rerun`、`reapply`、`resume`、`approve`、`reject`
- 外部效果：`POST /api/tasks/:task_id/effects/recall-message`

`output` 和执行控制接口只有在 Executor 注入时注册。

## 背景与世界状态

- Projects：`GET/POST /api/projects`、`GET/PUT/DELETE /api/projects/:project_id`
- Persons：`GET/POST /api/persons`、`POST /api/persons/resolve`、`GET/PUT/DELETE /api/persons/:person_id`
- Groups：`GET /api/groups`、`PUT /api/groups/:group_id`
- Principal：`GET/PUT /api/profile`
- Managed resources：`GET/POST /api/resources`、`GET/PUT/DELETE /api/resources/:resource_id`
- Facts：`GET/POST /api/facts`
- Relation facts：`GET/POST /api/relation-facts`、`PUT/DELETE /api/relation-facts/:fact_id`

`DELETE /api/projects/:project_id` 的业务语义是归档，不是物理删除。

## Agent 配置面

- Shared memory：`GET/PUT /api/shared-memory`、`POST /api/shared-memory/append`
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
