# 世界模型维护、日压缩与渐进式上下文

> Status: mixed；§5 持续世界维护和 §6 事实日压缩为 current，其余章节保留原渐进式上下文方案状态
> Authority: `docs/00-overview.md` 是总纲；本文记录现行运行协议与后续上下文方案
> Last verified baseline: 2026-08-08 @ `01ec2c4`

本文同时记录两件事：已经落地的 factengine 世界维护与事实日压缩，以及世界建模数据（fact / task / todo）如何进入 M3/M5 上下文、如何从摘要下钻到细节的原方案。若旧描述与 §5、§6 或 `docs/00-overview.md` 冲突，以 current 章节和总纲为准。

## 1. 原始问题（historical baseline）

调查结论（代码事实，均已核实）：

- M3 抽取阶段是唯一"推"世界数据的地方，`renderUserPrompt`（`internal/extract/prompt.go:74`）把 principal / project / group / participants / resources / facts / open_todos 渲染成 Markdown 段落。
- M5 执行首轮只读取 M3 冻结 `Todo.ContextSnapshot` 的小投影；完整背景保存在 Task 上，通过 `get-task` 按需读取，没有独立的实时世界段落。
- `fact` 只装载 group 和 project 两个主体（`internal/extract/worker.go:239`），持续世界建模 Agent 产出的 person 主体事实没有任何读取点。
- `task` 完全没进过任何提示词。`Task.Summary` / `Task.LastProgressAt` 是只写字段，全仓库无读取点。
- `todo` 进上下文的是"未闭环"清单，按 status 过滤而不按时间，一条三周前的和今天的混排。
- 下钻通道断裂：`jarvis-tools` 没有任何 todo / task 查询命令，模型看到摘要后无法取细节。
- 限流散在四处（`snapshotFactLimit=50`、`cfg.Extract.FactLimit`、`cfg.Extract.OpenTodoLimit`、`maxPriorRunsInPrompt=5`），且 M3 超预算时砍的是原始会话消息，世界数据一条不动。

## 2. 已拍板的设计决定

实施时以此为准，不要另作取舍。

1. **M5 侧保持冻结。** 首轮只投影当前项目、群、交办人和引用消息 ID，不新增"当前时刻世界切片"的装配。创建时完整背景与最新世界信息都由 M5 在确有需要时调工具获取。
2. **todo 不加摘要字段。** 进上下文只有 `todo_id` / `action_type` / `title` / `status`，细节靠新增的 `get-todo` 下钻。
3. **`daily_digest` 完全不碰。** 它是给人看的日报，与本方案无关，也不进任何提示词。不要复用、不要改造、不要给它加 project scope。
4. **推送层不设天窗。** "按天"是下钻维度（工具的 `--date`），不是推送维度。
5. **fact 每个主体两层**：今天的明细事实最多 10 条（排除 rollup），加前一天的 1 条 rollup。更早的一律靠工具查。
6. **person 主体只取关键人**：交办人 assigner、`is_leader` 的参与者、本轮实际发言者，三者取并集后按上限截断。
7. **`subject_type="task"` 的事实不推进上下文。** 任务进展走 `task.summary`，task 主体的事实由 `get-task` 一并带出。
8. **超长降级先砍世界数据**，每类留一个下限，全部到下限后才开始砍原始会话消息。
9. **rollup 覆盖前一天所有产出过明细事实的主体**，无事实的主体自然跳过，不需要白名单。
10. **rollup 产物写回 fact 表本身**，`source_kind="rollup"`，原始明细不删。不新建表。
11. **一份背景、每五个主体一个 Agent。** 每轮先用现有 `contextsnap.Assembler` 组装一次全局当前背景，按 `subject_type / subject_id` 稳定排序后每五个主体切一批；每批启动一个独立的 `DeepSeek-V4-Pro` Agent，一次输入、一次输出，最多产生五条 rollup。背景不按主体重建，也不让压缩 Agent 调工具。
12. **持续维护是一批一次 Agent。** message、TodoEvent、TaskEvent 只是同一批世界变化的不同机械投影，不按来源、任务或单条材料分别调用模型。
13. **10 万字符只做粗装箱。** 不计算 token，不截断单条材料，不为字符预算建立新状态机或分片协议。
14. **世界维护直接写工具。** Agent 自己查询当前世界，通过通用 CRUD 和 `append-fact` 完成真实写入；最终自然语言只作审计，不再承担 `facts` JSON 机器协议。
15. **游标以整次会话成功为边界。** Agent 失败不推进本批来源游标；重放时依靠写前查询、写后读回避免重复，不增加 checkpoint 表或自动格式重试。

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
- `GET /api/tasks`（`internal/api/tasks.go` + `internal/execute/store.go` 的 `TaskFilter`）同样增加 `from` / `until`，锚定 `COALESCE(last_progress_at, created_at)`。
- 新增 `GET /api/tasks/:task_id` 返回单个 `TaskView`。现在只有 `/runs` 和 `/events`，缺主体详情。

