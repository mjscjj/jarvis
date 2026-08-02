# M5 判断环节

> Status: current
> Authority: normative module guide
> Last verified: 2026-08-02 @ `89fa24b`
> Code source: `internal/execute/decision_*.go`

判断环节回答“这条 extracted Todo 是否值得交给执行 Agent 推进”。它和执行环节同属 M5、共用协调队列；不是独立的 M4，也没有 Todo 层人工确认队列。

## 1. 边界

- 输入：`status=extracted` 的 Todo、M3 冻结的 `context_snapshot` 和完整 `extraction_result`。
- 输出：`ready / observe / drop` disposition，以及宽松 `plan/payload`。
- 允许：通过 `jarvis-tools`、`lark-cli`、`bytedcli`、`git` 等查证事实。
- 禁止：在判断阶段产生外部副作用。
- 不做：审批、副作用执行、等待 principal 回答。

“禁止副作用”来自阶段提示词。当前默认 sandbox 是 `danger-full-access` 并允许联网，不是 OS 级 read-only。

## 2. 三种判断

| Disposition | 存储 route/status | Task | 语义 |
|---|---|---|---|
| `ready` | `auto` | 创建 | 值得 M5 调查/推进 |
| `observe` | `observing` | 不创建 | 值得保留，但没人需要行动 |
| `drop` | `dropped` | 不创建 | 不值得做或不是有效行动线索 |

需要 principal 选择或提供本人独有信息的线索仍可 `ready`。执行 Agent 先完成安全调查，再通过 Task `needs_human` 提问。`observe` 不是等待用户的替代状态。

M3 也可以直接创建 `observing` Todo；判断 worker 只领取 `extracted`。

## 3. 数据流

```text
Todo extracted
  -> DecisionWorker
  -> CodexEvaluator
       读取 snapshot / extraction_result / 最近判断
       注入 shared memory / rules / Skills / tool catalog
  -> CodexDecider
       严格外壳 disposition + plan + payload
  -> EvaluationStore
       ready   -> Todo auto + TodoEvent + DecisionAudit + Task pending
       observe -> Todo observing + TodoEvent + DecisionAudit
       drop    -> Todo dropped + TodoEvent + DecisionAudit
```

正常由 M3 提交后按 Todo ID/version 实时触发；`decide.schedule` 扫描遗漏的 extracted Todo。版本冲突、非法状态、模型错误和非法输出都 fail-fast，Todo 留待后续补偿。

## 4. Prompt 与运行方式

可信区依次组合：

- `conf/prompts/m5-decision-system-prompt.md`
- `conf/rules/all.md` + `conf/rules/decide.md`
- decide 阶段 Skills
- shared memory
- `internal/toolcatalog` 的 decide 工具目录

不可信业务区主要包含：

- M3 完整 `extraction_result`
- 冻结 `context_snapshot`
- 最近判断记录

当前 evaluator 没有给 CodexDecider 传固定 repo cwd；仓库信息在上下文中，Agent 可自行调查。不要把它写成 runtime 已绑定 Project repo。

## 5. 输出契约的已知不一致

当前 Go 语义希望 `plan/payload` 接受任意非空 JSON，`observe/drop` 允许 plan 为空；但 `internal/execute/decision_codex.go` 交给 CLI 的 JSON Schema 仍把二者声明为非空 string。本文不把其中任一侧包装成更强承诺。

这是代码契约债务：应先统一 schema、提示词、解码器和测试，再把最终形状升级为 current 文档真相。当前 disposition 的三值映射是稳定的。

## 6. Task 物化

`ready` 时：

- `Task.background` 复用 M3 snapshot；
- `Task.source_clue` 保存完整 M3 extraction result；
- `Task.plan` 和 `decision_payload` 保存判断方向与语义上下文；
- `confirmed_by=m5_decision`；
- Todo 与 Task 通过唯一键保证一对零或一。

这些字段在执行 prompt 中只是 clue/direction/context，不是不可挑战的最终计划。AGENTS 目标设计要求执行期可修改并留痕，但当前没有通用 Store/API/tool 实现；在实现前不能声称它已经可写。

## 7. 运维

```bash
./bin/jarvis-server -config conf/config.yaml -decide-once
```

基线配置在 `decide.*` 和 `codex.*`；有效值还会叠加 `conf/config.runtime.yaml`。
