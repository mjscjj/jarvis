# 「用 Agent 做决策」开源与前沿调研报告

> Status: research snapshot
> Authority: non-normative
> Last verified against project: 2026-08-02 @ `89fa24b`
> Warning: §6 的 Jarvis M4 / 人工确认 / confidence-risk 对照基于已退役架构，不代表当前 M5。通用研究部分仍可参考。

> 定位：一份**系统性、可追溯**的历史调研。末尾项目对照使用的是当时的 M4 确认闸门，当前实现请看 `docs/00-overview.md` 和 `docs/modules/05-execution.md`。
> 调研时间：2026-07-19。来源以 2025/2026 最新资料为主，逐条附 arXiv 编号 / 官方文档 / GitHub 链接。
> 阅读顺序建议：先看 §0 认知地图 → §5 通用设计思想归纳 → §6 对照 Jarvis M4。§1~§4 是可追溯的细节支撑，按需查阅。

---

## 0. 认知地图：agent 决策 = 三条正交轴的组合

调研下来最重要的一个结论：市面上所有「用 agent 做决策」的做法，本质都是**三条正交主轴**的不同组合。任何一个项目（包括我们的 Jarvis M4）都能在这三条轴上定位。

```
轴 A｜推理拓扑（如何组织"想"）
    单链(CoT) → 交替(ReAct) → 树/图(ToT/GoT) → 带回溯的搜索(MCTS/LATS)

轴 B｜反馈信号从哪来（如何"打分/防错"）—— 全领域争议最大、最关键
    模型内省(Self-Refine/Reflexion) ↔ 外部硬信号(工具/单测/检索/独立verifier) ↔ 人类(HITL)
    可靠性排序：外部环境/工具/单测 ≳ 独立verifier(PRM) ≳ 多agent辩论 > 裁判LLM > 多数投票 > 纯内省自评

轴 C｜决策主体数量（"谁来决策、何时问人"）
    单 agent → 多 agent(辩论/MoA/Actor-Critic) → 人机协同(升级/deferral)
```

贯穿三轴的一条工程主线（安全侧）：**控制平面(control plane) 与 数据平面(data plane) 分离**——agent 读到的任何不可信内容都属于数据平面，它**不能反向改写「谁能做什么」的控制平面决策**。这条线直接决定了「审批闸门为什么不能由 agent 自己协商」。

一句话记忆：
- **想清楚** → 轴 A（拓扑）
- **信谁的反馈** → 轴 B（外部 > 内省，这是全领域最硬的实务判断）
- **谁拍板、何时问人** → 轴 C（分级 + 升级）

---

## 1. 学术 / 前沿的决策范式

> 详见历史调研任务 `5214d29b-3605-4ff4-97ae-710fc1aa2015`。每条给「核心思想 / 机制 / 优缺点 / 代表论文」。

### 1.1 推理拓扑类（轴 A）

| 范式 | 核心思想 | 机制 | 代表 |
|---|---|---|---|
| **ReAct** | 思考(Thought)与行动(Action)交替，用外部观测对账推理 | `Thought→Action→Observation→…→Final` 循环 | arXiv:2210.03629（ICLR 2023）。几乎所有生产 agent 框架的内核 |
| **Tree of Thoughts (ToT)** | 把"一条链"升级成"在多条路径上有意识搜索"，可前瞻/回溯/剪枝 | thought 作节点建树，LLM 既是生成器又是价值评估器，BFS/DFS 搜索。Game of 24：CoT 4% → ToT 74% | arXiv:2305.10601（NeurIPS 2023） |
| **Graph of Thoughts (GoT)** | 把树推广成任意有向图，支持聚合多条思路 | thought 可多父多子，支持 aggregate/refine/反馈回路。排序任务比 ToT 质量 +62%、成本 −31% | AAAI 2024 / arXiv:2308.09687 |
| **LATS** | 用 MCTS 把 reasoning+acting+planning 缝在一起 | LLM 同时当动作生成器/价值函数/反思器 + 外部环境反馈 + 回溯。HumanEval pass@1 92.7% | arXiv:2310.04406（ICML 2024） |

> 拓扑综述 arXiv:2401.14295 提出「reasoning topology」分类学。前沿趋势是**缝合而非择一**：LATS = ToT + ReAct + Reflexion + MCTS。

### 1.2 反馈 / 纠错类（轴 B，最关键）

| 范式 | 核心思想 | 反馈来源 | 可靠性 |
|---|---|---|---|
| **Self-Refine** | 同一 LLM 既生成又批评又改稿，迭代 | **纯内省** | ⚠️ 有"相关性错误"风险（同一盲点跨轮持续） |
| **Reflexion** | 把失败反馈转成自然语言反思存进记忆，下轮当上下文（"语义梯度"） | 环境标量/二值反馈 | 依赖有意义反馈信号；HumanEval pass@1 91% |
| **CRITIC** | 不信内省，用外部工具（搜索/代码解释器/schema/单测）验证再改 | **外部工具硬信号** | ✅ 生产最可靠一路 |
| **Verifier / PRM** | 用独立训练的验证器打分，过程监督 > 结果监督 | **独立 verifier** | ✅ 用独立验证器而非让生成者自评（arXiv:2305.20050，PRM800K） |

