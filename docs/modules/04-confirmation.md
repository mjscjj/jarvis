# M4 用户确认模块 技术方案

> 所属项目：基于飞书的本地个人 Jarvis 管家系统（用户：字节研发工程师 chujiejie.1）
> 流水线定位：采集(M2) → 记忆化(M2) → 提取(M3) → **打分路由(M4·本模块)** → 执行(M5)
> 版本：v0.1（待用户校准阈值与强制确认清单后定稿）

---

## 0. 模块定位与边界

M4 是"人在环路(Human-in-the-Loop)"的**决策闸门(escalation gate)**：接收 M3 产出的候选 Task，用 `confidence × risk` 二维打分决定它是**直接自动执行**、**需要补充信息**还是**需要用户决策**，并驱动"飞书卡片 + 管理后台"双通道让用户处置，最后把用户的补充/决策回流并把"可执行"的 Task 交给 M5。

核心立场（对齐 2026 HITL 最佳实践）：**打分只用来决定"要不要问人"，不用来替人拍板。** LLM 的自评置信度系统性过度自信（声称 90% 往往只有 ~75% 真实正确率），所以本模块把置信度当**信号**而非**真相**，并在任何不确定处一律**向人**。

### 0.1 上下游接口

| 方向 | 对端 | 输入/输出 | 说明 |
|---|---|---|---|
| 入 | M3 提取 | 候选 `Task{action_type, slot, source, project_id, person_id}` | M3 负责语义提取，M4 不重复做提取 |
| 出 | M3 提取 | 重新提取请求 `{task_id, source_ctx, supplement}` | 信息回流时把用户补充回喂 M3 再提取 |
| 出 | M5 执行 | 已 `approved` 的 Task | M5 只执行 M4 判定为可执行的 Task |
| 读 | mem0 | 记忆检索（佐证强度） | 只读，用于 confidence 打分；不可用时不臆造佐证 |
| 读写 | MySQL | Task 确认态 + 审计表 | 本地可信明文存储，审计表 append-only |
| 出入 | lark-cli(`--as bot`) | 发交互卡片 / 监听 `card.action.trigger` | 双通道之一 |

### 0.2 子组件拆分

```
M4 用户确认
├── Scorer          LLM 结构化打分（逐因子 0-1 + basis + uncertainty + recommended_review）
├── RuleEngine      规则修正 + 强制确认清单（action_manifest，配置驱动，不进 LLM prompt）
├── Router          二维矩阵 + 阈值 → 三路由（fail-fast 优先级判定）
├── Calibrator      置信度校准（从审计结果学习，阶段二启用，样本不足时不启用）
├── ConfirmStore    Task 确认态 + 审计（MySQL）
├── Channels
│   ├── BackendAPI      FastAPI 待确认队列页 + 处置接口
│   └── FeishuCard      lark-cli 发送/回调/延迟更新
└── ReflowHandler   信息回流（补 slot / 回喂 M3）+ 决策回写
```

---

## 1. 设计原则与 fail-fast 约束

本模块严格遵循全局设计原则，落到具体规则：

1. **打分失败 → 向人，绝不自动执行。** LLM 超时/报错/返回非法 JSON/缺 confidence 字段，一律路由到 `need_decision`，记录异常，不静默降级、不猜测。
2. **未知 action_type → block 到人工。** 不在 `action_manifest` 里的动作 = 未分类 = 未知最坏后果，路由 `need_decision` 并要求先补分类，绝不 auto。
3. **强制确认清单是配置项，不是硬编码护栏。** 哪些 action_type 无论分数都必须人工（发群消息、约会议等对外不可逆动作），写在 `action_manifest` 配置里、由用户校准；审批要求在**工作流/配置层**判定，**不由 LLM 运行时协商**（防 prompt injection 把 Agent"说服"跳过确认）。不擅自堆叠过度护栏。
4. **规则修正单向。** 规则只能**拉高 risk / 拉低 confidence**（fail-safe 方向），不能反向；避免规则把本该问人的动作"洗白"成自动。
5. **mem0 不可用不臆造。** 佐证因子记为未知（=0 贡献），自然拉低 confidence 趋向人工，不假装有佐证。
6. **超时 ≠ 批准。** 待确认超 TTL 未处理 → 进 `expired` 加急重发，绝不自动执行（timeout=approval 是治理事故）。
7. **执行前漂移校验。** `approved → executing` 前校验 `action_hash`；若源消息或 slot 在待确认期间变化，打回重评，不执行陈旧决策。

