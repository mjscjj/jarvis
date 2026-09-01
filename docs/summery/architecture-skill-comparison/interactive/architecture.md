# Jarvis：从线索准入到真实结果闭环

> 线索只是起点，真实结果才是终点。M3 做低成本准入，M5 在同一个执行循环内持续调查、判断、动作和验证；结果沉淀回世界状态，并真正影响下一轮决策。

交互版请打开 [architecture.html](architecture.html)：选择场景后用「上一步 / 下一步」或自动播放，观察当前交接、整条路径和右侧证据详情。

## 一级概念

- **外部现实**：消息、日程、文档、代码仓库、定时 Skill、主动巡视或人工介入产生新事实。
- **M2 机械采集**：保存完整原文、来源与外部幂等键，成功后唤醒 M3；不分类、不下结论、不按来源开专线。
- **M3 低成本准入**：只调查到足以决定 `extracted` 或 `observing`；证据足够就停止，准入时冻结完整 `context_snapshot`。M3 不是“快脑”，不制定执行计划、不选择副作用、不判断审批。
- **Todo → Task**：`extracted Todo` 无模型、幂等固化为 `pending Task`。`source_payload` 与冻结 `background` 是开放线索和审计证据，不是不可修改的执行合同。
- **M5 唯一语义决策与执行核心**：每次运行装配来源证据、冻结背景、实时 `current_world`、人工 supplements 和最近运行记录；按需读取 Summary、Fact 与外部系统，在同一循环里完成调查 → 判断 → 动作 → 验证。
- **世界状态**：执行结果、`effects`、TaskEvent、ExecutionRun 等进入持续世界建模，形成 Summary、Fact 和下一轮可读的 `current_world`。

## 可点击演示的四条流程

### 1. 常规闭环

1. 外部现实 → M2：原始事实经统一入口到达。
2. M2 → M3：原文幂等落库后唤醒准入。
3. M3 → Todo/Task：决定 `extracted` 并冻结完整上下文。
4. Todo/Task → M5：机械固化为普通 Task，交给同一 M5。
5. M5 → 工具：装配实时上下文，调查、判断并选择动作。
6. 工具 → 真实结果：外部适配产生具体副作用。
7. 真实结果 → M5：回读现实，核验目标是否真正成立。
8. M5 → 世界状态：沉淀结果、effects 与审计事实。
9. 世界状态 → M5：下一轮从 `current_world` 和按需的 Summary / Fact 继续。

### 2. 验证失败后重规划

M5 执行动作后回读真实现实。即使工具报告成功，只要目标未成立或现实已改变，M5 就记录新事实，重新读取世界状态，改写当前目标并进入下一轮调查 / 动作。这个回路在同一 M5 Agent 内，没有独立 Verifier 模块。

### 3. 等待 / 审批后恢复

M5 可以把 Task 暂停为 `waiting`、`needs_human` 或 `awaiting_approval`。时间到期、人工补充或审批决定到达后，系统恢复同一 Task；M5 读取原进展和新证据后重新判断，不机械照搬旧计划。审批只针对具体副作用，不是所有 Task 必经的固定阶段。

### 4. 主动巡视触发

主动巡视读取同一世界状态；已确认的内部变化可通过通用 CRUD 直接沉淀。一旦需要外部行动，巡视只创建 `source_type=proactive` 的普通 Task，仍由同一 M5 调查、执行、验证和沉淀，不形成第二套决策链路。

## 程序硬边界

Go/runtime 只保证持久化、状态、幂等、调度、权限 / 审批载体与 effects / event 审计。自然语言目标、调查、判断和计划留给模型。Task 可以结束为 `done`、`observing`、`waiting`、`needs_human`、`awaiting_approval` 或 `failed`。

## 边界声明

- 本图不引入 M4、Goal Supervisor 或独立 Verifier；这些不是当前架构。
- Qdrant 当前只用于 Todo 语义去重，不是长期事实真源，因此没有在本一级概念图中作为世界模型绘制。
- 本图不为会议、邮件或飞书等来源绘制专用 Go 流水线。
- 没有虚构“开发 / 生产”、“离线 / 在线”等架构模式；顶部仅标识“当前架构”。

## 验证记录

验证日期：2026-08-21。

```bash
/Users/bytedance/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node \
  validate-semantic.js architecture.html
```

结果：10 个节点；4 条 flow，步数分别为 9 / 6 / 7 / 6；所有 `from` / `to` 均有对应节点，flow tab 与 JS key 完全一致，没有 `{{...}}` 模板残留。

```bash
NODE_PATH=/Users/bytedance/.npm/_npx/1ade4bf2e2bf80fd/node_modules \
  /Users/bytedance/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node \
  validate-local.js architecture.html
```

结果：在 934×460 的实际 stage 中，节点重叠 `0`，节点溢出 `0`，全部 flow 的连线穿过非端点节点 `0`。`human` 和 `world` 底边分别为 451.4 与 450.4，均在 460 高的 canvas 内完整可见。

```bash
NODE_PATH=/Users/bytedance/.npm/_npx/1ade4bf2e2bf80fd/node_modules \
  /Users/bytedance/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node \
  screenshot-local.js architecture.html .
```

结果：使用 macOS Google Chrome 产生完整页预览，并回看了常规闭环的真实结果验证、验证失败、等待 / 审批、主动巡视创建 Task 等关键步骤；未发现文字裁切或节点卡片重叠。

---

**架构事实源：** `goal.md`、`docs/00-overview.md` 与当前代码；本图为表达产物。