> ⚠️ **必读反面证据**：`Large Language Models Cannot Self-Correct Reasoning Yet`（arXiv:2310.01798，ICLR 2024）——**无外部反馈的纯内省自我纠错，准确率不升反降**。很多"看起来有效"的自纠论文其实偷偷用了 oracle 标签决定何时停。综述 `When Can LLMs Actually Correct Their Own Mistakes?`（TACL 2024）三条结论：①无任何工作证明纯 prompt 自反馈能成功自纠；②有可靠外部反馈时自纠才 work；③大规模微调能带来自纠能力。
>
> **对决策系统的直接含义**：任何"让 agent 自己检查自己"的护栏，若不接外部可验证信号（工具/单测/检索/独立 verifier/人），很可能是安慰剂甚至有害。

### 1.3 打分 / 评判类

| 范式 | 核心思想 | 关键坑 |
|---|---|---|
| **Self-Consistency** | 采样 N 条 CoT 取多数票 | ⚠️ **只降方差、不降偏差**——系统性误读时全体一致地错，投票救不回来 |
| **LLM-as-a-Judge** | 用强 LLM 当裁判打分/两两比较 | ⚠️ 位置偏差、冗长偏差、自我增强偏差；GPT-4 裁判与人一致率 >80%。缓解：换位去偏、CoT、多裁判、校准（arXiv:2306.05685） |
| **Multi-Agent Debate** | 多 agent 各给答案再互相批评，多轮收敛 | 成本随 agent×轮数膨胀；可能"错误地趋同"。事实性场景 debate > reflection（arXiv:2305.14325） |

### 1.4 决策主体 / 路由类（轴 C）

| 范式 | 核心思想 | 代表 |
|---|---|---|
| **Plan-and-Execute** | 强模型出全局计划、弱/快模型或 runner 逐步执行、re-planner 决定完成/重规划 | Plan-and-Solve(ACL 2023)、ReWOO、LLMCompiler(DAG 并行,延迟降 3.6×)、**ADAPT**(按需递归分解,arXiv:2311.05772) |
| **Routing / Cascade** | 查询进来先选/级联到合适模型（简单→便宜，不够好→升级更强） | RouteLLM(arXiv:2406.18665,成本降 85%)、FrugalGPT(成本降 98%) |
| **Mixture-of-Agents** | 分层：多 Proposer 出候选 → Aggregator 逐层综合 | arXiv:2406.04692。开源模型即达 AlpacaEval 65.1% > GPT-4o 57.5% |
| **Constitutional AI / RLAIF** | 用一套自然语言"宪法"原则让模型自我批评修订，AI 产生偏好标签 | arXiv:2212.08073（把价值/安全约束注入决策边界） |

### 1.5 置信度 / 校准（决策系统的地基）

- **为什么 LLM 置信度不可裸信**：RLHF 会**破坏校准、制造系统性过度自信**——根因是奖励模型偏爱"高置信"表述，不管答案对错（arXiv:2410.09724，ICLR 2025）。
- **怎么榨出相对可信的置信度**：verbalized confidence（直接让模型用自然语言说把握度，往往比 token 概率更校准）+ "先列多个候选再报置信"（借鉴人类考虑替代项抑制过度自信）+ temperature scaling，可把 ECE 降 >50%（arXiv:2305.14975，EMNLP 2023）。
- **更硬的替代**：用独立 verifier / 外部信号，而非让生成者自评（PRM）。

### 1.6 Human-in-the-Loop / Escalation（学术侧）

- **核心思想**：让系统"知道自己不知道"，高风险/低把握时**弃权(abstain)并升级给人**——abstention 是一等公民的协调信号，不是失败。
- **谱系**（AAAI 2025 综述）：Rejection/Selective Prediction（低于阈值拒答）、**Learn-to-Defer**（训练策略决定"自动答 vs 交人"，且**显式建模人类复核容量有限**）、Dynamic model selection。
- **带数学保证的升级**（强烈推荐范式）：**Cascaded Selective Evaluation / Trust or Escalate**（arXiv:2407.18370，ICLR 2025）——先用便宜模型判，置信不足才升级更强模型，用小校准集 + fixed-sequence testing，**可证明**最终与人类一致率 ≥ 用户设定的 (1−α)。

---

## 2. 主流开源 agent 框架的决策与审批机制

> 详见历史调研任务 `5ba30a41-8300-4bfc-bd8c-0929a4533196`。核心问题：这些框架把"是否需要人工"建模成什么？

### 2.1 四大流派

