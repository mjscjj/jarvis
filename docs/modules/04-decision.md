# M5 判断环节技术方案

> **契约（2026-07-24 起）**：模型输出只固定 `disposition + plan + payload` 外壳，plan/payload 内部保持宽松，以 `docs/design-loose-semantic-contract.md` 为准。不要为固定 factor / clarification / `PlanDraft` 这类旧 DTO 新增兼容层。

> 所属项目：基于飞书的本地个人 Jarvis 管家系统（用户：字节研发工程师 chujiejie.1）
> 隶属总纲：`docs/00-overview.md`（技术栈、7 实体、Todo/Task 拆分的权威定义在总纲）
> 技术栈：**Go 1.26 + Hertz + GORM + codex CLI**（判断）+ robfig/cron v3（补偿扫描）。**不引入 Eino/Kitex**。
> 当前流水线定位：采集(M2) → 提取 Todo(M3) → **判断 + Todo→Task 转化（M5 判断环节·本模块）** → 执行 Task（M5 执行环节）

> **判断环节不是独立流水线阶段**：它和执行环节同属 M5，共用一个工作队列和一个 worker 池，代码在 `internal/execute/decision_*.go`。两者的区别只在权限与目标——判断环节 read-only 只定"值不值得做"，执行环节工具全开真正落地。
>
> **【2026-07 变更，见 `docs/design-context-pipeline.md`】** Task 背景不再于固化时临时从 MySQL 拼装：M3 生成 Todo 时已**固化** `context_snapshot`，判断环节建 Task 时直接复用该快照。**不兼容旧数据**——实施时清空 `todo`/`task` 等表；判断环节强制要求 `context_snapshot` 非空，为空即 fail-fast 报错，**不保留临时拼接回退路径**。

---

## 0. 模块定位与边界

判断环节是 **Todo → Task 的唯一转化点**，回答的问题只有一个：**这条线索值不值得做？**

- **输入**：M3 产出的 `Todo`（行动线索/候选，可能模糊、信息不足）。
- **做什么**：把 Todo + M3 冻结的背景快照 + 历史判断结论组装成 prompt，交 codex（read-only）自己读上下文定 `disposition`。
- **输出**：`ready` → **固化生成一个 `Task`**（含背景快照 + 判断方向 + 原始线索）交给执行环节，Todo 进 `auto`；`drop` → Todo 进 `dropped`，终结，不建 Task。

**核心立场**：**线索永远不停下来等人。** 判断环节没有"挂起线索问用户"这一档——需要 principal 在多个方案里挑一个、或需要他提供某个只有他知道的事实，这条线索照样是 `ready`：问题写进 `decision_payload` 跟着 Task 走，由执行环节先把自己该做的调查做完，再带着完整结论去问。**唯一的人工闸门在 Task 上**（`awaiting_approval` / `needs_human`），不在线索层。

### 0.1 Todo → Task 转化契约

```
M3 产出 Todo(线索)
   │
   ▼
┌──────────── M5 判断环节 ────────────┐
│ ① 组装判断上下文:                    │
│    Todo + context_snapshot(M3 冻结) │
│    + 历史判断结论 + 共享记忆         │
│    + 工作规则 + Skill + 工具目录     │
│ ② codex exec(read-only) 定 disposition│
│    - ready → route auto             │
│    - drop  → route dropped          │
│ ③ ready 时同事务:                    │
│    - 复用 M3 的 context_snapshot     │
│      作为 task.background            │
│    - 写 plan / decision_payload      │
│    - 冻结 source_clue(M3 抽取原文)   │
│    - INSERT task(status=pending)     │
│    - todo.status = auto              │
└──────────────────┬──────────────────┘
                   ▼
             Task(可执行) → M5 执行环节
```

