# M2 消息识别模块（采集 + 记忆化）技术方案

> **⚠️ 记忆层已退役（2026-08）**：本文写于 mem0 sidecar 时代，文中所有 mem0 / `jarvis_memories` / 记忆化相关设计都**不再成立**。事实沉淀已改为离线事实引擎：`internal/factengine` 按水位消费原料、用 traex + `DeepSeek-V4-Flash` 蒸馏成 `fact` 表，M3/M5 只读不写。权威描述见 [`docs/00-overview.md` §5](../00-overview.md)。本文其余部分仍作历史设计参考。

> 模块定位：Jarvis 流水线的第一段。负责把「飞书全量消息」可靠地落到本地 MySQL（明文，source of truth），沉淀 `Group` / `Resource` 两个一等实体与 `ScanRecord` 扫描流水，并把有价值的对话蒸馏进 mem0 记忆层（Qdrant 后端，经 Python sidecar），为下游 M3（**Todo 提取**）提供可检索的结构化事实与语义记忆。
>
> 设计原则：本地可信明文存储 / fail-fast 暴露问题 / 不乱加 fallback 与旧数据兼容 / 模块化 / 优先官方包与已验证能力。

---

## 版本说明

| 项 | 内容 |
| --- | --- |
| 隶属总纲 | `docs/00-overview.md`（顶层设计与跨模块契约，7 实体权威定义、mem0 sidecar 契约、LLM 分工均以总纲为准） |
| 技术栈 | **Go 1.26 / Hertz `v0.10.5`（`github.com/cloudwego/hertz`）/ GORM（`gorm.io/gorm` + MySQL driver）/ robfig/cron v3（`github.com/robfig/cron/v3`）** |
| 本次改写 | 后端由 Python 全面改 Go；APScheduler→robfig/cron；lark-cli 由 Go `exec.Command` 子进程调用；mem0 改 Python FastAPI sidecar，Go 经 HTTP 调用；新增 `Resource` 沉淀与 `ScanRecord` 流水；下游产出物由 Task 改为 **Todo（行动线索）**。 |
| 不引入 | Eino / Kitex（Jarvis 是本地单体，LLM 抽取直连 model API，见总纲 §6）；不部署 Neo4j 等外部图库（mem0 内建实体链接）。 |
| 核心算法 | 分层扫描（热/温/冷）、每会话 checkpoint 高水位、`message_id` 幂等去重、令牌桶限流、失败重试不漏消息、mem0 窗口化——**语言无关，保留自原方案**。 |

---

## 0. 元信息

| 项 | 内容 |
| --- | --- |
| 模块编号 | M2 |
| 职责 | 消息采集（capture）+ Group/Resource 沉淀 + 记忆化（memorize） |
| 上游 | 飞书（lark-cli user-token 轮询为主，bot 事件流为辅），经 Go `exec.Command` 子进程封装 |
| 下游 | M3 Todo 提取（读 MySQL + mem0 检索接口） |
| 存储 | MySQL（`feishu_group`/`message`/`resource`/`scan_record`/`chat_checkpoint`），Qdrant（mem0 向量 + 实体，经 sidecar） |
| 关键依赖 | `lark-cli`（子进程）、`gorm.io/gorm`、`github.com/robfig/cron/v3`、mem0 sidecar（`127.0.0.1:18900`，Python FastAPI 包 `mem0`） |
| 运行环境 | 本地 Mac 可信环境，明文存储，launchd 托管 |

### 实体边界（7 实体中 M2 负责的部分）

- **Group**（一等实体，原 `jarvis_chat` 升格；物理表名 `feishu_group`）：M2 完全拥有其自动发现与扫描分层字段（`tier`/`pinned`/`last_active_at`/…）；`project_id` 关联由 M1 维护。DDL 见总纲 §2.4 `feishu_group`。
- **Message**（支撑表，非核心实体）：M2 完全拥有，明文 source of truth，写入 MySQL `message`。DDL 见总纲 §2.5 / §2.4。
- **Resource**（一等实体，新增）：M2 在采集消息时把附件（图片/文件/音视频/妙记/文档/链接/卡片）**沉淀为独立 `resource` 行**（仅元数据），按 `file_key`/`minute_token`/`doc_token`/`url` 去重。**M2 不下载、不解析**（`downloaded=0`、`extracted_text=NULL`）；按需下载/解析由下游触发、且本期仅妙记（总纲 §11.4）。DDL 见总纲 §2.4 `resource`。
- **ScanRecord**（一等实体，新增）：M2 每次扫描（`discover`/`scan_hot`/`scan_warm`/`scan_cold`/`event`）**追加一条流水**，记录时间窗、条数、成败、错误、前后高水位。用于后台展示扫描历史、排障、监控某群多久没扫到新消息。DDL 见总纲 §2.4 `scan_record`。（**无 `backfill` 类型**：已定不回溯历史，总纲 §11.3。）
- **Person / Project**：M2 只做「原料沉淀」——把 `sender`、`mention`、会话元数据落库，并在 mem0 metadata 里带上外键；实体规范化建模由 M1 负责。
- **Todo / Task**：M2 **不产出** Todo/Task，只产出「可被 M3 提取成 **Todo（行动线索）** 的记忆与明文」。

会议与妙记采集遵守同一边界：成功、无权限、暂不可用都是客观采集结果，M2 将会议信息、`minute_token` 和原始错误写成中立证据交给 M3。M2 可以独立安排重试，但不申请权限、不联系主持人，也不把错误直接翻译成 Todo。

> **ScanRecord（流水） vs chat_checkpoint（状态）**：`chat_checkpoint` 每会话一行，只存「扫到哪了」（高水位游标），用于断点续扫；`scan_record` 每次扫描一行（追加），存「这次扫描发生了什么」。**一次成功扫描 = 更新 checkpoint 高水位 + 追加一条 scan_record**。详见 §3.2 / §3.8。

---

## 1. 模块定位与边界

### 1.1 输入

1. `lark-cli im +chat-list --as user --types=p2p,group --sort active_time`：枚举当前用户的全部群 + 单聊（分页、按活跃时间降序）。
2. `lark-cli im +chat-messages-list --as user --chat-id oc_xxx`：拉取单会话消息（时间范围 `--start/--end`、`--order asc|desc`、`--page-size 1-50`、`--page-token`）。
3. （可选增强）`lark-cli event consume im.message.receive_v1 --as bot`：bot 所在会话/被 @ 消息的低延迟事件流。

> 以上均由 Go 侧 `internal/larkcli` 统一封装为 `exec.Command` 子进程调用（`--format json` 解析、fail-fast、令牌桶限流），见 §3.6。

### 1.2 输出

1. MySQL：`feishu_group` / `message` / `chat_checkpoint` / `resource` / `scan_record`（明文）。
2. mem0/Qdrant：从对话窗口蒸馏出的记忆（事实/关系/项目上下文/偏好），经 mem0 sidecar 写入。
3. 供 M3 的检索接口：Go 侧 `MemoryClient.Search(...)`（HTTP 调 sidecar）+ 直接查 MySQL。

### 1.3 非目标（Non-goals）

- 不做 Todo 判定与路由（M3 / M5 判断环节）。
- 不根据采集错误决定后续动作；错误进入 M3 后，由 M3 结合上下文决定创建、合并或忽略 Todo。
- **采集期不下载任何二进制、不 OCR**（仅沉淀 `resource` 元数据）。按需下载/解析**仅妙记**、由下游触发，M2 提供 `ResourceFetcher`（§3.9.1）；图片 OCR、文档/表格/附件解析本期不做（总纲 §11.4）。
- 不改写/删除飞书侧任何数据（M2 全程只读飞书）。

### 1.4 覆盖策略（结论已定，不再论证）

**user 轮询为主（只覆盖 `related_group=1`）+ bot 事件流为辅（关键群低延迟增强）。**

原因（实测约束）：`im.*` 事件仅 bot 授权、scope 为 `im:message.p2p_msg:readonly`，只能拿到 bot 所在会话/被 @ 的消息；若要完整捕获选定相关群，必须走 user-token 轮询。两条链路以 `message_id`（`om_` 前缀）为幂等键合流。会话发现仍枚举全部可见会话，但只同步元数据，不拉消息。

---

## 2. 总体架构与数据流

