# 审议式决策 Agent（Deliberation Loop）技术方案

> Status: obsolete / unimplemented
> Authority: non-normative archive
> Last verified: 2026-08-02 @ `89fa24b`

> 归档：本文是一份**未落地**的研究原型方案，仓库里从未存在对应的 `deliberation` module 与代码。保留作为设计思路参考，不代表当前实现。
> 目标：实现一种区别于 ReAct 的 agent 决策机制——agent 同时持有多个关注事项，每一拍自主决定「此刻推进哪一件、以及是否行动」，并输出可解释的决策轨迹。
> 语言：Go 1.26。LLM 调用抽象为接口，原型用 mock 实现，可替换为真实模型。

---

## 0. 设计原则

1. **fail-fast**：输入非法、状态非法、评估缺失即返回错误，不静默降级、不猜测补全。
2. **模块化与边界**：本原型与 Jarvis 的 M2–M5 是平行关系，独立 module，不互相依赖。
3. **非行动是一等决策**：`WAIT / ASK / SKIP / NOTHING` 与 `ACT` 地位相同，抉择层可以合法地输出「本拍不行动」。
4. **hybrid 决策**：规则打分为主（快、零成本、确定），仅纠结区调用 LLM 深判，控制成本并保留可解释性。
5. **可解释**：每一拍产出一条决策记录（当前关注项、各维度分、是否进灰区、最终决定、理由），供人工判读。
6. **配置化**：打分权重、灰区边界、tick 周期为配置项，给保守默认，不硬编码。

---

## 1. 与 ReAct 的区别

ReAct 是反应式循环：`Thought → Action → Observation → Thought`。其隐含假设与本方案的差异如下。

| 维度 | ReAct | 审议循环 |
|---|---|---|
| 决策对象 | 单一当前任务 | 全部关注事项（多 Concern 并行权衡） |
| 每拍是否必行动 | 是，每轮必产一个 action | 否，`WAIT/ASK/SKIP/NOTHING` 为一等决策 |
| 关注事项来源 | 外部给定的任务 | agent 从环境自主派生 |
| 审思深度 | 每步一致（均过 LLM） | 快思为主，仅灰区调用 LLM |
| 驱动方式 | 循环运行至任务结束 | 被唤醒（定时 tick + 事件），空闲时不动作 |

本方案的核心主轴是前两行：**自主选择**（在多关注事项间自己排优先级）+ **行动闸门**（自己决定是否出手）。其余为增强项（见 §7）。

---

## 2. 内核：审议循环

一个 tick 产出一个**决定**，而非一个动作。决定可以是「不行动」。

```
被唤醒（定时 tick 每 N 秒；事件到达即时触发）
   │
[1] Perceive 感知    读取环境自上一拍以来的新变化，自主派生或更新 Concern
   │
[2] Appraise 评估    对每个 open 状态 Concern 用规则算四个分（[0,1]，零 LLM 成本）：
   │                   importance / urgency / readiness / confidence
   │
[3] Choose 抉择      枚举 {Concern × 候选下一步}，逐个规则粗判：
   │                   明确该做 / 明确不该做 → 直接定；落灰区 → 转 [3.5]
   │                  候选池恒含 WAIT / ASK / SKIP / NOTHING
   │                  产出「本拍唯一决定」
   │
[3.5] Deliberate 深想（仅灰区）  LLM 输入心智全景与灰区候选，输出选择与理由
   │
[4] Act 行动         决定为 ACT 才执行对应动作；其余决定各自处理（见 §5）
   │
[5] Learn 更新       结果、新观察、时间流逝写回 Concern；追加决策轨迹
   │
   └── 回到待命
```

### 2.1 唤醒（驱动）

两种唤醒源并存：

- **定时 tick**：每 `tick_interval` 触发一次，处理时间流逝相关的判断（等待条件到点、时机成熟度变化）。
- **事件触发**：环境有新输入时立即触发一次，避免固定周期造成的响应延迟。

