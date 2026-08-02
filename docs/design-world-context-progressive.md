# 世界建模进上下文：按天、限量、渐进式加载

本文是一份待实施方案。它规定世界建模数据（fact / task / todo）以什么形状进入提示词，以及模型如何从摘要下钻到细节。所有设计决定已由 principal 拍板，实施时不要重新讨论方向。

## 1. 现状与问题

调查结论（代码事实，均已核实）：

- M3 抽取阶段是唯一"推"世界数据的地方，`renderUserPrompt`（`internal/extract/prompt.go:74`）把 principal / project / group / participants / resources / facts / open_todos 渲染成 Markdown 段落。
- M5 判断与执行阶段读的是 M3 冻结的 `Todo.ContextSnapshot`，整块塞进 `BEGIN_DECISION_CONTEXT` / `BEGIN_TASK_CONTEXT` 的 `background` 字段，没有独立的世界段落。
- `fact` 只装载 group 和 project 两个主体（`internal/extract/worker.go:239`），离线事实引擎产出的 person 主体事实没有任何读取点。
- `task` 完全没进过任何提示词。`Task.Summary` / `Task.LastProgressAt` 是只写字段，全仓库无读取点。
- `todo` 进上下文的是"未闭环"清单，按 status 过滤而不按时间，一条三周前的和今天的混排。
- 下钻通道断裂：`jarvis-tools` 没有任何 todo / task 查询命令，模型看到摘要后无法取细节。
- 限流散在四处（`snapshotFactLimit=50`、`cfg.Extract.FactLimit`、`cfg.Extract.OpenTodoLimit`、`maxPriorRunsInPrompt=5`），且 M3 超预算时砍的是原始会话消息，世界数据一条不动。

## 2. 已拍板的设计决定

实施时以此为准，不要另作取舍。

1. **M5 侧保持冻结。** 不为 M5 新增"当前时刻世界切片"的装配。M5 想要比快照更新的世界信息，自己调工具拉。这是有意接受的代价——因此下钻工具必须先做扎实。
2. **todo 不加摘要字段。** 进上下文只有 `todo_id` / `action_type` / `title` / `status`，细节靠新增的 `get-todo` 下钻。
3. **`daily_digest` 完全不碰。** 它是给人看的日报，与本方案无关，也不进任何提示词。不要复用、不要改造、不要给它加 project scope。
4. **推送层不设天窗。** "按天"是下钻维度（工具的 `--date`），不是推送维度。
5. **fact 每个主体两层**：今天的明细事实最多 10 条（排除 rollup），加前一天的 1 条 rollup。更早的一律靠工具查。
6. **person 主体只取关键人**：交办人 assigner、`is_leader` 的参与者、本轮实际发言者，三者取并集后按上限截断。
7. **`subject_type="task"` 的事实不推进上下文。** 任务进展走 `task.summary`，task 主体的事实由 `get-task` 一并带出。
8. **超长降级先砍世界数据**，每类留一个下限，全部到下限后才开始砍原始会话消息。
9. **rollup 覆盖前一天所有产出过明细事实的主体**，无事实的主体自然跳过，不需要白名单。
10. **rollup 产物写回 fact 表本身**，`source_kind="rollup"`，原始明细不删。不新建表。

## 3. 数据层

### 3.1 `FactFilter` 增加 source_kind 过滤

`internal/progress/service.go` 的 `FactFilter` 现在只有 SubjectType / SubjectID / From / Until / Limit。增加两个字段：

```go
// SourceKind restricts to facts written by one producer; ExcludeSourceKind
// removes one. They exist because the prompt needs the two layers separately:
// today's detail is "everything except the rollup", the previous day is
// "the rollup only".
SourceKind        *string
ExcludeSourceKind *string
```

在 `ListFacts` 的查询里落成 `source_kind = ?` 与 `(source_kind IS NULL OR source_kind <> ?)`。注意 `source_kind` 可空，排除条件必须放过 NULL。

在 `internal/progress` 里定义常量 `FactSourceRollup = "rollup"`，供压缩任务与 M3 共用，不要在两处写字面量。

`/api/facts` 的 GET handler（`internal/api/progress_events.go`）同步支持 `source_kind` 与 `exclude_source_kind` 两个 query 参数。

### 3.2 `todo.last_evidence_at` 补索引

`internal/domain/models.go` 的 `Todo.LastEvidenceAt` 现在没有索引，而按天查询要用它做锚。加 `index:idx_todo_last_evidence`，由 AutoMigrate 生效，不需要手写迁移。

### 3.3 不需要迁移和历史回填

