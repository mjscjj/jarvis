# M4 确认模块（打分 + Todo→Task 转化闸门）技术方案

> 所属项目：基于飞书的本地个人 Jarvis 管家系统（用户：字节研发工程师 chujiejie.1）
> 隶属总纲：`docs/00-overview.md`（技术栈、7 实体、Todo/Task 拆分的权威定义在总纲）
> 技术栈：**Go 1.26 + Hertz + GORM + codex CLI**（决策）+ robfig/cron v3（过期扫描）。**不引入 Eino/Kitex**。
> 流水线定位：采集(M2) → 记忆化(M2) → 提取 Todo(M3) → **打分 + Todo→Task 转化(M4·本模块)** → 执行 Task(M5)

---

## 0. 模块定位与边界

M4 是"人在环路(Human-in-the-Loop)"的**决策闸门**，也是 **Todo → Task 的唯一转化点**：

- **输入**：M3 产出的 `Todo`（行动线索/候选，可能模糊、信息不足）。
- **做什么**：用 `confidence × risk` 打分决定这条 Todo 是**自动确认**、**需要补充信息**还是**需要用户决策**；并驱动"飞书卡片 + 管理后台"双通道让用户处置。
- **输出**：确认通过的 Todo → **固化生成一个 `Task`**（明确、含问题背景快照 + 明确方案）交给 M5；被否的 Todo → `dismissed`。

**核心立场（对齐 2026 HITL 最佳实践）**：**打分只用来决定"要不要问人"，不用来替人拍板。** 复杂决策交给带代码库上下文的 **codex CLI** 做（见 §2），任何不确定处一律**向人**。

### 0.1 Todo → Task 转化契约（本次调整核心）

```
M3 产出 Todo(线索)
   │
   ▼
┌──────────────── M4 ────────────────┐
│ ① 打分(confidence × risk)           │
│    - 明确+低风险的用规则/轻量判定     │
│    - 边界/复杂的用 codex exec 决策    │
│ ② 路由:                             │
│    - auto        → 自动确认          │
│    - need_info   → 问用户补信息      │
│    - need_decision → 用户决策        │
│ ③ 确认(用户 或 auto)后:             │
│    - 固化 background(问题背景快照)    │
│    - 固化 plan(明确方案,确认过的)     │
│    - 计算 action_hash                │
│    - INSERT task(status=pending)     │
│    - todo.status = confirmed         │
└──────────────────┬──────────────────┘
                   ▼
             Task(明确可执行) → M5
```

**关键规则**：
- **一个 Todo 最多生成一个 Task**（DB 层 `task.uk_task_todo` 唯一约束兜底）。
- **Task 的 `background` 和 `plan` 是确认时刻的快照**：把"问题背景（关联 project / 关键消息 / 相关记忆 / 交办人）"和"明确方案（用户确认过的，或自动确认的明确方案）"冻结进 Task。执行时方案明确、可复现，不受源数据后续变化影响。
- **确认前不存在 Task**：Todo 在 `extracted→need_info/need_decision→confirmed` 之间流转，只有 `confirmed` 那一刻才 INSERT task。`dismissed` 的 Todo 永不产生 Task。
- **方案变了 = 新 Todo → 新 Task**：M4 不篡改已生成 Task 的 plan。若讨论推进导致方案变化，由 M3 产出新 Todo、M4 重新走一遍。

### 0.2 上下游接口

| 方向 | 对端 | 输入/输出 | 说明 |
|---|---|---|---|
| 入 | M3 提取 | `Todo{action_type, slots, source, group_id, project_id, assigner_open_id, is_leader_assigned, missing_info}` | M3 只产 `extracted` 状态的 Todo |
| 出 | M5 执行 | 新建的 `Task{todo_id, background, plan, slots, action_hash, status=pending}` | M5 只执行 M4 固化的 Task |
| 回流 | M3 提取 | 重新提取请求 `{todo_id, source_ctx, supplement}` | 自由文本补充回喂 M3 |
| 读 | mem0 sidecar | HTTP `POST /memories/search`（佐证强度） | 只读；不可用时不臆造佐证 |
| 决策 | codex CLI | `codex exec -s read-only`（复杂决策） | 见 §2 |
| 读写 | MySQL(GORM) | Todo 状态 + Task 生成 + 审计表 | 本地明文；审计 append-only |
| 出入 | lark-cli(`--as bot`) | 发交互卡片 / 监听 `card.action.trigger` | 双通道之一 |

### 0.3 子组件拆分（Go package `internal/decide/`）

```
M4 确认
├── Scorer          打分：规则打分 + (边界情况) codex 决策
├── CodexDecider    codex exec 子进程封装(read-only, 结构化 JSON 输出)
├── RuleEngine      规则修正 + 强制确认清单(action_manifest, 配置驱动)
├── Router          二维矩阵 + 阈值 → 三路由(fail-fast 优先级判定)
├── TaskFactory     ★Todo→Task 固化(background/plan 快照 + action_hash)
├── Calibrator      置信度校准(阶段二启用, 样本不足不启用)
├── ConfirmStore    Todo 状态 + Task 生成 + 审计(GORM)
├── Channels
│   ├── BackendAPI      Hertz 待确认队列页 + 处置接口
│   └── FeishuCard      lark-cli 发送/回调/延迟更新
└── ReflowHandler   信息回流(补 slot / 回喂 M3) + 决策回写
```

