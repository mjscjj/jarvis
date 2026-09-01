# Context：Jarvis 主动式数字分身 Slides

## 决策快照

- deck_root: `docs/summery/jarvis-active-digital-twin-slides`
- source: 飞书 Wiki《主动式数字分身探索实践：世界模型与决策方案》+ Jarvis 当前代码
- audience: Agent、产品与研发协作者
- talk_duration: 25–30 分钟
- presentation_format: 现场技术分享 + PDF 独立复看
- final_scene_count: 20
- scene_count: 20
- mode: hybrid
- density: high
- audit_profile: hybrid
- style: Engineering Whiteboard Explainer 2.0
- motion: beat-driven；总图连续高亮，局部结构按语义展开
- stage: 1920×1080，16:9 固定画布
- navigation: 键盘、空白点击、触控滑动、右侧局部页码轮
- technology_stack: React + TypeScript + Vite + Playwright
- test_command: `npm test`
- delivery_target: 互动 HTML + 20 页 PDF

## 语义边界

- 世界模型结构图与行动/决策闭环图保持分离。
- 世界模型：实体与关系 + 当前认知 + 时间化证据 + 行动状态。
- Summary 回答现在怎样；Fact 回答发生过什么；PageRevision 保存 Jarvis 以前怎么看。
- M3 只做低成本准入；Todo 机械固化为 Task；M5 持有调查、重判、执行、等待、询问和停止的语义决策权。
- `context_snapshot` 是冻结的审计背景；执行时的新鲜状态来自当前 Summary / Fact 与 Task / Todo。
- FactEngine 只在完整成功后推进独立游标，失败材料保留重放；实体页使用 CAS 并读回验证。

## Selected Theme Notes & Design DNA

- chosen_direction: Engineering Whiteboard Explainer 2.0
- keep: 真白工程画布、淡蓝网格、精确连线、底部结论栏
- typography: 楷体风标题与批注；Noto Sans SC 正文；JetBrains Mono 技术标识
- palette: 蓝=系统路径，绿=认知/验证，橙=证据/准入，紫=状态/模型，红=风险
- navigation_treatment: 右侧局部页码轮
- motion_vocabulary: 总图连续高亮、卡片滑入、连线绘制、状态盖章
- interaction_vocabulary: 认知记录、阶段节点可 hover / click；局部交互不触发翻页
- density_and_copy_tone: 高密度但不缩小核心字体；一页一个判断，结构与证据同屏
- custom_metaphors: 运行总图、认知账页、快慢脑泳道

## 进度

- 2026-08-25：完整文档读取并完成 20 页压缩方案。
- 2026-08-25：用户确认开始制作，先实现第 10、12、15 页同风格高密度预览。
- FlowForge 源图：`assets/diagrams/jarvis-operating-loop.drawio`、`assets/diagrams/jarvis-world-model-anatomy.drawio`。
- FlowForge 结构校验：两图均为 0 errors / 0 warnings；本机未安装 draw.io CLI，PNG 级验证由 Slides 的 DOM/SVG 重建与浏览器截图承担。
- 2026-08-25：三页最终帧完成逐页视觉检查；世界模型默认聚焦 Fact，实体节点与关系语法分层无覆盖。
- 2026-08-25：`npm run build` 通过；Chromium + WebKit 共 19 项 harness、交互、字体、移动端、打印与视觉回归测试全部通过。
- 2026-08-25：重新读取飞书原文最新版本 `revision_id=3665`，确认完整叙事为能力证据 → 产品命题 → 世界模型 → 决策执行 → 通用化展望。
- 2026-08-25：完成 1–20 页注册表与全部最终帧；10 张能力案例截图已本地化，避免现场依赖飞书临时下载链接。
- 2026-08-25：完成全套接触表视觉巡检，重点页 5、7、13、18 额外按原尺寸复核。
- 2026-08-25：最终 `npm run build` 通过；Chromium + WebKit 共 36 项渲染、稳定路由、逐 beat、交互隔离、移动端、字体、打印与视觉回归测试全部通过。
- 2026-08-25：导出 `artifacts/jarvis-active-digital-twin-20p.pdf`，Spotlight 元数据显示 20 页、3,811,888 bytes。
- 2026-08-25：完整 HTML 总入口固定为 `http://127.0.0.1:4188/`。