| 流派 | 把"是否需要人"建模成 | 代表框架 |
|---|---|---|
| **① 状态机中断 + Checkpoint** | 图/工作流里的一个**可持久化暂停态** | **LangGraph**、LlamaIndex、Google ADK、Temporal |
| **② 工具级审批闸门** | 工具声明 `needs_approval`，命中就不执行、返回待审批 interruption | **OpenAI Agents SDK**、Pydantic AI、Vercel AI SDK、Semantic Kernel |
| **③ 人 = 团队里一个 Agent** | `human_input_mode`（ALWAYS/NEVER/TERMINATE）+ UserProxyAgent | **AutoGen** |
| **④ 自动护栏 / Tripwire** | 独立分类器给输入输出打分，命中就**直接拦，不问人** | **OpenAI guardrails** |

### 2.2 重点框架细节

**LangGraph（最值得学）**：
- `interrupt()` 函数在图节点里**暂停**，把值抛给客户端；用 `Command(resume=...)` 恢复。支持静态断点（`interrupt_before`/`interrupt_after`）和动态中断。
- 状态存 checkpointer，可持久化、可 time-travel（回到任意历史状态重放）。
- ⚠️ **恢复时节点从头重跑**——所以 `interrupt()` 之前的副作用要幂等，否则会重复执行。

**AutoGen**：`human_input_mode` 三档——`ALWAYS`（每步问）、`NEVER`（全自主）、`TERMINATE`（仅在终止前问）。人被建模成 `UserProxyAgent`，在 GroupChat 里就是一个可发言的 agent。

**OpenAI Agents SDK**：
- **Guardrails**（input/output）= 独立运行的检查，命中 **tripwire** 就中止——这是"护栏"，不是"审批"。
- **Tool approval**：工具声明 `needs_approval`，命中则返回 interruption 等人批。
- 二者分层清晰：guardrail 自动快拦，approval 人工慢审。

**Anthropic `Building Effective Agents`**：主张**能用 workflow（确定性编排）就别用 agent**；agent 只在需要动态决策/不可预测步数时用；对外部动作要有人工检查点。

### 2.3 最重要的可迁移经验

1. **审批 ≠ 护栏，要分层**（OpenAI 分得最清）：
   - **护栏层**（自动、快、便宜）：分类器拦注入/离题/泄密。
   - **审批层**（人工、慢、贵）：只在**有副作用**的操作前暂停。
2. **暂停点必须可持久化可恢复**：阻塞式等待只适合秒级；长等待必须"存盘→交还控制权→异步恢复"（Temporal 做到极致：零 CPU、扛崩溃、等数月）。
3. **恢复时防重复副作用**（幂等）、**审批要能编辑参数而非只批/拒**、**防伪造审批**（服务端重校验 schema / OPA 策略即代码）。

---

## 3. 编码类 agent 的自主度控制（对本项目最贴——我们用 codex CLI 决策）

> 详见历史调研任务 `f07db0e9-b626-4f71-8ce4-9d4f8c4fbd47`。核心问题：编码 agent 怎么决定"要不要执行某个动作（改代码/跑命令/提交）"？

### 3.1 四种正交控制手段

| 手段 | 本质 | 谁判定 | 代表 |
|---|---|---|---|
| **① Sandbox 隔离** | 技术边界：这个动作**能不能造成破坏**（能写哪、能否联网） | OS 内核/容器（seatbelt、bwrap+seccomp、Docker、cloud VM） | Codex、Cursor、OpenHands、Jules |
| **② Approval Policy** | 决策策略：这个动作**要不要停下来问人** | 引擎按策略档位判定 | Codex、Claude Code、OpenHands |
| **③ 动作白名单/分类** | 规则匹配：这个具体命令/路径**在不在允许集里** | 前缀匹配/glob/LLM 分类器/hook 脚本 | Cursor、Claude Code、Cline |
| **④ Plan/Act 两阶段** | 阶段隔离：先只读产出计划 → 人批准 → 才进入可写执行 | 显式模式切换（人是闸门） | Cline、Aider、Claude Code、Jules |

成熟编码 agent 的决策链（Codex/Claude 最典型）：
```
模型想执行动作
   ├─(④) 处于 plan 阶段? ──是──▶ 阻断写操作，只允许读/探索
   ├─(③) 命中 deny 规则? ──是──▶ 直接拒绝（最高优先级）
   ├─(③) 命中 allow 白名单? ──是──▶ 自动执行
   ├─(②) 按 approval policy 判定 ──需要问人──▶ 停下等批准
   └─(①) 在 sandbox 边界内执行；越界(写外部/联网) ──▶ 触发升级 → 回到(②)问人
```

### 3.2 OpenAI Codex CLI —— 二维正交设计（对本项目最关键）

Codex 的核心哲学：**用两个独立正交的旋钮定义自主度**。

