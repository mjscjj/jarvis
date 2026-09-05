# FlowForge 试验版：Jarvis 真实结果闭环

## 交付内容

- `jarvis-reality-loop.drawio`：可编辑的 draw.io XML 源文件。
- 本说明同时记录设计假设、校验结果和本地渲染限制。

打开方式：使用 draw.io 桌面应用，或在 [app.diagrams.net](https://app.diagrams.net/) 中导入 `.drawio` 文件。

## 选择与假设

- 图型：`swimlane`。原因是本题同时要求表达阶段所有权、跨阶段主链和反馈回路；泳道比单纯线性 flow 更能区分 M2、M3、Runtime、M5 与世界建模的职责。
- 主题：FlowForge 默认 `tech-blue`。
- 语言：跟随用户使用中文，保留 Task、Runtime、effects、current_world 等实现术语。
- 场景：subagent / batch，按 Skill 规则跳过人工确认。
- 一级阅读顺序：外部现实 → M2 → M3 → M5 → 世界状态；Runtime / Task 泳道使用较弱标签，作为机器硬边界而非第二个语义决策器。

生成前采用的结构草图：

```text
方向：左到右｜类型：swimlane｜主题：tech-blue

外部现实    [primary] 新事实 --------------------------------------> [success] 真实结果
M2                    [process] 原样采集
M3                              [warning] 低成本准入
Runtime                                  [process] Todo → Task
M5                                                [process] 上下文 → [accent] 调查/判断/行动 → [process] 验证
                                                                        ^          |
                                                                        |-- 失败 ---|
世界建模                                                                                  [primary] 世界状态
                                                  ^------------------------------------------|
                                                        current_world 影响下一轮决策
```

## 语义检查

- M2 只保留原始证据、来源和外部幂等键，并唤醒 M3。
- M3 明确是低成本准入，不是“快脑”；只产生 `extracted` 或 `observing`，准入时冻结 `context_snapshot`。
- Todo → Task 被画在 Runtime 硬边界泳道，明确是机械、幂等固化；`source_payload` 和 `background` 是证据，不是不可修改的执行合同。
- M5 是唯一语义决策核心，内部包含上下文装配、调查/判断/行动和现实验证。
- 验证失败或新事实以红色局部回路返回 M5 调查与判断。
- 真实结果通过 `ExecutionRun`、`TaskEvent`、`effects`、Summary、Fact 等事实载体进入世界状态；绿色外部回路把 `current_world` 带回下一轮上下文装配。
- 等待、人工补充和审批作为 M5 可选择的暂停/恢复动作，不是所有 Task 必经阶段。
- 主动巡视需要外部行动时创建普通 Task，仍进入同一 M5。

## 验证记录

结构校验命令：

```bash
python3 /Users/bytedance/.codex/skills/FlowForge/scripts/validate.py \
  /Users/bytedance/workspace-local/jarvis/docs/summery/architecture-skill-comparison/flowforge/jarvis-reality-loop.drawio \
  --theme tech-blue
```

结果：

```text
jarvis-reality-loop.drawio: 0 error(s), 0 warning(s) [theme=tech-blue]
```

校验覆盖 XML 完整性、ID 唯一性、箭头引用、正交连线、画布边界、节点重叠、容器层级、字号、文本适配估算和主题色合规。

本地导出命令：

```bash
bash /Users/bytedance/.codex/skills/FlowForge/scripts/render.sh \
  /Users/bytedance/workspace-local/jarvis/docs/summery/architecture-skill-comparison/flowforge/jarvis-reality-loop.drawio
```

结果：draw.io CLI 不在 PATH，`/Applications/draw.io.app/Contents/MacOS/draw.io` 等常见位置也不存在；脚本按预期返回 `draw.io CLI not found`。因此本轮没有生成 PNG，也不能完成基于渲染图的像素级视觉回看。

已完成的替代检查仅限确定性结构与几何检查：画布边界、节点互不重叠、泳道先于节点绘制、主链正交、M5 局部反馈走泳道下沿、世界反馈走泳道区域外的底部通道。限制是 draw.io 最终自动路由及中文字体的实际像素效果仍需在有桌面 CLI 的环境中复核。

架构事实源：`goal.md`、`docs/00-overview.md` 与当前代码；本图为表达产物。
