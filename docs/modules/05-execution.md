# M5 任务执行模块 技术方案

> **版本说明**：本次项目重大调整后重写。隶属总纲 `docs/00-overview.md`（顶层设计与跨模块契约以总纲为准）。
> **技术栈**：Go 1.26（后端）+ robfig/cron v3（调度）+ GORM/MySQL 8（存储），子进程 codex CLI / lark-cli / ripgrep 走 `os/exec`。**不引入 Eino/Kitex**。
> **消费物**：M5 消费 **Task**（`status=pending` 的明确可执行任务），不消费 Todo。Task 由 M4 把 Todo 确认后固化生成（含问题背景快照 `background` + 明确方案 `plan`）。
> **当前 MVP（2026-07-19）**：先采用人工执行闭环。`GET /api/tasks` 展示待执行 Task，用户完成实际动作后调用 `POST /api/tasks/:task_id/finish` 回写 `done/failed + result`。暂不实现 executor 注册表、自动子进程、并发队列、`execution_*` 三表、飞书通知和 mem0 回写。
>
> 所属系统：基于飞书的本地个人 Jarvis 管家（字节研发工程师 chujiejie.1 本地 Mac）。
> 模块定位：流水线 `采集(M2) → 提取Todo(M3) → 人工确认生成Task(M4) → 【人工执行并回写Task(M5)】 → 回写后台(M0)` 中的执行环节。
> 设计原则（全程遵守）：①本地可信明文存储；②fail-fast 暴露问题、不静默降级；③不乱兼容旧数据/逻辑；④模块化；⑤优先官方包与已有实现。

---

## 0. 模块定位与边界

### 0.1 上游契约（M4 → M5）

M4 是 `Todo → Task` 的唯一转化闸门：把 Todo 确认（用户确认或自动确认）后**生成 `Task`（`status=pending`）**。M5 只消费 `status=pending` 的 Task，每个 Task 已是"方案明确、含问题背景快照"的可执行任务，关键字段（权威定义见总纲 `docs/00-overview.md` §2.4 Task 表）：

- `action_type`：动作类型（决定路由到哪个 executor）。
- `background`：**问题背景快照（JSON）**——确认时刻固化的 `{project, 关键消息, 相关记忆, 交办人}`，保证执行时可复现、不受源数据后续变化影响。
- `plan`：**明确方案（JSON，用户确认过）**——M4 确认时刻固化的 `{steps, params, 依据}`。**M5 按 `plan` 执行**。
- `slots`：执行所需结构化参数（M3/M4 已补齐，M5 只做校验与执行，不再做语义提取）。
- `action_hash`：**方案指纹**——执行前做漂移校验，防止 M4 确认后 `plan` 被篡改。
- `confirmed_by` / `confirmed_at`：确认来源（`user | system(auto)`）与时刻。
- `autonomy_mode`：本 Task 的自治模式（`autopilot | copilot | draft`）。
- `project_id`：关联项目（可含本地 repo 路径，来自 `Project.repos`）。
- 关联 `Resource`（按需）：如 `summary_post` 读妙记 `Resource`、`code_change` 的 repo 来自 `Project.repos`。

> **M5 与 M4 的判断边界（重要）**：M5 拿到的 Task 已经方案明确（`plan` 是 M4 确认时固化的明确方案），M5 **不再做"内容层面该不该做"的判断**（M4 已确认）。M5 只做**"执行层面对外/高危动作的最终放行"**。M5 按 `plan` 执行，执行前先做 `action_hash` 漂移校验（见 §5.0）。

### 0.2 下游契约（M5 → Task / M0 / mem0 / 用户）

- **回写 Task**：执行结果写入 `Task.execution_result`（JSON：`{summary, artifacts, error}`）+ `Task.status = done | failed`。
- **详细留痕**：另写 `execution_run` / `execution_step` / `execution_artifact` 表（总纲支撑表已列，供可观测与排障）。
- 可选：飞书通知本人（`notify_self`）。
- 可选：结论型结果回写 mem0，作为后续流水线上下文。

### 0.3 明确不属于 M5 的职责

- 不做行动项抽取 / 打分 / 路由 / `Todo→Task` 转化（M3/M4）；M5 不碰 Todo。
- 不做"该不该做这件事"的业务判断（M4 已确认并固化进 `plan`）；M5 只判断"这个对外/高危动作是否需要最终放行确认"。
- 不做数据采集与记忆化（M2）。

---

## 1. 执行引擎架构

### 1.1 设计参考（2026 最佳实践）

综合 2026 年 agent 工具执行的主流范式（Tool Execution Layer / AI Agent Runtime Policy / OWASP AISVS C09 High-Impact Action Approval / Plan-Validate-Execute）：

1. **执行层是一道确定性控制关卡**：Task 不直接调工具，先经过统一的「校验（含 action_hash 漂移校验）→ 授权/护栏 → 执行 → 结果归一化」管线。
2. **风险分层 + 默认拒绝对外/破坏性动作**：只读默认放行，写/对外/不可逆动作要显式放行。
3. **幂等键**：对外副作用动作用 idempotency key 防重复执行（lark-cli 原生支持）。
4. **确认边界由编排层强制**，而非散落在业务代码里：hold 的动作暂停、持久化状态、等待人工信号再恢复。
5. **fail-fast**：失败返回受控错误，暴露给上层，不在执行层内部静默重试/降级掩盖。

> 本模块是**本地个人单租户**系统，Go 单体进程内实现，不引入 Temporal/LangGraph 等重编排；用 **robfig/cron v3** 触发 + MySQL 持久化状态（`Task.status` + `execution_run`）+ 飞书交互卡片做轻量 human-in-the-loop 即可满足。是否需要更强的 durable execution 列入开放问题。

### 1.2 分层结构

