# Context：Jarvis 世界模型与维护逻辑 Slides

## 决策快照

- deck_root: `docs/summery/jarvis-world-model-maintenance-slides`
- audience: Jarvis 内部产品、Agent 与研发协作者
- talk_duration: 18–20 分钟
- content_orientation: 技术讲解 + 架构解释
- presentation_format: 现场讲述为主、可独立复看
- content_mix: 3 页问题与定义，6 页模型，5 页维护，2 页消费与收束
- mode: hybrid
- density: medium-high
- audit_profile: hybrid
- motion: beat-driven continuity；只为解释状态变化服务
- stage: 1920×1080，16:9 固定画布
- navigation: 键盘、点击、滑动、画布内导航
- technology_stack: React + TypeScript + Vite + Playwright
- testing_plan: 稳定帧、注册表、导航隔离、固定画布、移动端、字体、运行时错误与视觉基线
- test_command: `npm test`
- delivery_target: 互动 HTML + 16 页 PDF
- non_goals: 不做完整产品方案、不展开完整 M2/M3/M5 流水线、不把设计文档旧描述当成当前实现
- context_document: 本文件

## 事实边界

- 长期实体页：Principal、Person、Project、KeyMatter、Group、Resource。
- 关系来自 Summary 中的实体 URI 引用并派生 backlinks，不单设关系表。
- Summary 表达当前认知；Fact 记录世界事实；PageRevision 记录 Jarvis 的旧认知。
- 行动状态是 Todo、Task、ExecutionRun，与长期实体页并列，不混成一套表单。
- FactEngine 当前统一处理 Message、TodoEvent、TaskEvent 投影。
- 程序负责材料投影、批次、独立游标和粗粒度预算；维护 Agent 负责语义判断。
- 完整维护成功后才推进游标；单条 SourceUnit 不截断；实体页用更新时间 CAS 防止覆盖并读回验证。
- FactEngine 是自动主维护者；M5 / Proactive 通过通用工具辅助维护，不画成专用硬编码实时同步链路。

## 视觉方向选择记录

- anchor_scene: 世界模型如何被持续编译
- beats: 4
- candidates: Engineering Whiteboard Explainer / Research Memo / Signal Pipeline Flow
- status: selected_whiteboard
- preview_port: 4186
- preview_routes:
  - `/?scene=whiteboard&beat=3&snapshot=1`
  - `/?scene=memo&beat=3&snapshot=1`
  - `/?scene=signal&beat=3&snapshot=1`

## Selected Theme Notes & Design DNA

- chosen_direction: Engineering Whiteboard Explainer
- keep: 真白工程画布、可检查的卡片与连线、明确 active state、底部 takeaway lane
- borrow_from_other_previews: 不借用 Memo 的纸张质感或 Signal 的暗色仪表盘
- typography: 中文标题与批注使用楷体风格；正文 Noto Sans SC；代码和状态 JetBrains Mono
- colors_and_background: 白底淡蓝网格；蓝=实体/系统路径，绿=Summary/健康写回，橙=Fact/证据，紫=PageRevision/模型状态，红=风险
- navigation_treatment: 右侧白板标记轮，仅显示当前页附近 4 个节点
- motion_vocabulary: 卡片滑入、路径绘制、标签盖章、旧判断退场、最终结论落定
- interaction_vocabulary: 实体和认知卡片 hover；关键页点击展开局部说明；交互不触发全局翻页
- annotation_style: 楷体 marker label、黄色重点划线、代码位置小标签
- asset_or_emoji_strategy: 主要使用 DOM/SVG；emoji 只作为绑定到实体节点的语义对象
- density_and_copy_tone: medium-high hybrid；每页一个主判断，代码事实做小型证据标记
- custom_invented_metaphors: 世界坐标、双层世界、认知账页、证据编译器、维护飞轮、渐进式镜头
- pacing_log: Low | Medium | Low | Medium | High | Medium | Medium | Medium | Low | Medium | High | Medium | High | Medium | Medium | Low

## 完整制作阶段

- selected_preview: `whiteboard`
- final_scene_count: 16
- runtime_preview_routes_to_remove: `memo`, `signal`
- final_delivery: interactive HTML + 16-page PDF
- status: full_deck_ready

## 预览实现记录

- 2026-08-25：建立 React/Vite 固定 1920×1080 harness。
- 三个候选方向使用同一内容、同一 4-beat 语义状态和同一稳定路由契约。
- 三种方向均包含 hover、点击展开、beat reveal 和场景连续切换。
- 修复隐藏 reveal 仍拦截点击的问题：隐藏状态禁用 pointer events，显示后恢复。
- 预览截图写入 `docs/previews/`；视觉基线写入 `tests/visual.spec.ts-snapshots/`。

## 当前验证状态

- `npm run build`: pass
- `npm test`: pass，35 tests
- Chromium: pass
- WebKit: pass
- 覆盖：16 场景全部 beat 深链、资源与运行时错误、错误路由、键盘、点击、滑动、交互隔离、固定画布、移动端可见性、本地字体、16 页打印状态、排版溢出和代表页视觉基线。
- 视觉验收：已生成 16 页最终帧并检查总览；重点复查定义、实例、关系、编译循环、可靠性、维护所有权和渐进披露页。
- 当前交付：本地互动预览、静态构建和 16 页 PDF；未做线上部署。