---

## 2. 打分模型

两个独立维度，各输出 `[0,1]`：

- **confidence**：对"任务理解是否正确/明确"的把握（越高越有把握）。
- **risk**：执行出错的负面影响（越高越危险）。

采用 **LLM 结构化打分 + 规则修正（rule override）** 两段式。LLM 逐因子打分并显式暴露不确定性；RuleEngine 再做单向修正。

### 2.1 confidence 因子表

| 因子 | 符号 | 含义 | 默认权重 | 打分来源 |
|---|---|---|---|---|
| slot 完整度 | c1 | 必填 slot 是否齐全（缺 → 低） | 0.30 | 规则(必填命中率) + LLM |
| 消息明确度 | c2 | 原始消息表述清晰、指令性强 | 0.20 | LLM |
| 无歧义度 | c3 | `1 − 歧义程度`（多解/指代不清 → 低） | 0.20 | LLM |
| mem0 佐证强度 | c4 | 记忆中有一致上下文支撑该理解 | 0.20 | mem0 检索 + LLM |
| 来源可靠度 | c5 | source（谁说的/哪个渠道）可靠、直接面向用户 | 0.10 | 规则 + LLM |

```
confidence_raw = 0.30·c1 + 0.20·c2 + 0.20·c3 + 0.20·c4 + 0.10·c5
```

### 2.2 risk 因子表

| 因子 | 符号 | 含义 | 默认权重 |
|---|---|---|---|
| 不可逆性 | r1 | 执行后能否干净撤销（不可逆 → 高） | 0.30 |
| 对外触达 | r2 | 是否对外发消息/触达他人 | 0.25 |
| 改代码/系统 | r3 | 是否改代码、配置、生产系统 | 0.20 |
| 影响范围 | r4 | 影响人数/资产范围（blast radius） | 0.15 |
| 涉敏感对象 | r5 | 是否涉及 leader / 外部人员 | 0.10 |

```
risk_raw = 0.30·r1 + 0.25·r2 + 0.20·r3 + 0.15·r4 + 0.10·r5
```

### 2.3 LLM 结构化输出契约

打分调用要求 LLM 返回结构化 JSON（借鉴 2026 production 模式，强制模型显式暴露"它不确定什么"）：

```json
{
  "confidence_factors": {"c1": 0.9, "c2": 0.8, "c3": 0.7, "c4": 0.5, "c5": 0.9},
  "risk_factors": {"r1": 0.2, "r2": 0.0, "r3": 0.0, "r4": 0.1, "r5": 0.0},
  "confidence_basis": "slot 齐全，消息为明确的待办指令",
  "uncertainty_factors": ["未确认截止时间是否为本周五"],
  "recommended_review": false
}
```

- `confidence_basis` / `uncertainty_factors`：既提升路由可靠性，也直接作为**给人的上下文**（避免审阅时"盲批"）。
- `recommended_review`：模型自评"即使分高我也建议人看看"的信号；**为 true 时强制离开 AUTO 区**（模型自升级检查是比单一置信度更诚实的信号）。

### 2.4 规则修正（rule override，单向）

confidence 硬下压（只降不升）：

| 触发条件 | 修正 |
|---|---|
| 必填 slot 缺失 | `confidence_eff = min(raw, 0.30)`，`blocker = info_gap` |
| `recommended_review = true` 或 `len(uncertainty_factors) ≥ K`(默认 2) | `confidence_eff = min(raw, info_conf_floor − ε)`（挤出 AUTO） |
| mem0 检索失败/不可用 | `c4 = 0`（不臆造佐证） |
| 打分异常/超时/JSON 非法 | `confidence_eff = 0`（fail-fast） |

risk 硬上抬（只升不降）：