- **维度一 · sandbox_mode（技术边界）**：`read-only` / `workspace-write`（默认，workspace 内可写、默认禁网）/ `danger-full-access`。**sandbox 作用于所有 spawned 子进程**（git、包管理器、测试），不只内置文件操作。受保护路径 `.git`/`.codex`/`.agents` 即使在可写根内也只读。
- **维度二 · approval_policy（何时问人）**：`untrusted`（只自动跑已知安全只读）/ `on-request`（默认，越界才问）/ `never`（从不问，不允许的直接**失败**而非升级）。
- **组合示例**：`--sandbox workspace-write --ask-for-approval on-request`（Auto 预设）；CI 用 `--sandbox read-only --ask-for-approval never`。
- **更精细**：`granular` 分类审批（按 sandbox 升级/规则/MCP/权限/skill 分档）；execpolicy **rules**（命令前缀 allow/prompt/forbid）；`approvals_reviewer=auto_review`（把已需批准的请求先交 reviewer agent 审，**prompt-build/parse/review 失败一律 fail closed**）。

> 我们方案用的 `codex exec -s read-only`（决策阶段只读、无副作用）正对应「维度一锁死为只读」——这是很干净的选择：决策时物理上不可能改代码。

### 3.3 其它编码 agent 的关键设计

| Agent | 自主度控制要点 |
|---|---|
| **Claude Code** | 6 种权限模式（default/acceptEdits/**plan**/auto/dontAsk/bypassPermissions）+ 权限规则 `ToolName(specifier)` allow/deny/ask + **PreToolUse hooks**（exit 2 阻断，优先于 allow 规则）。`ExitPlanMode` 需批准=计划闸门。**deny 恒定最高优先级**。 |
| **Cline** | **Plan/Act 硬分离，切换需人批准**；按类别的 auto-approve 复选框矩阵；`Max Requests` 连续动作上限强制回来问人；**Checkpoint（独立 shadow git）让 auto-approve 变得可用**——"Checkpoints make auto-approve practical"。 |
| **OpenHands** | **SecurityAnalyzer（只打风险分）与 ConfirmationPolicy（只判要不要问）完全解耦、各自可插拔**；`ConfirmRisky(threshold)` 只对高风险停；`WAITING_FOR_CONFIRMATION` 状态；**拒绝可带反馈让 agent 重试更安全方案**；headless 强制 `NeverConfirm`（故必须跑 Docker）。 |
| **Aider** | `/ask`(只读) vs `/code` vs `/architect`(planner+editor 两步)；auto-commit + `/undo` 用 git 做**可回滚**护栏（事后而非事前拦截）。 |
| **SWE-agent** | 不做审批，护栏内建在 **ACI（Agent-Computer Interface）**：编辑后自动回显、lint 拦语法错、输出截断防迷失——证明"**界面设计 = 决策质量**"。 |
| **Cursor** | 3 档 Run Mode + `permissions.json`（前缀 allowlist，deny 优先）+ 分类器。官方明确警告：**"allowlist is best-effort, not a security boundary"**，真正边界靠 sandbox + hooks。 |
| **Google Jules / Devin** | 云隔离 VM + Plan/Execute/Review + **Planning Critic**（二级 agent 审查计划，任务失败率降 9.5%）+ PR diff 作主闸门。 |

### 3.4 控制哲学分类

- **双维度正交**（技术边界 × 决策策略分开）：Codex、Cursor、OpenHands
- **权限模式 + 可编程 hook**：Claude Code、Cursor
- **阶段隔离**（plan 只读 → 人批准 → act 可写）：Cline、Aider、Jules
- **风险分级驱动**（按动作风险决定问不问）：OpenHands、Codex(auto_review)
- **完全自主 + 界面约束**：SWE-agent
- **云隔离 + PR 闸门**：Jules、Devin、Codex cloud

---

## 4. HITL 治理 + 安全护栏的工程实践与行业标准

> 详见历史调研任务 `93b9b1c6-e563-4bcc-a79e-43d6947ba4c8`。这一段反映当时的 HITL 治理结论，不代表当前 Jarvis 的模型审批政策。

### 4.1 HITL 设计模式与反模式

**按后果分级（不是按置信度）**——四级动作风险 tier：

| Tier | 类型 | 监督模式 |
|---|---|---|
| 1 | 只读 | 完全自主，不打断 |
| 2 | 可逆 | 自主执行 + 全量日志 |
| 3 | 外部/第三方 | 暂存队列审查 或 置信度路由 |
| 4 | **高风险/不可逆**（生产部署、资金、删数据、改权限、对外通讯） | **强制人工审批，无例外**（不论置信度多高） |

> 核心原则：`how sure is the agent`（多有把握）是次要问题，`how bad is it if the agent is wrong`（错了多严重）才是决定是否 gate 的第一问题。

**审批 UX 反模式（要避免的坑）**：

| 反模式 | 为什么危险 |
|---|---|
| 无上下文的二元批准/拒绝（甩原始 JSON payload） | 要么橡皮图章盲批，要么审查缓慢 |
| **超时 = 默认批准** | 绝对禁止。超时 fallback 应是阻塞/拒绝/升级 |
| **过度 gating（confirm everything）** | 最反直觉但最危险：训练出"approve, approve"反射 → 真正 Tier 4 也被反射点过（**点击穿透 clickthrough 漏洞**：prompt injection 只要触发一个用户会习惯性点过的审批就绕过监督） |
| 缺"带修改拒绝"路径 | 审查者无法表达"方向对但参数错" |
| 同步阻塞审批 | 网关超时(29s)、token 过期、状态漂移 → 生产会崩 |

