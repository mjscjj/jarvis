# M3 Todo 提取模块技术方案

> 所属系统：基于飞书的本地个人 Jarvis 管家（Principal = 字节研发工程师 `chujiejie.1`）
> 模块定位：流水线第 3 环，把「新捕获消息（明文）+ 项目/人员/会话背景 + mem0 记忆 + Resource 线索」转成「真实、可落地的行动线索」——即 **`Todo`**（候选，非最终任务）。
> 设计原则：本地可信明文存储 · fail-fast 暴露问题 · 不加兼容/fallback · 模块化 · 优先官方包与已有实现。

---

## 版本说明

| 项 | 内容 |
| --- | --- |
| 隶属总纲 | `docs/00-overview.md`（顶层设计与跨模块契约。7 实体权威定义、`todo` 表 DDL、mem0 sidecar 契约、LLM 分工均以总纲为准，本文不重复） |
| 技术栈 | **Go 1.26 / GORM（`gorm.io/gorm` + MySQL driver）/ robfig/cron v3（`github.com/robfig/cron/v3`）/ go-playground/validator（`github.com/go-playground/validator/v10`）** |
| 产出物 | **`Todo`（行动线索 / 候选）**——M3 的唯一产出物。M3 **不产出 `Task`**；`Task` 是 M4 把 `Todo` 经确认后固化的明确可执行任务（见总纲 §2.3）。 |
| LLM 用法 | M3 的高频结构化抽取用**可配置 model API**（OpenAI 兼容端点，structured output / JSON schema），Go 侧直接 HTTP 调用。**M4 的确认决策才用 codex CLI，M3 不用 codex。** |
| 本次改写 | 后端由 Python 全面改 Go（Pydantic→Go struct + validator；OpenAI Python SDK→Go HTTP；APScheduler→robfig/cron）；单一 `Task` 实体拆为 `Todo`/`Task`，M3 产出物由 `Task` 改为 **`Todo`**；引入 `Group`（来源会话）与 `Resource`（妙记/文档作方案依据）；mem0 改 Python FastAPI sidecar，Go 经 HTTP `MemoryClient` 调用。 |
| 不引入 | Eino / Kitex（Jarvis 本地单体，抽取直连 model API，见总纲 §6）；不部署 Neo4j 等外部图库（mem0 内建实体链接，见总纲 §5）。 |
| 语言无关、保留自原方案 | `action_type` 分类体系、提取 pipeline 10 步、prompt 工程（system/user 模板 + strict JSON schema）、两层去重（指纹 + 语义 + LLM 裁决）、leader 交办识别、闲聊 vs 行动项区分、防自激励循环——**设计不变，仅落地语言改 Go**。 |

---

## 0. 模块边界与上下游契约

流水线（cron `extract` job，每 10 min 一轮）：

```text
采集 M2 ──► 记忆化 mem0 M2 ──► 【Todo 提取 M3】──► 打分+确认 M4 ──► 执行 M5
   message/group/resource        产出 Todo        Todo→Task 闸门     执行 Task
```

### 0.1 M3 的职责（做什么）

1. 从「本轮新消息」中，按会话/话题聚合上下文，识别**真实、可落地的行动线索**，抽象成 `Todo`。
2. 每个 `Todo` 必须映射到**唯一 `action_type` + 该类型必填 slot**；否则以 `missing_info` 标记信息不足（fail-fast，不臆测），仍产出但显式标注「信息不足」。
3. 重点识别 **leader 交办**（`Person.role='leader'` + `open_id` 命中），高优先级不漏。
4. 去重（指纹 + 语义 + LLM 裁决）与更新（讨论推进时刷新已有 `Todo`）。
5. 检索 mem0 记忆辅助抽取（经 sidecar HTTP）；把「决策 / 交办事实」有节制地回写 mem0。
6. 关联背景实体：来源会话挂 `Group`，方案依据可引用 `Resource`（妙记 / 文档）。

### 0.2 M3 不负责（交给别人）

| 事项 | 归属 |
| --- | --- |
| 消息采集、消息落库、`Group`/`Resource`/`ScanRecord` 沉淀、消息级 mem0 记忆化 | M2 |
| 置信度 / 风险打分、路由决策、`need_info`/`need_decision`/`confirmed`/`dismissed` 状态流转 | M4 |
| **`Todo` → `Task` 固化**（补齐信息、快照 background + plan、生成 `task` 行） | **M4**（唯一转化闸门） |
| 真正执行（改代码 / 发群 / 约会 / 查证） | M5（消费 `Task`） |
| `Project` / `Person` / `Group` / `Message` / `Resource` 实体建模与写入 | M1/M2（M3 只读引用，外键指向） |

> **关键边界（对齐总纲 §2.3）**：M3 **只写 `Todo`**，且 M3 产出的 `Todo` 状态**只有 `extracted`**（带 `missing_info` 标记信息是否足）。`scoring`/`need_info`/`need_decision`/`confirmed`/`dismissed`/`expired` 均由 M4 流转，M3 越权置这些态是设计红线。`Task` 由 M4 生成，M3 完全不碰。

### 0.3 输入 / 输出契约

**输入（M3 读取）**

- `Message`（MySQL 明文支撑表，M2 产出）：本轮新捕获消息。关键字段：`message_id`（`om_` 前缀）、`chat_id`、`root_id`/`thread_id`（话题）、`sender_open_id`、`msg_type`、`content`（明文）、`create_time`（ms）、`parent_id`。
- `Group`（MySQL，一等实体，M2 产出 / M1 关联项目）：来源会话。`Group.id`、`Group.chat_id`、`Group.project_id`（会话→项目关联）、`Group.is_key_group`。M3 用它把 `Todo.group_id` 挂到来源会话，并借 `Group.project_id` 反查项目背景。
- `Project` / `Person`（MySQL，M1 产出）：`Person.role`（含 `leader`）、`Person.open_id`、`Person.priority_weight`；`Project.repos`、`Project.description`、`Project.key_decisions` 等背景。
- `Resource`（MySQL，一等实体，M2 沉淀）：会话涉及的妙记 / 文档 / 文件。M3 抽取时可把 `Resource` 作为**方案依据线索**引用（如 `summary_post` 的 `source_ref` 指向妙记 `Resource.minute_token`）。
- mem0 记忆：Go 侧 `MemoryClient.Search(...)`（HTTP 调 sidecar `POST /memories/search`）检索到的相关决策 / 偏好 / 历史 `Todo` 事实。

**输出（M3 写入）**

- `todo` 表新增/更新行，**`status` 恒为 `extracted`**；信息不足体现在 `missing_info` 字段（非独立状态）。DDL 见总纲 §2.4 `todo`（本文不重复，见 §1）。
- `todo_event` 审计表（M3 产生的迁移，见 §1.3）。
- （可选）回写 mem0 的「决策 / 交办」事实（经 sidecar `POST /memories`）。
- 更新扫描水位 `todo_extract_watermark`（M3 私有，见 §1.2）。

> 依赖顺序：`todo` 表外键指向 `project` / `feishu_group`，迁移需在总纲 §9 M0.1 建表之后。若上游表未就绪，迁移**直接失败**（fail-fast），不做软外键兼容。

---

## 1. Todo 实体建模

### 1.1 权威 DDL 引用（不重复）

`Todo` 的完整 DDL 已在**总纲 `docs/00-overview.md` §2.4 `todo` 表**给出（权威定义，各模块不重复）。M3 只说明与提取强相关的关键字段如何写：