| 触发条件（示例，最终以 `action_manifest` 配置为准） | 修正 |
|---|---|
| `action_type` ∈ 对外发消息类 | `risk_eff = max(raw, 0.80)` |
| `action_type` ∈ 改代码/部署类 | `risk_eff = max(raw, 0.70)` |
| 涉及 leader / 外部人员 | `risk_eff = max(raw, 0.60)` |
| 动作不可逆（r1 高） | `risk_eff = max(raw, 0.60)` |

> "对外发消息强制拉高 risk"即上表第一行——这是**规则修正**，独立于强制确认清单（§3.3）。两者都由 `action_manifest` 配置，用户可校准。

### 2.5 校准（confidence calibration）

LLM 置信度不可直接信任，需**分箱校准**：

```
confidence_cal = calibrate(confidence_raw)   # 默认 identity
```

- 从审计表按 0.1 分箱，比较"声称置信 vs 实际正确率"，学习单调映射（isotonic 回归）。若某箱声称 0.9 实际仅 0.6，说明过度自信，需上调该动作的 auto 阈值。
- **校准前保守**：未达每箱最小样本（默认 ≥10）不启用校准，保持保守阈值、宁可多问。
- 个人系统样本天然稀疏，校准周期长（见开放问题 §9）。阈值调整属**治理动作**，走配置版本 + 评审，不在页面随手改。

---

## 3. 三路由决策

三条路由对齐用户原始需求：`auto_approved`(直接交 M5) / `need_info`(补信息) / `need_decision`(做决策)。

### 3.1 判定优先级（fail-fast，first-match-wins）

```python
def route(task) -> Route:
    if task.scoring_failed:                    return NEED_DECISION   # fail-fast，绝不 auto
    if task.action_type not in action_manifest: return NEED_DECISION   # 未知动作 = block 人工
    if task.action_type in force_confirm_list:  return NEED_DECISION   # 强制确认清单(配置)
    if task.has_missing_required_slot and task.risk_eff < RISK_GATE:
                                               return NEED_INFO       # 缺信息且非高风险 → 补
    if task.risk_eff >= RISK_GATE:             return NEED_DECISION   # 高风险强制人工
    if (task.confidence_eff >= AUTO_CONF_FLOOR
        and task.risk_eff < RISK_AUTO_CEILING
        and not task.recommended_review):      return AUTO_APPROVED   # 唯一自动区
    if task.confidence_eff < INFO_CONF_FLOOR and task.blocker == "info_gap":
                                               return NEED_INFO
    return NEED_DECISION                       # 灰区默认安全 → 人工
```

### 3.2 二维矩阵（分数驱动部分的可视化）

在通过"未知动作 / 强制清单 / 缺 slot"三道前置检查后，剩余由 `confidence × risk` 决定：

分箱：confidence 低 `<0.60` / 中 `0.60–0.85` / 高 `≥0.85`；risk 低 `<0.25` / 中 `0.25–0.60` / 高 `≥0.60`。

| confidence ＼ risk | 低 `<0.25` | 中 `0.25–0.60` | 高 `≥0.60` |
|---|---|---|---|
| **高 `≥0.85`** | ✅ auto_approved | 🟡 need_decision | 🔴 need_decision |
| **中 `0.60–0.85`** | 🟡 need_decision ＊ | 🟡 need_decision | 🔴 need_decision |
| **低 `<0.60`** | 🟠 need_info | 🟠 need_info → decision | 🔴 need_decision |

> ＊ **中 confidence + 低 risk** 是主要的"**校准拨盘**"：默认保守判 `need_decision`，当审计数据证明该区实际正确率达标后，可放宽为 `auto_approved`（对应"何时安全地减少人工"）。这是用户随信任增长逐步放开自动化的抓手。

设计取向：**默认自动区很窄**（只有右上角）。这是刻意保守——个人管家早期宁可多问，避免"自动区过宽"是本模块最贵的错误。

### 3.3 阈值建议（均需用户校准）

| 阈值 | 默认值 | 含义 |
|---|---|---|
| `AUTO_CONF_FLOOR` | 0.85 | 自动执行的置信下限（因 LLM 过度自信而取高） |
| `RISK_AUTO_CEILING` | 0.25 | 自动执行的风险上限 |
| `INFO_CONF_FLOOR` | 0.60 | 低于此且卡在信息缺口 → 补信息 |
| `RISK_GATE` | 0.60 | 达到即强制人工决策 |
| `K`(uncertainty 数) | 2 | 不确定因子达到即挤出 AUTO |

