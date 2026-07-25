# summarize-person-week — 详细方案（先落方案，后实现）

本文件是「个人周报」技能的落地方案。实现完成后本文件保留为设计参考。
定位：**完全自包含的 Skill**，不改 Go / 不建表 / 不动 `internal/dailydigest`。
周报由 codex agent 加载本 Skill、按本方案自跑 `lark-cli`/`bytedcli`/`git`/`jarvis-tools`
调查一周证据，落成本地 Markdown 工作区，最终写出以「我」为中心、成果优先的中文周报。

参照姊妹技能 `summarize-person-day`（已成熟）的同构结构来实现；周报是它的「7 天窗口 +
成果优先周框架」变体。

## 1. 目标与硬约束（来自用户）

1. **重采 7 天**：不滚动汇总 7 份日报，直接对一周窗口重新并行采集证据（工程侧 `git log --since`、
   `bytedcli` MR/commit 拉宽窗口本来就便宜且最全；飞书侧按周重采）。
2. **节奏**：周五 **17:00** 生成「本周」；同时支持随时手动运行/重算。
3. **注入 all.md 业务背景**：把 `conf/rules/all.md` 的「业务背景 + 能力地图」在 references 里
   固化一份（海外 i18n 控制面、服务域名、lark-cli 23 域 / bytedcli 能力地图与用法），
   让周报 agent 不再盲跑。参照 all.md 先把能力地图概览和背景弄好放进 Skill。
4. **整体打包成一个 skill**：所有内容在 `.agents/skills/summarize-person-week/` 内，
   注册进 `conf/skills.yaml`。
5. **按天存储、Markdown 落盘**：一周一个工作区目录，证据可按天拆成独立 md 文件，
   全部 Markdown，不落 JSON DTO、不进数据库。
6. **超时宽松**：整技能预算放宽（周窗口更大）；单个外部 collector、单个 follow-up 都给更长上限。
7. **取证宽松、可迭代**：先并行收一波，分析中发现新问题时**允许再起 follow-up 波次**继续查证据，
   不是一次性收完就封口。
8. **多用并行 + 管理上下文**：初始 lane 并行；lane 内可再按天/按项目并行；每个并发 worker
   独占一个 md 文件；主控只在 barrier 汇总，follow-up/verifier 只给必要证据 ID，避免上下文膨胀。

## 2. 技能目录结构（自包含）

```text
.agents/skills/summarize-person-week/
├── SKILL.md                                  # 入口：周报的调查→采集→归并→验证→写作流程
├── DESIGN.md                                 # 本方案（保留）
├── agents/
│   └── openai.yaml                           # display_name / default_prompt
├── references/
│   ├── context-and-capabilities.md           # principal 背景 + all.md 业务背景 + lark-cli/bytedcli 能力地图
│   ├── storage-and-report.md                 # 周工作区 md 布局 + 周报最终结构
│   └── channel-methods.md                    # 7 天证据采集、follow-up 波次、归并、验证、上下文管理
└── scripts/
    └── init-week.sh                          # 初始化一周工作区（按 Monday 日期建目录 + 骨架 md）
```

三个 reference 直接改编自 `summarize-person-day/references/` 的同名文件，
按「一周窗口 + 成果优先 + 按天证据」调整。

## 3. 存储布局（Markdown、按天）

一周一个工作区目录，以该周**周一日期**命名（文件系统清晰、复用日期校验）；ISO 周号
（`YYYY-Www`）写进 `00-context.md`。

```text
<workspace-root>/data/personal-weekly/YYYY-MM-DD/        # YYYY-MM-DD = 该周周一
├── 00-context.md                                        # 周范围/身份/绑定/run 日志/覆盖/线索/开放问题/当前综合
├── 10-evidence-jarvis.md                                # Jarvis 确定性事实（整周）
├── 20-evidence-feishu.md                                # 飞书证据汇总；可按天拆分
│   └── 20-evidence-feishu-<YYYY-MM-DD>.md               #   （可选）按天并行 worker 各自文件
├── 30-evidence-engineering.md                           # 工程证据汇总；可按天/按仓库拆分
│   └── 30-evidence-engineering-<YYYY-MM-DD|repo>.md      #   （可选）并行 worker 各自文件
├── 2x-evidence-followup-<topic>-<seq>.md                # 分析中发现的 follow-up 波次证据
├── 40-work-items.md                                     # 跨源归并后的工作项（按项目/主题）
├── 50-verification.md                                   # 原子结论核验
├── 5x-verification-<topic>.md                           # 并行 verifier 各自文件，再并回 50
├── 60-insights.md                                       # 关联洞察 / 本周发现候选
├── 90-report-draft.md                                   # 周报草稿
└── 99-report.md                                         # 最终周报（canonical）
```