**Async-first 的正确模式**：状态序列化为 checkpoint → 带 TTL 队列（普通 7 天/敏感 24h）→ 从 checkpoint 恢复。两个使异步"正确"的护栏：**幂等键**（保证恰好执行一次）+ **动作哈希校验**（审批期间数据漂移则拒绝陈旧决策）。

**给人决策上下文而非原始 payload**：自然语言动作描述 + 推理 + 影响估计 + 可逆性标志 + 备选方案 + session ID + 审批截止；审批型给**字段 diff** 而非裸 payload。参考 `12-Factor Agents` Factor 7："把人机交互当**结构化工具调用**，不是纯文本"。

### 4.2 Prompt Injection / Excessive Agency 防护

- **OWASP LLM Top 10 (2025 v2.0)**：LLM01 Prompt Injection（连续两版第一，模型无法内在区分"数据"与"命令"，RAG/fine-tune 都不能根除）；LLM06 Excessive Agency（功能/权限/自主 过度）。
- ⭐ **审批必须在工作流层判定，不能由 agent 运行时协商**：否则一次 prompt injection 就能把 agent "劝"得不去问。OWASP 原话：**"一次成功的 prompt injection，按定义就是一次权限提升"**——所以升级闸门不仅是可靠性控制，更是安全边界。
- **Control plane vs Data plane 分离**："一旦 agent 摄入不可信输入，就必须约束它，使该输入不可能触发有后果的动作"。六个防注入设计模式：Action-Selector、Plan-Then-Execute、LLM Map-Reduce、Dual-LLM、CaMeL（Code-Then-Execute，arXiv:2503.18813）、Context-Minimization。
- **致命三要素(lethal trifecta)**：当系统同时具备①访问私有数据 ②接触不可信内容 ③能对外通讯，就极度危险——设计上应打破至少一个。
- **OWASP LLM06 官方缓解**：最小扩展/功能/权限；给应用自己的 token、敏感功能用代码实现而非交给模型；在用户安全上下文执行（OAuth 最小 scope）；高影响动作用 HITL 作**不可绕过闸门**；⭐ **完全仲裁（Complete mediation）——授权在下游系统强制，不依赖 LLM 判断动作是否被允许**；系统提示不作安全控制、不含密钥。

### 4.3 置信度校准的工程做法

- **"声称 90% ≈ 真实 75%"**：RLHF 模型系统性过度自信。**误差在链上复利放大**：每段 miscalibrated ~15pt、各报 90%，三段链全对概率约 **42%**（而非天真 0.9³≈73%）——这是"升级闸门不是软建议、是定量必需"的数学论据。
- **生产校准选型**：Platt scaling（默认，小样本≥100 稳健）/ isotonic regression（≥500 样本，ECE 降幅最大）；监控 ECE/Brier/log-loss。⚠️ **单调变换保序，不改善 AUROC**（校准修"数值准不准"，不改善"区分对错能力"）。
- ⭐ **样本不足时不启用校准、保持保守阈值**（把不确定当"更可能错"，倾向 escalate，不粉饰不确定性）——与 fail-fast 一致。
- **更诚实的信号**：任务越难 agent check-in 频率越高的"自我升级"行为、多样本一致性、轨迹级校准（Holistic Trajectory Calibration）。

### 4.4 监管与治理框架

| 框架 | 与"人类监督/审批"相关的硬要求 |
|---|---|
| **EU AI Act Art.14**（高风险 AI 义务 2026-08-02 生效） | 人须能：理解能力局限、警惕**自动化偏见**、正确解读输出、**决定不用/推翻/逆转输出**、通过 **stop 按钮**中断；远程生物识别需 **two-person verification**。监督措施须与风险/自主度/场景**相称**。 |
| **NIST AI RMF + CSA Agentic Profile** | GOVERN/MAP/MEASURE/MANAGE 四功能；**自主性 4 级分类**；有效监督需**事前定义 interrupt conditions**（超影响阈值/越权 scope/触发异常/涉敏感数据），而非事后审计日志回看。 |
| **ISO/IEC 42001**（可认证 AIMS） | 要求**有真实权限 override AI 决策的审查者**——橡皮图章式监督不满足；审计员查：定义的干预点/升级路径/override 程序、人工干预日志、**override/halt 经技术测试**的证据。 |

四框架收敛到同一组：**审计留痕、可追溯、明确负责人、可解释、可 override/halt**。

### 4.5 审计与决策留痕

- **两层架构（缺一不可）**：①存储不可变（WORM/append-only，任何层不暴露 DELETE）；②防篡改证据（捕获时刻 Ed25519 签名 + SHA-256 **哈希链** prev_hash）。只做其一都不够。
- **记录结构化字段**：agent 身份/版本、input_hash、推理轨迹、**policy_decision（打分/路由结果）**、risk_score、**human_oversight_required**、**审批人/时间/理由/授权链**、动作与结果、prev_hash/event_hash。
- **理念**：让审计留痕成为执行流的**内在副产品**，而非事后附加。
- 标准/参考：IETF SCITT（`draft-ietf-scitt-architecture`）、`draft-sharif-agent-audit-trail`；开源 agenttrace / ai-audit-trail / Decision-Trace-Ledger-Core。