> **所有阈值都是起点，不是常量**，必须对着真实流量校准。文档不替用户拍板具体值。

### 3.4 强制确认清单（`force_confirm_list`，配置项）

- **本质**：`action_manifest` 中每个动作声明 `approval_required` / `force_confirm`，与 severity/reversible 一同版本化管理，**不写进 LLM prompt**。
- **建议纳入（需用户确认）**：发群消息、发外部消息、约会议/发日程邀请、代码提交/部署、删除数据、涉及 leader 的对外动作。
- **fail-fast 立场**：不擅自设计过度护栏；清单内容、是否强制、超时行为全部交用户校准（§9）。

`action_manifest` 片段示例：

```yaml
actions:
  send_group_message:
    severity: high
    reversible: false
    force_confirm: true          # 强制确认清单
    ttl_hours: 24
  reply_dm_ack:                  # 回复私聊"收到"这类低风险
    severity: low
    reversible: true
    force_confirm: false
  commit_code:
    severity: high
    reversible: false
    force_confirm: true
    ttl_hours: 24
  # 未在清单中的 action_type → Router 直接 block 到 need_decision
```

---

## 4. Task 状态机

```
                          ┌─────────────┐
   M3 候选 Task ────────▶ │  extracted  │
                          └──────┬──────┘
                                 │ 进入打分
                                 ▼
                          ┌─────────────┐   打分异常 / 未知动作 (fail-fast)
                          │   scoring   │─────────────────────────┐
                          └──────┬──────┘                          │
             ┌───────────────────┼────────────────────┐           │
             ▼                   ▼                    ▼           ▼
      ┌─────────────┐    ┌─────────────┐      ┌───────────────────────┐
      │auto_approved│    │  need_info  │      │     need_decision      │
      └──────┬──────┘    └──────┬──────┘      └───┬───────────────┬───┘
             │(系统)            │用户补充            │用户批准/带修改批准 │用户拒绝
             │                  ▼                   │               │
             │        补 slot / 回喂 M3 重新提取     │               ▼
             │                  │                   │         ┌──────────┐
             │                  └──▶ 回到 scoring 重评│         │ rejected │(终态)
             │                                       │         └──────────┘
             ▼                                       ▼
      ┌──────────┐  approver=system            ┌──────────┐ approver=user
      │ approved │◀────────────────────────────│ approved │
      └────┬─────┘                             └────┬─────┘
           │  执行前 action_hash 漂移校验（漂移→打回 scoring）
           │  交 M5
           ▼
      ┌───────────┐
      │ executing │
      └─────┬─────┘
        ┌───┴───┐
        ▼       ▼
     ┌────┐  ┌──────┐
     │done│  │failed│   (终态)
     └────┘  └──────┘

附加状态 expired：
   need_info / need_decision 超过 action_manifest 配置的 TTL 未处理
   → expired（加急重发通知，绝不自动执行）
   → 用户重新激活可回到对应待确认态
```

状态转移守卫（保证幂等与一致性）：

- 每个 Task 带 `version`（乐观锁）；`need_* → approved/rejected` 只允许一次，重复请求变 no-op。
- `auto_approved` 是路由结果，随即自动置 `approved`（`approver=system`）并落审计，再交 M5。
- 双通道（后台 + 卡片）操作同一 Task，靠状态守卫 + `event_id`/`idempotency_key` 去重防重复批准。

---

## 5. 交互设计（双通道）

两个通道对同一份 Task 确认态操作，后端落库是 **single source of truth**，卡片仅为交互入口与状态镜像。

### 5.1 管理后台（FastAPI）