**关键规则**：
- **一个 Todo 最多生成一个 Task**（DB 层 `task.uk_task_todo` 唯一约束兜底）。
- **`background` 复用 M3 快照，不重新拼**：`context_snapshot` 为空即 fail-fast。`source_clue` 冻结 M3 抽取结论原文，让执行环节读到的是原始线索而不只是判断环节压缩过的方向。
- **建 Task 前不存在 Task**：Todo 在 `extracted → auto / dropped` 之间流转，只有 `auto` 那一刻才 INSERT task。`dropped` 的 Todo 永不产生 Task。
- **`plan` / `decision_payload` 不是冻结的契约**：它们是"当时最好的理解"。执行环节发现情况变了可以直接改，bump `version` 并写 `task_event`（见 `modules/05-execution.md`）。
- **`auto` 只负责生成 Task**：不代表已经授权申请权限、发消息或修改飞书；这些对外副作用要不要先请示，由执行环节的模型结合上下文判断。

### 0.2 上下游接口

| 方向 | 对端 | 输入/输出 | 说明 |
|---|---|---|---|
| 入 | M3 提取 | `Todo{action_type, target, context, open_questions, context_snapshot, extraction_result, group_id, project_id, assigner_open_id, is_leader_assigned}` | M3 只产 `extracted` 状态的 Todo |
| 出 | M5 执行环节 | 新建的 `Task{todo_id, background, source_clue, plan, decision_payload, status=pending}` | 执行环节只执行已固化的 Task |
| 判断 | codex CLI | `codex exec --sandbox <配置> --output-schema`（结构化 JSON 输出） | 见 §2 |
| 读写 | MySQL(GORM) | Todo 状态 + Task 生成 + 审计表 | 本地明文；审计 append-only |

### 0.3 子组件拆分（Go package `internal/execute/`，文件前缀 `decision_`）

```
M5 判断环节
├── DecisionWorker    decision_worker.go     批处理 + 实时单条入口(EvaluateTodo)
├── EvaluationSource  decision_worker.go     捞 extracted Todo(leader 优先, 旧证据优先)
├── CodexEvaluator    decision_evaluator.go  组装上下文 → 调 codex → disposition 映射为 route
├── CodexDecider      decision_codex.go      codex CLI 子进程封装(schema 约束输出)
├── BuildCodexPrompt  decision_prompt.go     prompt 组装 + 版本号(CodexPromptVersion)
├── loadPriorEvaluations decision_prior.go   从 todo_event 读历史判断结论
├── EvaluationStore   decision_apply.go      落库:Todo 状态 + 建 Task + 事件 + 审计
└── 锁/事件辅助       decision_helpers.go    行锁 Todo、写 todo_event、快照校验
```

---

## 1. 设计原则与 fail-fast 约束

1. **判断失败 → 不落库，等下轮重判。** codex 超时/报错/输出不符合 schema/返回未知 disposition，一律向上返回错误，Todo 留在 `extracted`。绝不猜一个 disposition，也不静默降级。
2. **不设阈值、不设灰区、不做规则快判。** 每条 `extracted` Todo 都交给 agent 自己读上下文判断。风险与价值在线索的具体内容里，不在 `action_type` 这种类型标签里。
3. **不为特定来源开分支。** 群消息、单聊、会议妙记、邮件走同一套判断，来源差异只能通过系统提示词、Skill 和工作规则表达。
4. **背景快照缺失即报错。** `context_snapshot` 为空不允许"临时查库拼一份看起来等价的背景"。
5. **身份漂移即报错。** 评估返回的 `todo_id`/`version` 与载入的不一致，立即失败，不基于陈旧信息固化。
6. **落库全在一个事务里**：Todo 状态推进 + `todo_event` + `decision_audit` +（`auto` 时）`INSERT task` 一起提交，保证"判定"与"生成 Task"原子。

---

## 2. 判断引擎：codex CLI（read-only）

### 2.1 为什么用 codex 而非 model API

判断环节要回答的是"这条线索是否值得固化成任务"，这**往往需要看项目代码、查飞书**（例如"按 XX 方案改鉴权"到底动哪些文件、这个群到底属于哪个项目）。codex/traex 有代码库上下文能力、能自己跑 `jarvis-tools`/`lark-cli`/`bytedcli`/`git` 找证据，判断质量比纯文本 model API 高。M2/M3 的高频抽取才用 model API。

### 2.2 输出契约（严格外壳 + 宽松内部）

