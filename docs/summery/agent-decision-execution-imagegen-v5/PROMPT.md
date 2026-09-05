# Agent 决策与执行技术全景图（ImageGen v5）

## 生成模式

- 模式：内置 ImageGen，全新生成后进行局部编辑
- 用途：飞书文档和演示文稿中的 16:9 技术架构大图
- 最终图片：`agent-decision-execution-panorama.png`

## 主生成提示词

```text
Use case: infographic-diagram
Asset type: a premium full-width technical architecture panorama for a Chinese Feishu document and keynote presentation

Create a beautiful, restrained, information-rich architecture diagram titled “Agent 决策与执行”. Explain how an Agent moves from external changes to value admission, deep decision, delegated execution, verification, world-model maintenance, and closeout.

Core visual concept:
A short linear inlet on the left transforms into one dominant continuous decision-and-action loop in the center, then exits into a compact closeout on the right. A thin persistent world-model foundation runs underneath the whole system. M3 only admits value; M5 is the real recursive decision and execution core. Do not make an admin dashboard or a row of equal boxes.

Style:
- 16:9 wide landscape with generous margins.
- Mature editorial systems-map aesthetic and Swiss information design.
- Warm off-white background, dark navy text, cobalt-blue main path, one muted-amber accent for M3, soft cool-gray dividers.
- Flat vector-like raster appearance, hairline strokes, disciplined whitespace.
- No icons, illustrations, gradients, shadows, glow, 3D, decorative patterns, watermark, footer, page number, or fake UI chrome.
- Use open typography groups and connector lines. Only major system boundaries receive a frame.
- Keep Chinese typography modern, clean, and legible. Render supplied text verbatim and invent no extra labels.

Header:
“Agent 决策与执行”
“外部变化触发，Agent 持续理解、决策、行动并验证结果”

Control plane:
“决策约束”
“Principal 目标”  “工作规则”  “审批规则”  “可信记忆”  “Tools / Skills”

Left inlet:
“外部变化”
“消息 · 会议 · 日程 · 文档 · 代码 · 系统事件”
“Adapter｜数据接入”
“采集 · 规范化 · 保留原始证据 · 统一投递”
“只记录事实”
“快脑 M3｜价值准入”
“与 Principal 相关？”  “存在未闭环结果？”
“需要 Jarvis 介入？”  “已经完成或重复？”
Two exits: “观察 / 丢弃” with “停止推进，不删除事实”; “值得推进” into M5.

Central core:
“慢脑 M5｜主 Agent 决策与执行闭环”
“持有真实目标、行动决策、审批判断与完成判定权”

Upper lane “理解与决策”:
“形成当前世界观” → “还原真实目标” → “深度调查” → “决定下一步”
Fork: “不做” exits to closeout; “要做” descends to action.

Lower lane “行动与验证”:
“拆解行动” → “调度执行” → “收集结果与证据” → “验证真实结果”
Under dispatch: “调查子 Agent”  “执行子 Agent”  “Tools / Skills”
Caption: “只执行边界明确的任务”
Inline optional actions: “等待 · 询问 · 审批（按需）”
One feedback arrow: “未通过：更新认知，重新决策”, from verification back to deep investigation.
One forward arrow: “通过”.

Right closeout:
“结果收口”
“完成”  “观察”  “等待”  “需人工”  “待审批”  “失败”
“总结与交付”
“对外结果”
“进展与 effects”
“完成证据与下一步”
“结束或恢复”
“条件变化后可重新进入 M5”

Bottom foundation:
“世界模型｜持续读取与维护”
“实体与关系”
“当前认知 Summary”
“时间证据 Fact”
“行动状态”
“外部事实与执行结果共同更新世界模型”
Relationships: world model to M3 “摘要读取”; bidirectional with M5 “渐进读取 / 新认知维护”; result to world model “验证后沉淀”.

Acceptance:
- Three-second reading: 外部变化 → M3 准入 → M5 循环 → 结果, with world model as persistent foundation.
- M5 is the unmistakable visual center.
- Spacious and premium despite complete content.
- No crossing arrows, ambiguous directions, or equal-weight box matrix.
```

## 局部校正

1. 将 M3 与世界模型之间的“摘要读取”改为从世界模型向上读取，箭头只指向 M3。
2. 删除重复的验证回流，只保留“验证真实结果 → 深度调查”的一条回路。
3. 删除跨越 M5 底部的多余灰色长线；“观察 / 丢弃”作为 M3 的明确终止结果，右侧结果状态保留“观察”。