---

## 1. 设计原则与 fail-fast 约束

1. **打分/决策失败 → 向人，绝不自动确认。** codex 超时/报错/返回非法 JSON、model 缺字段，一律路由 `need_decision`，记录异常，不静默降级、不猜测。
2. **未知 action_type → block 到人工。** 不在 `action_manifest` 里的动作 = 未分类 = 未知最坏后果，路由 `need_decision` 并要求先补分类，绝不 auto。
3. **强制确认清单是配置项，不是硬编码护栏。** 哪些 action_type 无论分数都必须人工（发群消息、约会议等对外不可逆动作），写在 `action_manifest` 配置里、由用户校准；审批要求在**工作流/配置层**判定，**不由 codex 运行时协商**（防 prompt injection 说服 Agent 跳过确认）。不擅自堆叠过度护栏。
4. **规则修正单向。** 规则只能**拉高 risk / 拉低 confidence**（fail-safe 方向），不能反向。
5. **mem0 不可用不臆造。** 佐证因子记为未知（=0 贡献），自然拉低 confidence 趋向人工。
6. **超时 ≠ 批准。** 待确认超 TTL 未处理 → 进 `expired` 加急重发，绝不自动确认/生成 Task。
7. **生成 Task 前漂移校验。** 确认到生成 Task 之间若源 Todo 的 slot/源消息变化，重新打分，不基于陈旧信息固化方案。生成的 Task 带 `action_hash`，M5 执行前再校验一次。

---

## 2. 决策引擎：codex CLI 做复杂决策（本次调整核心）

M4 的打分决策分两档，**避免每条 Todo 都调 codex（成本/延迟）**。**灰区边界、频率/成本上限、超时全部可配置**（总纲 §11.2），不硬编码。

### 2.1 两档决策

| 档 | 触发 | 用什么 | 说明 |
|---|---|---|---|
| **规则快判** | slot 齐全度、action_type severity、发件人权重等可由规则直接算；**且规则分不落灰区** | Go 规则引擎（无 LLM） | 高频、零成本、确定性。多数明确/明显要人工的 Todo 走这里 |
| **codex 深判** | 规则分**落入可配置灰区**（`confidence∈[conf_low,conf_high]` 且 `risk∈[risk_low,risk_high]`，中等把握、需理解项目背景/代码才能判断风险与方案是否明确） | **codex exec -s read-only** | 结合项目背景 + mem0 记忆 + 代码库上下文，做高质量判断，并**给出建议的明确方案（plan 草案）** |

> 为什么决策用 codex 而非 model API：M4 要判断的是"这条线索是否值得固化成明确任务、方案是否清楚、风险多大"，这**往往需要看项目代码**（例如"按 XX 方案改鉴权"到底动哪些文件、风险多大）。codex 有代码库只读上下文能力，判断质量比纯文本 model API 高。M2/M3 的高频抽取才用 model API。

### 2.1.1 灰区判定与预算闸（可配置）

只有**落入灰区**的 Todo 才调 codex；灰区外（明确 auto / 明显 need_decision）走规则快判，零 codex 成本。灰区边界由配置给定：

```go
// 是否需要 codex 深判：规则分落灰区才调（阈值来自配置，见 §2.3）
func needCodexDeepJudge(base *RuleScore, cfg CodexConfig) bool {
    inConf := base.Confidence >= cfg.GrayZone.ConfLow && base.Confidence <= cfg.GrayZone.ConfHigh
    inRisk := base.Risk >= cfg.GrayZone.RiskLow && base.Risk <= cfg.GrayZone.RiskHigh
    return inConf && inRisk
}
```

**频率/成本上限（预算闸）**：codex 调用受每小时/每天上限约束（配置 `max_calls_per_hour`/`max_calls_per_day`）。超预算时按 `on_budget_exceeded` 处置：

| `on_budget_exceeded` | 行为 |
|---|---|
| `route_need_decision`（默认） | 直接把该灰区 Todo 路由到 `need_decision`（人工），不调 codex。**fail-safe** |
| `degrade_to_rule` | 用规则分继续判定，并在审计里标 `codex_skipped=budget`。仅在用户显式接受降级时启用 |

> 预算计数用进程内滑动窗口计数器（`internal/decide/budget.go`），超限即触发上述处置，绝不"排队等下个窗口"阻塞流水线。默认 `route_need_decision`：宁可多问人，不省成本冒风险。

### 2.2 codex 决策形态