| 字段 | 写入方 | M3 提取语义 |
| --- | --- | --- |
| `title` | M3 | 一句话线索（动宾结构，可落地） |
| `description` | M3 | 展开描述：做什么、为什么、依据 |
| `action_type` | M3 | 行动类型枚举（见 §2），Go 应用层 validator 校验，未知类型直接 fail |
| `slots` | M3 | 该 `action_type` 的结构化参数（JSON，可能不全），Go struct + validator 分型校验（见 §4） |
| `commitment_strength` | M3 | `firm` / `tentative` / `mentioned` |
| `source_message_ids` | M3 | 证据消息 `om_` id 数组（可回溯） |
| `source_quote` | M3 | 逐字证据原文（防幻觉，可回溯） |
| `group_id` | M3 | 外键 → `feishu_group.id`，**来源会话**（新增引用） |
| `project_id` | M3 | 外键 → `project.id`，经 `Group.project_id` 或 `project_hint` 解析 |
| `assigner_open_id` | M3 | 交办人 open_id（冗余，便于 leader 判定） |
| `is_leader_assigned` | M3 | 是否 leader 交办（高优先级信号） |
| `due_at` | M3 | 绝对截止时间（相对时间已解析），无则 NULL |
| `missing_info` | M3 | 必填 slot 缺失时列出缺什么 / 原因（fail-fast 标记，**不猜测填充**） |
| `dedup_fingerprint` | M3 | 稳定身份指纹 sha256（见 §5） |
| `extraction_model` | M3 | 抽取所用 model（可回溯/复现） |
| `prompt_version` | M3 | Prompt 模板版本（可回溯/复现） |
| `revision` | M3 | 更新次数（每次讨论推进 +1） |
| `first_seen_at` / `last_evidence_at` | M3 | 最早 / 最新证据消息时间 |
| `status` | **M3 只写 `extracted`** | 其余态（`scoring`/`need_info`/`need_decision`/`confirmed`/`dismissed`/`expired`）由 M4 流转 |
| `confidence` / `risk` / `route` | **M4** | M3 不写（总纲 `todo` 表已含这些 M4 占位字段） |
| `ttl_at` | **M4** | 待确认过期时间，M3 不写 |
| `version` | M3/M4 | 乐观锁（并发更新用，见 §5.4） |

> 设计取舍（保留）：`action_type` 用 `VARCHAR` + 应用层枚举而非 MySQL `ENUM`，因为要求「可抽象扩展」，`ENUM` 增删值需改表、迁移僵硬。校验放 Go 应用层并 fail-fast（未知类型直接返回 error，不静默落库）。
>
> 与原方案字段差异：原 `task` 表的 `owner_open_id`（执行责任人，默认 principal）、`confidence_score`/`risk_level`/`routing_decision`（M4 占位）、`execution_plan`/`execution_result`（M5 占位）、`embedding_synced` 在拆分后**不再属于 `Todo`**——执行相关字段随 `Task`（M4/M5 拥有）；打分字段总纲 `todo` 用 `confidence`/`risk`/`route`（M4 写）；向量同步标记改为 §5.2 的 Qdrant 同步逻辑（不落 `todo` 表列）。

### 1.2 扫描水位表（M3 私有状态，保证 10min 扫描幂等）

原 `task_extract_watermark` 改名 **`todo_extract_watermark`**，语义不变：

```sql
-- 每个会话维度记录「已提取到哪条消息」，避免重复扫描/漏扫。M3 私有。
CREATE TABLE `todo_extract_watermark` (
  `chat_id`                 VARCHAR(64) NOT NULL,
  `last_scanned_message_id` VARCHAR(64) NOT NULL,
  `last_scanned_at`         DATETIME    NOT NULL,
  `updated_at`              TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
                                          ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`chat_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

> 水位语义：本轮取 `create_time > last_scanned_at` 的消息为「新消息」；另叠加**有界回看窗口**（见 §3.2）提供上下文，但回看消息只作背景、不重复建 `Todo`（靠指纹去重）。
>
> 与 M2 的 `chat_checkpoint` 区分：`chat_checkpoint` 是 **M2 采集**的高水位（"消息拉到哪了"）；`todo_extract_watermark` 是 **M3 提取**的高水位（"消息提取到哪了"）。两者独立，采集快于提取时靠此表兜住不漏提。

### 1.3 状态审计表 `todo_event`（总纲 §2.5 支撑表）

状态机跨 M3/M4，`todo_event` 记录每次状态迁移，便于排障与复盘。**M3 只写自己产生的迁移**（`NULL → extracted`，及更新时的证据追加事件）：

```sql
CREATE TABLE `todo_event` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `todo_id`     BIGINT UNSIGNED NOT NULL,
  `from_status` VARCHAR(32)     NULL,
  `to_status`   VARCHAR(32)     NOT NULL,
  `actor`       VARCHAR(16)     NOT NULL,   -- m3 / m4 / user
  `detail`      JSON            NULL,        -- 如证据追加、revision、missing_info 变化
  `created_at`  TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_todo_event_todo` (`todo_id`),
  CONSTRAINT `fk_todo_event_todo` FOREIGN KEY (`todo_id`)
    REFERENCES `todo` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 1.4 Go 侧模型映射（GORM）

`Todo` 领域模型放 `internal/domain`，`slots` / `source_message_ids` / `missing_info` 用自定义 JSON 类型映射：

```go
package domain

import "time"

// Todo 是 M3 的唯一产出物：从消息提取的行动线索/候选。
// 权威表结构见总纲 §2.4 todo。此处仅列 M3 关心字段。
type Todo struct {
    ID                 uint64      `gorm:"primaryKey"`
    Title              string      `gorm:"column:title"`
    Description        string      `gorm:"column:description"`
    ActionType         string      `gorm:"column:action_type"`
    Slots              Slots       `gorm:"column:slots;type:json"`
    CommitmentStrength string      `gorm:"column:commitment_strength"` // firm|tentative|mentioned
    SourceMessageIDs   StringSlice `gorm:"column:source_message_ids;type:json"`
    SourceQuote        string      `gorm:"column:source_quote"`
    GroupID            *uint64     `gorm:"column:group_id"`  // 来源会话 (Group)
    ProjectID          *uint64     `gorm:"column:project_id"`
    AssignerOpenID     *string     `gorm:"column:assigner_open_id"`
    IsLeaderAssigned   bool        `gorm:"column:is_leader_assigned"`
    DueAt              *time.Time  `gorm:"column:due_at"`

    // M3 只写 extracted；其余状态与 confidence/risk/route/ttl_at 归 M4。
    Status       string      `gorm:"column:status;default:extracted"`
    MissingInfo  StringSlice `gorm:"column:missing_info;type:json"` // 信息不足标记(非独立状态)

    DedupFingerprint string    `gorm:"column:dedup_fingerprint"`
    ExtractionModel  string    `gorm:"column:extraction_model"`
    PromptVersion    string    `gorm:"column:prompt_version"`
    Revision         int       `gorm:"column:revision;default:1"`
    Version          int       `gorm:"column:version;default:0"` // 乐观锁
    FirstSeenAt      time.Time `gorm:"column:first_seen_at"`
    LastEvidenceAt   time.Time `gorm:"column:last_evidence_at"`
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

func (Todo) TableName() string { return "todo" }
```

---

## 2. action_type 分类体系

> 本章为语言无关设计，整体保留自原方案。落地校验从 Pydantic 改 Go struct + validator（见 §4.4）。

### 2.1 抽象模型

一个 `action_type` = **一种 M5 可执行的能力**。抽象成三要素：

- **语义**：这类行动线索是什么。
- **必填 slot（identity + required）**：结构化参数；缺失或歧义 → 标 `missing_info`（fail-fast）。
- **执行映射（M5 hint）**：将来由哪个执行器落地（此处只给方向；`Todo` 阶段不做执行，执行细节归 M5 消费 `Task` 时定）。