规则（与日报一致）：
- 主控拥有 `00-context.md`、派生文件、两个报告文件；每个并发 worker 独占一个证据文件；
  证据 append-only，不为迎合结论回改原始证据；`99-report.md` 在新草稿通过验证前保留旧版。
- `<workspace-root>` 由显式入参或当前 Git 仓库根解析，不静默写 home 目录。

**按天并行的价值**：7 天窗口下，飞书/工程 lane 可按天（或按项目）切成多个并发 worker，
各写各的 `-<day>.md`，主控在 barrier 读取汇总。这既满足「按天存储」，又天然并行、控上下文。

## 4. 周窗口与节奏

- **窗口**：`[周一 00:00, cutoff)`，本地时区 `Asia/Shanghai`。
  - 周五 17:00 触发：cutoff = 触发时刻（本周截至此刻，周一→周五下午）。
  - 手动触发：cutoff = 触发时刻；对已过去的整周则 cutoff = 周日 24:00。
- **ISO 周**：`00-context.md` 记录 `YYYY-Www` 与周一/周日日期，避免歧义。
- **节奏落地**：本期先做**手动运行**（`init-week.sh` + 加载 Skill 调查）验证质量；
  周五 17:00 自动触发用系统既有的调度能力承接（见 §7，不在本 Skill 内写 cron）。

## 5. 周报最终结构（成果优先，围绕 我/项目/人/事/物，留发现空间）

`99-report.md`，第一人称中文。**稳定锚点**只固定三处：`本周概要` / `本周主线` / `数据覆盖`；
其余板块「有才出、按内容生长」，允许模型合并/改名/增删，只要覆盖到。

```markdown
# 我的本周全景 · YYYY-Www（周一–cutoff）

## 本周概要            # 2–4 句：本周最重要的推进 + 最需要我关注/决策的事。一眼抓重点。
## 本周主线            # 按项目/主题聚合的核心进展；按推进程度/影响排序；成果优先、活动垫底；行内挂证据。重项目多写，没动的不写。
## 我：推进、决策与状态   # 我直接做的 / 委派 agent 的 / 协作；本周做出的决策与已接受承诺；待我回应 / 我在等谁。
## 项目：我负责范围内的进展 # 从周初到 cutoff 的状态变化；有进展/风险变化/优先级变化/该动却没动的项目。不平均分配篇幅。
## 人：协作、老板待办与相互承诺 # 老板交办/在等我的、我需同步的；谁在等我 review/回应、我在等谁；关键协作与依赖。
## 事：关键事件、决策与风险 # 会议、评审、发布、事故、审批、新请求、该发生却没发生的事。会议完整底账放最后一节。
## 物：交付与资产变化      # 本周变更的代码/commit/MR/CR、bug 修复、发布上线、文档、妙记、Task、部署等产物（带远端状态）。
## 关联洞察与本周发现      # 【强制】写完主线后主动扫一遍「有没有漏掉该我关注的事」：新群/新项目、反复被@、话题升温、承诺临近 deadline 未动、值得记一笔的一条线索。只留证据支撑的，猜测标 hypothesis。
## 下一步工作台          # 从未决承诺/风险/等待/deadline 推导：我该做的 / 别人欠我的 / 需盯的 / 可停的。不编造计划。
## 数据覆盖与完整底账      # 窗口、cutoff、三源覆盖、partial/error、未决结论；会议完整底账 + 消息/文档/Task/commit-MR/部署索引 + 证据文件链接。
```

**贯穿质量标准**（比结构更重要）：成果优先（先产出/结果，活动只作证据）、动词开头无第一人称流水账、
行内证据（`MR!123 · commit abc · 妙记 · 文档`）、不编造（缺指标写 `[待补]`，light 的一周如实写 light）、
跨源去重按项目/主题归并、排序按重要性不按时间。

## 6. 采集流程（宽松、可迭代、并行）

改编 `channel-methods.md`，核心差异：**窗口 7 天**、**取证宽松可多波**、**超时放宽**、**按天/按项目并行**。

1. **Seed**：主控用 Jarvis 确定性数据（`jarvis-tools` + 本地库）拼 Seed：身份别名、周窗口/cutoff、
   本人消息/群 ID、会议 ID、Todo/Task/Run/Session 关系、相关 repo/commit/MR/文档/项目、
   周初仍开放的承诺/风险。