`--output-schema` 约束模型只能返回三个键：

```json
{
  "type":"object",
  "additionalProperties":false,
  "required":["disposition","plan","payload"],
  "properties":{
    "disposition":{"type":"string","enum":["ready","drop"]},
    "plan":{"type":"string","minLength":1},
    "payload":{"type":"string","minLength":1}
  }
}
```

- `disposition` 是**权威的**：route 直接由它推导（`ready → auto`、`drop → dropped`），不再由我们从语义字段反推。未知值 → fail-fast。
- `plan` 是完整执行意图。判断环节**不规定它的语义形状**，`ready` 只要求它是非空 JSON 值。
- `payload` 装理由、证据、风险、留给 principal 的问题，以及未来模型自己想写的任何语义——扩展 payload 不需要改 Go 结构。

### 2.3 codex 调用形态

```
codex exec --ephemeral --sandbox <decide.codex_sandbox> --color never --json \
  --output-schema <tmp>/decision.schema.json --output-last-message <tmp>/result.json \
  --model <codex.model> [--cd <repo_path> | --skip-git-repo-check] -
```

- prompt 经 **stdin** 输入，不进 argv。
- 有关联 repo 时带 `--cd` 提供代码库上下文；无 repo 则 `--skip-git-repo-check`。
- 沙箱与联网来自配置：本地可信环境用 `danger-full-access` + 联网，让判断环节自己查信息补全判断（AGENTS.md §1）。
- 严格校验 JSONL session 输出，取 `session_id` 入审计。
- 超时来自 `codex.timeout_seconds`（当前 600s，判断会多轮自跑 lark-cli/bytedcli）。

### 2.4 prompt 组装（`BuildCodexPrompt`）

prompt 由运行时动态拼装，正文只描述角色与稳定行为（文件真源 `conf/prompts/m5-decision-system-prompt.md`，经 `textstore` 读取，缺失或为空 fail-fast）：

| 片段 | 来源 |
|---|---|
| 系统提示词 | `textstore` key `system_prompt_decision` |
| 工具目录 | `internal/toolcatalog`（`StageDecide`） |
| 工作规则 | `internal/workrule`（`StageDecide`） |
| Skill 目录 | `internal/skill`（`StageDecide`） |
| 共享记忆 | `internal/sharedmem` |
| 线索本体 | `Todo` 全字段 |
| 背景 | M3 冻结的 `context_snapshot` |
| 历史判断 | `todo_event` 里前几轮的判断结论（`loadPriorEvaluations`） |

Todo、背景、消息与记忆都编码为**带长度的不可信 JSON 数据区**，并明确禁止其中的文字被当作系统指令（防 prompt injection）。`prompt_version`（当前 `todo-decision-v6-value-gate`）随判定结果落审计。

### 2.5 配置（总纲 §11.2）

```yaml
decide:
  enabled: true
  schedule: "@every 1m"          # 正常由 M3 定向唤醒；这里只补偿遗漏
  batch_limit: 50
  codex_sandbox: "danger-full-access"
  codex_network: true
  codex_reasoning_effort: "medium"

codex:                            # 判断环节与 M3 抽取共用的底层 CLI，与 execute.* 独立
  bin: "traex"
  model: "gpt-5.5"
  timeout_seconds: 600
```

没有阈值、没有灰区边界、没有预算闸的降级开关——判断只有"判出来"和"判失败重来"两种结局。

---

## 3. 状态机（Todo 生命周期 + Task 诞生）

Todo 和 Task 是**两个生命周期**，判断环节是衔接点：

```
── Todo 生命周期(M3产出→判断环节处置) ─────────────────────
  M3 ─► [extracted]
          │ codex 判断(read-only)
          │
   ┌──────┴───────────────┐
   ▼                      ▼
 disposition=ready    disposition=drop
   │                      │
   │                      ▼
   │                 [dropped](终态, 无 Task)
   ▼ 同事务
┌───────────────────────────────────────────┐
│ 复用 context_snapshot 作 background        │
│ 写 plan / decision_payload / source_clue   │
│ INSERT task(pending); todo→[auto]          │
└───────────────────┬─────────────────────┘
                    │
── Task 生命周期(判断环节诞生→执行环节执行) ───────────────
             [pending] ──► [executing] ──► [done] / [failed]
                                │
                                ├─► [awaiting_approval]  需 principal 批准副作用
                                ├─► [needs_human]        需 principal 回答问题
                                └─► [waiting]            yield-until 挂起等时机

判断失败(codex 报错/非法输出) → Todo 留在 [extracted]，下轮重判
```