```
                    M4 生成的 Task (status=pending)
                                 │
┌────────────────────────────────▼─────────────────────────────────────────┐
│                          M5 执行引擎 Execution Engine (Go)                    │
│                                                                            │
│  ① ExecutionScheduler  调度层                                               │
│     - 拉取 status=pending 的 Task，入执行队列                                 │
│     - robfig/cron v3 触发 + worker 并发控制 + 超时管理 + 幂等(run 去重)        │
│                                 │                                          │
│  ② GuardLayer  护栏预检层        ▼                                          │
│     - 执行前校验 action_hash(与当前 plan 一致)，不一致 → failed/打回          │
│     - 依据 risk_tier × autonomy_mode × 配置决策矩阵 → allow / hold / deny    │
│     - hold → 生成确认请求(飞书卡片/后台)，Task 置 awaiting_confirm            │
│     - deny → 直接 failed(原因透出)                                          │
│                                 │ allow / 已确认                            │
│  ③ ExecutorRegistry  注册表      ▼                                          │
│     - action_type → Executor 实例（插件化，新增动作只注册不改引擎）           │
│                                 │                                          │
│  ④ Executor  执行层              ▼   (按 Task.plan 执行)                    │
│     - Plan(ctx)  → ActionPlan（可预览、可 dry-run 的动作计划）               │
│     - Run(plan)  → ExecutionResult（调子进程/CLI/LLM，产出结果+产物）         │
│         后端子进程(exec.Command)：codex/cursor-agent · lark-cli · ripgrep    │
│                                 │                                          │
│  ⑤ ObservabilityRecorder  可观测（贯穿②③④）                                │
│     - 每步留痕：命令/输出/耗时/退出码/token → execution_step + 本地日志       │
│                                 │                                          │
│  ⑥ ResultWriter  回写层          ▼                                          │
│     - ExecutionResult → Task.execution_result(JSON) + Task.status          │
│     - 详细留痕 → MySQL(execution_run/step/artifact)                        │
│     - Task.status = done|failed → M0                                       │
│     - 产物落地：diff/报告/日志 → 本地 runs/；message_id/event_id/url → ref   │
│     - 可选：飞书通知本人 · 结论回写 mem0                                      │
└───────────────┬──────────────────┬───────────────────┬────────────────────┘
                ▼                  ▼                   ▼
          M0 管理后台          mem0 记忆(可选)      飞书通知本人(可选)
```

### 1.3 同步 / 异步 / 超时 / 并发

| 维度 | 策略 |
| --- | --- |
| 同步执行（秒级） | `summary_post` / `schedule_meeting` / `reply_message` / `notify_self`：请求-响应内完成，直接返回结果。 |
| 异步执行（分钟级） | `code_change`（codex 子进程）/ `investigate`（检索+联网）：入队交给 worker，`Task.status` 先置 `executing`，完成后回写 `done`/`failed`。 |
| 超时 | 每个 Executor 声明 `DefaultTimeout`；到时 **kill 子进程（`exec.CommandContext` + `context.WithTimeout`）→ status=failed + timeout 错误**（不重试掩盖）。 |
| 并发 | 全局 + 分动作类型并发上限（配置，Go 用带缓冲 channel / `golang.org/x/sync/semaphore` 控并发）；`code_change` 建议低并发（子进程重、写磁盘）。 |
| 幂等 | 一个 Task 一次成功执行只产生一个 `done` 的 `execution_run`；重复触发先查是否已有 `done`/`executing`。对外发送再叠加 lark-cli `--idempotency-key`。 |

---

## 2. 核心数据结构与 Executor 接口定义

> 语言：Go 1.26。以下为**提议接口**（尚未落地代码），包 `internal/execute`。风格 fail-fast：校验不过直接返回 `error`（`ExecError`），不返回"部分成功"。
> 结构映射约定：`RiskTier/ExecMode/ExecStatus/ArtifactKind` 用常量 + `iota`；`ExecutionContext/ActionPlan/ActionStep/Artifact/StepRecord/ExecError/ExecutionResult` 用 `struct`；`Executor` 用 `interface`；子进程（codex/lark-cli/rg）用 `exec.Command` / `exec.CommandContext`。

### 2.1 枚举与基础类型（常量 + iota）

```go
package execute

// RiskTier 风险分层：只读默认放行，写/对外/不可逆动作要显式放行。
type RiskTier int

const (
	RiskT0SafeRead      RiskTier = iota // 本地/公开只读：grep、web 检索
	RiskT1TenantRead                    // 读飞书租户数据：读消息/文档/妙记
	RiskT2LocalWrite                    // 本地写/草稿/仅通知本人
	RiskT3ReversibleExt                 // 可逆对外写：建飞书任务、加评论、更新文档
	RiskT4ExternalEffect                // 对外副作用：发群消息、约会议、邀请他人
	RiskT5Destructive                   // 破坏性/高危：删除、git push、生产变更、lark high-risk-write
)

// ExecMode 执行模式。
type ExecMode string

const (
	ModeSync  ExecMode = "sync"
	ModeAsync ExecMode = "async"
)

// ExecStatus 单次执行(run)的状态。注意与 Task.status 区分：
// Task.status ∈ {pending, executing, done, failed, cancelled}（总纲定义）；
// run 级状态额外区分护栏结果 awaiting_confirm / denied，回写 Task 时映射为 executing/failed。
type ExecStatus string

const (
	StatusExecuting       ExecStatus = "executing"        // 执行中
	StatusDone            ExecStatus = "done"             // 成功
	StatusFailed          ExecStatus = "failed"           // 失败(必带 ExecError)
	StatusAwaitingConfirm ExecStatus = "awaiting_confirm" // 被护栏 hold，等人工确认
	StatusDenied          ExecStatus = "denied"           // 被护栏 deny
)

// ArtifactKind 产物类型。
type ArtifactKind string

const (
	ArtifactDiff          ArtifactKind = "diff"
	ArtifactBranch        ArtifactKind = "branch"
	ArtifactFile          ArtifactKind = "file"
	ArtifactReport        ArtifactKind = "report"
	ArtifactLink          ArtifactKind = "link"
	ArtifactFeishuMessage ArtifactKind = "feishu_message" // ref = message_id
	ArtifactCalendarEvent ArtifactKind = "calendar_event" // ref = event_id
	ArtifactFeishuTask    ArtifactKind = "feishu_task"     // ref = task_guid
)
```

### 2.2 执行上下文与动作计划（envelope 模式）