```text
                         ┌──────────────────────────────────────────────┐
                         │            robfig/cron v3 (Go)                 │
                         │  discover(1h) scan_hot(5m) scan_warm(30m)      │
                         │  scan_cold(6h) memorize(10m)                   │
                         └───────────────┬────────────────────────────────┘
                                         │ 驱动
     ┌───────────────────────┐   token  ▼
     │  会话发现 & 分层         │  bucket ┌───────────────────────┐
     │  chat-list --sort       │────────▶│   分层扫描 Capture      │
     │  active_time            │  限QPS  │  per-chat checkpoint    │
     │  upsert group           │         │  纯增量(无 backfill)     │
     └───────────────────────┘         │  + Resource 沉淀         │
                                         │  + ScanRecord 流水       │
   （可选）bot 事件流                     └───────────┬───────────┘
   ┌──────────────────────┐   同一 om_ 去重合流         │ 幂等 upsert (om_ 唯一键)
   │ event consume (daemon)│──────────────────▶ ┌──────────────────────────┐
   │  ready marker/优雅退出 │                     │   MySQL  message          │
   └──────────────────────┘                     │  (明文, source of truth)   │
                                                 │   + group + resource      │
                                                 │   + scan_record + ckpt    │
                                                 └───────────┬──────────────┘
                                                             │ 拉 mem0_processed=0
                                                             │ 按 group 窗口化
                                                             ▼
                                              ┌──────────────────────────────┐
                                              │ Memorize worker (Go)          │
                                              │  MemoryClient.Add() ──HTTP──▶  │
                                              └───────────────┬────────────────┘
                                                              ▼
                                     ┌────────────────────────────────────────┐
                                     │ mem0 sidecar (Python FastAPI, :18900)    │
                                     │  POST /memories → mem0.add(infer=True)   │
                                     │  LLM 抽取用 model API (可配置)            │
                                     └───────────────┬──────────────────────────┘
                                                     ▼
                                              ┌──────────────────────────────┐
                                              │   Qdrant  (mem0 向量 + 实体)    │
                                              └───────────────┬────────────────┘
                                                              │ MemoryClient.Search()
                                                              ▼
                                                         M3 Todo 提取
```

**核心不变量**：

1. MySQL 是唯一 source of truth，明文，永不因 mem0 失败而丢消息。
2. 采集与记忆化解耦：`mem0_processed` 标志驱动，记忆化失败不影响采集，可独立重跑（sidecar / Qdrant 挂了只影响 `memorize` job）。
3. 轮询链路独占「扫描游标（checkpoint）」的推进权；事件链路只做「额外插入」，绝不推进游标（见 §3.2 / §7）——从而保证无论事件是否乱序/丢失，轮询都能补齐，不产生空洞。
4. **每次扫描一条 `scan_record`（可观测流水），checkpoint 只在扫描成功时前进（断点续扫状态）**，二者职责分离（见 §3.8）。

---

## 3. 捕获算法（核心）

### 3.1 会话发现与分层扫描

**分层依据**：以本地记录的 `last_active_at`（来自该会话最新消息 `create_time`）为主，`chat-list --sort active_time` 的返回顺序为「发现新活跃会话」的辅助信号。

| 层 | 判定（默认，可调） | 扫描 cron | 说明 |
| --- | --- | --- | --- |
| HOT | `last_active_at < 6h` 或 `pinned=1` | 每 5 min | leader 单聊、核心项目群应 `pinned` 强制进 HOT |
| WARM | `6h ≤ last_active_at < 7d` | 每 30 min | 一般活跃会话 |
| COLD | `last_active_at ≥ 7d` | 每 6h | 沉睡会话，兜底补漏 |

> 阈值（6h / 7d）与 cron 间隔为默认值，属可调参数；HOT 白名单（leader open_id、核心项目群，对应 `group.pinned`/`is_key_group`）**需与用户确认**。

**扫描准入**：`ScanTier` 查询必须包含 `related_group=1`；`ScanChat` 同样拒绝非相关会话。名单由 `ReplaceRelatedGroups` 在事务内完整替换，数量不写死；当前 20 个只是初始候选，可按工作变化动态增删。

**发现流程（`DiscoverChats`，每 1h）**：全量分页枚举 `chat-list --sort active_time`，`upsert group`（name/owner/external/tenant 等元数据），并根据 `last_active_at` 重算 `tier`。发现是「元数据同步 + 分层刷新」，不拉消息，但**追加一条 `scan_type=discover` 的 scan_record**（`group_id` 为空，记录枚举了多少会话、耗时、成败）。

> 为什么发现要全量分页而不是只扫前几页：`active_time` 降序只能保证「最近活跃的在前」，但被静音群、久未活跃后突然被拉回的群可能排序靠后；fail-fast 语义下宁可每小时全量枚举一次，避免漏掉会话本身。会话总数通常几百量级，成本可忽略（见 §9）。

### 3.2 每会话 checkpoint（high-water create_time）

每个会话在 `chat_checkpoint` 维护一个「高水位」游标：

- `high_water_create_time`（ms）：该会话**已成功落库**消息里的最大 `create_time`。
- `last_message_id`：高水位对应的 `om_` id（辅助排障）。

**增量扫描**：以 `start = high_water_create_time`（**含**，不做 +1ms）、`order=asc` 请求，分页直到 `has_more=false`。

- 之所以用「含高水位」而非「高水位+1」：`create_time` 为毫秒，同一毫秒可能有多条消息；若从 `HW+1` 开始，会漏掉与 HW 同毫秒的后到消息。用「含 HW」会重复拉到边界消息，但由 `message_id` 唯一键去重兜住 → **保证不漏、允许重复**。

**游标推进**：每成功落一页，`HW = max(HW, max(page.create_time))`，消息 upsert 与 checkpoint 更新放在**同一事务**里逐页提交（GORM `db.Transaction(...)`）。中途崩溃后下轮从最后一次提交的 HW 恢复，天然可续。

> 关键设计决策：**只有轮询会推进 `high_water_create_time`**。事件流（§7）只 upsert 消息、绝不改 checkpoint，避免「事件把游标推过某毫秒、而轮询以为该毫秒已扫完从而漏掉同毫秒其它消息」的空洞。

### 3.3 幂等 upsert 去重

以 `message_id`（`om_`）为唯一键做 upsert（GORM `clause.OnConflict`）：

- 新消息：`INSERT`。
- 已存在且 `update_time` 变大（消息被编辑）：更新 `content/content_raw/update_time`，并把 `mem0_processed` 重置为 0（触发重新抽取）。
- 已存在且无变化：忽略（幂等）。

> 编辑消息重抽取属于「当前正确行为」，不是历史数据兼容；是否需要对编辑消息重抽记忆**需与用户确认**（默认：需要）。

### 3.4 不回溯历史：首次发现即建高水位（已定）

**决策（总纲 §11.3）：不拉任何历史消息**，只采集"系统首次发现该会话之后"的新消息。因此**没有 backfill 阶段**。

- 会话首次被 `DiscoverChats` 发现时，直接初始化 checkpoint：`high_water_create_time = 发现时刻的当前毫秒时间戳(now_ms)`、`backfill_done = 1`（无 backfill 语义，仅保留字段兼容）。此时**不拉消息**，只建游标。
- 之后按增量（§3.2）从该高水位（含）向后扫，记 `scan_type=scan_hot/scan_warm/scan_cold`。
- 效果：`create_time ≤ 首次发现时刻` 的历史消息永不进入本系统；不存在"首次回溯多久"的问题，也不会一次性拉海量历史。

> **与原方案的差异**：原设计要求用户显式配置 `backfill_since`、否则 fail-fast 拒绝首扫。现按用户决策改为"首次发现即以当前时刻建高水位、直接进入增量"。`chat_checkpoint.backfill_since` 字段保留但不再使用（值恒为 `now_ms` 快照，仅审计参考）；`backfill` 类 `scan_record` 不再产生。
> **边界说明**：若用户后续想改为"回溯 N 天"，只需把首次发现时的初始高水位从 `now_ms` 改为 `now_ms - N*天`（单点可配），当前默认不回溯。

### 3.5 限流 / 令牌桶控制 QPS

飞书 OpenAPI 有租户级 QPS 限额。采集侧用**全局令牌桶**统一限速，所有扫描 job 共享：

- 桶容量 `C`，补充速率 `R` tokens/s（默认保守值，如 `R=5`，具体额度**需与用户确认**）。
- 每次 `lark-cli` 请求前 `Acquire(1)`；桶空则阻塞等待。
- 批量采集时用 `--no-reactions` 关闭 reaction 富化（否则每批消息额外触发 `reactions/batch_query`，接近翻倍请求）。
- 并发用带缓冲 channel（信号量）限制同时在跑的 `lark-cli` 子进程数（默认 `concurrency=2~3`），与令牌桶叠加。

### 3.6 lark-cli 调用（Go `exec.Command` 封装）

Go 侧 `internal/larkcli` 把每次 lark-cli 调用封装为子进程，统一 `--format json` 解析、fail-fast、令牌桶与超时控制。**不做静默降级**：非 0 退出码或 JSON 解析失败直接返回 error。

```go
package larkcli

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "time"
)

type Client struct {
    bin    string        // lark-cli 绝对路径
    bucket *TokenBucket  // 全局共享，跨所有扫描 job
    sem    chan struct{} // 并发信号量(concurrency=2~3)
}

// Run 执行一次 lark-cli 子进程，args 形如 {"im","+chat-messages-list","--as","user",...}。
// fail-fast：退出码非 0 或输出非 JSON → 返回 error，绝不吞。
func (c *Client) Run(ctx context.Context, out any, args ...string) error {
    if err := c.bucket.Acquire(ctx, 1); err != nil { // 全局限流
        return fmt.Errorf("token bucket: %w", err)
    }
    c.sem <- struct{}{}
    defer func() { <-c.sem }()

    ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()

    args = append(args, "--format", "json")
    cmd := exec.CommandContext(ctx, c.bin, args...)
    stdout, err := cmd.Output()
    if err != nil {
        // 保留 stderr 便于排障；不降级
        if ee, ok := err.(*exec.ExitError); ok {
            return fmt.Errorf("lark-cli %v exit=%d stderr=%s", args, ee.ExitCode(), ee.Stderr)
        }
        return fmt.Errorf("lark-cli %v: %w", args, err)
    }
    if err := json.Unmarshal(stdout, out); err != nil {
        return fmt.Errorf("lark-cli %v: bad json: %w", args, err) // fail-fast
    }
    return nil
}
```