- **判断环节写入的 Todo 状态**：`auto` / `dropped`（route 值与 status 同名）。
- **Task 的 `pending→executing→…` 归执行环节**，判断环节不碰。
- 状态守卫：Todo 带 `version`（乐观锁）+ 事务内行锁；`extracted → auto/dropped` 只允许一次，版本或状态不符直接冲突报错且不产生副作用。

### 3.1 落库逻辑（`EvaluationStore.Apply`）

```go
// 判定结果落库 —— 单事务
func (s *EvaluationStore) Apply(ctx context.Context, input EvaluationInput) (*EvaluationResult, error) {
    return s.db.Transaction(func(tx *gorm.DB) error {
        lockTodo(tx, input.TodoID, &todo)                  // 行锁
        if todo.Version != input.ExpectedVersion { ... }   // 乐观锁冲突 → fail
        if todo.Status != "extracted" { ... }              // 状态冲突 → fail
        tx.Model(&domain.Todo{}).                          // status = auto | dropped
            Where("id = ? AND version = ? AND status = ?", todo.ID, input.ExpectedVersion, "extracted").
            Updates(map[string]any{"status": input.Route, "version": gorm.Expr("version + 1")})
        createTodoEvent(tx, todo.ID, "extracted", input.Route, eventDetail) // 含 plan/payload/prompt_version
        tx.Create(&domain.DecisionAudit{...})                              // append-only 留痕
        if input.Route == RouteAuto {
            background := requireContextSnapshot(&todo)     // 空即 fail-fast
            createAutoTask(ctx, tx, now, &todo, input.Plan, input.DecisionPayload, background)
        }
        return nil
    })
}
```

- **`auto` 必须带非空 plan**：`validateEvaluationInput` 校验；无 plan 的 `ready` 视为非法输出。
- **`confirmed_by = "m5_decision"`**：Task 上标明固化来源是判断环节自己，不是用户。
- **一 Todo 一 Task**：建 Task 前先查 `todo_id` 是否已有 Task，加上 `uk_task_todo` 唯一约束双重兜底。
- 建 Task 后额外写一条 `auto_task_created` 的 `todo_event`，串起线索到任务的追溯链。

### 3.2 触发方式

- **实时**：M3 提交 Todo 后按 ID/version 调 `EvaluateTodo`，版本不符或重复通知在调模型之前就失败。
- **补偿**：`decide.schedule` 定时扫 `extracted`（leader 交办优先、旧证据优先），只兜漏。
- **手工**：`--decide-once` 跑一轮用于验收。

---

## 4. 数据模型（MySQL / GORM）

Todo、Task 的完整 DDL 在总纲 `docs/00-overview.md` §2.4 定义。判断环节只需：
- 读写 `todo`（状态流转）；
- `ready` 时 `INSERT task`；
- 写 `todo_event` 与 `decision_audit`（后者为本模块私有）。