```go
// ExecutionContext 一次执行的全部外部依赖与配置，由引擎注入，Executor 只读使用。
type ExecutionContext struct {
	RunID        string             // 本次执行 id
	Task         *Task              // M4 生成的 pending Task（含 plan/background/slots/action_hash）
	WorkspaceDir string             // 本次执行的隔离产物目录 runs/<task_id>/<run_id>/
	AutonomyMode string             // autopilot | copilot | draft（取自 Task.autonomy_mode）
	LLMClient    LLMClient          // 可配置 model API 客户端（summary_post 等轻 LLM 用）
	LarkIdentity string             // bot | user（按动作与授权决定）
	Config       map[string]any     // executor 级配置（repo 映射、超时、后端选择等）
	Logger       *slog.Logger       // 结构化日志，run_id 贯穿
	Ctx          context.Context    // 承载超时/取消，传给 exec.CommandContext
}

// ActionStep ActionPlan 中的一个原子步骤，既用于 dry-run 预览，也用于真正执行。
type ActionStep struct {
	Kind        string         `json:"kind"`              // cmd | lark_cli | llm | web | git
	Description string         `json:"description"`       // 人类可读：这一步要做什么
	Command     []string       `json:"command,omitempty"` // 将执行的命令行（子进程类）
	Payload     map[string]any `json:"payload,omitempty"` // 请求体/参数（结构化预览）
}

// ActionPlan Executor 解析 Task 得到的、将要执行的具体动作。可预览、可（简化）签名。
type ActionPlan struct {
	ActionType     string         `json:"action_type"`
	Steps          []ActionStep   `json:"steps"`
	RiskTier       RiskTier       `json:"risk_tier"`
	Reversible     bool           `json:"reversible"`
	ExternalEffect bool           `json:"external_effect"` // 是否对外产生副作用
	Summary        string         `json:"summary"`         // "将要做什么"的一句话说明（给确认卡用）
	Meta           map[string]any `json:"meta,omitempty"`
}
```

### 2.3 执行结果结构

```go
// Artifact 执行产物引用。
type Artifact struct {
	Kind  ArtifactKind   `json:"kind"`
	Ref   string         `json:"ref"`   // 本地路径 / URL / message_id / event_id / 分支名
	Label string         `json:"label"`
	Meta  map[string]any `json:"meta,omitempty"`
}

// StepRecord 单步留痕，写入 execution_step，用于可观测与排障。
type StepRecord struct {
	Seq         int    `json:"seq"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"` // 指针区分"无退出码"与 0
	StdoutRef   string `json:"stdout_ref,omitempty"` // 大输出落文件，这里存路径
	StderrRef   string `json:"stderr_ref,omitempty"`
	DurationMs  int64  `json:"duration_ms"`
}

// ExecError fail-fast 的错误载体：失败时必填，禁止为空。实现 error 接口。
type ExecError struct {
	ErrorType     string `json:"error_type"` // e.g. ActionHashMismatch / SlotValidationError / SubprocessNonZeroExit / Timeout / OpenIdResolveFailed
	Message       string `json:"message"`
	FailedCommand string `json:"failed_command,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	StderrExcerpt string `json:"stderr_excerpt,omitempty"`
}

func (e *ExecError) Error() string { return e.ErrorType + ": " + e.Message }

// ExecutionResult 一次执行的归一化结果。
type ExecutionResult struct {
	TaskID        int64          `json:"task_id"`
	RunID         string         `json:"run_id"`
	ActionType    string         `json:"action_type"`
	Status        ExecStatus     `json:"status"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    time.Time      `json:"finished_at"`
	DurationMs    int64          `json:"duration_ms"`
	OutputSummary string         `json:"output_summary"` // 人类可读结论
	Artifacts     []Artifact     `json:"artifacts,omitempty"`
	Steps         []StepRecord   `json:"steps,omitempty"`
	Metrics       map[string]any `json:"metrics,omitempty"` // token / 子进程数 / 联网次数
	Error         *ExecError     `json:"error,omitempty"`   // status=failed 时必填
}
```

### 2.4 统一 Executor 接口

Go 的 `interface` 不含字段，动作元信息（`action_type / risk_tier / mode / ...`）用方法暴露；`Descriptor()` 一次性返回静态元信息，避免逐个 getter：

```go
// ExecutorMeta 动作的静态元信息（注册与护栏预检用）。
type ExecutorMeta struct {
	ActionType     string
	RiskTier       RiskTier
	Mode           ExecMode
	DefaultTimeout time.Duration
	ExternalEffect bool
	Reversible     bool
}