待确认队列页 + 处置接口：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/confirmations?status=need_info,need_decision` | 待确认队列 |
| GET | `/confirmations/{task_id}` | 详情（分数分解、命中规则、源消息、mem0 佐证、备选项） |
| POST | `/confirmations/{task_id}/approve` | 批准（带 `expected_version` 乐观锁） |
| POST | `/confirmations/{task_id}/reject` | 拒绝 `{reason}` |
| POST | `/confirmations/{task_id}/supplement` | 补充 `{slot_values? , free_text?}` |
| POST | `/confirmations/{task_id}/decide` | 决策 `{option, edits?}` |

队列/详情页必须呈现的**决策上下文包**（对齐"给人可决策的上下文，而非原始 payload"）：

- 动作明文（自然语言，不是 JSON）
- 参数 **diff**（before/after 字段）而非裸 payload
- 决策理由（`confidence_basis`）+ 不确定点（`uncertainty_factors`）
- confidence / risk（带色）+ 逐因子分解 + 命中的规则
- 不可逆标记、影响范围、涉敏感对象标记
- 源消息链接 / 摘录、关联 project/person
- 备选方案 + **"带修改批准(reject with edits)"** 入口 + SLA 截止时间

并发控制：处置接口带 `expected_version`；状态已变 → 返回 409，前端提示"已被另一通道处理"。

### 5.2 飞书交互卡片（lark-cli `--as bot`）

给用户本人私聊发交互卡片。**前置**：开发者后台「应用 - 事件与回调 - 回调配置」必须开启，否则收不到 `card.action.trigger`（无预检）。回调经现有 WebSocket 长连接下发，无需公网回调 URL。

**发送**（bot → 用户私聊）：

```bash
lark-cli im +messages-send --as bot --user-id ou_<chujiejie> \
  --msg-type interactive --content '<card_json>' \
  --idempotency-key conf_<task_id>_v<version>
```

**卡片结构（Card 2.0）**，按路由分形态：

- header 颜色语义：高 risk = red，中 risk = orange，`need_info` = blue。
- body：动作明文（markdown）→ 分数区（confidence/risk + 因子）→ 不可逆/涉敏感标记 → 源消息摘录 → 备选。
- 操作区：
  - **need_decision（批准/拒绝）**：`批准` 按钮 `primary_filled` + `behaviors:[{type:"callback", value:{act:"approve", task_id, ver}}]`；`拒绝` 按钮 `danger` + callback `{act:"reject", ...}`。危险动作加 `confirm` 二次确认。多选决策用 `select_static` 列选项 + 批准。
  - **need_info（补信息）**：`form` 容器内放 `input`（对应缺失 slot，`required:true`）+ 提交按钮（`form_action_type:"submit"`）。

批准/拒绝按钮 callback 值示例：

```json
{
  "tag": "button",
  "text": {"tag": "plain_text", "content": "批准并执行"},
  "type": "primary_filled",
  "width": "fill",
  "confirm": {"title": {"tag":"plain_text","content":"确认批准？"},
              "text": {"tag":"plain_text","content":"将对外发送，动作不可逆"}},
  "behaviors": [{"type": "callback",
                 "value": {"act": "approve", "task_id": "T123", "ver": 3}}]
}
```

**监听回调**：

```bash
lark-cli event consume card.action.trigger --as bot \
  --jq 'select(.action_tag=="button")'
```

关键回调字段：`operator_id`(须 == 用户本人)、`action_value`(fromjson → `act/task_id/ver`)、`form_value`(补充信息表单值)、`token`(延迟更新，30 分钟内、最多 2 次)、`event_id`(去重)、`message_id`。

**回写 + 更新卡片**（顺序即一致性保证）：

1. `event_id` 去重（幂等）；校验 `operator_id`、Task 当前为待确认态且 `ver` 匹配（乐观锁）。
2. **先写 Task 状态 + 审计（落库）**，再更新卡片——落库是 source of truth，卡片更新失败不影响状态一致性（fail-fast 记录卡片更新错误，不回滚状态）。
3. 用 `token` 做**一次**延迟更新，把卡片改成终态：

```bash
lark-cli api POST /open-apis/interactive/v1/card/update --as bot \
  --data '{"token":"<token>","card":<new_card_json>}'
