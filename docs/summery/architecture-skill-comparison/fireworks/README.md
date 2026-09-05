# Fireworks Tech Graph 试验版

主图使用 `fireworks-tech-graph` 的 **Style 1 · Flat Icon**，将 Jarvis 表达为一张“Agent 架构 + 过程/反馈流”复合图。

## 产物

- `jarvis-reality-loop.svg`：可编辑语义 SVG，1600 × 1120。
- `jarvis-reality-loop.png`：浏览器高分辨率导出，3200 × 2240。
- `VALIDATION.md`：结构、几何、渲染与视觉检查记录。

## Semantic contract

- **主执行流（蓝）**：外部现实 → M2 机械采集 → M3 低成本准入 → Todo/Task 机械固化 → M5 上下文装配 → 调查 → 判断 → 行动 → 验证 → 真实结果。
- **验证反馈（红虚线）**：验证失败、现实变化或新事实 → 回到 M5 调查与判断。
- **世界状态读写（绿）**：真实结果 → 持续世界状态；世界状态 → 下一轮 M5 上下文。
- **语义边界**：M3 不制定计划/副作用/审批；M5 是唯一语义决策与执行核心；Go/runtime 只负责机器硬边界。
- **暂停恢复**：`waiting`、`needs_human`、`awaiting_approval` 恢复同一个 Task，不形成第二套链路。

## 构图选择

第一眼保留五个一级区块：外部现实、低成本准入、Task 固化、M5 核心、世界状态；实现名和状态降为二级标签。主链从左向右，两条反馈各占独立走线走廊。图内只使用自绘通用图标，没有产品品牌图标，因此无需加载产品 icon 资源。

架构事实源：`goal.md`、`docs/00-overview.md` 与当前代码；本图为表达产物。