`fact` 表结构不变（只是开始使用现有的 `source_kind`）。rollup 从任务首次运行起自然产生，**不回填历史**。

## 4. 下钻通道（先做，这是地基）

### 4.1 API

- `GET /api/todos`（`internal/api/todos.go`）增加 `from` / `until` 两个 RFC3339 query 参数，锚定 `last_evidence_at`，半开区间。沿用 fact 的做法：不加 date 列、不在服务端猜时区，自然日由调用方在本地时区算好再传。
- `GET /api/tasks`（`internal/api/tasks.go` + `internal/execute/store.go` 的 `TaskFilter`）同样增加 `from` / `until`，锚定 `COALESCE(last_progress_at, confirmed_at)`。
- 新增 `GET /api/tasks/:task_id` 返回单个 `TaskView`。现在只有 `/runs` 和 `/events`，缺主体详情。

### 4.2 `scripts/jarvis-tools` 新增四个只读命令

输出 JSON，风格与现有命令一致（参考 `get-project` 如何把项目与 facts 合并成一份输出）：

- `list-todos [--date YYYY-MM-DD] [--status S] [--limit N]`
- `get-todo --id N` —— 带上 `description` / `context` / `open_questions` / `resolution`，这些是上下文里被省略的部分。
- `list-tasks [--date YYYY-MM-DD] [--status S] [--limit N]`
- `get-task --id N` —— 合并四样东西：task 详情（含 `plan` / `summary` / `background`）、最近若干 run 摘要与 effects、task_event 时间线、以及 `subject_type=task` 且 `subject_id` 等于该 task 的 facts。

`--date` 的解析照抄 `list-facts` 的实现（`scripts/jarvis-tools:908` 附近）：校验 `YYYY-MM-DD`，在本地时区算出 `from` 与次日 `until`，失败 fail-fast。

### 4.3 `internal/toolcatalog/catalog.go` 注册

四个命令注册到 `StageExtract`、`StageExecute`、`StageChat`。**`StageDecide` 不给**，与 facts 现在的处理一致——判断环节是廉价价值判断，不做调查。

## 5. 事实日压缩（旁路）

### 5.1 位置

放在 `internal/factengine` 内新增文件（如 `rollup.go`），复用该包已有的 `StartScheduler` 与 db 句柄，不要新建包、不要复制调度样板。它与现有事实抽取是同构的旁路：定时 + 模型 + 写 fact。

### 5.2 逻辑

每天定时运行，处理前一个自然日（本地时区）：

1. 找出前一天产出过明细事实的所有主体：`SELECT DISTINCT subject_type, subject_id FROM fact WHERE occurred_at >= ? AND occurred_at < ? AND (source_kind IS NULL OR source_kind <> 'rollup')`。
2. 对每个主体，读出那天的明细事实，连同主体名称（项目名 / 群名 / 人名，便于模型写出可读的句子）送进压缩提示词。
3. 模型返回一段话。**先删除该主体该天已有的 rollup 记录，再写入新的一条**——这是重跑幂等的方式。不用事务，按顺序写，中途出错 fail-fast 报错退出、下次重跑（AGENTS.md §5）。
4. 写入的 fact：`subject_type` / `subject_id` 保持原主体，`occurred_at` 取被压缩那天的本地 00:00，`source_kind = "rollup"`，`description` 为模型返回的那段话。

原始明细一条都不删。压缩只是加了一层更粗的记录，`list-facts --date` 下钻时仍能看到那天的全部原文。

### 5.3 提示词

按 AGENTS.md §6：在 `internal/textstore/defaults.go` 注册稳定 key（如 `fact-rollup-system-prompt`），正文提交到 `conf/prompts/fact-rollup-system-prompt.md`，运行时通过注入的 `textstore.Reader` 实时读取，缺失或空正文直接报错。

提示词只描述角色与稳定行为：把某个主体某一天的若干条事实压成一段能独立读懂的话，讲清那天定了什么、推进到哪、留下什么没解决；不要罗列、不要评价、不要编造原文没有的内容。不要在提示词里点工具名。

### 5.4 配置与手动触发

cron spec 加进配置，命名与现有事实引擎的调度配置项对齐。同时提供一个手动触发入口（如 `POST /api/fact-rollups/generate` 接受 `date`），便于验证与补算某一天——这比等第二天跑定时任务便宜得多。

## 6. M3 推送层改造

### 6.1 fact 改成两层装载

改造 `internal/extract/worker.go` 的 `loadFacts`。主体列表从"group + project"扩展为"group + project + 关键人"，每个主体做两次查询：