> lark-cli 的绝对路径按本机为准（本地可信环境，明文配置）；user/bot 身份通过 `--as user` / `--as bot` 参数区分，凭据由 lark-cli 自身管理。

### 3.7 失败重试与「不漏消息」保证

fail-fast，但采集有硬约束「不能漏消息」，两者的平衡：

- **瞬时错误**（网络抖动 / 429 限流）：小次数指数退避重试（如 3 次，1s/2s/4s），Go 侧 `retryTransient(fn, 3)`。
- **重试仍失败**：中止**该会话本轮**扫描，写 `scan_record.status=error` + `error_type/error_message`、`checkpoint.last_error`，抛错告警（fail-fast），**checkpoint 高水位保持不动**。下一轮从上次成功 HW 续扫。
- 因为「逐页事务提交 + 含高水位 + message_id 幂等」，任何中断都不会产生空洞，重跑安全。
- **不做静默降级**：不吞异常、不跳过失败页继续（那样会造成空洞）。失败就暴露，游标不前进。

### 3.8 ScanRecord 写入逻辑（新增）

`scan_record` 是**追加型流水表**，与 `chat_checkpoint`（状态表）分离。每次 `ScanChat` / `DiscoverChats` 进入即建行、结束即补齐，无论成败都落一条。

**写入时机与字段**：

| 时机 | 动作 | 写入字段 |
| --- | --- | --- |
| 扫描开始 | INSERT 一行 | `scan_type`、`group_id`/`chat_id`、`window_start=起扫 start_ms`、`high_water_before=进入时 HW`、`started_at=now`、`status='partial'`（占位） |
| 逐页成功 | 累加（内存计数，收尾一次性回写或按页更新） | `fetched_count += len(page)`、`inserted_count += 实际新增`、`page_count += 1` |
| 扫描成功结束 | UPDATE 该行 | `window_end=最终 HW`、`high_water_after=最终 HW`、`status='ok'`、`finished_at=now`、`duration_ms` |
| 扫描失败中止 | UPDATE 该行 | `status='error'`、`error_type`、`error_message`、`finished_at`、`duration_ms`、`high_water_after=high_water_before`（未推进） |

**用途**（后台可观测与排障）：

- **扫描历史**：后台按 `group_id` / `scan_type` 列出每次扫描的时间窗、条数、成败（走 `idx_scan_group_time` / `idx_scan_type_time`）。
- **排障**：`status='error'` 的记录带 `error_type/error_message`，定位「哪次扫描、哪个群、什么错」（走 `idx_scan_status`）。
- **监控「某群多久没扫到新消息」**：对比某群最近若干条 scan_record 的 `inserted_count`（长期为 0 = 沉睡或漏扫）、`finished_at`（多久没扫）与 `high_water_after`（游标是否推进）。
- **区分 checkpoint**：`scan_record.high_water_before/after` 是「这次扫描前后」的游标快照（流水视角）；`chat_checkpoint.high_water_create_time` 是「当前最新」游标（状态视角）。一次成功扫描后两者的 `after` 应一致。

> `scan_record` 只增不改历史行（除本次扫描收尾的那条 UPDATE），保留期清理属运维策略（开放问题）。

### 3.9 Resource 沉淀逻辑（新增）

采集消息时，除了在 `message` 里记 `content`（人类可读文本）/`content_raw`（原始 JSON），遇到承载附件/资源的消息类型，**同时 upsert 到 `resource` 表**，把原来散在 `message.resources_json` 的信息升为一等实体行。

**触发类型 → `resource_type` 与去重键**：

| 消息类型 | `resource_type` | 飞书标识（去重键） |
| --- | --- | --- |
| `image` | `image` | `file_key`（image_key） |
| `file` | `file` | `file_key` |
| `audio` | `audio` | `file_key` |
| `media`/`video` | `video` | `file_key` |
| `post` 内嵌图片/附件 | `image`/`file` | `file_key`（拍平富文本时逐个抽取） |
| `merge_forward` 合并转发内嵌资源 | 按内嵌类型 | 对应 `file_key`（深展开留待后续，见开放问题） |
| 妙记链接（消息含 minutes token） | `minutes` | `minute_token` |
| 云文档链接（docx/wiki token） | `doc` | `doc_token` |
| 外链 URL | `link` | `url` |
| `interactive` 卡片内资源 | `card` | `url`/`file_key`（按卡片内容） |

**去重原则（两层，已定，总纲 §11.4 / §2.4）**：

1. **同消息幂等**：唯一键 `(source_message_id, file_key)`，同一条消息重扫不重复插（飞书 `file_key` 与消息绑定）。
2. **跨消息内容去重**：靠 `content_hash`（内容 SHA256），**下载后才有**。M2 采集期只沉淀元数据（`downloaded=0`、`content_hash=NULL`、`extracted_text=NULL`），**不下载、不解析**；同一文件跨多条消息此时会各记一行（保留证据），待下游按需下载后回填 `content_hash`、相同 hash 复用同一 `local_path`（只存一份本地文件）。

**沉淀伪代码（Go 风格）**：

```go
// 在 upsertMessage 落库后调用；从渲染阶段抽出的 resource 描述列表沉淀为 resource 行。
func sinkResources(tx *gorm.DB, m *Message, refs []ResourceRef) error {
    for _, r := range refs {
        res := Resource{
            ResourceType:    r.Type,           // image|file|audio|video|minutes|doc|link|card
            FileKey:         r.FileKey,         // 按类型二选一或都填
            MinuteToken:     r.MinuteToken,
            DocToken:        r.DocToken,
            URL:             r.URL,
            Name:            r.Name,
            MimeType:        r.MimeType,
            SizeBytes:       r.SizeBytes,
            SourceMessageID: m.MessageID,       // om_
            GroupID:         m.GroupID,
            Downloaded:      false,             // M2 只沉淀元数据，不下载
        }
        // 同消息幂等：唯一键 (source_message_id, file_key)，命中已存在则忽略。
        // 跨消息去重不在采集期做——靠下游下载后回填的 content_hash（总纲 §11.4）。
        if err := tx.Clauses(clause.OnConflict{
            Columns:   []clause.Column{{Name: "source_message_id"}, {Name: "file_key"}},
            DoNothing: true,
        }).Create(&res).Error; err != nil {
            return fmt.Errorf("sink resource msg=%s key=%s: %w", m.MessageID, r.FileKey, err) // fail-fast
        }
    }
    return nil
}
```

> **去重键与 DDL（已定）**：采集期只依赖 `(source_message_id, file_key)` 唯一键做同消息幂等（总纲 §2.4 已建 `uk_resource_msg_key`）。**跨消息同一文件去重靠 `content_hash`**（内容 SHA256，下载后回填，总纲 §2.4 已建 `idx_resource_content` 普通索引）——采集期不下载、故此时不去重跨消息重复，各来源消息各记一行保留证据，待下游按需下载后按 `content_hash` 复用本地文件。不给 `file_key`/`minute_token`/`doc_token`/`url` 建唯一索引（同一文件在不同消息 key 不同，强行唯一会误合并）。

### 3.9.1 按需下载 / 解析（仅妙记，已定）

**决策（总纲 §11.4）**：采集期不下载任何二进制、不 OCR；**只在下游（M3/M5）需要某 `Resource` 内容时**才按需拉取，且**本期仅妙记**（`resource_type=minutes`）。图片 OCR、飞书文档/表格、附件解析本期都不做。

M2 提供一个可被下游调用的 `ResourceFetcher`（放在 `internal/capture` 或独立 `internal/resource` 包），职责：拉妙记逐字稿 → 回填 `extracted_text` / `content_hash` / `downloaded` / `local_path`，并按 `content_hash` 复用本地文件。

```go
// 下游(M3/M5)按需调用：确保某 minutes Resource 的 extracted_text 已就绪。
// 本期只处理 resource_type=minutes；其它类型直接返回(不下载、不解析)。
func (f *ResourceFetcher) EnsureMinutesText(ctx context.Context, resID uint64) (string, error) {
    var r Resource
    if err := f.db.First(&r, resID).Error; err != nil {
        return "", err
    }
    if r.ResourceType != "minutes" {
        // 本期仅妙记；其它类型不解析（fail-fast：不静默假装成功）
        return "", fmt.Errorf("resource %d type=%s 本期不支持按需解析", resID, r.ResourceType)
    }
    if r.Downloaded && r.ExtractedText != "" {
        return r.ExtractedText, nil // 已就绪，直接复用
    }
    if r.MinuteToken == "" {
        return "", fmt.Errorf("resource %d 缺 minute_token", resID) // fail-fast
    }

    // lark-cli 拉妙记逐字稿/产物（子进程，见 §3.6 封装）
    var mrsp MinutesResp
    if err := f.lark.Run(ctx, &mrsp, "minutes", "+get-transcript",
        "--as", "user", "--minute-token", r.MinuteToken); err != nil {
        return "", fmt.Errorf("fetch minutes %s: %w", r.MinuteToken, err) // fail-fast，不降级
    }
    text := mrsp.Transcript
    hash := sha256Hex([]byte(text))

    // 跨消息去重：同 content_hash 复用已有本地文件/文本
    localPath := f.reuseOrPersist(hash, text) // 命中已存在则复用其 local_path，否则落一份

    r.ExtractedText, r.ContentHash, r.Downloaded, r.LocalPath = text, hash, true, localPath
    if err := f.db.Save(&r).Error; err != nil {
        return "", err
    }
    return text, nil
}
```