```sql
CREATE TABLE decision_audit (
  id                       BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  todo_id                  BIGINT UNSIGNED NOT NULL,
  task_id                  BIGINT UNSIGNED NULL,
  ts                       DATETIME NOT NULL,
  route                    VARCHAR(16) NOT NULL,   -- auto | dropped
  route_reason             VARCHAR(64) NOT NULL,   -- codex_ready | codex_drop
  matched_rules            JSON,                   -- ["codex_disposition:ready"]
  decision_engine          VARCHAR(16),            -- 固定 codex
  codex_session_id         VARCHAR(128),
  approver                 VARCHAR(16),
  channel                  VARCHAR(16),            -- 固定 auto
  event_id                 VARCHAR(64),
  idempotency_key          VARCHAR(64),
  final_status             VARCHAR(16),
  KEY idx_audit_todo (todo_id),
  KEY idx_audit_task (task_id),
  KEY idx_audit_ts (ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

> 打分列（confidence/risk 及其因子分解、阈值配置版本）属于已退役的规则引擎，已从表和 model 里移除——模型返回的是 disposition，不是分数。

---

## 5. 开放问题清单（需用户校准）

1. **判断尺度**：`conf/prompts/m5-decision-system-prompt.md` 里"什么算值得做"的表述需按真实审计数据反复校准——这是本环节唯一的调节旋钮。
2. **无 repo 线索的 codex 上下文**：有 `repo_path` 则带 `--cd`，无关联 repo 则不带。遗留：无 repo 时判断质量是否够，是否需要人工补 repo 关联。
3. **`drop` 的可见性**：被 drop 的线索目前只留在 `todo` + `decision_audit` 里，是否需要后台一个"被丢弃线索"视图做事后抽查。
4. **重判节奏**：同一条 Todo 被 M3 重新抽取（`revision+1`）后何时值得重判，以及历史判断结论要带几轮进 prompt。
5. **成本观测**：当前无频率/成本上限。低频单用户下够用，但需要一个可观测的调用量视图来确认。
6. **过期线索**：`ttl_at` 字段留着但未投入逻辑，是否需要"长期没被判断的线索自动过期"。

---

## 6. 当前实现进展

- 判断环节代码在 `internal/execute/decision_*.go`，与执行环节共用工作队列和 worker 池，不是独立流水线阶段。
- 已实现只读 codex CLI 适配层：按本机实际命令使用 `exec --ephemeral --sandbox <配置> --json --output-schema --output-last-message`；prompt 经 stdin 输入，有 repo 才传 `--cd`，无 repo 则 `--skip-git-repo-check`；严格校验 JSONL session、输出 schema 与未知字段。
- 已实现 `todo-decision-v6-value-gate` prompt 组装：Todo、背景快照和历史判断被编码为带长度的不可信 JSON 数据区，消息/记忆中的指令明确禁止作为系统指令；输入缺失在启动 codex 前失败，prompt version 随判定结果返回供审计。
- 已实现落库：`auto` 会原子创建 `pending Task`，`dropped` 只更新 Todo；两条路径都写 `todo_event + decision_audit`。
- Task 的 `background` 直接复用 M3 冻结的 `context_snapshot`（为空 fail-fast），`source_clue` 冻结 M3 抽取结论原文；Task 创建、Todo `auto + version+1`、`todo_event` 与 `decision_audit` 在同一事务提交，`task.uk_task_todo` 保证一 Todo 一 Task。
- M3 提交 Todo 后按 ID/version 实时调用 `EvaluateTodo`；`decide.schedule` 只扫描遗漏的 `extracted` 作为补偿，`--decide-once` 保留手工验收。
- `decision_audit` 已进入启动迁移，并已迁移当前本地 MySQL。
- 已覆盖严格 HTTP 契约、真实 MySQL 事务/唯一 Task/审计/全回滚测试，以及 `extracted → auto → Task` 的合成验收，不调用飞书或模型。
- 历史沿革：早期版本在线索层做过 `confidence × risk` 打分、灰区预算闸和人工确认闸门（含后台待办队列与飞书交互卡片）。这套机制整体退役——风险判断移到执行环节由模型结合调查结果做，线索层只保留"值不值得做"。

---

## 7. 参考资料

- codex CLI：`codex exec --sandbox ... --output-schema` 做结构化判断，退出码非 0 / 输出非法即 fail-fast，不猜结论。
- `docs/design-loose-semantic-contract.md`：严格外壳 + 宽松语义 payload 的契约原则。
- `docs/design-context-pipeline.md`：`context_snapshot` 一次组装、全程传递、下游不重建。
- `docs/design-file-backed-text-config.md`：系统提示词以本地 Markdown 为唯一真源。
- AGENTS.md §2 / §4：不为特定来源开专用链路；审批判断归模型，代码只提供载体与留痕。