```go
// internal/decide/codex_decider.go（示意）
type CodexDecision struct {
    ConfidenceFactors  map[string]float64 `json:"confidence_factors"`
    RiskFactors        map[string]float64 `json:"risk_factors"`
    ConfidenceBasis    string             `json:"confidence_basis"`
    UncertaintyFactors []string           `json:"uncertainty_factors"`
    RecommendedReview  bool               `json:"recommended_review"`
    ProposedPlan       *PlanDraft         `json:"proposed_plan"` // 建议的明确方案(供固化 Task)
    PlanIsClear        bool               `json:"plan_is_clear"` // 方案是否已明确到可执行
}

// CodexDecider 持有可配置项(gray zone/预算/超时/model)，见 §2.3。
type CodexDecider struct {
    cfg    CodexConfig
    budget *Budget // 滑动窗口计数器(每小时/每天上限)
}

func (d *CodexDecider) Decide(ctx context.Context, todo *Todo, bg *Background, mems []Memory) (*CodexDecision, error) {
    // 预算闸：超上限直接返回哨兵错误，由上层按 on_budget_exceeded 处置(默认 need_decision)
    if !d.budget.Allow() {
        return nil, ErrCodexBudgetExceeded // 上层路由 need_decision 或按配置降级
    }
    prompt := buildDecisionPrompt(todo, bg, mems) // 组装:Todo+项目背景+记忆+代码线索

    // 超时来自配置(cfg.TimeoutSeconds)，非硬编码
    ctx, cancel := context.WithTimeout(ctx, time.Duration(d.cfg.TimeoutSeconds)*time.Second)
    defer cancel()

    // 有关联 repo 时带 -C 提供代码库只读上下文；无 repo 则纯文本决策(开放问题 #4)
    args := []string{"exec", "-s", "read-only", "-m", d.cfg.Model}
    if bg.Project != nil && bg.Project.RepoPath != "" {
        args = append(args, "-C", bg.Project.RepoPath)
    }
    args = append(args, prompt)
    cmd := exec.CommandContext(ctx, d.cfg.Bin, args...) // -s read-only 保证只读、无副作用
    out, err := cmd.Output()
    if err != nil {
        // 超时/退出码非 0 → fail-fast，上层按 cfg.OnTimeout(固定 need_decision)处置
        return nil, fmt.Errorf("codex decide failed: %w", err)
    }
    var dec CodexDecision
    if err := json.Unmarshal(extractJSON(out), &dec); err != nil {
        return nil, fmt.Errorf("codex output not valid JSON: %w", err) // fail-fast
    }
    return &dec, nil
}
```

- codex `-s read-only`：决策阶段**只读**，绝不写盘、不改代码。
- 要求 codex 输出结构化 JSON（confidence/risk 逐因子 + basis + uncertainty + recommended_review + **proposed_plan** + plan_is_clear）。
- **fail-fast**：codex 退出码非 0 / 超时 / 输出非法 JSON → `need_decision`（人工），绝不自动确认。
- **预算超限**：`ErrCodexBudgetExceeded` 由上层按 `on_budget_exceeded` 处置（默认 `route_need_decision`）。
- **proposed_plan 的作用**：codex 深判时顺带产出"建议的明确方案"，供后续固化 Task 的 `plan` 字段（用户确认或自动确认时采用/编辑）。
- 决策留痕入 `decision_audit`（含 codex 会话 id、prompt version、耗时、是否因预算跳过）。

### 2.3 codex 决策配置（可配置项，已定，总纲 §11.2）

全部落在配置文件（如 `config.yaml` 的 `decide.codex` 段），运行时可调、不硬编码：

```yaml
decide:
  codex:
    bin: /usr/local/bin/codex        # codex 可执行路径(本地明文配置)
    model: gpt-5.1-codex             # 决策用模型，可配
    timeout_seconds: 120             # 单次决策超时
    max_calls_per_hour: 30           # 频率上限(成本闸)
    max_calls_per_day: 200
    on_budget_exceeded: route_need_decision   # 或 degrade_to_rule(需显式接受降级)
    on_timeout: route_need_decision           # 固定 fail-safe(不可配成 auto)
    gray_zone:                        # 只有落此区间的 Todo 才调 codex 深判
      conf_low: 0.60
      conf_high: 0.85
      risk_low: 0.25
      risk_high: 0.60
```

| 配置项 | 含义 | 默认（待校准） |
|---|---|---|
| `gray_zone.conf_low/high` | 灰区 confidence 边界（含端点） | 0.60 / 0.85 |
| `gray_zone.risk_low/high` | 灰区 risk 边界（含端点） | 0.25 / 0.60 |
| `max_calls_per_hour` / `max_calls_per_day` | codex 决策频率/成本上限 | 30 / 200 |
| `timeout_seconds` | 单次决策超时 | 120 |
| `on_budget_exceeded` | 超预算处置：`route_need_decision`（默认）/ `degrade_to_rule` | `route_need_decision` |
| `on_timeout` | 超时处置：固定 `route_need_decision`（不允许配成 auto，fail-safe） | `route_need_decision` |
| `model` / `bin` | 决策模型 / codex 路径 | 本机为准 |