---

## 5. 通用设计思想归纳（跨项目分类学）

把四路调研收敛成一张「用 agent 做决策」的通用分类学。任何这类系统都在回答这五个问题：

### 5.1 谁来决策？（轴 C）
从「单 agent」→「多 agent（辩论/MoA/投票/Critic）」→「人机协同（升级/deferral）」。群体法用**多样性**换正确率，但成本随主体数放大。生产上最常见是「单 agent 内循环 + 关键处升级给人」。

### 5.2 何时问人？（HITL 触发）
- **公认原则：按后果分级，不按置信度**。`错了多严重`（不可逆性 + blast radius）是第一问题，`多有把握`是次要问题。
- Tier 4（不可逆/对外/高危）= 强制人工，无例外。
- 前沿方向：从"拍脑袋阈值"走向"**带保证的选择性升级**"（Learn-to-Defer + Cascaded Selective Evaluation，可证明与人一致率 ≥ 1−α），且显式建模"人类复核容量有限"。

### 5.3 怎么打分 / 信谁的反馈？（轴 B，最关键）
- **可靠性排序**：外部环境/工具/单测 ≳ 独立 verifier(PRM) ≳ 多 agent 辩论 > 裁判 LLM(需去偏) > 多数投票 > **纯内省自评（不可靠）**。
- **LLM 自报置信度不可裸信**（RLHF 系统性过度自信）；要校准 + 用外部信号锚定；样本不足就保守。

### 5.4 怎么防错 / 防越权？（安全护栏）
- ⭐ **审批闸门在配置/工作流层，不由 agent 运行时协商**（防 prompt injection）。
- ⭐ **授权在下游做完全仲裁**，LLM 视为不可信用户；不可信输入不能改变授权决策。
- **两个正交旋钮**（编码 agent 的最佳实践）：sandbox（技术边界，错了也限死破坏）× approval policy（何时问人）。
- **护栏 ≠ 审批**：护栏自动快拦（分类器 tripwire），审批人工慢审（有副作用才停）。
- **白名单是 best-effort 不是安全边界**；不可逆动作单独硬拦（deny 最高优先级）；最小权限。
- **fail closed / fail-fast**：判定失败/超时/歧义 → 默认拒绝或转人工，绝不放行。

### 5.5 怎么恢复 / 留痕？（工程闭环）
- **异步优先**：checkpoint + TTL 队列 + 从 checkpoint 恢复；**幂等键 + 动作哈希**防重复副作用/防陈旧决策。
- **给人决策上下文**（自然语言 + diff + 可逆性 + 备选 + 理由），不给原始 payload；提供"带修改批准"。
- **append-only + 签名哈希链**审计，记录谁在何时因何批准；决策链可回放。
- 治理：override/kill-switch 经测试、具名负责人、事前定义 interrupt conditions、对冲自动化偏见。

### 一页纸红线（五句话）
1. **闸门在配置层，不在模型嘴里**（who-can-do-what 不受不可信输入影响）。
2. **授权在下游做完全仲裁**，LLM 只是不可信用户。
3. **按后果分级、异步优先、超时即拒**，给人 diff 不给 payload。
4. **置信度要校准，样本不够就保守**，高置信度买不到跳过审批的权利。
5. **全程 append-only + 签名哈希链**，记录谁在何时因何批准。

---

## 6. 对照 Jarvis M4：可借鉴点与可改进点

> 历史基准：已删除的 `docs/modules/04-confirmation.md`（当时的 `extracted Todo → need_decision → 用户批准/拒绝 → Task`）。本节不再是当前架构评价。

### 6.1 我们已经踩在正确道路上的（与业界最佳实践一致）

| M4 现有设计 | 对应业界共识 | 评价 |
|---|---|---|
| **打分只决定"要不要问人"，不替人拍板**（§0） | HITL 核心原则；Learn-to-Defer | ✅ 方向完全正确 |
| **codex `-s read-only` 决策**（§2） | Codex 二维旋钮之「sandbox 锁只读」；决策阶段无副作用 | ✅ 很干净——决策时物理上不可能改代码 |
| **action_manifest 强制确认清单是配置项、不写进 prompt**（§1.3、§4.4） | ⭐ "审批在配置层不由 agent 协商"防注入原则；OWASP 完全仲裁 | ✅ 这是最关键的红线，我们做对了 |
| **未知 action_type → block 到人工**（§1.2） | Excessive Agency 防护；fail closed | ✅ |
| **打分/codex 失败/超时/非法 JSON → need_decision，绝不自动确认**（§1.1） | fail closed / fail-fast；Codex auto_review 失败即不放行 | ✅ |
| **规则修正单向**（只拉高 risk/拉低 confidence，§3.3） | fail-safe 方向 | ✅ |
| **两档决策：规则快判 + 灰区才 codex 深判**（§2.1） | Routing/Cascade（便宜先行，不确定才升级贵的） | ✅ 成本-质量权衡合理 |
| **超时 ≠ 批准；expired 加急重发不自动确认**（§1.6） | ⭐ "超时=默认批准"是绝对反模式 | ✅ |
| **action_hash 漂移校验 + 生成前重校验**（§1.7、§5.0） | 异步审批的"动作哈希校验拒绝陈旧决策" | ✅ 与业界异步护栏一致 |
| **decision_audit append-only**（§8.1） | 审计留痕、可追溯 | ✅ 结构化字段基本齐全 |
| **给可决策上下文包（自然语言+方案+diff+理由+不确定点）**（§6.1） | "给人决策上下文而非 payload" | ✅ |
| **双通道 version 乐观锁 + event_id 去重 + uk_task_todo 兜底**（§6.3） | 幂等键、防重复副作用 | ✅ |