> 原则：`action_type` 是**封闭可扩展枚举**，不设「万能兜底」。无法映射到任何类型、或必填 slot 填不出来 → 以 `missing_info` 浮现，绝不硬塞。

### 2.2 核心 4 类（对应用户原始需求）

| action_type | 语义 | 必填 slot | 可选 slot | M5 执行方向 |
| --- | --- | --- | --- | --- |
| `code_change` | 按讨论方案改项目代码 | `repo_ref`、`change_summary` | `based_on`（依据方案/消息/Resource）、`scope`（文件/模块）、`acceptance`（验收标准） | codex exec / IDE agent / 生成 PR |
| `summary_post` | 据妙记/会议总结 todo 到人并发群 | `source_ref`（妙记 `Resource` token / 消息）、`target_chat_id`、`summary_scope` | `assignees`（todo 到人 open_id 列表）、`format` | lark-cli 发消息/卡片 |
| `investigate` | 查项目代码或网上信息，把不明确问题弄明确 | `question`、`lookup_sources`（`code`/`web`/`docs`/`people`） | `context`、`deliverable`（结论形式） | rg + codex `-s read-only` + web |
| `schedule_meeting` | 约下一次会议 | `meeting_title`、`attendees`（open_id 列表）、`proposed_time` | `duration_minutes`、`agenda`、`meeting_room` | lark-cli calendar |

### 2.3 扩展类（自然延伸，同框架）

| action_type | 语义 | 必填 slot | M5 执行方向 |
| --- | --- | --- | --- |
| `reply_message` | 回复/通知某人某群一段明确内容 | `target_chat_id`、`message_body` | lark-cli 发消息 |
| `doc_write` | 新建/更新一篇文档 | `doc_title`、`summary_scope` | lark-cli / 云文档 |
| `manual_followup` | 真实、明确、可验证，但无自动执行器 → 仅提醒 principal 手动做 | `followup_action`（具体动宾，可验证） | M5 仅生成提醒 |

> `manual_followup` 有沦为「垃圾桶」的风险，**必须**是具体、可验证的动宾（如「在 PR #123 上评审 X 并留意 Y」），不能是「看看 X」这类模糊表述；否则退化为 `missing_info`。此风险列入开放问题（§8）。

### 2.4 slot 超集与「身份 slot」

抽取阶段用**扁平 slot 超集**（所有 key 都存在、按需为 `null`），落库前按 `action_type` 做 Go struct + validator 分型校验（见 §4.4）。选扁平超集而非 `oneOf`/`anyOf` 判别联合，是为了**跨 model provider 可移植**（strict 模式对判别联合支持不一，栈里 model API 可配置）。

「身份 slot」用于计算去重指纹（§5），是必填 slot 中**不随讨论变动**的那部分：

| action_type | 身份 slot（进指纹） |
| --- | --- |
| `code_change` | `repo_ref` + `change_summary` |
| `summary_post` | `source_ref` + `target_chat_id` |
| `investigate` | `question` |
| `schedule_meeting` | `meeting_title` + `sorted(attendees)`（**不含** `proposed_time`，改期视为同一线索更新） |
| `reply_message` | `target_chat_id` + `message_body` 归一 |
| `doc_write` | `doc_title` |
| `manual_followup` | `followup_action` |

### 2.5 用户 4 个原始例子的映射示范

| 原始诉求 | action_type | 关键 slot |
| --- | --- | --- |
| XXX 项目按讨论 XX 方案做修改 | `code_change` | `repo_ref=XXX`、`change_summary=…`、`based_on=方案消息/记忆` |
| 据上次会议妙记总结 todo 到人发群 | `summary_post` | `source_ref=妙记 Resource token`、`target_chat_id=…`、`assignees=[…]` |
| 会上不明确的，查代码/网上弄明确 | `investigate` | `question=…`、`lookup_sources=[code,web]` |
| 用 lark-cli 约下一次会议 | `schedule_meeting` | `meeting_title=…`、`attendees=[…]`、`proposed_time=…` |

---

## 3. 提取 Pipeline

### 3.1 流程图（文字）

```text
                    ┌──────────────────────────────────────────────┐
                    │  robfig/cron v3 触发 extract job（每 10 min）  │
                    └───────────────────────┬──────────────────────┘
                                            │
   ① 取新消息  ──────────────────────────────▼───────────────────────────
      读 todo_extract_watermark，按 chat_id 取 create_time > 水位 的新消息
                                            │
   ② 会话/话题聚合  ────────────────────────▼───────────────────────────
      按 (chat_id, root_id/thread_id) 分组为「会话单元」，关联 Group；
      单元内按 create_time 排序；叠加有界回看窗口（背景，不重复建 Todo）
                                            │
   ③ 背景富化  ──────────────────────────────▼───────────────────────────
      参与者 open_id → Person(role, is_leader)；Group.project_id / 提及 → Project；
      会话涉及的妙记/文档 → Resource（作方案依据线索）
                                            │
   ④ mem0 检索  ────────────────────────────▼───────────────────────────
      MemoryClient.Search(会话要点, filters={metadata 标量等值})  ← HTTP 调 sidecar
      → 相关决策/方案/偏好/历史 Todo 事实（仅作背景）
                                            │
   ⑤ leader 交办标注  ──────────────────────▼───────────────────────────
      sender_open_id ∈ leader open_id 集合 → 标记；@principal / leader 私聊 → 定向
                                            │
   ⑥ LLM 结构化抽取  ────────────────────────▼───────────────────────────
      每个会话单元一次 model API structured-output 调用（Go HTTP，一单元一 schema）
      输入：会话 + 参与者/角色 + 项目/会话背景 + Resource + 记忆 + 当前时间 + 已开放 Todo 摘要
      输出：candidates[]（action_type/slots/证据/commitment/info_sufficient…）
                                            │
   ⑦ 校验 (Go struct + validator)  ─────────▼───────────────────────────
      schema 反序列化 + 分型 slot 必填校验；
      解析/调用失败 → 返回 error 暴露（不编造）；必填 slot 缺 → info_sufficient=false
                                            │
   ⑧ 去重 & 更新  ──────────────────────────▼───────────────────────────
      指纹精确匹配 + Qdrant 语义近邻 + LLM 裁决；
      新建 / 更新已有（补 slot、追加证据、revision+1）
                                            │
   ⑨ 落库 & 回写  ──────────────────────────▼───────────────────────────
      写/更新 todo 行（status 恒 extracted，信息不足记 missing_info）；
      写 todo_event；有节制回写 mem0（HTTP sidecar）；推进 watermark；同步 Qdrant 向量
                                            │
   ⑩ 交接 M4  ──────────────────────────────▼───────────────────────────
      status=extracted 的 Todo 交 M4 打分 + Todo→Task 确认闸门
```

### 3.2 会话/话题聚合细节

- **分组键**：飞书群话题用 `(chat_id, root_id)`；无话题的普通群/单聊用 `chat_id`。一个分组 = 一个「会话单元」，对应一个 `Group`（`Todo.group_id` 挂此）。
- **回看窗口**：每个「有新消息」的会话单元，向前补 `min(N 条, T 分钟)` 历史消息（默认 N=20 / T=120min，受 token 预算裁剪）作为上下文连续性。回看消息在 prompt 中明确标注 `[context]` vs `[new]`；**只有触及 `[new]` 证据的行动线索**才产出，纯回看不重复建（叠加指纹去重双保险）。
- **token 预算**：单元过大时按时间切片多次调用，各片抽取结果按指纹/语义合并（见 §5）。切片边界与合并策略列入开放问题（§8）。

### 3.3 背景富化：Group / Project / Resource 的使用（新增）