> 灰区默认值与 §4.2 二维矩阵的分箱边界一致（confidence 中档 0.60–0.85、risk 中档 0.25–0.60）——即"矩阵里既非明确 auto 也非明确 need_decision 的中间地带"才值得花 codex 深判。三个默认值都待用户按审计数据校准（总纲 §11.5 #10）。

---

## 3. 打分模型

两个独立维度，各输出 `[0,1]`：

- **confidence**：对"任务理解是否正确/明确、方案是否清楚"的把握。
- **risk**：执行出错的负面影响。

规则快判先算基础分；落灰区再用 codex 深判覆盖/细化因子分。最后 RuleEngine 单向修正。

### 3.1 confidence 因子表

| 因子 | 符号 | 含义 | 默认权重 | 来源 |
|---|---|---|---|---|
| slot 完整度 | c1 | 必填 slot 是否齐全 | 0.25 | 规则(必填命中率) |
| 消息明确度 | c2 | 原始消息表述清晰、指令性强 | 0.15 | codex/model |
| 无歧义度 | c3 | `1 − 歧义程度` | 0.15 | codex/model |
| mem0 佐证强度 | c4 | 记忆中有一致上下文支撑 | 0.15 | mem0 检索 |
| 来源可靠度 | c5 | 谁说的/哪个渠道可靠 | 0.10 | 规则(发件人权重) |
| **方案明确度** | c6 | **plan 是否明确到可执行**（本次新增，直接影响能否固化 Task） | 0.20 | codex(plan_is_clear) |

```
confidence_raw = 0.25·c1 + 0.15·c2 + 0.15·c3 + 0.15·c4 + 0.10·c5 + 0.20·c6
```

> `c6 方案明确度` 是 Todo→Task 拆分带来的新因子：只有方案足够明确（codex `plan_is_clear=true` 或用户补齐方案）才可能固化出可执行 Task。方案不明确 → 低 c6 → 倾向 `need_info`/`need_decision` 补方案。

### 3.2 risk 因子表

| 因子 | 符号 | 含义 | 默认权重 |
|---|---|---|---|
| 不可逆性 | r1 | 执行后能否干净撤销 | 0.30 |
| 对外触达 | r2 | 是否对外发消息/触达他人 | 0.25 |
| 改代码/系统 | r3 | 是否改代码、配置、生产 | 0.20 |
| 影响范围 | r4 | blast radius | 0.15 |
| 涉敏感对象 | r5 | 是否涉及 leader / 外部人员 | 0.10 |

```
risk_raw = 0.30·r1 + 0.25·r2 + 0.20·r3 + 0.15·r4 + 0.10·r5
```

### 3.3 规则修正（单向）

confidence 硬下压（只降不升）：

| 触发 | 修正 |
|---|---|
| 必填 slot 缺失 | `confidence_eff = min(raw, 0.30)`，`blocker=info_gap` |
| 方案不明确(c6 低 / codex plan_is_clear=false) | `confidence_eff = min(raw, 0.40)`，`blocker=plan_unclear` |
| `recommended_review=true` 或 `len(uncertainty_factors) ≥ K`(默认2) | 挤出 AUTO 区 |
| mem0 检索失败 | `c4=0`（不臆造佐证） |
| 打分/codex 异常 | `confidence_eff=0`（fail-fast） |

risk 硬上抬（只升不降，最终以 `action_manifest` 配置为准）：

| 触发 | 修正 |
|---|---|
| `action_type` ∈ 对外发消息类 | `risk_eff = max(raw, 0.80)` |
| `action_type` ∈ 改代码/部署类 | `risk_eff = max(raw, 0.70)` |
| 涉及 leader / 外部人员 | `risk_eff = max(raw, 0.60)` |
| 动作不可逆 | `risk_eff = max(raw, 0.60)` |

### 3.4 校准

LLM/codex 置信度系统性过度自信，需**分箱校准**（isotonic）。样本不足（每箱 <10）不启用，保持保守阈值宁可多问。阈值调整属治理动作，走配置版本 + 评审。

---

## 4. 三路由决策

三条路由：`auto`(自动确认→生成 Task) / `need_info`(补信息) / `need_decision`(用户决策)。

### 4.1 判定优先级（fail-fast，first-match-wins）

```go
func Route(t *ScoredTodo, cfg *ActionManifest) RouteResult {
    if t.ScoringFailed             { return NeedDecision } // fail-fast
    if !cfg.Known(t.ActionType)    { return NeedDecision } // 未知动作 block
    if cfg.ForceConfirm(t.ActionType) { return NeedDecision } // 强制确认清单
    if t.Blocker == PlanUnclear    { return NeedDecision } // 方案不明确必须人工明确
    if t.HasMissingSlot && t.RiskEff < RiskGate { return NeedInfo }
    if t.RiskEff >= RiskGate       { return NeedDecision } // 高风险强制人工
    if t.ConfidenceEff >= AutoConfFloor &&
       t.RiskEff < RiskAutoCeiling &&
       !t.RecommendedReview        { return Auto }         // 唯一自动区
    if t.ConfidenceEff < InfoConfFloor && t.Blocker == InfoGap { return NeedInfo }
    return NeedDecision // 灰区默认安全 → 人工
}
```