**结论**：M4 的设计密度和方向在个人级项目里相当扎实，几条最硬的红线（配置层审批、fail closed、只读决策、审计留痕）都踩对了。

### 6.2 可借鉴 / 可改进点（按优先级）

> 遵循项目「复杂度红线」：以下**只是调研发现的选项**，是否引入、何时引入都属**【需与用户确认】**，不擅自加重。多数只需在现有文档里补一句约束或换个默认值，不需要新设施。

**P1 · 低成本、高价值，建议尽快确认：**

1. **【概念纠偏】把"打分驱动"降级为"分级驱动"的备选，突出"按后果分级"是第一性的**
   - 业界最强共识：`错了多严重` > `多有把握`。我们的 §4.2 二维矩阵是 confidence×risk，本质已含 risk 维度，但**叙事上仍以打分/置信为主线**。
   - 建议：在 §4 明确一句——**risk（后果/不可逆性）是第一闸门，confidence 只在低 risk 区才有资格换取 auto**。这与现有矩阵"只有右上角(高 conf+低 risk)才 auto"其实一致，只是把原则讲透，避免未来校准时被"置信度够高就放行"带偏。
   - 成本：改几句话。

2. **【反模式规避】显式写入"禁止过度 gating / 防点击穿透"**
   - 我们当前 MVP 是"**所有 extracted Todo 都进 need_decision**"——这在跑通阶段没问题，但业界明确警告：**长期对所有动作都要确认会训练出"approve 反射"，反而让真正高危的审批被反射点过（clickthrough 漏洞）**。
   - 建议：在 §4 补一条设计约束——"低风险高置信动作在校准数据支撑后应放行 auto，**刻意保留人工闸门的稀缺性**，不为了'安全感'对所有动作强制确认"。这正是我们矩阵"中 conf+低 risk 未来可放宽为 auto"的动机，值得写成显式原则。
   - 成本：改几句话 + 明确未来放宽 auto 的判据。

3. **【校准务实化】把 §3.4 校准与"样本不足保守"讲成硬规则，并降低对 codex 自报置信度的信任**
   - 业界：LLM 自报置信度系统性过度自信（90%≈75%）；纯内省不可靠；样本不足**不该启用校准**。
   - 我们 §3.4 已提"样本不足不启用校准"，很好。但可再补两点：① **codex 深判返回的 confidence 因子应视为"待校准的弱信号"，不可直接当阈值判据**——优先用可外部验证的信号（slot 齐全度、action_type severity、发件人权重这些**规则/结构化**因子）主导，codex 的主观置信度权重压低；② 明确"校准器上线前，一律用保守阈值宁可多问"。
   - 成本：改几句话，权重是配置项，不动结构。

**P2 · 中等价值，值得记录为后续演进方向：**

4. **【解耦借鉴 OpenHands】把"风险打分"与"是否确认判定"在代码层解耦成两个可插拔组件**
   - OpenHands 的 `SecurityAnalyzer`（只打分）与 `ConfirmationPolicy`（只判要不要问）完全分离、各自可替换，是很干净的设计。
   - 我们 §0.3 已有 `Scorer` / `Router` 拆分，方向一致。建议在实现时**严格保持 Scorer 不做路由决策、Router 不做打分**的边界（现在文档是这么写的，落地时守住即可）。
   - 成本：0（现有拆分已符合，属"落地时守边界"提醒）。

5. **【拒绝可带反馈】need_decision 拒绝时支持"带理由驳回 → 回流让 M3/codex 出更安全方案"**
   - OpenHands `reject_pending_actions(feedback)` 让 agent 据反馈重试更安全方案；我们 §7.2 有"带修改批准"，但"拒绝"目前是终态 dismissed。
   - 建议（可选）：增加一种"驳回并要求改方案"的中间动作（reason 回喂 M3 重提 Todo），区别于"彻底 dismiss"。这更接近人机协作而非一刀切。
   - 成本：新增一个回流路径，属功能增强，**【需与用户确认】是否值得**（MVP 可不做）。