- **触发方**：M3 抽取 `summary_post` 需要妙记结论时、或 M5 判断/执行环节需要妙记作方案依据时调用（M3 文档 §3.3 会引用此接口）。
- **lark-cli 命令**：以本机实测为准（`minutes` 下的取逐字稿/产物子命令）；权限/授权范围需实测（总纲 §11.5）。
- **fail-fast**：拉取失败直接 error，不静默返回空文本；非妙记类型明确拒绝，不假装解析成功。
- **去重复用**：`reuseOrPersist(hash, ...)` 命中相同 `content_hash` 时复用已有 `local_path`（只存一份），实现"同一妙记多处引用只下一次"。

### 3.10 完整扫描伪代码（Go 风格）

把分层入口、单会话扫描、checkpoint 推进、Resource 沉淀、ScanRecord 写入串起来。

```go
// ---------- 会话发现与分层（每 1h） ----------
func DiscoverChats(ctx context.Context) error {
    rec := beginScanRecord("discover", nil) // 追加 scan_record，group 为空
    var pageToken string
    for {
        var resp ChatListResp
        args := []string{"im", "+chat-list", "--as", "user",
            "--types", "p2p,group", "--sort", "active_time", "--page-size", "100"}
        if pageToken != "" {
            args = append(args, "--page-token", pageToken)
        }
        if err := lark.Run(ctx, &resp, args...); err != nil { // fail-fast
            return finishScanRecordErr(rec, err)
        }
        for _, c := range resp.Data.Chats {
            isNew := upsertGroup(c) // name/owner/external/tenant/chat_mode...；返回是否首次发现
            if isNew {
                // 不回溯：首次发现即以当前时刻建高水位，无 backfill 阶段（总纲 §11.3）
                initCheckpointNoBackfill(c.ChatID, nowMs())
            }
            rec.FetchedCount++
        }
        rec.PageCount++
        if !resp.Data.HasMore {
            break
        }
        pageToken = resp.Data.PageToken
    }
    recomputeTiers() // 依据 last_active_at 刷新 tier；pinned 恒为 hot
    return finishScanRecordOK(rec, 0, 0)
}

// ---------- 分层扫描入口（被不同 cron 调用） ----------
func ScanTier(ctx context.Context, tier string) {
    for _, g := range selectRelatedGroupsByTier(tier) { // related_group=1；hot 额外含 pinned
        if err := ScanChat(ctx, g, scanTypeOf(tier)); err != nil {
            // fail-fast：已在 ScanChat 内写 scan_record.status=error + checkpoint.last_error
            markGroupError(g.ChatID, err)
            // 不吞、不跳页；该 chat 本轮止步，checkpoint 不动，下轮续
        }
    }
}

// ---------- 单会话扫描（纯增量，无 backfill） ----------
func ScanChat(ctx context.Context, g *Group, scanType string) error {
    // 不回溯：首次发现时已由 initCheckpointNoBackfill 把高水位设为发现时刻(now_ms)。
    // 若因异常缺失 checkpoint，这里补建（同样以当前时刻建高水位，绝不回溯历史）。
    ckpt := getOrInitCheckpointNoBackfill(g.ChatID, nowMs())

    startMs := ckpt.HighWaterCreateTime // 含高水位，只向后拉新消息

    rec := beginScanRecord(scanType, g)          // started_at, window_start=startMs, high_water_before
    hw := ckpt.HighWaterCreateTime
    var pageToken string
    for {
        var resp MessagesResp
        args := []string{"im", "+chat-messages-list", "--as", "user",
            "--chat-id", g.ChatID, "--order", "asc",
            "--start", iso8601(startMs), "--page-size", "50", "--no-reactions"}
        if pageToken != "" {
            args = append(args, "--page-token", pageToken)
        }
        // 仅瞬时错误重试；重试仍失败 → 写 error scan_record，checkpoint 不动
        if err := retryTransient(func() error { return lark.Run(ctx, &resp, args...) }, 3); err != nil {
            return finishScanRecordErr(rec, err)
        }

        msgs := flattenThreadReplies(&resp) // 展开内联 thread_replies 为独立行

        // 逐页事务：消息 + Resource + 游标一起提交
        err := db.Transaction(func(tx *gorm.DB) error {
            for _, m := range msgs {
                refs := upsertMessage(tx, m, "poll") // om_ 幂等；编辑则重置 mem0_processed
                if err := sinkResources(tx, m, refs); err != nil { // §3.9
                    return err
                }
                if m.CreateTime > hw {
                    hw = m.CreateTime
                }
                rec.InsertedCount += m.insertedDelta // 实际新增(0/1)
            }
            lastID := ckpt.LastMessageID
            if len(msgs) > 0 {
                lastID = msgs[len(msgs)-1].MessageID
            }
            return advanceCheckpoint(tx, g.ChatID, hw, lastID) // 高水位 + last_message_id
        })
        if err != nil {
            return finishScanRecordErr(rec, err)
        }

        rec.FetchedCount += len(msgs)
        rec.PageCount++
        if !resp.Data.HasMore {
            break
        }
        pageToken = resp.Data.PageToken
    }

    return finishScanRecordOK(rec, hw, hw) // window_end/high_water_after=hw, status=ok
}

// ---------- 令牌桶 ----------
type TokenBucket struct {
    capacity, rate float64
    tokens         float64
    ts             time.Time
    mu             sync.Mutex
}

func (b *TokenBucket) Acquire(ctx context.Context, n float64) error {
    for {
        b.mu.Lock()
        b.refill()
        if b.tokens >= n {
            b.tokens -= n
            b.mu.Unlock()
            return nil
        }
        b.mu.Unlock()
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(20 * time.Millisecond):
        }
    }
}

func (b *TokenBucket) refill() {
    now := time.Now()
    b.tokens = math.Min(b.capacity, b.tokens+now.Sub(b.ts).Seconds()*b.rate)
    b.ts = now
}
```

> **已知限制（thread 回复）**：底层 `/im/v1/messages` 以 `only_thread_root_messages=true` 返回线程根消息并内联展开回复。若一条**旧根消息**（`create_time < HW`）在高水位之后收到**新回复**，纯 `create_time` 增量窗口不会返回该旧根，可能漏掉这条新回复。缓解方案：对活跃会话周期性用 `im +threads-messages-list` 深扫、或对重线程群启用 bot 事件流。此项优先级**需与用户确认**（不做静默兜底）。

---

## 4. 存储：DDL 引用与 GORM model

引擎 InnoDB，字符集 `utf8mb4`（覆盖 emoji），飞书时间戳裸存原始毫秒 `BIGINT`（避免时区歧义）。

> **DDL 权威定义在总纲 §2.4 / §2.5，M2 不重复大段 DDL**，只标注引用位置与 M2 关注的关键字段/写入语义。

### 4.1 表清单与引用

| 表 | 角色 | DDL 位置 | M2 关注点 |
| --- | --- | --- | --- |
| `feishu_group` | 一等实体（会话元数据 + 分层） | 总纲 §2.4 `Group` | `related_group` 控制消息扫描范围；`tier`/`pinned`/`include_in_memory`/`is_key_group`/`last_active_at` 管理分层与记忆；`project_id` 由 M1 维护 |
| `message` | 支撑表（消息明文，SoT） | 总纲 §2.5 + 下 §4.2 | 幂等键 `message_id`；`content`/`content_raw`；`mem0_processed`；`source` |
| `chat_checkpoint` | 支撑表（每会话扫描游标，状态） | 总纲 §2.5 | `high_water_create_time`（首次发现即置 now_ms，不回溯）/`last_message_id`/`last_error`；`backfill_*` 保留不用 |
| `resource` | 一等实体（附件/资源） | 总纲 §2.4 `Resource` | 沉淀写入见 §3.9；同消息幂等 `(source_message_id,file_key)` + 跨消息 `content_hash` 去重（已定） |
| `scan_record` | 一等实体（扫描流水） | 总纲 §2.4 `ScanRecord` | 写入时机见 §3.8 |

> **`message` 表补充**：总纲支撑表清单列了 `message`，其字段以采集需要为准。M2 关键字段：`message_id`(唯一键)、`chat_id`、`sender_open_id/sender_name/sender_type`、`message_type`、`content`(渲染文本)、`content_raw`(原始 JSON)、`mentions_json`、`reply_to/root_id/thread_id`、`create_time/update_time`、`source`(poll|event)、`render_ok`、`mem0_processed/mem0_processed_at`。**原 `resources_json` 字段随 Resource 升为一等实体而废弃**（附件改沉淀到 `resource` 表）——是否保留 `resources_json` 作为冗余快照【需与用户确认】。索引：`uk_message_id`、`idx_chat_create(chat_id,create_time)`、`idx_mem0(mem0_processed,create_time)`、`idx_sender`、`idx_thread`。