### 4.2 `scripts/jarvis-tools` 新增四个只读命令

输出 JSON，风格与现有命令一致（参考 `get-project` 如何把项目与 facts 合并成一份输出）：

- `list-todos [--date YYYY-MM-DD] [--status S] [--limit N]`
- `get-todo --id N` —— 带上 `description` / `context` / `open_questions` / `resolution`，这些是上下文里被省略的部分。
- `list-tasks [--date YYYY-MM-DD] [--status S] [--limit N]`
- `get-task --id N` —— 合并四样东西：task 详情（含 `source_payload` / `summary` / `background`）、最近若干 run 摘要与 effects、task_event 时间线、以及 `subject_type=task` 且 `subject_id` 等于该 task 的 facts。

`--date` 的解析照抄 `list-facts` 的实现（`scripts/jarvis-tools:908` 附近）：校验 `YYYY-MM-DD`，在本地时区算出 `from` 与次日 `until`，失败 fail-fast。

### 4.3 `internal/toolcatalog/catalog.go` 注册

四个命令注册到 `StageExtract`、`StageExecute`、`StageChat`。M3 和 M5 执行都可按需调查。

## 5. 持续世界维护（current）

### 5.1 所有权与职责

持续世界维护由 `internal/factengine` 负责，运行在 M2→M3→M5 关键路径之外。它不新增第二套世界状态，而是维护现有 Principal、Person、Project、Group、KeyMatter、ManagedResource、Fact 和 RelationFact。

Go 只负责以下机器边界：

- 从 `message` 投影原文，从 `todo_event`、`task_event` 投影状态和最终产物；Todo/Task 背景仍保存在原表，不重复送入世界维护；
- 为三个来源分别保存消费游标；
- 按配置调度、限制粗粒度批量，并保证同一调度器内不重入；
- 启动一次完整 Agent 会话，记录成功或失败；
- 会话成功后推进实际送入本轮的来源游标。

哪些内容值得长期记住、应该更新当前画像还是追加历史 Fact、多个材料是否描述同一件事，都由 Agent 结合当前世界状态判断。不得在 Go 中增加按来源、实体类型或事实类别分流的语义规则。

### 5.2 一轮调用粒度

一次 `Worker.ExtractOnce` 最多启动一个 Agent Session：

1. 按注册顺序读取 message、todo、task 三个来源游标之后的新材料；Message 首次接入从当前最大 ID 起步，Todo/Task 从已有事件继续消费。
2. 每个来源最多读取 `factengine.batch_limit` 行，当前基线为 50。消息可按对话窗口组成 `SourceUnit`，Todo/Task 可按自然日和事件结果组成 `SourceUnit`；这些只是材料边界，不是 Agent 调用边界。
3. 把所有选中的 `SourceUnit` 合成一个 `WORLD_CHANGES` 用户输入，只启动一个 `DeepSeek-V4-Flash` Agent。
4. 没有新增材料时不调用 Agent。

因此“每一个任务调用一次”“每条事实调用一次”“每个来源调用一次”都不是现行语义。一轮有多少 Message、Todo、Task 不决定调用次数，装箱后的整批才决定一次调用。

### 5.3 80 万字符粗装箱