> 新增 `PlanUnclear` 前置：方案不明确的 Todo 即使其他都齐，也不能自动固化成 Task（否则 M5 拿到模糊方案没法执行），必须人工/回流把方案明确。

### 4.2 二维矩阵

分箱：confidence 低 `<0.60` / 中 `0.60–0.85` / 高 `≥0.85`；risk 低 `<0.25` / 中 `0.25–0.60` / 高 `≥0.60`。

| confidence ＼ risk | 低 `<0.25` | 中 `0.25–0.60` | 高 `≥0.60` |
|---|---|---|---|
| **高 `≥0.85`** | ✅ auto → 生成 Task | 🟡 need_decision | 🔴 need_decision |
| **中 `0.60–0.85`** | 🟡 need_decision ＊ | 🟡 need_decision | 🔴 need_decision |
| **低 `<0.60`** | 🟠 need_info | 🟠 need_info→decision | 🔴 need_decision |

> ＊ 中 confidence + 低 risk 是主"校准拨盘"：默认保守 `need_decision`，审计数据证明该区实际正确率达标后可放宽为 auto。默认自动区很窄（只右上角）——个人管家早期宁可多问。

### 4.3 阈值建议（均需用户校准）

| 阈值 | 默认 | 含义 |
|---|---|---|
| `AutoConfFloor` | 0.85 | 自动确认置信下限 |
| `RiskAutoCeiling` | 0.25 | 自动确认风险上限 |
| `InfoConfFloor` | 0.60 | 低于此且卡信息缺口 → 补信息 |
| `RiskGate` | 0.60 | 达到即强制人工决策 |
| `K` | 2 | 不确定因子数达到即挤出 AUTO |

### 4.4 强制确认清单（`action_manifest`，配置项）

- 每个动作声明 `severity/reversible/force_confirm/ttl_hours`，版本化管理，**不写进 codex/LLM prompt**（防注入绕过）。
- 建议纳入（需用户确认）：发群消息、发外部消息、约会议、代码提交/部署、删除数据、涉 leader 对外动作。
- 未在清单中的 action_type → Router 直接 block 到 `need_decision`。

```yaml
actions:
  send_group_message: { severity: high, reversible: false, force_confirm: true,  ttl_hours: 24 }
  reply_dm_ack:       { severity: low,  reversible: true,  force_confirm: false }
  commit_code:        { severity: high, reversible: false, force_confirm: true,  ttl_hours: 24 }
  # 未列出的 action_type → need_decision
```

---

## 5. 状态机（Todo 生命周期 + Task 诞生）

Todo 和 Task 是**两个生命周期**，M4 是衔接点：

```
── Todo 生命周期(M3产出→M4处置) ──────────────────────────
  M3 ─► [extracted]
          │ 进入打分
          ▼
       [scoring] ── 打分/codex 异常、未知动作 (fail-fast) ──┐
          │                                               │
   ┌──────┼───────────────────┐                          │
   ▼      ▼                   ▼                          ▼
[auto] [need_info]      [need_decision] ◄────────────────┘
   │      │用户补充           │用户批准(可带修改方案) │用户拒绝
   │      ▼                   │                     ▼
   │  补 slot/回喂 M3          │                [dismissed](终态)
   │      │重评                │
   │      └──► 回 scoring      │
   │                          │
   └──────────┬───────────────┘
              ▼ 确认(auto=system / user)
        ┌───────────────────────────────────────┐
        │ TaskFactory 固化:                       │
        │   background 快照 + plan 快照 + action_hash │
        │   INSERT task(pending); todo→[confirmed] │
        └───────────────────┬─────────────────────┘
                            │
── Task 生命周期(M4诞生→M5执行) ──────────────────────────
                     [pending] ──M5 执行前 action_hash 校验──►
                     [executing] ──► [done] / [failed]
                            │
                     (M5 owns 执行态)

附加: need_info/need_decision 超 TTL → [expired]（加急重发, 绝不自动确认/生成 Task）
```

- **M4 写入的 Todo 状态**：`scoring / need_info / need_decision / auto(瞬态) / confirmed / dismissed / expired`。
- **M4 生成 Task**：只在 Todo 进入 `confirmed` 的**同一事务**里 `INSERT task(status=pending)`，保证"确认"与"生成 Task"原子。
- **Task 的 `pending→executing→done/failed` 归 M5**，M4 不碰。
- 状态守卫：Todo 带 `version`（乐观锁）；`need_* → confirmed/dismissed` 只允许一次，重复请求 no-op。双通道（后台+卡片）靠 `version` + `event_id` 去重。

### 5.0 TaskFactory 固化逻辑（Go）