### 4.2 GORM model struct

Go 侧用 GORM 映射；**表名已定 `feishu_group`**（避开 SQL 保留字，总纲 §11.1），struct 保留业务简称 `Group`，`TableName()` 返回物理表名。

```go
package model

import "time"

// Group 飞书群/单聊会话（一等实体，原 jarvis_chat）；物理表名 feishu_group。
type Group struct {
    ID              uint64    `gorm:"primaryKey;autoIncrement"`
    ChatID          string    `gorm:"column:chat_id;size:64;uniqueIndex:uk_group_chat_id"`
    ChatMode        string    `gorm:"column:chat_mode;size:16"` // group | p2p | topic（当前 lark-cli 实测会返回）
    Name            string    `gorm:"column:name;size:512"`
    Description     string    `gorm:"column:description"`
    OwnerOpenID     string    `gorm:"column:owner_open_id;size:64"`
    External        bool      `gorm:"column:external;default:0"`
    TenantKey       string    `gorm:"column:tenant_key;size:64"`
    ProjectID       *uint64   `gorm:"column:project_id"`                 // 关联项目(M1 维护)
    RelatedGroup    bool      `gorm:"column:related_group;default:0"`    // 本人工作相关扫描范围
    Tier            string    `gorm:"column:tier;size:8;default:cold"`   // hot|warm|cold
    Pinned          bool      `gorm:"column:pinned;default:0"`           // 强制 hot 白名单
    IncludeInMemory bool      `gorm:"column:include_in_memory;default:1"`
    IsKeyGroup      bool      `gorm:"column:is_key_group;default:0"`
    LastActiveAt    *int64    `gorm:"column:last_active_at"` // 最新消息 create_time(ms)
    CreatedAt       time.Time `gorm:"column:created_at"`
    UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (Group) TableName() string { return "feishu_group" } // 已定表名，避开保留字

// Message 消息明文（支撑表，source of truth）。
type Message struct {
    ID             uint64  `gorm:"primaryKey;autoIncrement"`
    MessageID      string  `gorm:"column:message_id;size:64;uniqueIndex:uk_message_id"` // om_ 幂等键
    ChatID         string  `gorm:"column:chat_id;size:64;index:idx_chat_create,priority:1"`
    GroupID        *uint64 `gorm:"column:group_id"` // 冗余外键，便于 join
    ChatMode       string  `gorm:"column:chat_mode;size:16"`
    SenderOpenID   string  `gorm:"column:sender_open_id;size:64;index:idx_sender"`
    SenderName     string  `gorm:"column:sender_name;size:256"`
    SenderType     string  `gorm:"column:sender_type;size:16"` // user | bot
    MessageType    string  `gorm:"column:message_type;size:32"`
    Content        string  `gorm:"column:content;type:mediumtext"`     // 人类可读渲染文本(喂 mem0/LLM)
    ContentRaw     string  `gorm:"column:content_raw;type:mediumtext"` // 原始 content JSON，保真/审计
    MentionsJSON   string  `gorm:"column:mentions_json;type:json"`
    ReplyTo        string  `gorm:"column:reply_to;size:64"`
    RootID         string  `gorm:"column:root_id;size:64"`
    ThreadID       string  `gorm:"column:thread_id;size:64;index:idx_thread"`
    CreateTime     int64   `gorm:"column:create_time;index:idx_chat_create,priority:2"` // ms
    UpdateTime     *int64  `gorm:"column:update_time"`                                  // ms，被编辑时>create_time
    Source         string  `gorm:"column:source;size:8;default:poll"`                   // poll | event
    RenderOK       bool    `gorm:"column:render_ok;default:1"`                          // 0=未知类型需人工关注
    Mem0Processed  bool    `gorm:"column:mem0_processed;default:0;index:idx_mem0,priority:1"`
    Mem0ProcessedAt *time.Time `gorm:"column:mem0_processed_at"`
    CreatedAt      time.Time  `gorm:"column:created_at"`
}

func (Message) TableName() string { return "message" }

// Checkpoint 每会话扫描高水位游标（状态，断点续扫）。
type Checkpoint struct {
    ChatID              string     `gorm:"column:chat_id;primaryKey;size:64"`
    HighWaterCreateTime int64      `gorm:"column:high_water_create_time;default:0"` // 已落库最大 create_time(ms)
    LastMessageID       string     `gorm:"column:last_message_id;size:64"`
    BackfillDone        bool       `gorm:"column:backfill_done;default:1"` // 已定不回溯：首次发现即置1(无 backfill 阶段)
    BackfillSince       *int64     `gorm:"column:backfill_since"`          // 不再使用(值=首次发现时刻快照，仅审计)；如需回溯改初始高水位
    LastScanAt          *time.Time `gorm:"column:last_scan_at"`
    LastScanStatus      string     `gorm:"column:last_scan_status;size:16"` // ok|error
    LastError           string     `gorm:"column:last_error"`
    UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (Checkpoint) TableName() string { return "chat_checkpoint" }

// Resource 附件/资源（一等实体）。
type Resource struct {
    ID              uint64    `gorm:"primaryKey;autoIncrement"`
    ResourceType    string    `gorm:"column:resource_type;size:24;index:idx_resource_type"` // image|file|audio|video|minutes|doc|link|card
    FileKey         string    `gorm:"column:file_key;size:128;uniqueIndex:uk_resource_msg_key,priority:2"`
    MinuteToken     string    `gorm:"column:minute_token;size:64"`
    DocToken        string    `gorm:"column:doc_token;size:64"`
    URL             string    `gorm:"column:url;size:1024"`
    Name            string    `gorm:"column:name;size:512"`
    MimeType        string    `gorm:"column:mime_type;size:128"`
    SizeBytes       *int64    `gorm:"column:size_bytes"`
    SourceMessageID string    `gorm:"column:source_message_id;size:64;uniqueIndex:uk_resource_msg_key,priority:1"` // om_
    GroupID         *uint64   `gorm:"column:group_id;index:idx_resource_group"`
    LocalPath       string    `gorm:"column:local_path;size:1024"`             // 同 content_hash 复用一份
    Downloaded      bool      `gorm:"column:downloaded;default:0"`             // M2 采集期恒 false
    ContentHash     string    `gorm:"column:content_hash;size:64;index:idx_resource_content"` // 内容 SHA256，下载后回填，跨消息去重
    ExtractedText   string    `gorm:"column:extracted_text;type:mediumtext"`  // 按需解析后文本(本期仅妙记逐字稿)
    CreatedAt       time.Time `gorm:"column:created_at"`
    UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (Resource) TableName() string { return "resource" }

// ScanRecord 扫描流水（一等实体，追加）。
type ScanRecord struct {
    ID              uint64     `gorm:"primaryKey;autoIncrement"`
    ScanType        string     `gorm:"column:scan_type;size:24;index:idx_scan_type_time,priority:1"` // discover|scan_hot|scan_warm|scan_cold|event（无 backfill）
    GroupID         *uint64    `gorm:"column:group_id;index:idx_scan_group_time,priority:1"`
    ChatID          string     `gorm:"column:chat_id;size:64"`
    WindowStart     *int64     `gorm:"column:window_start"` // ms
    WindowEnd       *int64     `gorm:"column:window_end"`   // ms
    FetchedCount    int        `gorm:"column:fetched_count;default:0"`
    InsertedCount   int        `gorm:"column:inserted_count;default:0"`
    PageCount       int        `gorm:"column:page_count;default:0"`
    Status          string     `gorm:"column:status;size:16;index:idx_scan_status"` // ok|partial|error
    ErrorType       string     `gorm:"column:error_type;size:64"`
    ErrorMessage    string     `gorm:"column:error_message"`
    HighWaterBefore *int64     `gorm:"column:high_water_before"`
    HighWaterAfter  *int64     `gorm:"column:high_water_after"`
    StartedAt       time.Time  `gorm:"column:started_at;index:idx_scan_group_time,priority:2"`
    FinishedAt      *time.Time `gorm:"column:finished_at"`
    DurationMs      *int       `gorm:"column:duration_ms"`
    CreatedAt       time.Time  `gorm:"column:created_at"`
}

func (ScanRecord) TableName() string { return "scan_record" }
```

**索引意图**（与总纲一致）：`uk_message_id` 幂等去重与两条链路合流核心；`idx_chat_create` 按会话 + 时间范围拉取（M3 取上下文、窗口化）；`idx_mem0` 记忆化 worker 高效拉取 `mem0_processed=0`；`idx_scan_*` 支撑扫描历史/排障/监控查询。

---

## 5. 消息内容处理

lark-cli 底层调用带 `with_sender_name=true`，因此**发送者姓名直接返回**（`sender_name`），M2 采集阶段无需再调通讯录解析 open_id→姓名；mention 的展示名也在 `mentions[].name` 内。

各类型落库策略（统一原则：`content` 存人类可读文本喂 LLM，`content_raw` 存原始 JSON 保真，**二进制只沉淀 `resource` 元数据不下载**，见 §3.9）：