`factengine.max_material_chars` 当前为 `800000`，只计算 `WORLD_CHANGES` 用户材料的 Unicode 字符数，不估算 token，也不把系统提示词和工具目录计入这个材料预算。

算法保持简单：

1. 先按 `batch_limit` 读取各来源并渲染完整批次。
2. 超过 80 万字符时，把各来源共同的候选行上限减半后重新读取，直到批次不超限或行上限降到 1。
3. 每来源一行合起来仍超限时，只保留能完整装下的来源批；装不下的来源不推进游标，留到下一轮。
4. 第一条完整材料自身超过 80 万字符时，允许整条超过预算进入 Agent，绝不截断原料。

这里故意不引入 tokenizer、精确 token 计算、跨轮分片表、材料摘要器或动态模型路由。80 万字符是保护单次调用规模的粗判断，不是严格上下文上限。

### 5.4 Agent 输入、背景与工具

世界维护 Agent 的显式输入是本轮消息原文和 Todo/Task 最终产物增量。Todo/Task 已持久化的背景、快照、来源 payload、计划和执行 prompt 不重复进入本阶段；Agent 通过统一工具目录按需读取 Principal、项目、人物、群、事项、资料、近期 Fact 和关系，再直接调用对应 CRUD，并把同一轮新增 Fact 通过一次 `append-facts-batch` 写入。

稳定行为由 `conf/prompts/fact-extract-system-prompt.md` 所有，工具能力由 `internal/toolcatalog` 和 `scripts/jarvis-tools` 所有：

- 写前查询现值和近期事实，确认确有新增或变化；
- 当前画像、资料和关系用通用 CRUD 维护；
- 决定、交付、进展、阻塞、承诺和方向变化聚合后用一次 `append-facts-batch` 记录，批量失败时不回退到逐条写入；
- 写后立即读回；
- 没有新认知时不写，最终回复 `NOTHING`；
- 不创建或推进 Todo、Task、ScheduledTask，不修改外部系统。

`JARVIS_AGENT_STAGE=factengine` 下调用 `append-facts-batch` 时，工具会逐项补齐发生时间并默认写入 `source_kind=factengine`，再把 CLI 的 `source` 规范化为 API 的 `source_kind`。最终回复是自然语言审计，Go 不从中解析 Fact，也不因格式问题自动重试。

### 5.5 成功、失败与重放

整次 Agent 会话成功后，Worker 才按实际纳入本轮的来源分别推进游标；因预算被推迟的来源保持原游标。Agent 或提示词读取失败时，本批所有来源游标都不推进。

工具写入和游标推进不使用跨步骤事务。若 Agent 已完成部分工具写入后失败，下轮会重放同一批材料，Agent 必须先查现值避免重复；若多个游标顺序推进时中途失败，已推进来源不重放，未推进来源下轮继续。系统选择暴露真实失败，不增加隐藏问题的 fallback。

### 5.6 模型、调度与观测

- 世界维护模型：`factengine.model=DeepSeek-V4-Flash`，临时追赶积压时使用 `factengine.reasoning_effort=medium`。
- 当前调度：`factengine.schedule=@every 1m`，这是临时追赶期的可配置运行频率，不是事实语义边界。
- 当前超时：1200 秒。
- sandbox：`danger-full-access`，以便使用本地通用工具维护内部世界。
- cron 日志记录 `calls`、`units`、`material_chars`、各来源游标和最终结果字符数。
- 手动验收入口：`jarvis-server -extract-facts-once`。

调度不要求固定 15 分钟。调整频率只需考虑平均材料到达量、单轮耗时和成本；不要为改变频率复制第二条处理链路。

## 6. 事实日压缩（current，旁路）

### 6.1 位置

实现位于 `internal/factengine/rollup.go`，复用 factengine 的 db、提示词读取和调度模式。它与持续世界维护职责分开：世界维护理解增量材料并可用工具修改整个内部世界；Rollup 只把已经写入的明细 Fact 压成按主体、按日的一段话。

### 6.2 逻辑

每天定时运行，处理前一个自然日（本地时区）：