- **Group（来源会话）**：会话单元直接对应一个 `Group` 行。用 `Group.project_id` 反查项目背景（会话已关联项目时无需再靠 LLM 猜 `project_hint`）；`Group.is_key_group=1`（leader/核心项目群）的会话，抽取更谨慎、疑似线索宁可浮现不丢弃。落库时 `Todo.group_id = Group.id`。
- **Project 背景**：优先 `Group.project_id → Project`；会话未关联项目时，LLM 输出 `project_hint`，Go 侧再按显式映射表/名称解析成 `project_id`（解析不出则留 NULL，交 M4，见开放问题 §8）。
- **Resource（方案依据线索）**：会话中出现的妙记 / 文档 / 文件由 M2 已沉淀为 `resource` 行（仅元数据）。M3 抽取时把相关 `Resource` 注入 prompt（`resource_type`、`minute_token`/`doc_token`、`name`、以及可选的 `extracted_text`），让 LLM 能把 `summary_post` 的 `source_ref` 指向具体妙记 token，或让 `code_change` 的 `based_on` 引用某份设计文档。
  - **妙记按需拉取（已定，总纲 §11.4）**：当 M3 判断某条线索强依赖妙记内容（典型：`summary_post` "据上次会议妙记总结 todo 到人"），且该 `Resource.resource_type=minutes` 尚未解析（`extracted_text` 空）时，M3 调 **M2 的 `ResourceFetcher.EnsureMinutesText(resID)`**（M2 §3.9.1）按需拉逐字稿，拿回 `extracted_text` 注入 prompt 提升抽取质量。
  - **其它类型不解析**：图片/飞书文档/附件本期不下载不解析，M3 只能引用其 `token`/`name` 作弱依据（`extracted_text` 恒空）。此约束会限制这些类型的 `source_ref`/`based_on` 精度，属本期已知取舍。
  - M3 不自己下载二进制、不做 OCR；一切内容获取都走 M2 的 `ResourceFetcher`（仅妙记）。

### 3.4 leader 交办识别（保留）

1. 预取 `Person WHERE role='leader'` 的 `open_id` 集合。
2. 会话单元内，`sender_open_id ∈ leader 集合` 的消息，标注为 leader 发言；prompt 中显式告知「以下消息来自 leader（open_id=…），其交办为高优先级」。
3. 命中后产出的 `Todo`：`is_leader_assigned=1`、`assigner_open_id=leader open_id`。
4. **兜底原则**：leader 来源的疑似行动线索，即使 `commitment_strength=tentative`，也**至少产出（带 `missing_info` 标注信息不足）**交 M4/用户确认，绝不静默丢弃（重点不能漏）。

### 3.5 闲聊 vs 真实行动线索 的区分（写进 prompt 约束）

| 判据 | 处理 |
| --- | --- |
| 社交寒暄 / 纯情绪 / 无动作讨论 | 丢弃，不产出 |
| 软建议（"要不要…""也许可以…"，`commitment_strength=tentative`） | 非 leader：产出 `Todo` 但标 `missing_info`（needs-confirm，交 M4）；leader：同 §3.4 兜底 |
| 明确承诺/交办（"我来做…""你去把…""@某人 负责…"，`firm`） | 产出候选 `Todo` |
| 已明确、但必填 slot 缺失/歧义 | 产出 `Todo` 但 `info_sufficient=false` → 记 `missing_info` |
| 具体、可落地、slot 齐全 | 产出 `Todo`（`missing_info` 为空） |

> 借鉴会议纪要抽取成熟做法：把 LLM 当**确定性抽取器**而非「总结器」；软陈述进「模糊桶」（`missing_info` 标记）交 M4/人工判断，而不是硬塞成明确任务；每条 `Todo` 必带**逐字证据**可回溯。所有「是否真任务 / 是否确认」的判定归 M4，M3 只负责把线索**如实浮现**。

---

## 4. Prompt 工程

采用 **native structured output + strict JSON schema**（2026 生产默认；legacy JSON mode 只保证语法不保证结构，弃用）。model API 保证结构后，落库前再用 **Go struct + go-playground/validator 二次校验**（业务规则仍需应用层兜住）。

### 4.1 System Prompt（模板）

```text
你是「个人 Jarvis 管家」的行动线索抽取器，服务对象（principal）是研发工程师 chujiejie.1（open_id={{principal_open_id}}）。
你的唯一任务：从给定飞书会话中，抽取 principal 需要执行、或其助手可代其执行的【真实、可落地的行动线索】，输出严格符合给定 JSON schema 的结构化数据。这些线索是「候选」，稍后由下游模块确认，你不做最终裁决。

【必须遵守】
1. 只抽取「真实承诺 / 明确交办 / 明确的行动倾向」。忽略寒暄、情绪、无动作的纯讨论。不要做总结，只做抽取。
2. leader 交办为高优先级：凡标注为 leader（见输入 participants.is_leader）发出的行动线索，必须输出，即使措辞较软也要输出（此时 commitment_strength 如实标 tentative，且 info_sufficient 按规则判定）。
3. 每条线索必须映射到唯一 action_type（枚举见下），并从【明确证据】中填满该类型的必填 slot。
   - 禁止编造：参会人、时间、仓库名、目标群、妙记 token 等 slot 只能来自会话/背景/Resource 明确出现的信息。
   - 必填 slot 缺失或有歧义时：info_sufficient=false，并在 missing_info 里列出缺了什么，绝不猜测填充。
4. 每条线索必须给出 source_message_ids（证据消息 id）与 source_quote（逐字原文片段，可回溯）。
5. 相对时间（如「下周五」「月底」）必须用 current_datetime={{current_datetime}}（时区 {{tz}}）解析成绝对 ISO-8601 日期；无明确时间则留 null。
6. commitment_strength：firm=明确承诺/交办；tentative=软建议待确认；mentioned=仅提及无归属。
7. 同一件事在多条消息重复出现，只输出一条（合并证据），不要重复。
8. 若整段会话无任何可落地行动线索，candidates 返回空数组。
9. 可引用背景中的 Resource（妙记/文档）作为 source_ref / based_on 的依据，但只能引用输入中明确给出的 Resource 标识。

【action_type 枚举与必填 slot】
- code_change：改代码。必填 repo_ref, change_summary。
- summary_post：总结 todo 发群。必填 source_ref, target_chat_id, summary_scope。
- investigate：查证澄清。必填 question, lookup_sources。
- schedule_meeting：约会议。必填 meeting_title, attendees, proposed_time。
- reply_message：回复/通知明确内容。必填 target_chat_id, message_body。
- doc_write：写/更新文档。必填 doc_title, summary_scope。
- manual_followup：真实明确但无自动执行器，仅提醒。必填 followup_action（须具体可验证；模糊则改判 info_sufficient=false）。

只输出 JSON，不要输出任何解释性文字。
```

### 4.2 User Prompt（模板）

```text
# 当前时间
{{current_datetime}}（时区 {{tz}}）

# 项目背景
{{project_background}}   # 例：project=XXX, repos=[...], 当前重点=...（经 Group.project_id 解析）

# 来源会话（Group）
{{group_block}}          # chat_id=... name=... is_key_group=true/false project=...

# 参与者（open_id / 姓名 / 角色 / 是否 leader）
{{participants_block}}   # 逐行：open_id=... name=... role=... is_leader=true/false

# 相关资源（Resource：妙记/文档/文件，可作方案依据）
{{resources_block}}      # 逐条：type=minutes/doc/... token=... name=... [摘要=...]

# 相关记忆（mem0 检索，仅作背景，勿直接当作新行动线索）
{{retrieved_memories}}   # 逐条：memory=... (metadata)

# 已存在的未闭环 Todo（用于判断是「新建」还是「更新已有」）
{{open_todos_digest}}    # 逐条：todo_id=... action_type=... title=... status=...

# 会话记录（按时间序；[new]=本轮新消息，[context]=回看背景）
{{conversation_transcript}}
# 每行：[new|context] msg_id=... time=... sender_open_id=...(is_leader) : 明文内容
```