| 类型 | content（渲染） | 附加处理 |
| --- | --- | --- |
| `text` | 纯文本，mention 渲染为 `@name` | `mentions_json` |
| `post`（富文本） | 标题 + 段落拍平为文本 | `content_raw` 保留结构；内嵌图片/文件 → `resource` 行 |
| `interactive`（卡片） | 卡片可读摘要（底层 `card_msg_content_type=raw_card_content`） | `content_raw` 存原始卡片 JSON；卡片内资源 → `resource` 行 |
| `image` | 占位 `[图片]` | `resource` 记 `file_key` |
| `file/audio/media/video` | 占位 `[文件:name]` | `resource` 记 `file_key` + name |
| `merge_forward`（合并转发） | 渲染摘要 | 嵌套子消息深展开留待后续，见开放问题；内嵌资源尽力沉淀 |
| `share_chat/share_user` | 渲染分享对象名 | — |
| `sticker` | 占位 `[表情]` | 不沉淀资源 |
| 含妙记/文档/外链 | 文本保留链接 | `resource` 记 `minute_token`/`doc_token`/`url` |
| 未知类型 | 尽力渲染，落 `content_raw` | `render_ok=0` 并告警（fail-fast，绝不丢弃原文） |

> 线程处理：`chat-messages-list` 内联展开 `thread_replies`，采集时需**拍平为独立行**（每条回复有自己的 `om_`），并回填 `root_id/thread_id`；去重仍靠 `message_id`。渲染阶段同时抽出资源引用列表（`[]ResourceRef`）交给 §3.9 沉淀。

---

## 6. mem0 集成（sidecar HTTP，核心）

mem0 是 Python 库、无 Go SDK。**Go 侧不直接调 mem0**，而是通过 HTTP 调用独立的 Python FastAPI **sidecar**（总纲 §5）。mem0 的窗口化/过滤/metadata 设计仍然有效，只是调用方式从「Python 进程内 `memory.add`」变为「Go `MemoryClient` HTTP 调 sidecar」。

### 6.1 进程拓扑与职责边界

```text
┌────────────────────┐   HTTP/JSON     ┌──────────────────────────────┐
│ jarvis-server (Go) │ ───────────────▶│ mem0-sidecar (Python FastAPI) │
│  Memorize worker   │                 │  薄封装 mem0.Memory            │
│  MemoryClient      │◀─────────────── │  POST /memories        (add)   │
│  (net/http)        │                 │  POST /memories/search (search)│
└────────────────────┘                 │  DELETE /memories/{id}         │
                                        │  内部 LLM 抽取用 model API      │
                                        │  ↕ Qdrant (localhost:6333)     │
                                        └──────────────────────────────┘
```

- **托管**：sidecar 由 launchd 独立托管（与 jarvis-server 平级），端口固定 `127.0.0.1:18900`（总纲开放问题 #4）。
- **fail-fast 与解耦**：Go 侧调用失败直接返回 error、不降级；sidecar 起不来 / Qdrant 不通 → `memorize` job 失败告警，但**不影响消息采集**（采集与记忆化解耦，`mem0_processed=0` 可独立重跑）。
- **LLM 抽取位置**：LLM 事实抽取在 **sidecar 内部**由 mem0 用 model API（可配置 OpenAI 兼容端点 / 本地 ollama）完成，**不经 Eino**（总纲 §6）。Go 侧只传原始 transcript + metadata，不参与抽取。
- **边界**：MySQL 存「全部原文」；mem0 存「蒸馏后的少量高价值事实/关系」。二者用 metadata 里的 `message_ids/chat_id/project_id` 互相回指。

### 6.2 mem0 架构要点（sidecar 内，基于 `mem0` Python v2.x / V3 pipeline）

sidecar 内的 mem0 行为按 2026 新版设计，**不沿用旧的外部图库写法**：

1. **单趟、ADD-only 抽取**：`add()` 一次 LLM 调用抽取所有新事实，不再有独立 UPDATE/DELETE 对比趟；哈希去重防完全重复，记忆随时间累积，靠检索期排序处理新旧。
2. **混合检索**：语义（向量）+ BM25 关键词 + 实体匹配，加性融合成单一 `score`；Qdrant 已内建 `keyword_search()`。
3. **内建实体链接取代外部图库**：Neo4j / Memgraph / Kuzu 等驱动已从 OSS 删除。实体在 `add()` 时自动抽取，存入向量库并行集合 `{collection}_entities`，检索时加权。**本方案不部署任何外部图数据库**，`enable_graph`/`graph_store` 已废弃，绝不能再写（总纲 §5）。
4. **检索默认值（V3）**：`top_k=20`、`threshold=0.1`、`rerank=False`。
5. **自托管入口**：sidecar 内 `from mem0 import Memory; Memory.from_config(config)`；依赖 `qdrant-client>=1.12.0`。

### 6.3 sidecar 接口契约（Python FastAPI 薄封装）

sidecar 直接透传 mem0，不加业务逻辑。接口与总纲 §5 对齐：

| 方法 | 路径 | 映射 mem0 | 说明 |
| --- | --- | --- | --- |
| POST | `/memories` | `mem0.add(messages, user_id, metadata, infer)` | 记忆化写入（M2 用） |
| POST | `/memories/search` | `mem0.search(query, user_id, filters, top_k, threshold, rerank)` | 检索（M3 用） |
| DELETE | `/memories/{id}` | `mem0.delete(memory_id)` | 单条删除 |
| POST | `/memories/delete_all` | `mem0.delete_all(user_id, filters)` | 批量删除（谨慎） |
| GET | `/health` | — | 存活探针（Qdrant 连通性） |

**请求/响应契约（示意）**：

```python
# sidecar/mem0/app.py  (FastAPI 薄封装，直接透传 mem0)
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from mem0 import Memory
from config import build_mem0_config, OWNER_ID   # OWNER_ID 默认 "owner"，见 §6.5

app = FastAPI()
memory = Memory.from_config(build_mem0_config())  # 启动即建连；失败即崩(fail-fast)

class AddReq(BaseModel):
    messages: str                       # 渲染好的 transcript（Go 侧窗口化）
    metadata: dict | None = None
    infer: bool = True                  # 消息蒸馏固定 True

class AddResp(BaseModel):
    results: list                       # mem0 返回的 {id, memory, event} 列表

@app.post("/memories", response_model=AddResp)
def add_memories(req: AddReq):
    # user_id 固定 OWNER_ID；不接收调用方传入，避免各模块各用各的
    res = memory.add(req.messages, user_id=OWNER_ID,
                     metadata=req.metadata, infer=req.infer)
    return {"results": res.get("results", res)}

class SearchReq(BaseModel):
    query: str
    filters: dict | None = None         # 标量等值为基线(见 §6.5)
    top_k: int = 20
    threshold: float = 0.1
    rerank: bool = False

@app.post("/memories/search")
def search_memories(req: SearchReq):
    filters = {"user_id": OWNER_ID, **(req.filters or {})}
    return memory.search(req.query, filters=filters, top_k=req.top_k,
                         threshold=req.threshold, rerank=req.rerank)

@app.delete("/memories/{memory_id}")
def delete_memory(memory_id: str):
    memory.delete(memory_id); return {"deleted": memory_id}

@app.get("/health")
def health():
    # 探测 Qdrant 连通性；不可达直接 500（fail-fast，不降级为内存）
    memory.vector_store.client.get_collections()
    return {"status": "ok"}
```

> sidecar 当前锁定 `mem0ai==2.0.12`，用 `Memory.from_config` 配置 `vector_store=qdrant`、`llm/embedder=OpenAI-compatible model API`，并以 `custom_instructions` 聚焦「交办/行动线索/决策/deadline/职责」；`embedding_model_dims` 与 embedder 维度严格对齐。Qdrant 采用 v1.18.2 Apple Silicon 原生 server + launchd（`localhost:6333`），不可达即 fail-fast；不降级为 qdrant-client 嵌入式 local mode。

### 6.4 记忆化 worker（Go 实现示意）

`memorize` job 每 10min 跑一次：拉 `mem0_processed=0`、按会话窗口化、HTTP 调 sidecar。**窗口化在 Go 侧完成**（切窗、渲染 transcript），抽取在 sidecar 内。