- 今天的明细：`From` = 今天本地 00:00，`Until` = 次日 00:00，`ExcludeSourceKind` = rollup，`Limit` = 每主体今日上限。
- 前一天的 rollup：`From` = 昨天本地 00:00，`Until` = 今天 00:00，`SourceKind` = rollup，`Limit` = 1。

复用现有的 `cfg.Extract.FactLimit` 作为"每主体今天的明细上限"，默认值改为 10，并在注释里写明它现在的语义。不要为此新增第二个近义配置项。

### 6.2 关键人主体

`ParticipantContext`（`internal/extract/pipeline_types.go:95`）现在只有 `open_id`，没有 `person.id`，需要解析。`internal/factengine/store.go` 已有把 open_id 映射成 person 主体的写法，照搬到 `internal/extract/pipeline_store.go`，给 `ParticipantContext` 加 `PersonID *uint64`。

关键人 = 交办人 assigner + `IsLeader` 的参与者 + 本轮实际发言者，三者取并集，去重后按上限截断（新增配置项，默认 5）。未录入 person 表的参与者解析不到 id，直接跳过，不报错。

### 6.3 新增 task 段落

新增 `loadRecentTasks`：取最近有进展的 task，按 `COALESCE(last_progress_at, confirmed_at) DESC` 排序，限量（新增配置项，默认 10）。范围限定为与当前会话相关的 task——通过 `todo_id` 关联到 `todo`，取 `todo.group_id` 等于当前群或 `todo.project_id` 等于当前项目的；没有 todo 的 task（scheduled_task / manual）不纳入。

每条只渲染 `task_id` / `title` / `status` / `summary` / `last_progress_at`。这让上一轮加的两个只写字段真正活起来。

`internal/contextsnap/snapshot.go` 的 `Snapshot` 增加对应字段（如 `RecentTasks`），`internal/extract/snapshot.go` 的 `buildContextSnapshot` 一并冻结进快照，这样 M5 也能看到（即使是 M3 时刻的值）。

`internal/extract/prompt.go` 增加渲染段落，标题风格与现有段落一致，如 `# 最近有进展的任务（仅作背景）`。

### 6.4 todo 段落不变

维持现有字段与 `cfg.Extract.OpenTodoLimit`，不加摘要字段。

## 7. 超长降级顺序反转

现在 `BuildPrompt`（`internal/extract/prompt.go:61-71`）超 `MaxChars` 时循环丢弃会话上下文消息，世界数据一条不动。改成先削世界数据：

1. 逐级收紧世界数据：person 主体的事实 → other_projects → recent_tasks → open_todos → group / project 的事实。每类保留一个下限（如各留 3 条），不砍到零。
2. 世界数据全部到下限后，才开始按现有逻辑丢弃会话上下文消息。
3. 最后仍然超限，保持现有行为——报错 fail-fast，不静默截断。

理由：世界摘要能靠工具查回来，原始对话是判断的一手证据，不可替代。

实现上给 `renderUserPrompt` 传一个降级档位参数，逐级收紧；不要在渲染函数里散落硬编码。

## 8. 提示词补渐进式加载契约

`conf/prompts/m3-system-prompt.md` 与 `conf/prompts/m5-system-prompt.md` 各加一段，说明上下文里给的是摘要层：今天的事实是明细、更早的是每天一条压缩、任务只有一句进展、线索只有标题和状态；需要细节自己去查，按主体和日期取。

按 AGENTS.md §6，只描述行为，**不要点工具名**——工具说明由 `internal/toolcatalog` 和 `jarvis-tools --help` 维护。仓库里有一条测试会校验系统提示词与工具目录分离，点了工具名会失败。

## 9. 范围之外

- 不改前端。rollup 事实在现有事实列表里作为普通 fact 显示即可。
- 不碰 `daily_digest`、不碰 `mem0`、不碰 `Snapshot.Memories`。
- 不为 M5 新增世界切片装配。
- 不回填历史 rollup。

## 10. 验收

- `go build ./...`、`go test ./...` 全绿；`gofmt -w` 无残留 diff。
- 前端未改动则无需 `tsc`；若改了 `web/`，跑 `npx tsc --noEmit`。
- 单测至少覆盖：`FactFilter` 的 source_kind 等值与排除（含 NULL 放行）、rollup 重跑幂等（同主体同天不叠加）、M3 两层装载（今天明细排除 rollup、前一天只取 rollup）、关键人并集与上限截断、降级顺序（世界数据先到下限、会话消息后砍）。
- 构建或重启主服务必须走 `./scripts/rebuild-server.sh`（AGENTS.md §7）。