```

终态文案示例：`✅ 已批准，已交执行` / `❌ 已拒绝` / `📝 已收到补充，重新评估中`。

**token 限制的设计约束**：30 分钟内、**最多 2 次**、需完整卡片 JSON。因此设计成"**一次点击 → 一次终态更新**"，不做中间"处理中"态以免耗尽次数；补充信息表单提交是另一次交互（带新 token）。

### 5.3 双通道一致性

同一 Task 被后台与卡片同时操作时，靠三层保证不重复批准/执行：

1. 状态机**单向转移守卫**（`need_* → approved/rejected` 仅一次）；
2. `event_id` / `idempotency_key` 去重；
3. `version` 乐观锁。

先到先得，后到者变 no-op，并在卡片/页面提示"已被处理"。

---

## 6. 信息 / 决策回流

### 6.1 need_info 回流（两条路径）

| 路径 | 触发 | 处理 |
|---|---|---|
| **直接补 slot（fast path）** | 表单字段 1:1 映射到已声明的缺失 slot | 直接写 `Task.slot` → 回到 `scoring` 重评 |
| **回喂 M3 重新提取** | 自由文本 / 改变理解的补充 | 作为源消息上下文增强交给 M3 → M3 重新提取产出更新后的候选 Task → 重新打分 |

判定：字段化且映射明确 → 直接补；自由文本 / 含新语义 → M3 重提。

**重评轮次上限 `N`（默认 2）**：超过仍低置信 → 转 `need_decision`，不无限循环（fail-fast）。

### 6.2 need_decision 回写

| 用户动作 | 写回 Task |
|---|---|
| 选定选项 | 写决策 slot / 动作参数 → 回 `scoring` 重评或直接 `approved` |
| 批准 | `approved`（`approver=user`, 记 channel） → 交 M5 |
| 带修改批准 | 先改 slot，再 `approved` |
| 拒绝 | `rejected` + `reason`（终态） |

`auto_approved` 则 `approver=system` 直接 `approved` 交 M5。所有交 M5 前都过 §1.7 的 `action_hash` 漂移校验。

---

## 7. 审计与校准闭环

### 7.1 审计留痕

每个决策（**尤其自动执行的**）写 append-only 审计记录：

| 字段 | 说明 |
|---|---|
| `id, task_id, ts` | 主键/关联/时间 |
| `route, route_reason` | 路由结果与命中原因 |
| `confidence_eff` + `confidence_factors`(JSON) | 生效置信度 + 逐因子 |
| `risk_eff` + `risk_factors`(JSON) | 生效风险 + 逐因子 |
| `matched_rules`(JSON) | 命中的规则修正 |
| `threshold_config_version` | 当时的阈值/清单配置版本 |
| `approver`(system/user), `channel`(backend/feishu/auto) | 谁、从哪批的 |
| `event_id, idempotency_key, action_hash` | 幂等与漂移校验凭据 |
| `final_status` | done/failed/rejected/expired |

配置治理：阈值 / 强制清单版本化，改动走 code review（治理动作），不在页面随手改。

### 7.2 校准闭环

审计喂校准：标注结果（M5 是否执行成功？用户是否事后推翻/投诉？）→ 按置信分箱算校准误差 → 产出阈值调整建议。**样本不足不启用校准**，保持保守。放开自动区（如 §3.2 的中-conf/低-risk 拨盘）的前置条件：足够历史结果、实际正确率达标、M5 侧回滚可用且测过、有监控告警、变更有明确负责人。

---

## 8. 数据模型（MySQL）

Task 增加确认态字段（不引入历史数据兼容——本项目为全新库，如需迁移请与用户确认）：

```sql
-- Task 表新增列（确认态）
ALTER TABLE task ADD COLUMN confirm_status   VARCHAR(16)  NOT NULL DEFAULT 'extracted';
ALTER TABLE task ADD COLUMN route            VARCHAR(16)  NULL;      -- auto_approved/need_info/need_decision
ALTER TABLE task ADD COLUMN blocker          VARCHAR(16)  NULL;      -- info_gap 等
ALTER TABLE task ADD COLUMN confidence_eff   DECIMAL(4,3) NULL;
ALTER TABLE task ADD COLUMN risk_eff         DECIMAL(4,3) NULL;
ALTER TABLE task ADD COLUMN action_hash      CHAR(64)     NULL;      -- 漂移校验
ALTER TABLE task ADD COLUMN ttl_at           DATETIME     NULL;      -- 待确认过期
ALTER TABLE task ADD COLUMN version          INT          NOT NULL DEFAULT 0;  -- 乐观锁