// Executor 统一执行接口。M5 按 Task.plan 执行；不做内容层判断（M4 已确认）。
type Executor interface {
	// Descriptor 返回静态元信息（action_type/risk_tier/mode/超时/对外副作用/可逆）。
	Descriptor() ExecutorMeta

	// Plan 校验 slots + 依据 Task.plan 解析上下文，产出结构化 ActionPlan。
	// slots 缺字段/类型错误/关联实体缺失 → 直接返回 *ExecError（fail-fast），不猜测补全。
	Plan(ctx *ExecutionContext) (*ActionPlan, error)

	// Run 执行 ActionPlan 的各步骤，产出 ExecutionResult。
	// 任何步骤失败 → 返回 status=failed 且 Error 必填（error 为 *ExecError）；禁止吞异常/静默降级。
	Run(plan *ActionPlan, ctx *ExecutionContext) (*ExecutionResult, error)
}
```

**dry-run 是引擎能力，而非每个 Executor 重复实现**：引擎调 `Plan()` 得到 `ActionPlan` 后，若处于 `draft` 模式或命中 hold，则渲染 `Steps`（命令行 + payload + summary）作为预览返回，**不调用 `Run()`**。对外类动作再叠加各后端 CLI 的原生 `--dry-run`（见 §5）。

---

## 3. action_type → Executor 映射表

> Executor 均为实现 §2.4 `Executor` 接口的 Go 类型（struct）。所有动作后端子进程均走 `exec.Command`/`exec.CommandContext`。

| action_type | Executor（Go struct） | 后端 | 同步/异步 | risk_tier | 对外副作用 | 可逆 | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `code_change` | `CodeChangeExecutor` | **codex exec**（默认，改代码）/ cursor-agent | 异步 | T4→T5 | 否（默认只出 diff/分支） | 是（不自动提交/推送） | 提交/推送为高危项，默认关，需用户确认 |
| `summary_post` | `SummaryPostExecutor` | **model API**（生成总结正文）+ `lark-cli im +messages-send/+messages-reply` | 同步 | T4 | 是（发群） | 否 | 按 `plan` 发送（M4 已确认内容）；轻 LLM 走 model API 不用 codex；发送幂等键防重 |
| `investigate` | `InvestigateExecutor` | ripgrep + `codex exec -s read-only` + web search | 异步 | T0→T1 | 否 | 是 | 只读检索，一般可 autopilot |
| `schedule_meeting` | `ScheduleMeetingExecutor` | `lark-cli contact` + `lark-cli calendar` | 同步 | T4 | 是（建日程/邀请） | 部分（可删） | 需先解析参会人 open_id |
| `reply_message` | `ReplyMessageExecutor` | model API + `lark-cli im +messages-reply` | 同步 | T4 | 是 | 否 | 回复指定 message |
| `create_task` | `CreateTaskExecutor` | `lark-cli task` | 同步 | T3 | 是（飞书任务） | 是 | 建/派飞书任务 |
| `doc_update` | `DocUpdateExecutor` | `lark-cli docs/base/sheets/markdown` | 同步/异步 | T3→T4 | 是 | 是 | 更新云文档/多维表格 |
| `notify_self` | `NotifySelfExecutor` | `lark-cli im +messages-send`（本人 p2p） | 同步 | T2 | 是（仅本人） | 否 | 低危，用于结果通知 |

> **LLM 分工提示**（对齐总纲 §6）：`code_change` / `investigate` 用 **codex CLI**（前者 `codex exec` 改代码，后者 `codex exec -s read-only` 只读调研）；`summary_post` / `reply_message` 里"生成正文"这类轻 LLM 用 **model API**（HTTP）即可，不必 codex。注意与 M4 区分：M4 的 codex 是做**决策**（是否固化成 Task / 方案是否明确），M5 的 codex 是**执行代码**——两者都用 codex CLI 但目的不同。

> **可扩展 Executor 清单**（同一 `Executor` 接口，注册即用，引擎零改动）：
> `run_script`（跑本地脚本，T5，需白名单+确认）、`file_op`（本地文件整理，T2）、`web_action`（受控网页操作，T4/T5）、`update_task`（更新飞书任务状态，T3）、`minutes_action`（妙记产物加工，T1）。

---

## 4. 各 Executor 详细设计

> 本机能力已只读探测确认（见附录 A）：`lark-cli 1.0.72`、`codex-cli 0.144.1`、`cursor-agent` 均可用。lark-cli 全域统一约定：`--dry-run` 预览不执行、`--jq` 过滤、`high-risk-write` 需 `--yes`、`--as bot|user` 选身份。

### 4.1 `code_change`：代码修改（CodeChangeExecutor）

对应需求："XXX 项目代码按讨论方案做修改"。**后端仍用 codex CLI**（`codex exec`）——codex 就是干代码活的，本次调整不变。

- **后端**：`codex exec`（默认，ChatGPT 登录态、非交互）；`cursor-agent` 作为可配置备选后端（二选一，不做自动 fallback）。Go 侧用 `internal/codex` 封装 `exec.CommandContext`。
- **repo 来源**：优先取 `Task.background`/`plan` 中固化的 repo，其次由 `project_id` → `Project.repos[].local_path`。
- **输入 slots**（示例，来自 `Task.slots`，方案细节在 `Task.plan`）：
  ```json
  {
    "repo_path": "/Users/bytedance/workspace-local/<repo>",
    "instruction": "按 plan 固化的方案：<结构化改动说明>",
    "base_ref": "main",
    "target_files": ["optional/hint.go"],
    "backend": "codex"
  }
  ```
- **Plan**：
  1. 从关联 `Project.repos` 或 slots 定位 `repo_path`；校验目录存在且是 git 仓库；校验工作树干净（脏工作树 → fail-fast，不擅自 stash）。
  2. 依据 `Task.plan`（M4 确认的明确方案）组装 codex prompt（方案 + 约束 + "只改代码不提交"）。**M5 不重新判断该不该改**，按 plan 执行。
  3. 决定隔离方式：**在目标 repo 新建独立分支 `jarvis/<task_id>` 或 git worktree**，避免污染当前工作树。
  4. 产出 `ActionPlan`（RiskTier=T4，ExternalEffect=false，Reversible=true）。
- **Run**（等价命令行 + Go `exec.CommandContext` 示意）：
  ```bash
  codex exec -C <repo_path> \
    -s workspace-write \
    -m <configurable-model> \
    "<组装后的改动指令>"
  # 执行后收集：git diff > runs/<task>/<run>/change.diff
  ```
  ```go
  // Go 子进程调用示意：超时用 ctx 控制，kill 即 status=failed（不重试掩盖）。
  cmd := exec.CommandContext(ctx.Ctx, "codex", "exec",
      "-C", repoPath,
      "-s", "workspace-write", // 固定只允许写工作区
      "-m", model,
      prompt,
  )
  var stdout, stderr bytes.Buffer
  cmd.Stdout, cmd.Stderr = &stdout, &stderr
  if err := cmd.Run(); err != nil {
      return nil, &ExecError{
          ErrorType:     "SubprocessNonZeroExit",
          Message:       "codex exec 失败",
          FailedCommand: strings.Join(cmd.Args, " "),
          ExitCode:      exitCodeOf(err),           // 从 *exec.ExitError 提取
          StderrExcerpt: truncate(stderr.String()), // 摘要，大输出落文件
      }
  }
  // git diff → runs/<task>/<run>/change.diff
  ```
  - sandbox 固定 `workspace-write`（只允许写工作区）；**禁止 `danger-full-access`、禁止 `--dangerously-bypass-approvals-and-sandbox`**。
  - 若改动需要装依赖/联网（超出 workspace-write）→ 视为高危，**不自动放开**，改为 failed 并提示需用户确认放开沙箱。
- **产物**：`change.diff`（DIFF）、分支名（BRANCH）、变更文件清单、codex 会话 id、stdout/stderr 日志。
- **⚠️ 高危项（务必标注、默认关闭、【需与用户确认】）**：
  - **是否自动 `git commit`**：默认 **否**（只产出 diff/分支）。
  - **是否自动 `git push`**：默认 **否**，**列为必须用户显式确认的高危项**——M5 不擅自设计任何自动 push；即便用户开启，也需配合分支保护 / 目标分支白名单。
  - sandbox 升级到 `danger-full-access`：默认 **否**，属高危。
- **fail-fast**：repo 不存在 / 非 git / 工作树脏 / codex 退出码非 0 / 无 diff 产出 → `status=failed` + `ExecError`（含命令、退出码、stderr 摘要），**不重试掩盖、不回退到"随便改点什么"**。

### 4.2 `summary_post`：总结并发群（SummaryPostExecutor）

对应需求："根据会议妙记总结 todo 到人、发在群里"。

- **后端**：**model API**（生成总结正文，轻 LLM，走 HTTP 不用 codex）+ `lark-cli im +messages-send`（或 `+messages-reply`，`exec.Command`）。妙记来源为 `Task` 关联的 `Resource`（`minute_token`），**逐字稿经 M2 的 `ResourceFetcher.EnsureMinutesText`（M2 §3.9.1）按需拉取**（单一入口，复用缓存/`content_hash` 去重，本期仅妙记）。
- **输入 slots**（示例，正文风格/@人以 `Task.plan` 固化为准）：
  ```json
  {
    "source": {"type": "minutes", "token": "<minute_token>"},
    "target_chat_id": "oc_xxx",
    "style": "todo_by_owner",
    "at_persons": [{"name": "张三"}],
    "reply_to_message_id": "om_xxx (可选)"
  }
  ```
- **Plan**：
  1. 只读拉取来源：妙记走 **`ResourceFetcher.EnsureMinutesText(resID)`**（M2 §3.9.1，内部调 `lark-cli minutes`，带缓存与去重）；消息走 `lark-cli im +messages-mget`。
  2. **model API** 生成结构化总结（"todo → 责任人"，按 `plan` 的风格），需要 @ 人时先经 `contact` 解析 open_id。
  3. 组装命令（`--markdown`，`--as bot`，`--idempotency-key=<task_id>`）。
  4. `ActionPlan`（RiskTier=T4，ExternalEffect=true，Reversible=false）。
- **Run**（等价命令行 + Go 示意）：
  ```bash
  lark-cli im +messages-send --as bot \
    --chat-id <target_chat_id> \
    --markdown "<总结正文>" \
    --idempotency-key "<task_id>"      # 防重复发送（原生支持，≤50 字符）
  ```
  ```go
  cmd := exec.CommandContext(ctx.Ctx, "lark-cli", "im", "+messages-send",
      "--as", "bot",
      "--chat-id", targetChatID,
      "--markdown", body,
      "--idempotency-key", idempotencyKey, // 防重复发送
      "--format", "json",                  // 结构化输出便于解析 message_id
  )
  ```
- **护栏**：对外动作，`copilot` 模式下默认 **hold 待确认**（§5）；`--idempotency-key` 防重复；发送前可 `--dry-run` 预览正文与目标群。
- **产物**：`ArtifactFeishuMessage`（ref=message_id）+ 发送正文快照（存本地）。
- **fail-fast**：来源拉取失败 / open_id 解析失败 / 发送非 2xx → failed + 错误详情，不"发个降级版本"。

### 4.3 `investigate`：查证澄清（InvestigateExecutor）

对应需求："把不明确的问题查项目代码或网上信息弄明确"。

- **后端**：本地 `ripgrep`（`rg`）+ `codex exec -s read-only`（让 codex 在 repo 内只读调研）+ web search（可配置 provider）。均走 `exec.Command`。
- **输入 slots**（示例）：
  ```json
  {
    "question": "<要澄清的问题>",
    "repo_paths": ["/Users/bytedance/workspace-local/<repo>"],
    "need_web": true
  }
  ```
- **Plan / Run**：
  1. 本地检索：`rg <关键词>`；必要时 `codex exec -C <repo> -s read-only "<调研问题>"`（只读沙箱，绝不写盘）。
  2. 联网：web 搜索（`need_web=true` 时）。
  3. **model API** 汇总证据 → 结论 + 引用（代码位置 / URL）。
  ```go
  // ripgrep 检索示意（JSON 输出便于结构化解析命中位置）。
  rg := exec.CommandContext(ctx.Ctx, "rg", "--json", keyword, repoPath)
  // codex 只读调研：-s read-only 保证无副作用、绝不写盘。
  cx := exec.CommandContext(ctx.Ctx, "codex", "exec", "-C", repoPath, "-s", "read-only", question)
  ```
- **产物**：`ArtifactReport`（结论 markdown，落本地）+ 证据链接与代码引用。
- **风险**：只读（T0→T1），一般 `autopilot` 可自动执行。
- **fail-fast**：检索/联网无结果 → 明确产出"未找到 / 不确定 + 已尝试的检索路径"，**禁止编造结论**。

### 4.4 `schedule_meeting`：约会议（ScheduleMeetingExecutor）

对应需求："用 lark-cli 约下一次会议"。

- **后端**：`lark-cli contact`（解析 open_id）+ `lark-cli calendar`（建日程/找空档/找会议室）。均走 `exec.Command`。
- **输入 slots**（示例，参会人/时间以 `Task.plan` 固化为准）：
  ```json
  {
    "summary": "下一次 XXX 项目同步",
    "attendees": [{"name": "李四"}, {"email": "wangwu@..."}],
    "time_range": "下周二下午 或 明确 start/end",
    "duration_min": 30,
    "need_room": true,
    "description": "议题：..."
  }
  ```
- **Plan**：
  1. **解析参会人 → open_id**：`lark-cli contact +search-user --as user`（按姓名/邮箱）。解析不到任何一个参会人 → fail-fast（不猜人、不漏人）。
  2. 时间不明确：`lark-cli calendar +suggestion` / `+freebusy` 找空档。
  3. 需会议室：`lark-cli calendar +room-find` 选候选。
  4. 组装 `+create` 命令（`--attendee-ids ou_...,omm_...`、`--start/--end` ISO8601、`--summary`、`--description`）。
  5. `ActionPlan`（RiskTier=T4，ExternalEffect=true，Reversible=部分）。
- **Run**（等价命令行 + Go 示意）：
  ```bash
  lark-cli calendar +create --as user \
    --summary "<标题>" \
    --start "2026-07-21T14:00:00+08:00" \
    --end   "2026-07-21T14:30:00+08:00" \
    --attendee-ids "ou_aaa,ou_bbb,omm_room" \
    --description "<议题>"
  ```
  ```go
  cmd := exec.CommandContext(ctx.Ctx, "lark-cli", "calendar", "+create",
      "--as", "user",
      "--summary", summary,
      "--start", startISO8601,
      "--end", endISO8601,
      "--attendee-ids", strings.Join(openIDs, ","),
      "--description", desc,
      "--format", "json",
  )
  ```
- **护栏**：对外动作（邀请他人），`copilot` 默认 hold；建前 `--dry-run` 预览日程与参会人。
- **产物**：`ArtifactCalendarEvent`（ref=event_id）+ 参会人解析结果 + 日程详情。
- **fail-fast**：open_id 解析失败 / 时间非法 / `+create` 失败 → failed + 详情。

---

## 5. 执行前护栏与 dry-run

> 遵守"不乱加护栏"：**不硬编码一堆防御逻辑**，而是用「风险分层 × autonomy 模式 × 配置化决策矩阵」统一驱动，并尽量复用后端 CLI 的原生 `--dry-run` / `--yes`。强护栏项一律做成**配置项 + 需用户确认**，默认值保守但可调。

### 5.0 前置：`action_hash` 漂移校验（执行前第一道闸）

M5 拿到 `pending` Task 后、进入护栏决策**之前**，先做 `action_hash` 漂移校验，防止 M4 确认后 `plan` 被篡改：

1. 用与 M4 确认时**同一套规范化算法**重算当前 `plan`（可含 `background` 关键字段）的指纹 `recomputed`（如 canonical-JSON + SHA-256）。
2. 与 `Task.action_hash` 比对：
   - **一致** → 放行，进入 §5.1 护栏决策。
   - **不一致** → 判为漂移，**不执行**：`Task.status=failed` + `ExecError{ErrorType: "ActionHashMismatch"}`（附期望/实际指纹），打回由用户复核（是否重新确认生成新 Task）。**不静默按新 plan 执行、不自动修复**。

```go
// 执行前漂移校验：不一致即 fail-fast，绝不按被改动的 plan 执行。
func verifyActionHash(t *Task) error {
	recomputed := hashPlan(t.Plan, t.Background) // 与 M4 确认时同一规范化算法
	if recomputed != t.ActionHash {
		return &ExecError{
			ErrorType: "ActionHashMismatch",
			Message:   fmt.Sprintf("plan 漂移：expected=%s actual=%s", t.ActionHash, recomputed),
		}
	}
	return nil
}
```

> 与 §5.4 的确认指纹区分：`action_hash` 防的是 **M4→M5 之间 `plan` 被改**（存储层篡改/并发写）；§5.4 的 `approval_token` 防的是 **plan 与 execute 之间参数被改**（hold 确认链路）。两道校验目的不同，都保留。

### 5.1 决策模型（allow / hold / deny）

护栏对每个 `ActionPlan` 只输出三种决策：

- `allow`：直接执行。
- `hold`：暂停，生成确认请求 → Task 置 `awaiting_confirm`，等人工放行后再执行。
- `deny`：拒绝执行 → `status=denied`（原因透出，不静默）。

决策由**配置化矩阵**给出（`risk_tier × autonomy_mode`），示例默认值：

| risk_tier \ autonomy | `autopilot` | `copilot`（建议默认） | `draft` |
| --- | --- | --- | --- |
| T0/T1 只读 | allow | allow | 仅预览 |
| T2 本地写/通知本人 | allow | allow | 仅预览 |
| T3 可逆对外写 | allow | hold | 仅预览 |
| T4 对外副作用 | hold | hold | 仅预览 |
| T5 破坏性/高危 | deny | deny | 仅预览 |

### 5.2 强制确认 / 拒绝清单（配置项，默认保守，均需用户拍板）

| 动作 | 默认 | 说明 |
| --- | --- | --- |
| `code_change` 自动 `git commit` | **deny** | 只产出 diff/分支 |
| `code_change` 自动 `git push` | **deny** | 高危，必须用户显式开启 + 目标分支白名单 |
| codex/cursor sandbox 升 `danger-full-access` | **deny** | 需联网/装依赖时才涉及，属高危 |
| 发群消息 / 约会议 / 邀请他人（T4） | **hold** | 最终确认后执行 |
| `lark-cli` `high-risk-write`（原生需 `--yes`） | **deny** | 默认不触碰；需要时单独确认 |
| 删除类 / 生产类 / `run_script` | **deny** | 需白名单 + 显式确认 |

### 5.3 dry-run 机制

- **引擎层**：`draft` 模式或 hold 时，只跑 `Plan()` 渲染 `ActionPlan.Steps`（命令行 + payload + summary），不跑 `Run()`。
- **后端层**：对外动作复用原生预览——
  - `lark-cli im/calendar ... --dry-run`：打印将发出的请求，不执行。
  - `code_change`：用独立分支 / worktree + `git diff` 作为"改了什么"的预览，提交/推送始终另需确认。
  - `investigate`：只读沙箱天然无副作用。

### 5.4 确认载体与防篡改（hold 链路）

- hold 的动作 → 通过飞书交互卡片（发给本人）或管理后台按钮呈现 `ActionPlan.Summary` + 关键 `Steps`，用户点"确认/取消"。
- 确认后携带 `approval_token` 回到引擎，引擎校验 token 与 `ActionPlan` 指纹一致后才执行，**防止 plan 与 execute 之间参数被篡改**（研究里的 HMAC-locked payload 的轻量版；是否需要完整 HMAC 见开放问题）。
- 确认有 TTL：超时未确认 → Task 保持 `awaiting_confirm`（不自动执行、不自动取消），透出给用户。
- 与 §5.0 的 `action_hash` 互补：`action_hash` 是**入口**校验（防 M4→M5 存储篡改），`approval_token` 是 hold **出口**校验（防确认→执行篡改）。

### 5.5 明确"不加什么"（fail-fast 边界）

- 不做全局自动重试来掩盖失败；不做静默 fallback（换模型/换动作/发降级内容）；不做异常吞没。
- 执行失败 = `Task.status=failed` + 完整 `ExecError`，交由用户决策，不自作主张。
- `action_hash` 漂移 = `failed`（`ActionHashMismatch`），**绝不按被改动的 plan 执行、不自动修复**。
- 自动 `git commit`/`push` 默认关（【需与用户确认】），M5 不擅自设计任何自动 push。

---

## 6. 结果回写

> **两层回写**：① 结论写回 **`Task.execution_result`（JSON）+ `Task.status`**（主链路，M0/前端读这个）；② 完整过程写 **`execution_run` / `execution_step` / `execution_artifact`** 三表做详细留痕（总纲支撑表已列，供可观测与排障）。两者不冲突：Task 上是"给人看的结论 + 状态"，三表是"给排障看的全过程"。

### 6.1 主链路：回写 `Task`

执行结束后，`ResultWriter` 把 `ExecutionResult` 归一化写回 Task（总纲 `task` 表已定义 `execution_result JSON` 与 `status`）：

- **成功**：`Task.status = done`，`Task.execution_result = {summary, artifacts, error: null}`。
- **失败**：`Task.status = failed`，`Task.execution_result = {summary, artifacts, error: <ExecError>}`。
- 执行中：`Task.status = executing`（`awaiting_confirm`/`denied` 属护栏中间态，映射为 `executing`/`failed` 落 Task）。

```go
// Task.execution_result 的 JSON 结构（回写主链路读它）。
type TaskExecutionResult struct {
	Summary   string     `json:"summary"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Error     *ExecError `json:"error,omitempty"` // status=failed 时必填
}
```

### 6.2 详细留痕：`execution_*` 三表（GORM model）

```go
// execution_run 一次执行一行。
type ExecutionRun struct {
	RunID           string    `gorm:"column:run_id;primaryKey;type:varchar(64)"`
	TaskID          int64     `gorm:"column:task_id;index"`
	ActionType      string    `gorm:"column:action_type;type:varchar(32)"`
	Status          string    `gorm:"column:status;type:varchar(24)"`
	AutonomyMode    string    `gorm:"column:autonomy_mode;type:varchar(16)"`
	StartedAt       time.Time `gorm:"column:started_at"`
	FinishedAt      time.Time `gorm:"column:finished_at"`
	DurationMs      int64     `gorm:"column:duration_ms"`
	OutputSummary   string    `gorm:"column:output_summary;type:text"`
	ErrorType       string    `gorm:"column:error_type;type:varchar(64)"`
	ErrorMessage    string    `gorm:"column:error_message;type:text"`
	ExecutorVersion string    `gorm:"column:executor_version;type:varchar(32)"`
}

func (ExecutionRun) TableName() string { return "execution_run" }

// execution_step 留痕，用于可观测/排障。
type ExecutionStep struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID       string `gorm:"column:run_id;index;type:varchar(64)"` // FK → execution_run.run_id
	Seq         int    `gorm:"column:seq"`
	Kind        string `gorm:"column:kind;type:varchar(16)"` // cmd/lark_cli/llm/web/git
	Description string `gorm:"column:description;type:varchar(512)"`
	Command     string `gorm:"column:command;type:text"`
	ExitCode    *int   `gorm:"column:exit_code"`
	StdoutRef   string `gorm:"column:stdout_ref;type:varchar(1024)"`
	StderrRef   string `gorm:"column:stderr_ref;type:varchar(1024)"`
	DurationMs  int64  `gorm:"column:duration_ms"`
}

func (ExecutionStep) TableName() string { return "execution_step" }

// execution_artifact 产物引用。
type ExecutionArtifact struct {
	ID    int64          `gorm:"column:id;primaryKey;autoIncrement"`
	RunID string         `gorm:"column:run_id;index;type:varchar(64)"` // FK → execution_run.run_id
	Kind  string         `gorm:"column:kind;type:varchar(24)"`
	Ref   string         `gorm:"column:ref;type:varchar(1024)"`
	Label string         `gorm:"column:label;type:varchar(512)"`
	Meta  datatypes.JSON `gorm:"column:meta"` // gorm.io/datatypes
}

func (ExecutionArtifact) TableName() string { return "execution_artifact" }
```

### 6.3 产物存储

- **本地大产物**：`~/jarvis/runs/<task_id>/<run_id>/`（`change.diff`、`report.md`、`step-N.log`）—— 本地可信明文存储。
- **飞书类产物**：只存引用（`message_id` / `event_id` / `task_guid` / `doc_url`），不复制内容。

### 6.4 状态更新 → M0 管理后台

- 成功：`Task.status = done`，`Task.execution_result.summary` + 产物引用（diff 路径 / 群消息链接 / 日程链接）。
- 失败：`Task.status = failed`，`Task.execution_result.error = ExecError`（类型 / 消息 / 失败命令 / 退出码 / stderr 摘要）。
- **回写方式**（对接项，见开放问题）：Task 与 `execution_*` 表同库，M5 直接经 GORM 写；M0/前端读 `Task.execution_result` + `status`。`execution_*` 表归属（M5 还是 M0 owns）见开放问题。

### 6.5 失败信息透出（fail-fast）

- 失败时 `Task.execution_result.error` 必填，管理后台以醒目状态展示，附最小可复现信息（命令 + 退出码 + stderr 摘要）。
- 可选：同时 `notify_self` 发飞书通知本人（"任务 X 执行失败：<原因摘要>"）。
- 严禁把 failed 记成 done，或用空结果掩盖失败。

---

## 7. 可观测

- **每步留痕**：`execution_step` 记录命令、退出码、耗时、token、输出引用；子进程 stdout/stderr 实时 `tee` 到 `runs/<task_id>/<run_id>/step-N.log`（大输出落文件、库里存 ref + 摘要）。
- **结构化日志**：`run_id` 贯穿全链路，关联 `task_id` / `project` / `action_type`，便于按任务/项目检索。
- **指标**：执行成功率、各 `action_type` 耗时分布、失败 Top 原因、`awaiting_confirm` 队列长度与停留时长、子进程超时率。
- **可排障性目标**：任一 `failed` 都能定位到具体 step + 命令 + stderr，无需复现即可判因。

---

## 8. 与记忆（mem0）

- **选择性回写**（默认策略，建议值）：
  - **回写摘要**：结论型结果进 mem0 作为后续上下文——`investigate` 结论、`code_change` 的方案与分支、`schedule_meeting` 结果、`summary_post` 的 todo 分配。关联 `project` / `person` / `task`，让后续 M3/M4/M5 能利用"这个项目上次改了什么""这个人负责哪些 todo"。
  - **不回写原始大产物**：完整 diff / 日志只留本地 + 后台，mem0 存摘要 + 引用，避免污染记忆。
- **失败边界（需确认）**：执行本身成功、但 mem0 回写失败时，**建议**保持 Task=`done`，另记 `memory_write_error` 告警透出（记忆回写是增强、不是主链路）。此边界与"fail-fast"的取舍列入开放问题，由用户拍板。

---

## 9. 当前 MVP 实现（2026-07-19）

- 新增 `internal/execute.Store`，直接使用现有 `task` 表，不新增支撑表。
- `GET /api/tasks` 默认列出 `pending`，也可显式查询 `done/failed`；返回确认时冻结的 background/plan/slots，供人工执行。
- `POST /api/tasks/:task_id/finish` 只允许 `pending → done/failed`，要求 `expected_version` 和非空 JSON result；状态、结果、version 在一个 MySQL 事务中更新。
- 状态不符、版本冲突、重复完成全部 fail-fast，不重试、不降级、不覆盖第一次结果。
- 真实 MySQL 合成回滚验收已覆盖 `extracted Todo → need_decision → approve → pending Task → done` 完整链路，不调用飞书、mem0、模型或 codex。

自动 executor、外部副作用、调度并发和详细执行留痕均为后续增强项；先通过人工闭环验证 Todo/Task 是否真的有用，再决定实现顺序。

---

## 10. 开放问题清单（需用户确认）

> 以下均为默认保守、待用户拍板的项；尤其涉及提交/推送、对外强制确认。技术栈相关（Go / Hertz / GORM / robfig/cron，不用 Eino/Kitex）已在总纲定稿，不再列为开放问题。

1. **`code_change` 自动 `git commit`**：默认关。是否开放？对哪些 repo 开放？
2. **`code_change` 自动 `git push`**（高危）：默认关且强烈建议保持人工。若开放，需要哪些约束（目标分支白名单 / 仅个人分支 / 禁 push 保护分支）？
3. **sandbox 升级**：`code_change` 需联网/装依赖时是否允许放开到 `danger-full-access`？还是一律 failed 交人工？
4. **强制确认清单粒度**：发群 / 约会议 / 建任务是否一律本人最终确认？是否允许对"仅本人/特定测试群"降为 autopilot？
5. **autonomy_mode 默认值**：整体默认 `copilot`（对外动作需确认）还是更激进的 `autopilot`？
6. **确认载体**：飞书交互卡片 vs 管理后台按钮，主用哪个？确认 TTL 设多久？
7. **防篡改强度**：`action_hash`（§5.0 入口漂移校验）与 `approval_token`（§5.4 hold 出口校验）用轻量指纹（canonical-JSON + SHA-256）够，还是要上完整 HMAC 签名？规范化算法需与 M4 生成 `action_hash` 时对齐（同一实现）。
8. **`action_hash` 漂移处理**：校验不一致时，除判 `failed` 外，是否需要自动回流 M4 重确认生成新 Task，还是仅打回等用户手动处理？
9. **回写契约**：Task 与 `execution_*` 三表同库直写（GORM）。`execution_*` 表归 M5 还是 M0 owns？M0/前端只读 `Task.execution_result`+`status` 是否足够？
10. **mem0 回写范围与失败处理**：哪些结果进长期记忆？回写失败是否维持 `done`+告警（建议）还是判 `failed`？
11. **`code_change` 默认后端**：codex 还是 cursor-agent？固定还是按 project 配？
12. **并发与超时默认值**：各 `action_type` 的并发上限与 `DefaultTimeout` 取值。
13. **`summary_post` / `schedule_meeting` 身份**：发消息用 `bot` 还是 `user`？（`schedule_meeting` 的 `contact +search-user` 需 `--as user` 授权，须确认 user 身份已授权。）
14. **是否需要 durable execution**：`awaiting_confirm` 的 TTL/escalation/断电恢复是否要更强编排（当前用 MySQL 持久化 `Task.status` + robfig/cron 轻量实现）？

---

## 附录 A：本机执行能力探测结论（只读探测，未执行任何写操作）

| 能力 | 结论 |
| --- | --- |
| `lark-cli` | `1.0.72`，`/Users/bytedance/.local/bin/lark-cli`。域含 `im` / `calendar` / `contact` / `task` / `docs` / `minutes` / `note` / `vc` 等。全域约定：`--dry-run` 预览、`--jq` 过滤、`high-risk-write` 需 `--yes`。 |
| `im +messages-send/+messages-reply` | 支持 `--as bot\|user`、`--chat-id`/`--user-id`、`--text`/`--markdown`/媒体、`--idempotency-key`（≤50，防重复）、`--dry-run`。Risk: **write**。 |
| `calendar +create` | 支持 `--as`、`--attendee-ids`（ou_/oc_/omm_ 逗号分隔）、`--start`/`--end`（ISO8601）、`--summary`、`--description`、`--rrule`、`--dry-run`。Risk: **write**。另有 `+suggestion` / `+freebusy` / `+room-find` 辅助定时与选室。 |
| `contact` | `+search-user`（需 `--as user`）、`+get-user`、`user_profiles batch_query` —— 用于姓名/邮箱 → open_id 解析。 |
| `codex` | `codex-cli 0.144.1`（ChatGPT 登录态）。`codex exec [PROMPT]` 非交互；`-C/--cd` 指定工作根、`-s read-only\|workspace-write\|danger-full-access`、`-m` 选模型、`--add-dir` 追加可写目录、`codex exec resume` 续会话、`codex exec review` 代码评审。`--dangerously-bypass-approvals-and-sandbox` 为极危选项，本方案禁用。 |
| `cursor-agent` | `/Users/bytedance/.local/bin/cursor-agent` 可用，作为 `code_change` 备选后端。 |
