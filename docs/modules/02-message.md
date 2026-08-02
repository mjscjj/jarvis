# M2 消息采集模块

> 隶属总纲 [`docs/00-overview.md`](../00-overview.md)。
>
> 模块定位：流水线第一段。把飞书会话消息与外部线索可靠落到 MySQL（明文，source of truth），沉淀 `Group` / `Resource` / `ScanRecord`，按 chat 唤醒下游 M3。**不做事实蒸馏、不写向量记忆**——那是离线事实引擎的事（总纲 §5）。

---

## 0. 边界

| 属于 M2 | 不属于 M2 |
|---|---|
| 会话发现、增量扫描、checkpoint | Todo 判定（M3）、值不值得做（M5） |
| `message` / `resource` / `scan_record` 落库 | 下载/解析妙记正文（按需由下游触发） |
| 通用线索投递 `POST /api/clues` | 为某个来源开专用 Go 链路或状态枚举 |
| 采集失败时原样写错误证据 | 把错误翻译成「申请权限」类 Todo |

硬规则（对齐 `AGENTS.md`）：

1. **错误是证据，不是决策。** 无权限、暂不可用都原样落库，由 M3 结合上下文判断。
2. **流水线通用。** 新来源 = 新 `source` 值 + Skill，不等于新模块。
3. **采集与事实沉淀解耦。** M2 只写原料；`factengine` 按自己的水位消费 `message`。

代码：`internal/capture/`（`service.go`、`scheduler.go`、`clue.go`）。

---

## 1. 数据流

```text
飞书 ──lark-cli──▶ [M2 capture]
                      │
                      ├─► feishu_group / chat_checkpoint
                      ├─► message（明文）
                      ├─► resource（元数据，不下载）
                      └─► scan_record
                      │
                      ▼
              pipeline.Coordinator（按 chat 唤醒 M3）

旁路（不在关键路径）：
message ──水位──▶ factengine ──▶ fact 表
```

覆盖策略：`related_group=1` 的会话用 user-token 轮询；bot 事件流作低延迟增强。两条链路以 `message_id`（`om_`）幂等合流。

---

## 2. 实体与表

| 表 | 职责 |
|---|---|
| `feishu_group` | 会话目录；`related_group` 白名单；`project_id` 由 M1 维护；`tier` 仅展示 |
| `chat_checkpoint` | 每会话一行高水位，断点续扫 |
| `message` | 明文 SoT；字段见 `internal/domain/capture.go` |
| `resource` | 附件/妙记/文档引用；采集期 `downloaded=0` |
| `scan_record` | 每次扫描追加流水 |

`message` **已删除** `mem0_processed` / `mem0_processed_at` 与索引 `idx_mem0`。事实引擎用 `fact_source_cursor.last_id` 推进，不再在消息行上打标。

索引意图：`uk_message_id` 幂等；`idx_chat_create(chat_id, create_time)` 供上下文回看与切窗。

---

## 3. 线索投递（新来源唯一入口）

```bash
jarvis-tools append-clue   # 即 POST /api/clues
```

M2 只做三件机械事：写入 `message`、按 `(source, external_id)` 幂等、唤醒 M3。不分类、不下结论、不截断原文。

每个 `source` 自动对应一个 `chat_mode=clue` 伪会话（如 `clue:feishu_meeting`）。示例 Skill：`.agents/skills/feishu-meeting-clue/`——只报「哪场会开完了」，妙记有没有由 M3 自己查。

---

## 4. 调度与运维

| Job | 配置 | 作用 |
|---|---|---|
| discover | `capture.discover_schedule` | 同步会话元数据 |
| scan | `capture.scan_schedule` | 增量拉白名单会话消息 |

```bash
./bin/jarvis-server -config conf/config.yaml -discover-once
./bin/jarvis-server -config conf/config.yaml -scan-chat <chat_id>
./bin/jarvis-server -config conf/config.yaml -set-related-groups <ids>
./bin/jarvis-server -config conf/config.yaml -open-p2p
```

lark-cli 封装：`internal/larkcli`（限流、超时、fail-fast）。

---

## 5. 开放问题

1. bot 事件流是否全面启用——当前以轮询为主。
2. Resource 按需下载的权限边界（妙记）需持续实测。
3. 编辑过的消息是否触发下游重抽——当前更新内容，是否唤醒 M3 由水位与业务决定，不再重置「记忆已处理」标志。