```go
// 确认(user 或 auto)后，把 Todo 固化成 Task —— 单事务
func (f *TaskFactory) Materialize(ctx context.Context, todo *Todo, plan *Plan, confirmedBy string) (*Task, error) {
    return f.db.Transaction(func(tx *gorm.DB) (*Task, error) {
        // 1. 漂移校验:确认期间源 Todo 是否变化
        if todo.Version != todo.ScoredVersion {
            return nil, ErrTodoDrifted // fail-fast, 打回重评
        }
        // 2. 固化背景快照(关联 project/关键消息/相关记忆/交办人)
        bg := f.snapshotBackground(ctx, todo)
        // 3. 固化方案(用户确认过的 或 auto 采用 codex proposed_plan)
        planJSON := mustJSON(plan)
        actionHash := sha256Hex(canonical(todo.ActionType, todo.Slots, planJSON))
        // 4. 生成 Task(pending)
        task := &Task{
            TodoID: todo.ID, Title: todo.Title, ActionType: todo.ActionType,
            Background: mustJSON(bg), Plan: planJSON, Slots: mustJSON(todo.Slots),
            ConfirmedBy: confirmedBy, ConfirmedAt: time.Now(),
            ActionHash: actionHash, Status: "pending",
            ProjectID: todo.ProjectID, AutonomyMode: f.cfg.AutonomyMode,
        }
        if err := tx.Create(task).Error; err != nil { return nil, err } // uk_task_todo 兜底一对一
        // 5. 推进 Todo 状态 + 审计
        todo.Status = "confirmed"; todo.Version++
        tx.Save(todo)
        f.audit(tx, todo, task, confirmedBy)
        return task, nil
    })
}
```

- **background 快照**：从 project + 关键源消息 + mem0 检索结果 + 交办人组装，固化进 Task，执行时不再依赖可变源。
- **plan 快照**：`need_decision` 时用用户确认/编辑后的方案；`auto` 时用 codex `proposed_plan`（且 `plan_is_clear=true` 才允许 auto）。
- **action_hash**：`sha256(action_type + slots + plan)`，M5 执行前重算比对，防篡改。

---

## 6. 交互设计（双通道）

两个通道对同一份 Todo 确认态操作，后端落库是 single source of truth，卡片仅为交互入口与状态镜像。

### 6.1 管理后台（Hertz REST）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/confirmations?status=need_info,need_decision` | 待确认 Todo 队列 |
| GET | `/api/confirmations/{todo_id}` | 详情(分数分解、命中规则、源消息、mem0 佐证、codex 建议方案、备选) |
| POST | `/api/confirmations/{todo_id}/approve` | 批准(带 `expected_version`；可带编辑后的 plan) → 生成 Task |
| POST | `/api/confirmations/{todo_id}/reject` | 拒绝 `{reason}` → dismissed |
| POST | `/api/confirmations/{todo_id}/supplement` | 补充 `{slot_values?, free_text?}` |
| POST | `/api/confirmations/{todo_id}/decide` | 决策 `{option, plan_edits?}` |

Hertz handler 签名 `func(ctx context.Context, c *app.RequestContext)`。处置接口带 `expected_version`，状态已变返回 409。

队列/详情页必须呈现的**决策上下文包**（对齐"给人可决策的上下文，而非原始 payload"）：
- 动作明文（自然语言，不是 JSON）
- **codex 建议的明确方案（proposed_plan）+ 可编辑**（这是要固化进 Task 的方案）
- 参数 diff（before/after）而非裸 payload
- 决策理由（`confidence_basis`）+ 不确定点（`uncertainty_factors`）
- confidence / risk（带色）+ 逐因子分解 + 命中规则
- 不可逆标记、影响范围、涉敏感对象标记
- 源消息链接/摘录、关联 project/person/group
- "带修改批准" 入口 + SLA 截止时间

### 6.2 飞书交互卡片（lark-cli `--as bot`，Go 子进程）

给用户本人私聊发交互卡片。**前置**：开发者后台「事件与回调-回调配置」必须开启，否则收不到 `card.action.trigger`。回调经 WebSocket 长连接下发，无需公网回调 URL。

**发送**（Go exec.Command 调 lark-cli）：

```go
cmd := exec.CommandContext(ctx, "lark-cli", "im", "+messages-send", "--as", "bot",
    "--user-id", ownerOpenID,
    "--msg-type", "interactive", "--content", cardJSON,
    "--idempotency-key", fmt.Sprintf("conf_%d_v%d", todoID, version))
```

**卡片结构（Card 2.0）**按路由分形态：
- header 颜色：高 risk=red，中 risk=orange，need_info=blue。
- body：动作明文(markdown) → codex 建议方案 → 分数区 → 不可逆/涉敏感标记 → 源消息摘录 → 备选。
- 操作区：
  - **need_decision**：`批准并生成任务` 按钮(primary_filled) + callback `{act:"approve", todo_id, ver}`；`拒绝` 按钮(danger) + callback `{act:"reject"}`。危险动作加 `confirm` 二次确认。
  - **need_info**：`form` 容器放 `input`(对应缺失 slot，required:true) + 提交按钮(form_action_type:"submit")。