两源最终都进入同一条 tick 处理路径；并发唤醒由单 goroutine 串行消费（原型阶段单线程，无并发写 Concern）。

---

## 3. 核心数据结构

### 3.1 Concern（关注事项：agent 的一个内心条目）

```go
type Concern struct {
    ID      string
    Summary string  // 一句话描述
    Origin  Origin  // 派生来源（哪条观察 / 哪个总目标）
    State   State   // open | waiting | done | dropped

    // Appraise 四维（规则算，范围 [0,1]）
    Importance float64
    Urgency    float64
    Readiness  float64 // 时机成熟度：所等条件是否满足
    Confidence float64 // 对「如何推进」的把握

    WaitingOn *WaitCond // State=waiting 时，记录在等什么
    History   []Event   // 经历过的事件，支撑可解释
}
```

### 3.2 State（Concern 生命周期）

```
open ──决定 WAIT / ASK──► waiting ──条件满足──► open
 │                          │
 │                          └──条件失效──► dropped
 ├──决定 ACT 且动作完成──► done
 └──决定 SKIP──► dropped
```

- `open`：可被本拍纳入抉择。
- `waiting`：在等条件（时间到 / 某事件 / 主人回复），不参与抉择，直到 `WaitCond` 满足回到 `open`。`ASK` 后进入此态等回复。
- `done`：已完成（终态），由 `ACT` 动作完成或某个 `waiting` Concern 的回复使其了结触发。
- `dropped`：已放弃（终态）。

### 3.3 Decision（抉择输出）

```go
type Decision struct {
    Kind      DecisionKind // ACT | WAIT | ASK | SKIP | NOTHING
    ConcernID string       // NOTHING 时为空
    Action    *Action      // Kind=ACT 时非空
    Until     *WaitCond    // Kind=WAIT 时非空
    Question  string       // Kind=ASK 时非空
    Reason    string       // 决策理由（可解释）
    ViaLLM    bool         // 是否经过灰区 LLM 深想
}
```

### 3.4 决策轨迹（可解释）

每拍追加一条记录：唤醒源、当前 open/waiting Concern 快照、各维度分、候选是否落灰区、最终 Decision、理由。原型将其打印为可读文本，作为「判读它是否像人」的依据（§6）。

---

## 4. Hybrid 抉择（快思 + 慢想）

### 4.1 规则粗判（快思，默认路径）

对每个候选 `{Concern, 候选下一步}` 用规则算优先级并做闸门判断：

- 优先级 `priority = w_imp·importance + w_urg·urgency + w_rd·readiness`，权重可配。
- `confidence` 低于 `conf_low` → 倾向 `ASK`（把握不足，先问）。
- `readiness` 低于 `readiness_gate` → 倾向 `WAIT`（时机未到）。
- 所有 open Concern 均无足够优先级 → `NOTHING`。

明确落在「该做」或「不该做」区间的，规则直接产出 Decision，不调用 LLM。

### 4.2 灰区判定

当候选同时满足「优先级中等」且「confidence 中等」（落配置的灰区区间），规则无法可靠拍板，转 §4.3 LLM 深想。灰区边界 `conf_low/conf_high`、`priority_low/priority_high` 为配置项。

### 4.3 LLM 深想（慢想，仅灰区）

```go
type Deliberator interface {
    // 输入心智全景与灰区候选，输出选择与理由。
    // 失败（超时 / 非法输出）由调用方 fail-fast 处理，不静默降级。
    Deliberate(ctx context.Context, mind MindSnapshot, candidates []Candidate) (*Decision, error)
}
```

- 原型提供 `MockDeliberator`（确定性规则模拟「深想」，便于跑通与测试），接口可替换为真实模型客户端。
- fail-fast：深想失败时，本拍对该灰区候选降级为 `ASK`（向人求助），并在轨迹标注失败原因；不猜测、不静默选一个。

---

