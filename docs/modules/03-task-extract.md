# M3 Todo 提取模块

> 隶属总纲 [`docs/00-overview.md`](../00-overview.md)。上下文组装见 [`docs/design-context-pipeline.md`](../design-context-pipeline.md)。
>
> 模块定位：把新消息 + 项目/人物/会话背景 + **已沉淀事实** 转成行动线索 `Todo`。默认引擎是 traex agent（可自跑工具推算项目与仓库），备用 `model_api`。M3 **只写 Todo**，不写 Task，也不手工记事实。

---

## 0. 流水线位置

```text
M2 采集 ──唤醒──▶ 【M3 提取 Todo】──▶ M5 判断环节 ──▶ M5 执行环节
                     │
                     └─ 读 fact 表（群 + 项目）作背景
旁路：factengine 持续把 message 蒸馏进 fact（M3 不写 fact）
```

M2 扫描落库后由 `pipeline.Coordinator` 按 chat 实时推进；`extract.schedule` 只做补偿。

---

## 1. 职责

**做什么**

1. 按会话/话题聚合上下文，识别真实可落地的线索，写成 `Todo`。
2. 给每条线索定 `status`：`extracted`（要我做）或 `observing`（值得知道但不需动手）。
3. 去重：精确指纹 + Qdrant `todo_semantic` 语义近邻 +（必要时）LLM 裁决。
4. 冻结 `context_snapshot`（一次组装、全程传递，下游不重建）。
5. `source_quote` 必须逐字来自 `[new]` 消息；失败则按 `evidence_retry_max` 重抽。

**不做什么**

| 事项 | 归属 |
|---|---|
| 采集与线索投递 | M2 |
| 值不值得做、`auto`/`dropped` | M5 判断环节 |
| Todo→Task 固化 | M5 判断环节 |
| 蒸馏 / 写入长期事实 | 离线事实引擎 |
| 对外写操作 | M5 执行环节 |

提示词（`conf/prompts/m3-system-prompt.md`）明确：已沉淀事实只作背景，不要手工 `append-fact`，也不要把事实本身再抽成新线索。

---

## 2. 输入 / 输出

**读**

- `message`：本轮新消息 + 受限回看上下文
- `feishu_group` / `project` / `person` / `principal_profile` / `resource`
- `fact`：按群 ID、项目 ID 各取最新 `extract.fact_limit` 条（`progress.Service.ListFacts`）
- 共享记忆、工作规则、Skill 目录、工具说明（动态注入）

**写**

- `todo` / `todo_event`
- `todo_extract_watermark`
- Qdrant `todo_semantic` 向量（去重用）

不再调用任何记忆 sidecar，不再提供 `search_memory` 工具。工具箱只保留历史消息、资源等检索（见 `internal/extract/tool_box.go`）。

---

## 3. 引擎与配置

| 项 | 说明 |
|---|---|
| `extract.engine=codex`（默认） | traex agent，可自跑 lark-cli / bytedcli / git / jarvis-tools |
| `extract.engine=model_api` | 百炼等 OpenAI 兼容端点 + function-calling |
| embedding | `model.embedding_model` / `embedding_dims`，只服务 Todo 去重 |
| Qdrant | `extract.qdrant_host` / `qdrant_grpc_port`，集合 `extract.semantic_collection` |
| 事实注入 | `extract.fact_limit` |

提示词正文：`conf/prompts/m3-system-prompt.md`（textstore key）。工具说明：`internal/toolcatalog`。

---

## 4. 关键实现

| 文件 | 作用 |
|---|---|
| `internal/extract/worker.go` | 编排；批级 `loadFacts` |
| `pipeline_store.go` | 加载批次、水位 |
| `prompt.go` | 组装 system/user；`renderFacts` |
| `persist.go` / `snapshot.go` | 落库与冻结快照 |
| `dedup.go` + `internal/semantic` | 语义去重 |
| `codexengine/` / `provider/` | 两套引擎 |

```bash
./bin/jarvis-server -config conf/config.yaml -extract-once
```

---

## 5. 与事实层的契约

1. M3 **只读** `fact`，不写。
2. 事实按主体绑定，不是按「和本条消息相似」检索；缺的用工具查，不靠 mem0 式向量召回。
3. observing 线索留在 Todo 视野里；「已经定了的结论」挂在实体上的那份由 factengine 写。

---

## 6. 开放问题

1. `fact_limit` 与提示词长度的平衡——实跑校准。
2. 语义去重阈值与近邻数是否仍合适。
3. 线索投递来源（会议等）的抽取质量靠提示词与 Skill，不在 Go 里开分支。