**监听回调**（Go 长驻子进程消费 NDJSON）：

```go
cmd := exec.CommandContext(ctx, "lark-cli", "event", "consume", "card.action.trigger",
    "--as", "bot", "--jq", `select(.action_tag=="button")`)
// 逐行解析 stdout NDJSON
```

关键回调字段：`operator_id`(须==本人)、`action_value`(fromjson→act/todo_id/ver)、`form_value`(补充表单值)、`token`(延迟更新，30分钟内、最多2次)、`event_id`(去重)、`message_id`。

**回写 + 更新卡片**（顺序即一致性保证）：
1. `event_id` 去重；校验 `operator_id`、Todo 当前为待确认态且 `ver` 匹配（乐观锁）。
2. **先写状态 + 审计（+ approve 时在同事务生成 Task）**，再更新卡片——落库是 SoT，卡片更新失败不回滚状态（fail-fast 记录卡片更新错误）。
3. 用 `token` 做**一次**延迟更新，把卡片改成终态：`✅ 已批准，已生成任务#<id>` / `❌ 已拒绝` / `📝 已收到补充，重新评估中`。

**token 限制**：30 分钟内最多 2 次，需完整卡片 JSON → 设计成"一次点击 → 一次终态更新"，不做中间"处理中"态。

### 6.3 双通道一致性

同一 Todo 被后台与卡片同时操作时，三层保证不重复确认/生成 Task：
1. 状态机单向转移守卫（`need_* → confirmed/dismissed` 仅一次）；
2. `event_id`/`idempotency_key` 去重；
3. `version` 乐观锁 + `task.uk_task_todo` 唯一约束（即便并发确认，DB 兜底一个 Todo 只生成一个 Task）。

---

## 7. 信息 / 决策回流

### 7.1 need_info 回流（两条路径）

| 路径 | 触发 | 处理 |
|---|---|---|
| **直接补 slot（fast path）** | 表单字段 1:1 映射到已声明缺失 slot | 直接写 `Todo.slots` → 回 `scoring` 重评 |
| **回喂 M3 重新提取** | 自由文本/改变理解的补充 | 作为源上下文增强交 M3 → M3 更新 Todo → 重新打分 |

判定：字段化且映射明确 → 直接补；自由文本/含新语义 → M3 重提。**重评轮次上限 N（默认 2）**：超过仍低置信 → 转 `need_decision`（fail-fast，不无限循环）。

### 7.2 need_decision 回写

| 用户动作 | 写回 |
|---|---|
| 选定选项 | 写决策/方案 → 回 `scoring` 重评 或 直接确认生成 Task |
| 批准 | 确认 → TaskFactory 生成 Task（`confirmed_by=user`），交 M5 |
| 带修改批准 | 先改 slot/plan，再确认生成 Task（plan 用编辑后的） |
| 拒绝 | `dismissed` + reason（终态，不生成 Task） |

`auto` 则 `confirmed_by=system` 直接确认生成 Task（plan 用 codex proposed_plan）。所有生成 Task 都过 §5.0 漂移校验，Task 带 action_hash 供 M5 再校验。

---

## 8. 审计与校准闭环

### 8.1 审计留痕（GORM，append-only）

每个决策（尤其自动确认的）写 `decision_audit`：

```go
type DecisionAudit struct {
    ID                     uint64    `gorm:"primaryKey"`
    TodoID                 uint64    `gorm:"index"`
    TaskID                 *uint64   `gorm:"index"` // 确认生成的 Task(如有)
    TS                     time.Time `gorm:"index"`
    Route                  string
    RouteReason            string
    ConfidenceEff          float64
    ConfidenceFactors      datatypes.JSON
    RiskEff                float64
    RiskFactors            datatypes.JSON
    MatchedRules           datatypes.JSON
    DecisionEngine         string // rule | codex
    CodexSessionID         string // codex 深判时
    ThresholdConfigVersion string
    Approver               string // system / user
    Channel                string // backend / feishu / auto
    EventID                string
    IdempotencyKey         string
    ActionHash             string
    FinalStatus            string
}
```

配置治理：阈值/强制清单版本化，改动走 code review，不在页面随手改。

### 8.2 校准闭环

审计喂校准：标注结果（M5 是否执行成功？用户是否事后推翻？）→ 按置信分箱算校准误差 → 产出阈值调整建议。**样本不足不启用校准**，保持保守。放开自动区前置条件：足够历史结果、实际正确率达标、M5 侧回滚可用且测过、有监控告警、变更有负责人。

---

## 9. 数据模型（MySQL / GORM）