-- 决策审计（append-only）
CREATE TABLE task_decision_audit (
  id                       BIGINT AUTO_INCREMENT PRIMARY KEY,
  task_id                  BIGINT      NOT NULL,
  ts                       DATETIME    NOT NULL,
  route                    VARCHAR(16) NOT NULL,
  route_reason             VARCHAR(64) NOT NULL,
  confidence_eff           DECIMAL(4,3),
  confidence_factors       JSON,
  risk_eff                 DECIMAL(4,3),
  risk_factors             JSON,
  matched_rules            JSON,
  threshold_config_version VARCHAR(32),
  approver                 VARCHAR(16),   -- system / user
  channel                  VARCHAR(16),   -- backend / feishu / auto
  event_id                 VARCHAR(64),
  idempotency_key          VARCHAR(64),
  action_hash              CHAR(64),
  final_status             VARCHAR(16),
  KEY idx_task (task_id),
  KEY idx_ts (ts)
);
```

`expired` 扫描由 APScheduler 定时任务驱动（对齐全局技术栈），到期 → 加急重发通知 + 置 `expired`，绝不自动执行。

---

## 9. 开放问题清单（需用户校准）

以下项目本文档给出**默认建议**，最终值/策略需用户拍板：

1. **阈值**：`AUTO_CONF_FLOOR / RISK_AUTO_CEILING / INFO_CONF_FLOOR / RISK_GATE / K` 的具体值（当前默认 0.85 / 0.25 / 0.60 / 0.60 / 2）。
2. **因子权重**：confidence/risk 各因子权重（§2.1/§2.2 默认值）。
3. **强制确认清单**：`force_confirm_list` 具体纳入哪些 action_type、是否强制。
4. **矩阵拨盘**：中-confidence + 低-risk 单元格默认 `need_decision`，何时/依据什么放宽为 `auto_approved`。
5. **TTL 与过期行为**：待确认 TTL（普通 vs 敏感，如 7 天/24 小时）；`expired` 后是仅重发还是需手动重激活。
6. **重评轮次上限 `N`**（默认 2）。
7. **need_info 路径边界**：何种补充走"直接补 slot"、何种走"回喂 M3 重提"。
8. **mem0 佐证打分方法**：检索 topK、相似度阈值、如何折算成 `c4`。
9. **校准节奏与最小样本**：个人系统样本稀疏，分箱最小样本、重算周期（周/月）、是否长期停用自动校准而靠人工调阈。
10. **单一审阅人假设**：本系统仅 chujiejie.1 一人审阅，故省略"按角色/负载路由到不同审阅人"；是否需要委托/代批场景。
11. **历史数据**：本项目为全新库，默认无历史 Task 迁移；如有存量需处理请确认。
12. **用户 open_id 与 bot 凭据**：卡片发送需用户 `ou_xxx` 与 app 凭据，作为配置注入（不硬编码），来源与 M2 采集共用配置待确认。

---

## 10. 参考资料（2026 HITL 最佳实践）

- Human-in-the-Loop Escalation Design for AI Agents（2026）：四层动作风险分级、校准数学（声称 90% ≈ 真实 75%，三段链 ~42%）、async-first、触发矩阵、EU AI Act Art.14 / NIST AI RMF / OWASP "Excessive Agency"。
- Omnithium：High-Stakes HITL Patterns —— action_manifest（配置而非 prompt）、结构化置信度输出（confidence_basis / uncertainty_factors / recommended_review）、审批 UX 反模式（无上下文的二元批/拒、缺"补信息"、timeout=approval）、分箱校准与"何时减少人工"。
- TuringPulse / Redis / LoopRails：两轴（severity × confidence / reversibility）门控、双信号（trust + risk）、consequence/reversibility 预览、置信度不可直接信任。
- 飞书 lark-cli：`im +messages-send`（交互卡片）、`event consume card.action.trigger`（回调，WebSocket 下发，token 30 分钟/2 次）、`interactive/v1/card/update`（延迟更新，需完整卡片 JSON）。