```go
package memory

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

const OwnerID = "owner" // 与总纲 OWNER_ID 一致；单用户系统全系统统一

// MemoryClient 对 sidecar 的薄 HTTP 封装（M2 写、M3 读共用）。
type MemoryClient struct {
    base string       // http://127.0.0.1:18900
    hc   *http.Client
}

type addReq struct {
    Messages string         `json:"messages"`
    Metadata map[string]any `json:"metadata,omitempty"`
    Infer    bool           `json:"infer"`
}

// Add 写入一个对话窗口；fail-fast：非 2xx 直接 error，不降级。
func (c *MemoryClient) Add(ctx context.Context, transcript string, meta map[string]any) error {
    body, _ := json.Marshal(addReq{Messages: transcript, Metadata: meta, Infer: true})
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/memories", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.hc.Do(req)
    if err != nil {
        return fmt.Errorf("mem0 sidecar add: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 {
        return fmt.Errorf("mem0 sidecar add: status=%d", resp.StatusCode) // fail-fast
    }
    return nil
}

// ---------- memorize job（每 10min，cron 注册见 §8） ----------
func MemorizeOnce(ctx context.Context, mc *MemoryClient, batchLimit int) error {
    // 只取 include_in_memory=1 会话的未处理消息，按会话+时间排序（走 idx_mem0）
    var rows []Message
    if err := db.WithContext(ctx).
        Where("mem0_processed = 0 AND chat_id IN (?)",
            db.Table("feishu_group").Select("chat_id").Where("include_in_memory = 1")).
        Order("chat_id, create_time").Limit(batchLimit).Find(&rows).Error; err != nil {
        return err
    }

    for chatID, msgs := range groupByChat(rows) {
        for _, window := range splitWindows(msgs, 30*time.Minute, 40) { // gap 30min 或 40 条切窗
            window = filterMeaningful(window) // 过滤 bot/表情/占位/验证码噪音
            if len(window) == 0 {
                markProcessed(ctx, idsOf(window)) // 已决策不入记忆，也置 1，避免反复扫
                continue
            }
            transcript := renderTranscript(window) // "12:03 张三: ...\n12:05 我: ..."
            meta := map[string]any{
                "source":      "message", // 与 M1 背景注入(source=background)区分
                "chat_id":     chatID,
                "chat_name":   chatNameOf(chatID),
                "message_ids": idsOf(window),
                "start_ms":    window[0].CreateTime,
                "end_ms":      window[len(window)-1].CreateTime,
                "senders":     sendersOf(window),
                // project_id 待 M1 实体归一后回填
            }
            if err := mc.Add(ctx, transcript, meta); err != nil {
                return err // fail-fast：本轮中止，未 mark 的消息下轮重试
            }
            markProcessed(ctx, idsOf(window))
        }
    }
    return nil
}
```

- **触发**：cron `memorize` 每 10min（§8）。
- **过滤**：跳过 `include_in_memory=0` 的会话、`sender_type=bot` 噪音、纯表情/占位、验证码类。被跳过的消息也置 `mem0_processed=1`（「已决策不入记忆」），避免反复扫描。
- **窗口化**：同 `chat_id` 内按 `create_time` 排序；相邻间隔 > 30min 切窗；或累计 40 条强制切窗；每窗渲染成带发言人与时间的 transcript，作为一次 `Add`。
- **为什么按窗口而非逐条**：一次 `Add`＝一次 sidecar 内 LLM 抽取，窗口化显著降调用量、且给 LLM 完整上下文（谁交办给谁、截止何时）。

> ADD-only 语义下，编辑消息重抽取会新增一条记忆（内容不同则不算重复）；旧记忆靠检索期 `score`/`created_at` 自然衰减。是否需要显式清理留作开放问题。

### 6.5 记忆统一约定（对齐总纲 §5.1）

| 约定 | 值 | 说明 |
| --- | --- | --- |
| `user_id` | 常量 `OWNER_ID`（默认 `"owner"`） | 单用户系统全系统统一；**由 sidecar 固定注入，Go 侧不传**，各模块不得各用各的 |
| scope | 只用 `user_id`，不用 `agent_id/run_id/app_id` | 避免 null-scope AND 求交返回空的坑 |
| 维度过滤 | 全放 `metadata` **标量等值** | Qdrant 后端对复杂操作符支持有限，**以标量等值为基线**；复杂 AND/OR 过滤需实测确认后才用（总纲开放问题 #5） |
| 消息蒸馏（M2） | `infer=True` 窗口化 | 让 sidecar 内 LLM 抽取事实 |
| `metadata.source` | `message`（M2）/ `background`（M1）/ `decision`（M5） | 区分来源，检索可过滤 |

> **命名对齐说明**：原 M2 文档把 `user_id` 写作 `"chujiejie"`，与总纲 `OWNER_ID`（默认 `"owner"`）不一致。本次统一改为 `OWNER_ID`（默认 `"owner"`）；最终字面值（`"owner"` vs 真实 open_id）见总纲开放问题 #7【需与用户确认】。

### 6.6 mem0 存什么 / 不存什么

**存（蒸馏事实，供 M3 提取 Todo）**：

- 交办/行动线索：谁要求谁在何时前做什么（下游 M3 据此提取 **Todo**）。
- 项目决策与状态：结论、里程碑、阻塞点、owner。
- 人物关系与职责：如「X 是项目 Y 的 PM」「Z 负责 W 模块」。
- 主人偏好与习惯：工作方式、汇报偏好。
- 关键事实：截止日期、重要链接/资源位置（可与 `resource` 行互指）。

**不存（留在 MySQL 即可）**：

- 报警群 / 机器人推送 / 监控噪音（源头用 `include_in_memory=0` 排除）。
- 寒暄、表情、纯占位消息。
- 大段富文本原文、附件二进制（只在 MySQL / `resource` / 文件系统）。
- 验证码、一次性敏感信息。

### 6.7 记忆检索接口（供 M3）

M3 通过同一个 `MemoryClient` 调 `POST /memories/search`（sidecar 固定注入 `user_id=OWNER_ID`）。metadata 过滤以**标量等值**为基线：

```go
type searchReq struct {
    Query     string         `json:"query"`
    Filters   map[string]any `json:"filters,omitempty"` // 标量等值，如 {"chat_id": "...", "source": "message"}
    TopK      int            `json:"top_k"`
    Threshold float64        `json:"threshold"`
    Rerank    bool           `json:"rerank"`
}

// Search 供 M3 召回相关记忆；rerank 默认关，召回不足时按需开启(+150~200ms)。
func (c *MemoryClient) Search(ctx context.Context, query string, filters map[string]any) ([]MemoryHit, error) {
    body, _ := json.Marshal(searchReq{Query: query, Filters: filters, TopK: 20, Threshold: 0.1, Rerank: false})
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/memories/search", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.hc.Do(req)
    if err != nil {
        return nil, fmt.Errorf("mem0 sidecar search: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 {
        return nil, fmt.Errorf("mem0 sidecar search: status=%d", resp.StatusCode)
    }
    var out struct{ Results []MemoryHit `json:"results"` }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return out.Results, nil
}
```

M3 典型用法：拿到候选消息/项目 → `Search("最近 leader 交办的事项", map[string]any{"project_id": pid})` 召回相关记忆 → 结合 MySQL 原文提取 **Todo（行动线索）**。

> **明文密钥**：本地可信环境按项目约定，sidecar 直接读环境变量中的明文 model API key（仅提示密钥风险即可）。

---

## 7. 可选 bot 事件增强（低延迟）

**定位**：仅对「已把 bot 拉进的关键群」做低延迟捕获，是轮询的**增强**而非替代（覆盖面受 `im:message.p2p_msg:readonly` + bot-only 限制）。

**事件字段**（`lark-cli event schema im.message.receive_v1`）：`message_id`(om_，幂等键)、`chat_id`、`chat_type`、`content`（多数类型已渲染为可读文本）、`create_time`、`sender_id`、`sender_type`、`message_type`、`mentions[]`、`reply_to/root_id/thread_id`、`update_time`。注意：`event_id` 是投递 ID，**不可**用作去重键，去重一律用 `message_id`。

**架构（Go 长驻子进程 + `exec.Command`）**：

```text
Supervisor (Go goroutine)
  ├─ exec.CommandContext: lark-cli event consume im.message.receive_v1 --as bot  (NDJSON→stdout)
  ├─ bufio.Scanner 读 stderr 的 ready marker，就绪后才计入健康
  ├─ bufio.Scanner 逐行解析 stdout → 规整成 Message 形状 → upsertMessage(source="event")
  │     · 同一 om_ 与轮询天然去重合流（clause.OnConflict）
  │     · 只插入，绝不推进 checkpoint 高水位（游标归轮询独占）
  │     · 事件路径也记 scan_type=event 的 scan_record（可选，便于观测事件流量）
  ├─ 优雅退出：ctx cancel → SIGTERM 转发子进程 → drain 剩余行 → flush → 退出
  └─ 子进程异常退出：指数退避重启；连续失败告警(fail-fast)，不静默吞
```

**与轮询合流的正确性**：事件只做「额外的幂等插入」，轮询独占游标推进。即使事件乱序、重复或漏投，轮询都会从 checkpoint 补齐；`message_id` 唯一键保证不重。这样事件带来「低延迟」，轮询保证「不漏」，职责清晰、无空洞。

> 本方案阶段只做设计；按约束**未真正 `consume`**（避免注册服务端订阅），仅读取了 schema 与 help。上线前需确认：把 bot 拉进哪些关键群、bot 应用是否已在控制台订阅该事件。

---

## 8. 调度（robfig/cron v3）

用 `github.com/robfig/cron/v3` 替代原 APScheduler，进程内单实例调度。

| Job | 触发（cron spec） | 职责 | 关键约束 |
| --- | --- | --- | --- |
| `discover` | `@every 1h` | 枚举会话、刷新元数据与 tier，记 scan_record | 同 job 不并发 |
| `scan_hot` | `@every 5m` | 扫 HOT + pinned | 同 job 不并发 |
| `scan_warm` | `@every 30m` | 扫 WARM | 同 job 不并发 |
| `scan_cold` | `@every 6h` | 扫 COLD 兜底补漏 | 同 job 不并发 |
| `memorize` | `@every 10m` | 拉 `mem0_processed=0` 经 sidecar 记忆化 | 同 job 不并发 |

