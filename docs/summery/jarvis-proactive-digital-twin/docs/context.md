# Jarvis Slides Context

## Decision snapshot

```text
deck_root: docs/summery/jarvis-proactive-digital-twin
audience: 内部 Agent / 研发 / 产品同学
talk_duration: 25 分钟
content_orientation: 技术产品分享
presentation_format: speaker-led live talk + reading-first proposal
content_mix: 10% 开场 / 20% 定位 / 30% 架构 / 20% 证据案例 / 15% 边界路线 / 5% 收束
mode: hybrid；Slides speaker-led，proposal reading-first
style: Signal Pipeline Flow 为主
density: Slides 低到中；方案稿高
audit_profile: hybrid
motion: 路由信号、节点点亮、Magic Move 式连续推进；警告场景克制
stage: 1920x1080
navigation: stable scene/beat URL + keyboard + tap/swipe + stage-local picker rail
technology_stack: React + Vite + TypeScript + Playwright
testing_plan: render / route / navigation / interaction isolation / layout / runtime / fonts / build / PDF
test_command: npm test && npm run build
visual_language: 深色仪表盘、青绿色信号、少量琥珀色人类决策、白板式解释清晰度
delivery_target: local HTML + static dist + PDF
non_goals: editable pptx / code walkthrough / external deployment / generic agent platform pitch
context_document: docs/summery/jarvis-proactive-digital-twin/docs/context.md
```

## Source material

- `goal.md`
- `README.md`
- `docs/00-overview.md`
- `docs/article-proactive-digital-twin.md`
- `docs/design-product-stage-1.md`（仅作提案来源，不视为当前实现）
- SQLite 只读快照：`var/jarvis.db`，2026-08-18 18:55 Asia/Shanghai

## Current evidence snapshot

- message: 6,164；distinct chat with messages: 68
- todo: 1,522；observing 1,166；materialized 356
- task: 562；done 336；observing 145；failed 72；awaiting_approval 7；needs_human 2
- execution_run: 707；succeeded 341；observing 149；waiting 120；failed 58；needs_human 39
- page coverage: person 41/103；project 4/7；group 33/5020；key_matter 1/1；resource 8/35
- Note: group total includes discovered cold chats; do not present 33/5020 as active-group coverage without qualification.

## Narrative plan

```text
orientation: persuasion + technical explanation + evidence
presentation_format: 25-minute internal live talk
duration: 25 minutes
section_mix:
  - 10% opening thesis
  - 20% input-side problem and product position
  - 30% world model and M2/M3/M5 mechanism
  - 20% observing, approval, waiting and real evidence
  - 15% limits and next priorities
  - 5% closing
pacing_notes: one claim per scene; architecture uses progressive beats; evidence scenes are self-contained
non_goals: implementation tutorial; generic platform pitch; hiding failures; presenting proposals as current
```

## Preview decision and Design DNA

Three real previews used the same anchor copy and data on 2026-08-18. Preview browser checks: 4 passed.

```text
chosen_direction: Signal Pipeline Flow
keep: dark technical ground; routed nodes; functional signal colors; instrument-like precision
borrow_from_other_previews: whiteboard preview's explicit hierarchy and annotation clarity; dialogue preview's warm human accent
typography: Noto Sans SC for Chinese; JetBrains Mono for ids, states and metrics
colors_and_background: near-black slate; emerald/teal signal; cyan evidence; amber human/approval; red only for failure
navigation_treatment: quiet vertical picker rail inside stage
motion_vocabulary: path travel; node activation; directional sweep; promote/resolve; frozen final states
interaction_vocabulary: stage click/tap; keyboard; swipe; local expansion with event isolation; navigator click/wheel
annotation_style: small mono labels; explicit state chips; short evidence footnotes
asset_or_emoji_strategy: DOM/SVG system diagrams; no remote critical assets; emoji avoided for baseline stability
density_and_copy_tone: concise Chinese; concrete numbers and boundaries; no consultant filler
custom_invented_metaphors: world-change signal; admission gate; one-page world state plus fact stream; patrol radar
pacing_log: Low | Low | Medium | Medium | Medium | Medium | High | Low | Medium | Medium | Medium | Medium | Low | High | High | Medium | Medium | Low
```

## Registry draft

| id | title | beats | section | mode | visual idea |
|---|---|---:|---|---|---|
| opening | 主动式任务数字分身 | 2 | opening | speaker-led | 世界信号围绕本地 Agent 核心 |
| input-shift | 分水岭在输入端 | 3 | position | speaker-led | “人给目标”与“世界变化”的双入口 |
| evidence-funnel | 六千条消息之后 | 3 | evidence | reading-first | 消息、Todo、Task 与 observing 分流 |
| hard-constraints | 两个概念，四条硬约束 | 3 | position | hybrid | 数字分身与主动式双支柱 |
| world-model | 页答现在，事实答历史 | 3 | architecture | hybrid | 页面与事实流的双轨世界状态 |
| progressive-context | 先看摘要，再按需下钻 | 3 | architecture | speaker-led | 索引、整页、事实明细三层镜头 |
| pipeline | 一条来源无关的流水线 | 4 | architecture | hybrid | M2→M3→固化→M5 路由图 |
| admission | 多数输入的正确答案是不行动 | 3 | decision | speaker-led | 76.6% observing 的判断闸门 |
| observing-case | Task 374 为什么没有干活 | 3 | evidence | hybrid | 重复任务识别与证据归并 |
| execute-loop | M5 不是计划执行器 | 3 | execution | speaker-led | 调查、选择、行动、验证循环 |
| approval | 风险在动作内容里 | 3 | execution | hybrid | 同类型动作的不同风险与审批载体 |
| resume | 等待不是失败 | 3 | execution | hybrid | waiting / needs_human / approval 续跑 |
| patrol | NOTHING 是高质量结果 | 3 | proactive | speaker-led | 每小时巡视雷达与克制结论 |
| system-loop | 闭环必须真的闭上 | 3 | architecture | hybrid | 世界变化、决策、动作、沉淀闭环 |
| proof | 当前运行水位 | 2 | evidence | reading-first | Todo / Task / Run 真实数据矩阵 |
| gaps | 现在还不成立的部分 | 2 | boundary | reading-first | 缺口仪表盘 |
| roadmap | 下一步不是更多自动化 | 3 | roadmap | hybrid | 判断可信度的三层升级 |
| closing | 正确地什么都不做 | 2 | closing | speaker-led | 信号安静熄灭，只保留结论 |

## Progress

- 2026-08-18: intake confirmed; deck root and delivery constraints confirmed.
- 2026-08-18: source documents and current SQLite metrics inspected read-only.
- 2026-08-18: three style previews implemented and checked; Signal Pipeline Flow selected.
- 2026-08-18: full 18-scene registry-driven harness and reading-first proposal implemented.
- 2026-08-18: Chromium visual/interaction suite and WebKit interaction suite passed; 21 tests total.
- 2026-08-18: static build produced 8 files / 4.7 MB; raster-backed PDF exported as 18 exact 16:9 pages and visually inspected.