6. **【审计留痕加固】decision_audit 增加防篡改（哈希链），对齐治理标准**
   - 业界（EU AI Act Art.12 / ISO 42001）要求审计**防篡改**：append-only 存储 + 每条记录含前一条的 hash（prev_hash）。
   - 我们 decision_audit 目前是 append-only（✅），但无哈希链/签名。对**个人本地系统**这多半是过度工程（我们规模小、单用户、本地可信），**倾向不做**；但值得在文档里记一句"如未来要满足合规/可对外审计，再引入 prev_hash 哈希链"，避免以后返工时才想起。
   - 成本：0（只记一句演进备注）。**按复杂度红线，当前不建议引入。**

**P3 · 明确"不做"的（避免过度工程，写清楚反而防止未来乱加）：**

7. **多 agent 辩论 / MoA / ToT 搜索式决策**：这些提升的是"难推理题"的质量，我们 M4 的决策（这条线索该不该固化成任务、方案清不清楚、风险多大）**不是数学难题**，是"结合背景做判断"，单 codex + 规则已足够。**不做**，除非未来发现单 codex 判断质量明显不足。
8. **带数学保证的 Cascaded Selective Evaluation（1−α 保证）**：优雅但需要校准集和统计检验框架，单用户样本稀疏根本喂不起。**不做**，保守阈值 + 人工兜底就够。
9. **CaMeL / Dual-LLM 等重型防注入架构**：我们的不可信输入（飞书消息）已经通过"action_manifest 在配置层判定 + codex 只读决策 + 强制确认清单"隔离了控制平面，**不需要**再上 DSL 沙箱解释器。保持现状即可。

### 6.3 一句话总结给你
M4 的骨架（HITL 闸门 + 只读 codex 决策 + 配置层强制确认 + fail closed + 审计）在业界坐标系里是**对的、扎实的**。真正值得动的就 P1 三条，且都是**改几句话/换个默认值**级别的"讲透原则"，不是加设施：① 点明"按后果分级"是第一性；② 显式规避"过度 gating 训练出 approve 反射"；③ 把 codex 自报置信度降级为弱信号、样本不足一律保守。其余按复杂度红线**明确不做**。

---

## 7. 完整来源索引

**学术范式**：ReAct arXiv:2210.03629 · Reflexion arXiv:2303.11366 · ToT arXiv:2305.10601 · GoT AAAI2024/arXiv:2308.09687 · 拓扑综述 arXiv:2401.14295 · Self-Consistency arXiv:2203.11171 · LLM-as-Judge arXiv:2306.05685 · Multi-Agent Debate arXiv:2305.14325 · Plan-and-Solve arXiv:2305.04091 · LLMCompiler arXiv:2312.04511 · ADAPT arXiv:2311.05772 · Self-Refine arXiv:2303.17651 · CRITIC arXiv:2305.11738 · Just Ask for Calibration arXiv:2305.14975 · RLHF 过度自信 arXiv:2410.09724 · Learn-to-Defer 综述 AAAI2025 · Trust or Escalate arXiv:2407.18370 · RouteLLM arXiv:2406.18665 · MoA arXiv:2406.04692 · Constitutional AI arXiv:2212.08073 · LATS arXiv:2310.04406 · Let's Verify Step by Step arXiv:2305.20050 · LLM 不能自纠 arXiv:2310.01798 / TACL2024

**框架**：LangGraph HITL 文档 · AutoGen human_input_mode · OpenAI Agents SDK guardrails/tool-approval · Anthropic Building Effective Agents · 12-Factor Agents（github.com/humanlayer/12-factor-agents）· Temporal durable execution

**编码 agent**：OpenAI Codex `developers.openai.com/codex/concepts/sandboxing` + `/agent-approvals-security` · Claude Code `code.claude.com/docs/en/permissions` + `/permission-modes` · Cline `docs.cline.bot/features/auto-approve` · OpenHands `docs.openhands.dev/sdk/arch/security`（arXiv:2511.03690）· Aider `aider.chat/docs/git.html` · SWE-agent arXiv:2405.15793 · Cursor `cursor.com/docs/agent/security/run-modes`

**HITL 治理护栏**：OWASP Top10 for LLM 2025 v2.0（LLM01/LLM06）· CaMeL arXiv:2503.18813 · Simon Willison 防注入设计模式（2025-06）· AWS Well-Architected Agentic AI Lens AGENTSEC04 · EU AI Act Art.14（ai-act-service-desk.ec.europa.eu）· NIST AI RMF + CSA Agentic Profile · ISO/IEC 42001 Annex A · IETF SCITT draft-ietf-scitt-architecture · ConfTuner arXiv:2508.18847

> 四路调研的历史任务 ID：学术范式 `5214d29b-3605-4ff4-97ae-710fc1aa2015`、框架审批 `5ba30a41-8300-4bfc-bd8c-0929a4533196`、编码 Agent `f07db0e9-b626-4f71-8ce4-9d4f8c4fbd47`、HITL 治理 `93b9b1c6-e563-4bcc-a79e-43d6947ba4c8`。