- **同 job 不并发**：robfig/cron 默认允许同一 entry 的上一次未跑完时下一次照常触发；用 `cron.SkipIfStillRunning`（长扫描撞上下一轮时跳过本次）包裹，等价于原 APScheduler 的 `max_instances=1`。
- 全局共享 `TokenBucket`，把所有扫描 job 的总 QPS 压在租户限额内。
- 事件 daemon（§7）不进 cron 定时体系，作为独立长驻 goroutine + 子进程由 Supervisor 管理、自愈。

```go
package pipeline

import (
    "context"

    "github.com/robfig/cron/v3"
)

func RegisterCron(ctx context.Context) *cron.Cron {
    // SkipIfStillRunning：同一 job 上轮未完成则跳过本轮（等价 max_instances=1 + coalesce）
    logger := cron.VerbosePrintfLogger(stdLogger)
    c := cron.New(cron.WithChain(
        cron.SkipIfStillRunning(logger),
        cron.Recover(logger), // panic 不打崩调度器；但业务内仍 fail-fast 返回 error 并告警
    ))

    mustAdd(c, "@every 1h",  func() { must(DiscoverChats(ctx)) })
    mustAdd(c, "@every 5m",  func() { ScanTier(ctx, "hot") })
    mustAdd(c, "@every 30m", func() { ScanTier(ctx, "warm") })
    mustAdd(c, "@every 6h",  func() { ScanTier(ctx, "cold") })
    mustAdd(c, "@every 10m", func() { must(MemorizeOnce(ctx, memClient, 500)) })

    c.Start()
    return c
}

func mustAdd(c *cron.Cron, spec string, fn func()) {
    if _, err := c.AddFunc(spec, fn); err != nil {
        panic(err) // 注册期错误 fail-fast，启动即暴露
    }
}
```

> `@every` 是 robfig/cron 的固定间隔语法（等价原 interval job）；若要「整点触发」可改标准 cron 表达式（如 `0 * * * *`）。`must(...)` 把 job error 上抛告警，不静默吞（fail-fast）；`cron.Recover` 只防单次 panic 打崩调度器，不掩盖业务错误。

---

## 9. 限流与性能分析

**采集侧（飞书 QPS）**：

- 每页 `chat-messages-list` = 1 次 API（`--no-reactions` 时不额外触发 reaction 批查）。
- 稳态增量只扫描动态选择的 `related_group`。按当前 20 个全为 HOT、每次各 1 页估算：~20 req/5min ≈ 0.067 req/s，远低于保守令牌桶 `R=5/s`；名单变化后需按实际数量重估。
- **无首次 backfill 峰值**（已定不回溯，总纲 §11.3）：新接入会话首次发现即以当前时刻建高水位，只增量拉新消息，不存在一次性拉海量历史的负载尖峰。稳态负载即上面的增量量级。
- `discover` 每 1h 只做元数据全量分页；当前账号实测 4,891 个可见会话 / 每页 100，约 49 次请求/小时，不触发消息拉取。
- Go 子进程开销：每次 `exec.Command` 拉起 lark-cli 有进程启动开销（几十 ms 量级），并发信号量 + 令牌桶已把总量压住；相比 API 往返可忽略。

**记忆化侧（LLM，在 sidecar 内）**：

- 1 窗口 = 1 次 HTTP 调 sidecar = 1 次 mem0 `add`（ADD-only 单趟 LLM）。窗口化（≤40 条/窗、30min 断点）显著降调用量。
- LLM 由 model API（可配置 ollama / 云网关）完成：本地 ollama 无外部费用，瓶颈是 CPU/GPU 与延迟；云网关按 token 计费。窗口数量级 = 有效对话片段/天，需按真实话务量估（开放问题）。
- Go↔sidecar 是本机 loopback HTTP，网络开销可忽略；瓶颈仍是 sidecar 内 LLM 抽取。

**检索侧（Qdrant，经 sidecar）**：

- 混合检索 ~100–150ms，`rerank` 再 +150–200ms（默认关）。本地单机量级下无压力。

---

## 10. 开放问题清单（需与用户确认）

> **本轮已定（不再列为开放问题）**：
> - **backfill 不回溯**：首次发现该会话即以当前时刻建高水位，不拉历史（§3.4，总纲 §11.3）。
> - **Resource 下载/解析**：采集期不下载不 OCR；按需下载/解析**仅妙记**，由下游触发（§3.9.1，总纲 §11.4）。
> - **Resource 跨消息去重**：靠 `content_hash`（内容 SHA256，下载后回填）；不给 file_key 等加唯一索引（§3.9）。
> - **`group` 表名**：改 `feishu_group`（总纲 §11.1）。
> - 早前已定：Neo4j/外部图库=否；Python→Go；LLM/embedder=model API 可配置。

1. **噪音会话白/黑名单**：哪些群属报警/机器人噪音（如 `[Critical]agency平台报警群` 等），应 `include_in_memory=0`？
2. **关键会话白名单**：leader 的 open_id、核心项目群，需 `pinned=1` / `is_key_group=1` 强制 HOT。
3. **飞书租户实际 QPS 限额**：用于设定令牌桶 `R`，当前保守取 5/s。
4. **编辑消息是否重抽记忆**：默认「是」（重置 `mem0_processed`），确认是否接受由此产生的记忆累积。
5. **妙记按需拉取可行性**：`lark-cli minutes` 取逐字稿的具体子命令、权限/授权范围需实测（总纲 §11.5）。
6. **`message.resources_json` 去留**：附件已升 `resource` 实体，是否仍保留该冗余字段作快照？
7. **p2p 与 external 会话是否纳入**：涉及隐私边界，是否全纳入采集与记忆。
8. **mem0 记忆保留/清理策略**：ADD-only 会持续累积，是否需要周期性清理或保留期。
9. **scan_record 保留期**：流水表持续增长，多久归档/清理？
10. **合并转发深展开**：`merge_forward` 嵌套子消息是否需要递归拆解为独立行 + 沉淀内嵌 resource。
11. **thread 旧根新回复**：纯 create_time 增量可能漏「旧根消息的新回复」，是否需为重线程群加深扫/事件流（§3.10）。

---

## 11. 依赖与版本

| 组件 | 版本 | 备注 |
| --- | --- | --- |
| Go | 1.26 | 本机 `go1.26.4` |
| Hertz | `v0.10.5`（`github.com/cloudwego/hertz`） | 管理后台/内部 REST；M2 主要是被 M0 编排调用，采集/记忆化本身不依赖 HTTP 框架 |
| GORM | `gorm.io/gorm` + MySQL driver | 消息/游标/资源/流水落库；不引入 bytedgorm |
| robfig/cron | `github.com/robfig/cron/v3` | 分层扫描 + 记忆化定时（替代 APScheduler） |
| lark-cli | 本机实测版本 | 经 Go `exec.Command` 子进程调用；`im +chat-list/+chat-messages-list --as user`、`event schema` 已核 |
| mem0（sidecar） | Python `mem0` v2.x（V3 pipeline） | 独立 FastAPI 进程 `127.0.0.1:18900`；ADD-only + 内建实体链接，**勿配外部图库** |
| qdrant-client | `>=1.12.0` | sidecar 内 mem0 要求 |
| Qdrant | `v1.18.2` | Apple Silicon 原生 server + launchd，`localhost:6333`；安装包 SHA256 固定 |
| MySQL | 8.x | InnoDB / utf8mb4 |
| model API | OpenAI 兼容端点（可配置） | sidecar 内 LLM 抽取，不经 Eino |

> 已核对项：Hertz `v0.10.5`（2026-06-11 发布）与 `github.com/robfig/cron/v3` 均为真实可用版本；`chat-list` 真实返回含 `chat_id/chat_mode/name/owner_id/external/tenant_key` + `has_more/page_token`；`chat-messages-list` 底层 `GET /im/v1/messages`（`with_sender_name=true`、`sort_type=ByCreateTimeAsc`、`only_thread_root_messages=true`、reaction 批量富化，可 `--no-reactions` 关闭）；事件 `im.message.receive_v1` 为 bot 授权、`message_id`(om_) 为推荐幂等键。

### 11.1 实现期实测补充（2026-07-19）

- 当前 `lark-cli 1.0.72` 的 `chat_mode` 除 `group` / `p2p` 外会真实返回 `topic`；M2 将其作为话题群会话正常建模。
- `chat-messages-list --format json` 的 `create_time` / `update_time` 是本地时区的 `YYYY-MM-DD HH:mm` 字符串，不是裸毫秒。Go 侧按配置的 `capture.timezone` fail-fast 解析后，以毫秒 `BIGINT` 落库。
- lark-cli 业务错误可能使用 `{ "ok": false, "error": ... }` 且进程退出码仍为 0；统一子进程层同时校验退出码、JSON 合法性和 `ok` 字段。
- 当前高层命令只返回渲染后的 `content`，不返回底层原始 content JSON，因此 `message.content_raw` 保持 NULL，不用渲染文本伪装原始 JSON。