2. **初始并行波**：三条 lane 并发——Jarvis（确定性查询）/ 飞书 collector / 工程 collector。
   lane 内可再按天或按项目切成多个并发 worker（各写各的 `-<day>.md`）。collector 只查自己 lane，
   不跨 lane 推断、不写结论；返回稳定 ID/时间/URL/观察事实/覆盖/缺口/后续线索。
3. **超时（放宽）**：整技能预算请求 ≥ 60 分钟；单个初始外部 collector 允许 ≤ 25 分钟；
   单个 follow-up ≤ 12 分钟。长命令轮询而非静默阻塞。
4. **barrier + follow-up 波次（宽松、可迭代）**：一波结束后在 `00-context.md` 记覆盖/缺口，
   读三份证据，抽稳定跨域线索（message/meeting ID、doc token、Task/Run/Session ID、repo 路径、
   commit SHA、MR/CR URL），并行派发 follow-up，各写新 `2x-evidence-followup-*.md`。
   **分析中随时可再起波次**：只要还能改变一个实质结论就继续查；结论被支撑/否定/显式未决/访问受限记录后才停。
5. **归并**：主控按项目绑定（冻结上下文 > 群/仓库绑定 > 显式引用 > 证据推断 > 未归属）合并同一
   交付/决策/事故/承诺/风险，`Activity → Output → Observed Outcome` 分析，写 `40-work-items.md`。
6. **验证**：把实质结论拆原子 claim，冲突/仅消息支撑/终态不清/会议决策缺逐字证据的，并行起 verifier，
   写 `5x-verification-*.md` 再并回 `50-verification.md`；不达标的降级或剔除。
7. **发现**：`60-insights.md` 记跨项目/人/事/物的关联发现；只保留证据支撑的进报告。
8. **delta 刷新**（进行中的本周）：写作前对可变项（Task/Run/MR/审批/部署/承诺）做一次窄 delta 查询。
9. **写作**：单一主 writer 写 `90-report-draft.md` → 核对每条实质陈述 → 写 `99-report.md`。

## 7. 集成与注册

- `conf/skills.yaml`：新增 `summarize-person-week`，`enabled: true`，`stages: [execute]`。
- **周五 17:00 自动触发**：本 Skill 不自带 cron。用系统既有的定时能力（`ScheduledTask` / 现有调度器）
  注册一个每周五 17:00 的任务，其指令为「用 $summarize-person-week 生成本周周报」。
  具体挂载方式在 Skill 完成、手动验证质量后再定（先手动跑通）。
- **注入 all.md**：不在运行时改 Go 注入；而是把 all.md 的业务背景 + 能力地图**固化进**
  `references/context-and-capabilities.md`（并注明「派生自 conf/rules/all.md，版本可能更新时读实时规则」），
  与日报 Skill 完全一致。这样周报 agent 天然带业务背景。

## 8. 与日报 Skill 的关系

- 结构同构：直接以 `summarize-person-day` 为模板改编，降低实现与维护成本。
- 差异点：窗口（7 天 vs 1 天）、存储根（`data/personal-weekly` vs `data/personal-daily`）、
  报告标题与「本周概要/本周主线」锚点、超时更宽、按天/按项目并行更强调、follow-up 波次更鼓励。
- 复用日报 references 里已固化的 all.md 能力地图与业务背景（`context-and-capabilities.md`
  两个技能内容基本一致，各存一份保持自包含）。

## 9. 实现清单（交给后台 agent）

1. `scripts/init-week.sh`：入参该周周一日期（YYYY-MM-DD）+ 可选 workspace-root；
   校验日期、算 ISO 周与周一/周日、建 `data/personal-weekly/<Monday>/` 及骨架 md（含周报导航）。
2. `references/context-and-capabilities.md`：改编日报同名文件，principal 背景 + all.md 业务背景 +
   lark-cli/bytedcli/Jarvis/本地工程 能力地图 + live discovery 命令。
3. `references/storage-and-report.md`：本文件 §3 的工作区布局 + §5 的周报最终结构与写作规范。
4. `references/channel-methods.md`：本文件 §6 的采集/follow-up/归并/验证/上下文管理（7 天、宽松、并行）。
5. `SKILL.md`：入口流程（Resolve Week → Init Workspace → Plan → Collect in Parallel →
   Follow-up Waves → Reconcile → Verify → Discover → Delta Refresh → Write → Finish），
   front-matter `name: summarize-person-week` + description。
6. `agents/openai.yaml`：`display_name: 我的本周全景`，default_prompt。
7. `conf/skills.yaml`：注册。
8. 验收：`bash scripts/init-week.sh <某周一>` 能建出工作区；`conf/skills.yaml` 合法；
   手动用技能跑通一次并产出 `99-report.md`。
```