### 4.3 输出 JSON Schema（strict 模式，保留）

要点：所有字段进 `required`；可选值用 `["type","null"]` 表达；所有对象 `additionalProperties:false`；`slots` 用扁平超集（跨 provider 可移植）。此 schema 与语言无关，Go 侧作为 `response_format.json_schema` 直接下发。

```json
{
  "name": "todo_extraction",
  "strict": true,
  "schema": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "candidates": {
        "type": "array",
        "items": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "action_type": {
              "type": "string",
              "enum": ["code_change", "summary_post", "investigate",
                       "schedule_meeting", "reply_message", "doc_write",
                       "manual_followup"]
            },
            "title": { "type": "string" },
            "description": { "type": "string" },
            "commitment_strength": {
              "type": "string",
              "enum": ["firm", "tentative", "mentioned"]
            },
            "assigner_open_id": { "type": ["string", "null"] },
            "project_hint": { "type": ["string", "null"] },
            "due_date": {
              "type": ["string", "null"],
              "description": "ISO-8601，如 2026-07-25；无则 null"
            },
            "source_message_ids": {
              "type": "array",
              "items": { "type": "string" }
            },
            "source_quote": { "type": "string" },
            "slots": {
              "type": "object",
              "additionalProperties": false,
              "properties": {
                "repo_ref":       { "type": ["string", "null"] },
                "change_summary": { "type": ["string", "null"] },
                "based_on":       { "type": ["string", "null"] },
                "scope":          { "type": ["string", "null"] },
                "acceptance":     { "type": ["string", "null"] },
                "source_ref":     { "type": ["string", "null"] },
                "target_chat_id": { "type": ["string", "null"] },
                "summary_scope":  { "type": ["string", "null"] },
                "assignees":      { "type": ["array", "null"], "items": { "type": "string" } },
                "question":       { "type": ["string", "null"] },
                "lookup_sources": { "type": ["array", "null"], "items": { "type": "string" } },
                "deliverable":    { "type": ["string", "null"] },
                "meeting_title":  { "type": ["string", "null"] },
                "attendees":      { "type": ["array", "null"], "items": { "type": "string" } },
                "proposed_time":  { "type": ["string", "null"] },
                "duration_minutes": { "type": ["integer", "null"] },
                "agenda":         { "type": ["string", "null"] },
                "meeting_room":   { "type": ["string", "null"] },
                "message_body":   { "type": ["string", "null"] },
                "doc_title":      { "type": ["string", "null"] },
                "followup_action":{ "type": ["string", "null"] }
              },
              "required": ["repo_ref","change_summary","based_on","scope","acceptance",
                           "source_ref","target_chat_id","summary_scope","assignees",
                           "question","lookup_sources","deliverable","meeting_title",
                           "attendees","proposed_time","duration_minutes","agenda",
                           "meeting_room","message_body","doc_title","followup_action"]
            },
            "info_sufficient": { "type": "boolean" },
            "missing_info": { "type": "array", "items": { "type": "string" } }
          },
          "required": ["action_type","title","description","commitment_strength",
                       "assigner_open_id","project_hint","due_date",
                       "source_message_ids","source_quote","slots",
                       "info_sufficient","missing_info"]
        }
      }
    },
    "required": ["candidates"]
  }
}
```

> 与原 schema 差异：删除 `owner_open_id`（执行责任人属 `Task`/M5，`Todo` 不建模）；`task_extraction` 重命名为 `todo_extraction`。其余字段保留。

### 4.4 调用与 Go 校验（model API + validator）

Go 侧直接 HTTP 调 model API（OpenAI 兼容 `POST /chat/completions`，`response_format=json_schema, strict=true`），**不经 Eino、不用 codex**。落库前统一在 Go 层用 struct + `go-playground/validator` 做业务规则校验（分型必填 slot），provider 差异隔离在 `internal/extract/provider` 一层适配里。

```go
package extract

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/go-playground/validator/v10"
)

// 分型必填 slot（与 §2.2/§2.3 一致，单一事实来源）。
var requiredSlots = map[string][]string{
    "code_change":      {"repo_ref", "change_summary"},
    "summary_post":     {"source_ref", "target_chat_id", "summary_scope"},
    "investigate":      {"question", "lookup_sources"},
    "schedule_meeting": {"meeting_title", "attendees", "proposed_time"},
    "reply_message":    {"target_chat_id", "message_body"},
    "doc_write":        {"doc_title", "summary_scope"},
    "manual_followup":  {"followup_action"},
}

// Candidate 对应 schema 的单条候选；validator tag 做结构层校验，
// 分型 slot 必填在 EnforceRequiredSlots 里做（依赖 action_type 动态判定）。
type Candidate struct {
    ActionType         string         `json:"action_type"          validate:"required,oneof=code_change summary_post investigate schedule_meeting reply_message doc_write manual_followup"`
    Title              string         `json:"title"                validate:"required"`
    Description        string         `json:"description"          validate:"required"`
    CommitmentStrength string         `json:"commitment_strength"  validate:"required,oneof=firm tentative mentioned"`
    AssignerOpenID     *string        `json:"assigner_open_id"`
    ProjectHint        *string        `json:"project_hint"`
    DueDate            *string        `json:"due_date"`
    SourceMessageIDs   []string       `json:"source_message_ids"   validate:"required,min=1"`
    SourceQuote        string         `json:"source_quote"         validate:"required"`
    Slots              map[string]any `json:"slots"                validate:"required"`
    InfoSufficient     bool           `json:"info_sufficient"`
    MissingInfo        []string       `json:"missing_info"`
}

type ExtractionResult struct {
    Candidates []Candidate `json:"candidates" validate:"dive"`
}

// EnforceRequiredSlots：fail-fast 未知 action_type 直接 error；
// 分型必填 slot 缺失 → 不 error，但明确降级 info_sufficient=false 并补 missing_info（不猜测填充）。
func (c *Candidate) EnforceRequiredSlots() error {
    req, ok := requiredSlots[c.ActionType]
    if !ok {
        return fmt.Errorf("unknown action_type: %s", c.ActionType) // fail-fast，不静默落库
    }
    missing := make([]string, 0)
    for _, s := range req {
        v, present := c.Slots[s]
        if !present || isEmpty(v) {
            missing = append(missing, s)
        }
    }
    if len(missing) > 0 {
        c.InfoSufficient = false
        c.MissingInfo = dedupSorted(append(c.MissingInfo, missing...))
    }
    return nil
}

// Extract：调 model API 结构化抽取；provider 由配置注入（OpenAI 兼容 HTTP）。
func Extract(ctx context.Context, cli ModelClient, req ChatRequest) (*ExtractionResult, error) {
    resp, err := cli.CreateChatCompletion(ctx, req) // Go HTTP，非 Eino/非 codex
    if err != nil {
        return nil, fmt.Errorf("model api call failed: %w", err) // fail-fast，不 fallback
    }
    // fail-fast：provider 拒答一律暴露，绝不编造或跳过
    if resp.Refusal != "" {
        return nil, fmt.Errorf("model refused extraction: %s", resp.Refusal)
    }
    var out ExtractionResult
    // fail-fast：JSON 解析失败直接 error，不 fallback 到正则/手搓解析
    if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
        return nil, fmt.Errorf("unmarshal extraction result: %w", err)
    }
    v := validator.New(validator.WithRequiredStructEnabled())
    if err := v.Struct(&out); err != nil {
        return nil, fmt.Errorf("schema validation failed: %w", err) // 结构层 fail-fast
    }
    for i := range out.Candidates {
        if err := out.Candidates[i].EnforceRequiredSlots(); err != nil {
            return nil, err // 未知 action_type → fail-fast
        }
    }
    return &out, nil
}
```