Todo、Task 的完整 DDL 在总纲 `docs/00-overview.md` §2.4 定义（Todo 含 confidence/risk/route/missing_info/ttl_at/version；Task 含 todo_id/background/plan/action_hash/status）。M4 只需：
- 读写 `todo`（打分结果、状态流转）；
- 在确认时 `INSERT task`（TaskFactory）；
- 写 `decision_audit`（本模块私有，DDL 如 §8.1 GORM model 对应）。

```sql
CREATE TABLE decision_audit (
  id                       BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  todo_id                  BIGINT UNSIGNED NOT NULL,
  task_id                  BIGINT UNSIGNED NULL,
  ts                       DATETIME NOT NULL,
  route                    VARCHAR(16) NOT NULL,
  route_reason             VARCHAR(64) NOT NULL,
  confidence_eff           DECIMAL(4,3),
  confidence_factors       JSON,
  risk_eff                 DECIMAL(4,3),
  risk_factors             JSON,
  matched_rules            JSON,
  decision_engine          VARCHAR(16),   -- rule | codex
  codex_session_id         VARCHAR(128),
  threshold_config_version VARCHAR(32),
  approver                 VARCHAR(16),   -- system / user
  channel                  VARCHAR(16),   -- backend / feishu / auto
  event_id                 VARCHAR(64),
  idempotency_key          VARCHAR(64),
  action_hash              CHAR(64),
  final_status             VARCHAR(16),
  KEY idx_audit_todo (todo_id),
  KEY idx_audit_task (task_id),
  KEY idx_audit_ts (ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

`expired` 扫描由 robfig/cron 定时任务驱动，到期 → 加急重发 + 置 `expired`，绝不自动确认/生成 Task。

---

## 10. 开放问题清单（需用户校准）

> **本轮已定（机制层，总纲 §11.2）**：codex 决策的**灰区边界 / 频率 / 成本上限 / 超时 / 超预算与超时处置全部配置化**（§2.1.1 / §2.3），不硬编码。下方 #3 仅剩"默认阈值按真实数据校准"这一治理项；#4 无 repo 处理已在 §2.2 代码定（有 repo 带 `-C`、无则不带）。

1. **阈值**：`AutoConfFloor / RiskAutoCeiling / InfoConfFloor / RiskGate / K`（默认 0.85/0.25/0.60/0.60/2）。
2. **因子权重**：confidence/risk 各因子权重（§3.1/§3.2，新增 c6 方案明确度权重 0.20）。
3. **codex 灰区默认值校准**：机制已定（§2.3 配置化）；灰区默认 `conf∈[0.60,0.85] / risk∈[0.25,0.60]`、`max_calls_per_hour=30 / per_day=200`、`timeout=120s` 需按真实审计数据校准，`on_budget_exceeded` 默认 `route_need_decision`（是否允许 `degrade_to_rule` 需你确认）。
4. **无 repo Todo 的 codex 上下文**：已定——有 `project.repo_path` 则带 `-C`，无关联 repo 则不带（纯文本决策）。遗留：无 repo 时决策质量是否够，是否需要人工补 repo 关联。
5. **强制确认清单**：`force_confirm_list` 具体纳入哪些 action_type。
6. **矩阵拨盘**：中-confidence + 低-risk 何时放宽为 auto。
7. **plan 明确度门槛**：c6/`plan_is_clear` 达到什么程度才允许 auto 固化 Task（避免固化模糊方案）。
8. **TTL 与过期行为**：待确认 TTL（普通 vs 敏感）；expired 后仅重发还是需手动重激活。
9. **重评轮次上限 N**（默认 2）。
10. **need_info 路径边界**：何种补充走直接补 slot、何种回喂 M3。
11. **mem0 佐证打分方法**：检索 topK、阈值、如何折算 c4。
12. **校准节奏与最小样本**：个人系统样本稀疏，分箱最小样本、重算周期。
13. **单一审阅人假设**：仅 chujiejie.1 一人审阅，是否需委托/代批。
14. **用户 open_id 与 bot 凭据**：卡片发送需本人 ou_ 与 app 凭据，作为配置注入，与 M2 共用。

---

## 11. 参考资料（2026 HITL 最佳实践）

- Human-in-the-Loop Escalation Design（2026）：动作风险分级、校准数学（声称 90%≈真实 75%）、async-first、触发矩阵、EU AI Act Art.14 / NIST AI RMF / OWASP "Excessive Agency"。
- High-Stakes HITL Patterns：action_manifest（配置而非 prompt）、结构化置信度输出（basis/uncertainty/recommended_review）、审批 UX 反模式（无上下文二元批拒、缺补信息、timeout=approval）、分箱校准。
- 两轴门控（severity × confidence / reversibility）、双信号（trust + risk）、置信度不可直接信任。
- 飞书 lark-cli：`im +messages-send`（交互卡片）、`event consume card.action.trigger`（回调，WebSocket，token 30 分钟/2 次）、`interactive/v1/card/update`（延迟更新）。
- codex CLI：`codex exec -s read-only` 做无副作用决策，结构化 JSON 输出，退出码/JSON 非法即 fail-fast 转人工。
