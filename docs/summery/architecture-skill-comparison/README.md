# Jarvis 架构图 Skill 横向试验

本目录用同一份 [统一简报](BRIEF.md) 对比三种架构图 Skill。三份图都表达同一条主线：外部现实经 M2 机械采集、M3 低成本准入和 Todo→Task 固化进入 M5；M5 持续调查、判断、行动与验证，结果再沉淀回世界状态并影响下一轮决策。

## 产物索引

| Skill | 主要产物 | 最适合的场景 | 本轮验证 |
|---|---|---|---|
| Fireworks Tech Graph | [`fireworks/jarvis-reality-loop.svg`](fireworks/jarvis-reality-loop.svg)、[`PNG`](fireworks/jarvis-reality-loop.png) | 文章首图、评审全景、可直接分享的静态图 | XML、marker、碰撞、语义几何、构图质量通过；浏览器渲染与视觉回看通过 |
| FlowForge | [`flowforge/jarvis-reality-loop.drawio`](flowforge/jarvis-reality-loop.drawio) | 工程协作、泳道职责、后续人工编辑 | `validate.py --theme tech-blue`：0 error / 0 warning；本机缺 draw.io CLI，未导出 PNG |
| Interactive Architecture Diagrams | [`interactive/architecture.html`](interactive/architecture.html)、[`说明`](interactive/architecture.md) | 现场讲解、逐步演示、分支与恢复流程探索 | 10 节点、4 flow、28 steps；端点有效、占位符 0、重叠 0、溢出 0、线穿无关节点 0；逐 flow 截图回看通过 |

## 横向结论

### Fireworks Tech Graph

- 静态信息压缩和视觉层级最好，一张图能同时承载主链、M5 内循环、失败反馈和世界状态反馈。
- SVG 是确定性可编辑源，PNG 可直接进入文档或飞书。
- 代价是复杂场景只能同时摊在一张画布上；继续增加分支会迅速提高信息密度。
- Skill 自带的 CairoSVG render gate 在本机缺 native `libcairo`，本轮按其浏览器导出路径完成 PNG 和视觉复核，没有把缺失门伪装成通过。

### FlowForge

- 泳道最清楚地表达“语义由谁负责、机器硬边界在哪里”，也最方便工程团队在 draw.io 中继续调整。
- XML 校验能较早发现 ID、主题、边界、重叠和连线错误。
- 本机没有 draw.io CLI，因此这版只有通过结构/几何校验的可编辑源，没有最终像素级预览；中文字体和 draw.io 最终路由仍需在装有桌面应用的环境中复核。

### Interactive Architecture Diagrams

- 最擅长把同一张架构拆成可播放场景：常规闭环、验证失败重规划、等待/审批恢复、主动巡视触发。
- 右侧面板可展示每一步的真实语义、证据载体和硬边界，避免把所有说明挤进主图。
- 交付是单文件 HTML，适合浏览器演示；不如 SVG/PNG 适合直接嵌入普通文档，维护成本也最高。

## 建议组合

- **主交付图**：Fireworks PNG/SVG，作为文章、飞书文档和评审材料的首图。
- **可编辑工程底稿**：FlowForge draw.io，后续由团队协作调整泳道与节点。
- **深度讲解附件**：Interactive HTML，用四条命名流程逐步讲清失败回路、恢复和主动巡视。

如果只保留一种，本轮更推荐 Fireworks；如果目标是让读者真正理解 M5 如何在变化中重新判断，则保留 Fireworks + Interactive 的组合价值最高。

架构事实源：`goal.md`、`docs/00-overview.md` 与当前代码；本目录均为表达产物。