## 5. 行动层（Act）

抉择产出 Decision 后：

| Decision | 处理 |
|---|---|
| `ACT` | 执行 `Action`（原型中作用于模拟世界 `world`），结果写回 Concern。 |
| `WAIT` | Concern → `waiting`，记录 `WaitCond`；不产生外部动作。 |
| `ASK` | 产出一个「向主人提问」动作（原型中记录到轨迹与 world），Concern → `waiting`（等回复）。 |
| `SKIP` | Concern → `dropped`，记录理由。 |
| `NOTHING` | 不产生任何动作，仅记录理由。 |

原型的动作集合限于模拟世界（回复、提交草稿、查询等抽象动作）。真实执行后端（对接 Jarvis M5）为后续增强项，本期不实现。

---

## 6. 成功标准与 Demo 场景

成功标准：**在一个模拟场景里跑通审议循环，且决策轨迹肉眼可判为「合理、克制、会等会问」**。不做与 ReAct 的量化对比（列为后续）。

Demo 场景（`cmd/demo`）：一个「个人助理」模拟世界，按时间注入事件，逐拍打印决策轨迹。预期行为示例：

| 时刻 | 环境事件 | 预期决策 | 说明 |
|---|---|---|---|
| t0 | leader 提出「下周要一份方案」 | `WAIT` | 派生 Concern，但 readiness 低（尚早），先等。 |
| t1 | 同事询问「review 是否完成」 | `ACT(回复)` | 急且时机成熟，出手。 |
| t2 | 出现模糊消息「那个事你看着办」 | `ASK` | confidence 低、落灰区，LLM 深想后判定为向主人提问。 |
| t3 | 无新事件，所有 Concern 在等或未到点 | `NOTHING` | 保持安静，不产生无用动作。 |

判读点：是否会在时机未到时 `WAIT`、把握不足时 `ASK`、无事时 `NOTHING`，而非每拍都行动。

---

## 7. 后续增强项（本期不实现，文档留位）

1. **C 可变深度审思**：高赌注候选在抉择前做多方案前向推演（快思 vs 慢想的深度可变），当前仅在灰区做一次 LLM 深想。
2. **D 元认知/自我修正**：每 N 拍额外自检「近期决策是否原地打转或方向错误」，必要时推翻既有 Concern。
3. **接入 Jarvis**：Concern ↔ Todo、Action ↔ M5 executor 的映射；本原型的抉择内核可作为 Jarvis 调度层候选。
4. **真实 LLM 深想**：以真实模型替换 `MockDeliberator`。

---

## 8. 目录结构

```
deliberation/
├── go.mod                       # 独立 module deliberation
├── README.md
├── docs/design.md               # 本文档
├── cmd/demo/main.go             # 跑通模拟场景，打印决策轨迹
└── internal/
    ├── agent/loop.go            # 审议循环内核（tick）
    ├── concern/concern.go       # Concern / State / Decision 数据结构
    ├── perceive/perceive.go     # 从环境自主派生/更新 Concern
    ├── appraise/appraise.go     # 规则四维打分（快思）
    ├── choose/choose.go         # 抉择：粗判 + 灰区路由
    ├── deliberate/llm.go        # LLM 深想接口 + MockDeliberator
    ├── act/act.go               # 行动层（ACT/WAIT/ASK/SKIP/NOTHING 处理）
    └── world/world.go           # 模拟环境（注入事件、承接动作、推进时间）
```

---

## 9. 开放问题（需校准，不阻塞原型）

1. Appraise 四维权重、灰区边界、`readiness_gate`、`conf_low/high` 的默认值需按实际轨迹校准。
2. Concern 自主派生的粒度：一条环境观察何时该派生新 Concern、何时并入已有 Concern。
3. `NOTHING` 与 `WAIT` 的边界：全局无可做 vs 单事项在等，轨迹如何清晰区分。
4. 增强项 C/D 的接入时机与形态。