1. 找出前一天产出过明细事实的所有主体：`SELECT DISTINCT subject_type, subject_id FROM fact WHERE occurred_at >= ? AND occurred_at < ? AND (source_kind IS NULL OR source_kind <> 'rollup')`。
2. 主体非空时，用现有 `contextsnap.Assembler.AssembleConversation(ctx, contextsnap.AssembleOptions{})` 组装一次全局当前背景。它与 `/api/context`、Task 创建复用同一份 Snapshot 语义；本轮所有 Agent 收到完全相同的背景 JSON 和 `captured_at`。背景读取失败整轮 fail-fast，不用空背景继续。
3. 按 `subject_type / subject_id` 稳定排序，每五个主体切成一批。对批内每个主体读出那天全部明细事实并按时间正序排列，附上主体类型、ID 和名称；不要按字符数或事实条数继续切片。
4. 每批启动一个独立的 `DeepSeek-V4-Pro` Agent。一次输入为「同一份全局背景 + 最多五个主体及其全部明细事实」，一次输出为 `{"rollups":[...]}`。Go 校验输入主体与输出主体一一对应、无重复、无额外主体且 description 非空；整批校验成功后才开始写入。
5. 对每条结果，**先删除该主体该天已有的 rollup 记录，再写入新的一条**——这是重跑幂等的方式。不用事务，按顺序写，中途出错 fail-fast 报错退出、下次重跑（AGENTS.md §5）。
6. 写入的 fact：`subject_type` / `subject_id` 保持原主体，`occurred_at` 取被压缩那天的本地 00:00，`source_kind = "rollup"`，`description` 为模型返回的那段话。

原始明细一条都不删。压缩只是加了一层更粗的记录，`list-facts --date` 下钻时仍能看到那天的全部原文。

一批模型失败或输出校验失败时，该批不写入并继续后续批次；整轮结束后汇总返回错误。成功批次保留，手动重跑同一天时按上述替换语义重新生成。不增加批次表、游标或自动重试状态机。

### 6.3 提示词

按 AGENTS.md §6：在 `internal/textstore/defaults.go` 注册稳定 key（如 `fact-rollup-system-prompt`），正文提交到 `conf/prompts/fact-rollup-system-prompt.md`，运行时通过注入的 `textstore.Reader` 实时读取，缺失或空正文直接报错。

提示词只描述角色与稳定行为：分别把最多五个主体某一天的事实压成各自一段能独立读懂的话，讲清那天定了什么、推进到哪、留下什么没解决；不要罗列、不要评价、不要编造原文没有的内容。全局背景只用于理解 Principal、项目、当前事项和术语，目标日期发生了什么只能依据 `DETAIL_FACTS`，不得把背景里的后来状态倒灌进历史。压缩 Agent 不调用工具、不维护世界模型。

### 6.4 配置与手动触发

世界维护使用 `factengine.model=DeepSeek-V4-Flash` 和 `factengine.reasoning_effort=medium`，日压缩单独使用 `factengine.rollup_model=DeepSeek-V4-Pro`，两者不共享 Agent Session 或 sandbox。当前 Rollup 调度为每天本地时间 02:00，处理前一自然日；`POST /api/fact-rollups/generate` 接受 `date`，用于手动验证与补算。

## 7. M3 推送层改造

### 7.1 fact 改成两层装载

改造 `internal/extract/worker.go` 的 `loadFacts`。主体列表从"group + project"扩展为"group + project + 关键人"，每个主体做两次查询：

- 今天的明细：`From` = 今天本地 00:00，`Until` = 次日 00:00，`ExcludeSourceKind` = rollup，`Limit` = 每主体今日上限。
- 前一天的 rollup：`From` = 昨天本地 00:00，`Until` = 今天 00:00，`SourceKind` = rollup，`Limit` = 1。

复用现有的 `cfg.Extract.FactLimit` 作为"每主体今天的明细上限"，默认值改为 10，并在注释里写明它现在的语义。不要为此新增第二个近义配置项。

### 7.2 关键人主体