> **fail-fast 落点**：① provider 返回 `refusal` → 返回 error；② JSON 解析 / schema 校验失败 → 返回 error（不吞、不 fallback 到正则解析）；③ 未知 `action_type` → 返回 error；④ 必填 slot 缺失 → 不 error 但**明确降级 `info_sufficient=false`** 并记 `missing_info`（落库 `Todo.missing_info`），交 M4/用户，而非静默猜测。单测须对以上四类各写用例，断言「暴露 error / 降级」行为，**禁止 mock 掉真实校验**来让测试变绿。
>
> **版本锁定**：mem0 sidecar 契约、model API schema、prompt 版本均**锁定**；跨版本接口变化直接 fail（不做跨版本 fallback，如需兼容【需与用户确认】）。`prompt_version` / `extraction_model` 落库以支持复现。

---

## 5. 去重与更新

同一 `Todo` 会在多条消息、多轮 10min 扫描里反复出现。采用**两层去重 + LLM 裁决 + 更新推进**（语言无关，保留自原方案）。

### 5.1 第一层：精确指纹（确定性，快）

- `dedup_fingerprint = sha256(canonical_json({action_type, project_id, identity_slots}))`。
- `identity_slots` 见 §2.4（只取稳定身份 slot，**不含**会变动的 time/agenda 等，保证补 slot/改期时指纹不变、命中同一行）。
- 归一化：`identity_slots` 值做 `NFKC + casefold + 压缩空白`（Go 侧用 `golang.org/x/text/unicode/norm` + `strings.ToLower` + 空白折叠），保证轻微措辞差异仍同指纹。
- DB 层 `uk_todo_fingerprint` 唯一键兜底（见总纲 `todo` 表）：并发/重扫下重复插入直接失败（fail-fast），转走「命中已有 → 更新」分支。

> 与原方案差异：指纹不再含 `owner_open_id`（`Todo` 无此字段），改为 `project_id` 领域隔离即可。

### 5.2 第二层：语义近邻（抓改写/换词）

- 每个落库 `Todo`，用配置的 embedding 模型（**复用 mem0 同一 embedder**，保证向量空间一致）对 `"{action_type}｜{title}｜{description}｜identity_slots"` 向量化，写入 **Qdrant 专用集合 `todo_semantic`**（与 mem0 集合分离，cosine 距离）。向量化经 mem0 sidecar 或独立 embedding 端点，Go 侧 HTTP 调用。
- 新候选先在 `todo_semantic` 里 `score_threshold` 检索，`filter` 限定 `project_id` + `status ∈ 活跃态` + `action_type`（**领域过滤防跨项目/跨类型串味**）。
- 阈值起点 **0.85**（cosine），必须在**真实数据上标定**——不同 embedding 模型甜点差异大（业界经验 0.80–0.92 之间浮动，>0.95 过严、<0.75 易误合）。阈值与模型写进配置，列入开放问题（§8）。

> 向量同步状态不落 `todo` 表列（原 `embedding_synced` 已移除）；同步失败按 §7 fail-fast 处理（抛错中断本轮，不写半截）。

### 5.3 LLM 裁决（消除向量误报）

语义命中 ≥ 阈值只算「疑似重复」。发一次**小型判定调用**（model API，strict 布尔输出）确认「是否同一行动线索」，再决定合并——向量相似不等于语义等价，关键路径用 LLM 复核候选对（成熟做法）。裁决同样 fail-fast：调用失败返回 error，不默认合并或默认新建。**注意此裁决仍用 model API，不用 codex（codex 只在 M4）。**

### 5.4 更新与推进逻辑

命中已有 `Todo`（指纹或语义+裁决确认同一件事）时，按新证据更新而非新建。更新用 `version` 乐观锁（`WHERE id=? AND version=?`），并发冲突则重试：

| 讨论进展 | 更新动作 |
| --- | --- |
| 补齐了原缺失 slot（如定了参会人/时间/方案） | 填 `slots`；若必填齐全，清空 `missing_info`（状态仍是 `extracted`，是否可执行交 M4 判） |
| 新增讨论/证据 | 追加 `source_message_ids`、刷新 `source_quote`、`last_evidence_at`、`revision+1` |
| 明确「已完成/取消」 | **不由 M3 置终态**；记信号入 `todo_event.detail` 交 M4 判定（避免 M3 越权） |
| 方案/目标发生实质变化，已非同一件事 | 视为新 `Todo` 新建，旧 `Todo` 记 `superseded` 信号入 `todo_event` 交 M4 |

> 边界：M3 只负责「抽取 + 去重 + 更新事实」，**不做状态终结裁决**（`confirmed`/`dismissed`/`expired` 归 M4；执行态归 M5 的 `Task`）。M3 产出的 `Todo` 始终停在 `extracted`。每次更新写 `todo_event`。

---

## 6. 与 mem0 的读写（经 Python sidecar）

mem0 无 Go SDK，全系统统一采用 **Python FastAPI sidecar**（`127.0.0.1:18900`，见总纲 §5）。M3 的读写全部走 Go 侧 `MemoryClient`（HTTP 调 sidecar），**不直连 mem0**。统一约定（总纲 §5.1）：`user_id` 固定 `OWNER_ID`（默认 `"owner"`）；scope 只用 `user_id`；**metadata 过滤以标量等值为基线**。

```go
package memory

import "context"

// MemoryClient：Go 侧薄 HTTP 客户端，调 mem0 sidecar（127.0.0.1:18900）。
// POST /memories/search 对应 mem0.search；POST /memories 对应 mem0.add。
type MemoryClient struct {
    baseURL string // http://127.0.0.1:18900
    http    HTTPDoer
}

type SearchRequest struct {
    Query     string            `json:"query"`
    UserID    string            `json:"user_id"`           // 固定 OWNER_ID
    Filters   map[string]string `json:"filters,omitempty"` // 标量等值基线
    TopK      int               `json:"top_k"`
    Threshold float64           `json:"threshold"`
}

func (c *MemoryClient) Search(ctx context.Context, req SearchRequest) ([]Memory, error) { /* POST /memories/search */ }

type AddRequest struct {
    Messages []Message         `json:"messages"`
    UserID   string            `json:"user_id"`   // 固定 OWNER_ID
    Metadata map[string]string `json:"metadata"`  // 标量等值，便于检索过滤
    Infer    bool              `json:"infer"`
}

func (c *MemoryClient) Add(ctx context.Context, req AddRequest) (eventID string, err error) { /* POST /memories */ }
```

### 6.1 抽取时读（检索哪些记忆）

在 pipeline ④ 步，用会话要点做检索，为「按讨论方案」「上次会议」等模糊指代补上真实内容，让 `Todo` 更可落地。**过滤以 metadata 标量等值为基线**——原方案的复杂 `AND/OR/ne` 过滤改为标量等值；跨字段布尔组合**需实测 Qdrant 后端支持后才用**（见总纲开放问题 #5、本文 §8）：

