# M3 Task 准入模块

> Status: current
> Authority: normative module guide
> Last verified: 2026-08-06
> Code source: `internal/extract/`, `internal/contextsnap/`

M3 把新证据和工作背景转成经过准入判断的 Todo。它回答“这条线索是否值得启动一次 M5”，只创建/更新 Todo，不创建 Task、不执行外部写操作、不手工写长期 Fact，也不替 M5 制定方案或完成调查。

## 1. 输入与输出

读取：

- 本轮新 message、受限会话上下文和工具补查消息；
- Group、Project、Person、PrincipalProfile、Resource；
- 既有 open Todos 和按群/项目加载的 Facts；
- shared memory、rules、Skills 与工具目录。

写入：

- `todo`、`todo_event`、`todo_extract_watermark`；
- Qdrant `todo_semantic` 去重向量。

M3 状态只有：

- `extracted`：存在需要 M5 执行 Agent 调查和判断的动作线索；
- `observing`：值得知道，但当前无需任何人行动。

重提取可以在 `extracted ↔ observing` 间调整，不随意重新打开已经 `materialized` 的 Todo。

## 2. Candidate 契约

Candidate 只保留机器确实消费的小外壳：`action_type`、`status`、`title`、`target`、`project_hint`、source message/quote，以及一段不解析的 `payload`。payload 是准入简报：自然表达与 principal 的相关性、未闭环状态、当前责任、已核验证据、status 依据和剩余不确定性。执行计划、候选方案、具体副作用、审批和最终完成标准由 M5 调查决定，Go 不逐字段投影。

`action_type` 是开放的 snake_case 字符串；代码不维护封闭业务枚举。完整协议以 `internal/extract/candidate.go` 和 `internal/extract/provider/schema.go` 为准。

至少一条 evidence 必须来自本轮 `[new]` message。模型可以用工具引用同一 chat 中批次外的消息，worker 会补载并校验；`source_quote` 必须逐字命中证据，否则按配置重抽，耗尽后 fail-fast。

## 3. 项目解析与上下文快照

项目归属顺序：

1. Group 已绑定 Project 时直接使用；
2. 否则用 `project_hint` 对 code/name 精确匹配；
3. 一次短查询仍无法确定则记录 unresolved resolution trace，交给 M5 在确有需要时继续调查。

冻结快照包含 principal、group、project、由证据发送者机械推导的 assigner、引用消息、会话上下文、参与人、资源、open Todos、其他项目和 Facts。`extraction_result` 保留完整 Candidate，`resolution` 保留项目/仓库推算轨迹。

当前快照不包含 shared memory，也未冻结 ManagedResource；它们可能进入运行时 prompt，但不能笼统写成快照已包含所有背景。

## 4. 去重与落库

去重依次使用：

- 精确 fingerprint；
- Qdrant 语义近邻；
- 必要时模型裁决。

Todo、事件、水位和去重向量在同一落库流程中协调；关键步骤失败时整轮报错，不留下“数据库成功但语义索引缺失”的伪成功。

## 5. 引擎与配置

| 配置 | 当前语义 |
|---|---|
| `extract.engine=codex` | 默认，traex Agent 自跑 lark-cli/bytedcli/git/jarvis-tools |
| `extract.engine=model_api` | 备用 OpenAI-compatible function-calling 引擎 |
| `extract.concurrency` | 不同 `chat_id` 的并发上限；同一单聊或群聊始终串行 |
| `extract.fact_limit` | 默认注入的已有 Fact 上限 |
| `extract.semantic_*` | Qdrant Todo 去重配置 |

稳定行为正文在 `conf/prompts/m3-system-prompt.md`；运行时组装在 `internal/extract/prompt.go`；工具说明来自 `internal/toolcatalog`。M3 的工具查询只服务四个准入问题：相关性、未闭环状态、责任归属和完成/重复检查；证据足够后立即停止。

M2 新消息实时唤醒 M3；`extract.schedule` 只做持久化补偿。单聊中的一个人和一个群都由各自的 `chat_id` 隔离：不同 chat 可以并行，同一 chat 的连续水位严格串行。补偿扫描与实时抽取互斥，拿到待处理 chat 后再按 `extract.concurrency` 并发，避免同一批证据被两条路径重复处理。

```bash
./bin/jarvis-server -config conf/config.yaml -extract-once
```

## 6. 当前限制

- ResourceContext 没有通用本地正文/路径能力，附件读取仍依赖 Agent 通过外部 token/url 查询。
- 快照是审计证据，不是 live world state。
- `fact_limit`、语义阈值和近邻数需要按真实运行数据持续校准。