`ParticipantContext`（`internal/extract/pipeline_types.go:95`）现在只有 `open_id`，没有 `person.id`，需要解析。`internal/factengine/store.go` 已有把 open_id 映射成 person 主体的写法，照搬到 `internal/extract/pipeline_store.go`，给 `ParticipantContext` 加 `PersonID *uint64`。

关键人 = 交办人 assigner + `IsLeader` 的参与者 + 本轮实际发言者，三者取并集，去重后按上限截断（新增配置项，默认 5）。未录入 person 表的参与者解析不到 id，直接跳过，不报错。

### 7.3 新增 task 段落

新增 `loadRecentTasks`：取最近有进展的 task，按 `COALESCE(last_progress_at, created_at) DESC` 排序，限量（新增配置项，默认 10）。范围限定为与当前会话相关的 task——通过 `todo_id` 关联到 `todo`，取 `todo.group_id` 等于当前群或 `todo.project_id` 等于当前项目的；没有 todo 的 task（scheduled_task / manual）不纳入。

每条只渲染 `task_id` / `title` / `status` / `summary` / `last_progress_at`。这让上一轮加的两个只写字段真正活起来。

`internal/contextsnap/snapshot.go` 的 `Snapshot` 增加对应字段（如 `RecentTasks`），`internal/extract/snapshot.go` 的 `buildContextSnapshot` 一并冻结进快照，这样 M5 也能看到（即使是 M3 时刻的值）。

`internal/extract/prompt.go` 增加渲染段落，标题风格与现有段落一致，如 `# 最近有进展的任务（仅作背景）`。

### 7.4 todo 段落不变

维持现有字段与 `cfg.Extract.OpenTodoLimit`，不加摘要字段。

## 8. 超长降级顺序反转

现在 `BuildPrompt`（`internal/extract/prompt.go:61-71`）超 `MaxChars` 时循环丢弃会话上下文消息，世界数据一条不动。改成先削世界数据：

1. 逐级收紧世界数据：person 主体的事实 → other_projects → recent_tasks → open_todos → group / project 的事实。每类保留一个下限（如各留 3 条），不砍到零。
2. 世界数据全部到下限后，才开始按现有逻辑丢弃会话上下文消息。
3. 最后仍然超限，保持现有行为——报错 fail-fast，不静默截断。

理由：世界摘要能靠工具查回来，原始对话是判断的一手证据，不可替代。

实现上给 `renderUserPrompt` 传一个降级档位参数，逐级收紧；不要在渲染函数里散落硬编码。

## 9. 提示词补渐进式加载契约

`conf/prompts/m3-system-prompt.md` 与 `conf/prompts/m5-system-prompt.md` 各加一段，说明上下文里给的是摘要层：今天的事实是明细、更早的是每天一条压缩、任务只有一句进展、线索只有标题和状态；需要细节自己去查，按主体和日期取。

按 AGENTS.md §6，只描述行为，**不要点工具名**——工具说明由 `internal/toolcatalog` 和 `jarvis-tools --help` 维护。仓库里有一条测试会校验系统提示词与工具目录分离，点了工具名会失败。

## 10. 范围之外

- 不新增 rollup 结果页；运行设置只增加独立的 `rollup_model` 字段，系统任务卡片显示真实压缩模型。
- 不碰 `daily_digest`、不碰 `mem0`、不碰 `Snapshot.Memories`。
- 不为 M5 新增世界切片装配。
- 不回填历史 rollup。

## 11. 验收

- `go build ./...`、`go test ./...` 全绿；`gofmt -w` 无残留 diff。
- 前端未改动则无需 `tsc`；若改了 `web/`，跑 `npx tsc --noEmit`。
- 单测至少覆盖：`FactFilter` 的 source_kind 等值与排除（含 NULL 放行）、rollup 每五个主体切批、每轮背景只组装一次且各批相同、输入输出主体严格对应、批失败不饿死后续批次、重跑幂等（同主体同天不叠加）、M3 两层装载（今天明细排除 rollup、前一天只取 rollup）、关键人并集与上限截断、降级顺序（世界数据先到下限、会话消息后砍）。
- 构建或重启主服务必须走 `./scripts/rebuild-server.sh`（AGENTS.md §7）。