```go
// 基线：标量等值过滤（Qdrant 后端稳定支持）。
mems, err := mem.Search(ctx, memory.SearchRequest{
    Query:  conversationSalientText,     // 会话要点（标题/首尾消息拼接）
    UserID: cfg.OwnerID,                 // 固定 OWNER_ID
    Filters: map[string]string{
        "project": projectKey,           // 命中当前项目（标量等值）
        // "source": "decision",         // 如需只取决策类，单条标量等值
    },
    TopK:      8,
    Threshold: 0.5,                       // 显式设阈值，避免默认漂移
})
if err != nil {
    return nil, err // fail-fast，不静默降级
}
```

> **防自激励循环的过滤**：原方案靠 `mem_type != "task_fact"`（`ne` 算子）排除 M3 自己回写的事实。标量等值基线下，改为**按 `source` 正向选取**（如只取 `source="decision"` 或 `source="background"`，天然排除 M3 回写的 `source="m3"` 事实），或在 Go 侧对返回结果做后过滤（丢弃 `metadata.source=="m3"` 的项）。`ne`/复杂组合过滤**需实测 Qdrant 支持**后再启用。

检索目标：① 项目历史**决策/方案**（解析「按 XX 方案改」）；② 上次会议/妙记结论（配合 `Resource` 定位 `summary_post` 的 `source_ref`）；③ 人员分工/偏好（`assignees`）；④ 历史相关 `Todo` 事实（辅助去重判断）。检索结果在 prompt 里明确标为「背景」，禁止直接当新行动线索（见 §4.1 规则 & §6.3 防循环）。

### 6.2 抽取后写（是否回写、写什么）

**有节制回写**——mem0 存「耐久事实」，`todo` 表才是 `Todo` 的 source of truth，不重复整表。回写经 sidecar `POST /memories`，`user_id` 固定 `OWNER_ID`，metadata 标量等值。回写仅限两类：

```go
// 1) 决策/交办事实（帮未来轮次解析模糊指代）
_, err = mem.Add(ctx, memory.AddRequest{
    Messages: []memory.Message{{
        Role:    "user",
        Content: "leader 张三(open_id=ou_xxx) 交办 owner：projectX 按方案A重构鉴权模块",
    }},
    UserID:   cfg.OwnerID,
    Metadata: map[string]string{"source": "decision", "project": "projectX", "todo_id": "1024"},
    Infer:    true,
})

// 2) Todo 生命周期里程碑（创建/重大更新），便于跨轮上下文
_, err = mem.Add(ctx, memory.AddRequest{
    Messages: []memory.Message{{
        Role:    "assistant",
        Content: "已登记线索#1024：projectX 鉴权重构（code_change, leader 交办）",
    }},
    UserID:   cfg.OwnerID,
    Metadata: map[string]string{"source": "m3", "kind": "todo_fact", "todo_id": "1024"},
    Infer:    false,
})
```

**不回写**：原始消息（M2 已记忆化）、完整 `Todo` 对象（在 MySQL）、`missing_info` 标记的臆测内容。

> metadata 用 `source`（`decision`/`m3`/`background`）+ `kind` 等**标量字符串**，便于 §6.1 标量等值过滤与防循环后过滤。`todo_id` 用字符串标量（sidecar 侧不做数值比较）。

### 6.3 防「自激励循环」（fail-fast 边界，保留）

M3 回写的记忆若被下轮检索回来、又被当成新行动线索，会自我增殖。三重防护：

1. **元数据隔离**：M3 写入统一带 `source="m3"`；§6.1 检索用**正向标量等值选取**（只取 `source="decision"/"background"`）或 Go 侧后过滤丢弃 `source="m3"` 的项——不依赖 `ne` 算子（标量等值基线）。
2. **角色标注**：记忆在 prompt 里明确归入「背景」，system prompt 规定「记忆仅作背景，勿当新行动线索」。
3. **去重兜底**：即便漏网，§5 指纹 + 语义去重会拦下重复 `Todo`。

> 边界须在单测覆盖：构造「M3 写入的 `source=m3` 记忆被检索」场景，断言**不产生**新 `Todo`（fail-fast 地暴露循环，而非靠运气）。mem0 `add` 经 sidecar 返回 `event_id`（2026 版 `add` 为异步）；如需确认落库须轮询 event。此同步点列入开放问题（§8）。

---

## 7. 状态机与错误处理

### 7.1 Todo 状态机（跨 M3/M4，M3 只产出 `extracted`）

对齐总纲 `todo.status` 枚举 `extracted | scoring | need_info | need_decision | confirmed | dismissed | expired`：

```text
        ┌──────── M3 产出（唯一状态）────────┐
        │                                    │
    [extracted]  （missing_info 为空=slot 齐全；非空=信息不足，仍是 extracted）
        │
        └───────────► M4 打分 ──────────────┐
                          │                  │
             ┌────────────┼──────────┬───────┴────────┐
             ▼            ▼          ▼                ▼
        [scoring]   [need_info]  [need_decision]  [dismissed]
                          │          │                
                          ▼          ▼   （用户/自动确认）
                     补信息回流   → [confirmed] ──► 生成 Task（M4 固化 background+plan）
                                                        │
                                                        ▼  M5 执行 Task
                                              task.status: pending→executing→done/failed
        （超时未确认 → [expired]，M4 管理）

 去重旁路（M3）：命中已有 → 更新已有行；重复插入靠 uk_todo_fingerprint 兜底
 取代旁路（M3）：实质变化 → 新建 + 旧行记 superseded 信号入 todo_event 交 M4
```

- **M3 写入的合法状态**：**仅 `extracted`**。信息不足体现在 `missing_info` 字段，**不是**独立状态（总纲无 `insufficient_info` 态；语义上的「信息不足」由 M4 依 `missing_info` 决定走 `need_info`）。
- `scoring` / `need_info` / `need_decision` / `confirmed` / `dismissed` / `expired` 均归 **M4**；`Task` 的 `pending`/`executing`/`done`/`failed` 归 **M5**。M3 越权置这些态是设计红线。

> 与原方案差异：原文档 M3 产出 `extracted` / `insufficient_info` / `duplicate` 三态。对齐总纲后：`insufficient_info` → 用 `extracted` + `missing_info` 标记表达；`duplicate` → 不落独立态（命中即更新已有行或被唯一键拦下）；语义「信息不足/重复」的后续处置全部上移 M4。

### 7.2 错误处理与 fail-fast 汇总

| 场景 | 处理（绝不静默） |
| --- | --- |
| model API 拒答（refusal） | 返回 error 暴露，记录该会话单元，不产出臆测 `Todo` |
| JSON 解析/schema 校验失败 | 返回 error；**不** fallback 到正则/手搓解析 |
| 未知 `action_type` | validator/EnforceRequiredSlots 返回 error，不落库 |
| 必填 slot 缺失/歧义 | 降级 `info_sufficient=false` + 记 `missing_info`（落 `Todo.missing_info`），交 M4；不猜测填充 |
| 相对时间无法解析 | `due_at` 留 NULL；`schedule_meeting` 记 `proposed_time` 缺失 → `missing_info` |
| leader 交办但措辞软 | 至少产出（带 `missing_info` 标注），绝不丢弃 |
| 语义去重 LLM 裁决失败 | 返回 error，不默认合并也不默认新建 |
| mem0 sidecar / DB / Qdrant / model API 连接失败 | 返回 error 中断本轮，等依赖恢复；不写半截数据 |

> 单测要求：上述每类各写用例，断言「返回 error」或「降级为 `info_sufficient=false` + `missing_info`」的确定性行为；**禁止 mock 掉真实校验**来「让测试变绿」。Go 侧用 `errors.Is`/`errors.As` 断言错误类型。

---

## 8. 开放问题清单（需与用户确认）

> 已定项（不再列）：后端语言（**Go，已定**）、是否部署 Neo4j（**否，mem0 内建实体链接**）、Web 框架/ORM/调度（总纲 §1 已定）、LLM 分工（M3 用 model API、M4 用 codex，已定）。

1. **`Todo` → `Task` 契约细化**：M3 产出 `extracted` 的 `Todo` 后，M4 依 `missing_info` 判 `need_info`、依打分判 `confirmed` 并固化 `Task`。M3 需要向 M4 显式传哪些**信息回流锚点**（如 `source_message_ids`、`group_id`、引用的 `Resource` token），以便 M4 补齐信息 / 快照 background？M4 补信息后是否回喂 M3 重抽取（M4 文档 §0.1 提到「重新提取请求」），回喂接口的入参口径需与 M4 对齐。
2. **`missing_info` 语义与 `need_info` 的映射**：M3 只标 `missing_info`，M4 据此决定 `need_info`。`missing_info` 的粒度（缺哪个 slot / 缺 leader 确认 / 缺时间）需与 M4 打分因子（M4 §2.1 `c1` slot 完整度）对齐口径。
3. **`manual_followup` 边界**：是否保留该类型？如何防它成「垃圾桶」稀释「可落地」？建议加硬约束（必须具体可验证）并人工抽检。
4. **chat → project 映射**：优先用 `Group.project_id`（M1 维护，准）；会话未关联项目时靠 LLM `project_hint` 推断再解析（省事、可能错）。未关联又推断不出时 `project_id` 留 NULL 交 M4，是否可接受？
5. **语义去重阈值标定**：`todo_semantic` 的 embedding 模型与 `score_threshold` 需在真实飞书数据上标定；上线前需一批标注样本。
6. **跨会话/跨群 `Todo` 聚合**：同一件事横跨多个群/话题（如私聊交办 + 群里跟进）如何聚合为一个 `Todo`？当前按会话单元切分可能拆散（`group_id` 只能挂一个来源会话）。
7. **长会话切片合并**：超 token 预算时按时间切片，多片抽取结果的合并规则（尤其跨片的同一 `Todo`）需定义。
8. **mem0 异步写确认**：2026 版 `add` 经 sidecar 异步返回 `event_id`。M3 是否需在本轮内轮询确认回写落库，还是 fire-and-forget？影响下轮检索一致性。
9. **mem0 metadata 复杂过滤**：基线只用标量等值（总纲 §5.1、开放问题 #5）。防自激励循环当前靠正向标量选取 / Go 侧后过滤；若需 `ne`/`AND`/`OR` 组合过滤，**需实测 Qdrant 后端支持**后才启用。
10. **回看窗口参数**：`N=20 条 / T=120min` 为初始值，需据真实群活跃度调参；过大增成本、过小丢上下文。
11. **两阶段抽取（备选）**：当前单次调用 + 扁平 slot 超集。若准确率不足，是否切换为「先分类、后按类型分 schema 填 slot」的 prompt-ladder（更准但更多调用/成本）。
12. **时区**：`current_datetime` 的时区来源（principal 本地 or 会话上下文）需固定，避免相对时间解析歧义。
13. **中英混合**：飞书消息常中英夹杂，prompt/enum 说明是否需双语强化以稳住抽取质量。
14. ~~**Resource 引用深度**~~ **已定（总纲 §11.4）**：**仅妙记按需拉取** `extracted_text`（M3 调 M2 `ResourceFetcher.EnsureMinutesText`，见 §3.3）；图片/文档/附件本期不解析，M3 只引用 token/name（弱依据）。遗留待实测：`lark-cli minutes` 取逐字稿的可行性与权限范围（总纲 §11.5）。

---

## 9. 参考（2026 实践依据）

- LLM 结构化输出：native structured output + strict JSON schema 为生产默认，legacy JSON mode 已弃用；全字段 required、可选用 nullable、`additionalProperties:false`、refusal 作一等错误；schema 保持扁平（≤5 层）、复杂任务拆多次调用；provider 保证结构后仍用应用层（此处 Go struct + validator）二次校验。
- Action item 抽取：把 LLM 当确定性抽取器而非总结器；只抽有明确 owner 的承诺、区分 firm/tentative、设「模糊桶」（`missing_info`）、每条带逐字证据、同一承诺只记一条、相对时间转绝对日期。
- 语义去重：Qdrant `score_threshold` cosine 近邻，阈值需按 embedding 模型在真实数据标定（常见 0.80–0.92），领域过滤防串味，关键路径用 LLM 复核候选对。
- mem0（2026，经 sidecar）：`search` 实体 id 入 `filters`，Qdrant 后端**以标量等值过滤为基线**（复杂 AND/OR/比较算子需实测），显式设 `top_k`/`threshold`；`add` 异步返回 `event_id`、hash 去重、实体自动抽取、内建实体链接（不需 Neo4j）。
- Go 落地：GORM 映射 `todo`（JSON 字段用自定义类型）；robfig/cron v3 触发 `extract` job；go-playground/validator 做结构层校验 + 应用层分型 slot 校验；model API 走标准 `net/http`（或 go-openai 库）直连，不经 Eino、不用 codex（codex 仅 M4/M5）。

### 9.1 实现进度（2026-07-19）

- `internal/domain/extract.go` 已落 `todo_extract_watermark` / `todo_event` GORM model，并纳入启动迁移。
- `internal/extract/candidate.go` 已落封闭 action/slot 词表、缺 slot 显式降级、strict JSON 解码和 NFKC + case-fold 指纹归一。
- `internal/extract/provider` 已落 OpenAI-compatible `POST /chat/completions` + `response_format=json_schema, strict=true` client；拒答、非 `stop`、非法 JSON/schema 均直接报错，不回退 JSON mode。
- `internal/extract` worker 已落 related group 增量读取、chat/topic 聚合、受限回看、Person/Project/Resource/Todo 背景、mem0 检索、逐字新证据校验、完整候选的精确指纹去重，以及 Todo/Event/水位的 per-chat 事务提交；`extract.schedule` 使用非重叠 cron，`--extract-once` 支持手工验收。
- `internal/embedding` + `internal/semantic` 已落第二层语义去重：复用 mem0 的 `bge_m3_embed`，官方 Qdrant Go client 连接 gRPC 6334，独立 `todo_semantic` 集合按 cosine 检索；集合启动时强校验 embedding 模型元数据、1024 维和距离类型，阈值/近邻数可配置。
- 语义近邻只作为疑似候选；`internal/extract/provider` 追加 strict boolean `same_action` 裁决。裁决失败、索引陈旧、领域不一致均直接中断，不默认合并或新建。确认同项后按 Todo ID 更新既有行，原精确指纹保持不变。
- Qdrant 同步位于 per-chat MySQL 事务末尾；同步失败会回滚 Todo/Event/watermark。无 outbox 或静默降级路径。
- 真实验收已覆盖 Kimi strict 抽取、strict 同事项裁决，以及 `MySQL → mem0 → model → embedding → Qdrant → Todo/Event/watermark` 全链路；全链路测试临时隔离真实群，只发送合成消息，MySQL 外层事务回滚并删除临时 Qdrant 集合，不残留 fixture。
- `GET /api/todos` / `GET /api/todos/{id}` 与 `web/` 只读看板已完成；M0.5 前不提供修改/确认接口。
- 身份 slot 缺失的候选当前允许通过领域校验，但指纹计算显式返回 `ErrFingerprintIncomplete`，整个 chat 不写 Todo/水位，避免用 `null` 形成跨 Todo 碰撞。其最终持久化身份策略仍需确认后再实现，不加临时 fallback。
- 当前 `semantic_threshold=0.85`、`semantic_neighbor_limit=3` 是上线起点，仍需用真实标注样本完成 §8.5 阈值标定；本轮没有读取或外发真实相关群消息。
