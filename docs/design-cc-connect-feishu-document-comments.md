# CC Connect 支持飞书文档评论

> Status: proposal
> Authority: non-normative
> Last verified: 2026-08-05 @ `b0aeaab`（评审后修订：事件字段分层、reaction ack、不支持形态可见拒绝、订阅粒度待实验）

## 1. 结论

这个能力应当实现为 **cc-connect 的 Feishu 平台新交互表面**，不进入 Jarvis 的 M2 → M3 → M5 流水线，也不新起 `lark-cli event consume` 进程。

第一阶段只闭环一件事：principal 在新版飞书文档的**局部评论**中 `@Jarvis Bot`，cc-connect 使用当前 Feishu App 已有的长连接收到 `drive.notice.comment_add_v1`，读取该评论卡片的完整讨论，将其交给原有 Agent/Session 引擎，并把最终回答写回同一评论卡片。

核心决策：

- 长连接仍只有 cc-connect 一个所有者；Jarvis 不保留 Feishu event consumer。
- 评论事件直接接入 cc-connect 当前 Feishu WebSocket dispatcher，不部署 sidecar。
- 业务逻辑落在 cc-connect 而不是 Jarvis，是因为要复用 cc-connect 现成的 Agent 与 Session 引擎；Jarvis 侧没有等价的对话执行器。「谁拥有长连接」只排除了 sidecar，并不单独构成这个选择的理由。
- 每一张评论卡片对应一个独立 Agent Session，避免同一文档的不同评论互相污染。
- 第一阶段严格要求 `@Bot`，并继续复用项目现有 `allow_from` 权限。
- 只回写最终文本，不把思考过程、工具调用或流式中间态刷进评论；执行期间的唯一反馈是评论 reaction。
- 不支持的评论形态给出可见拒绝，不静默丢弃事件。
- 不自动把整篇文档塞入上下文；提供评论、定位和文档引用，Agent 需要时自己用工具查正文。

### 1.1 本轮评审引入的决策

以下三项在初稿中是空缺或相反的取值，现按「fail-fast + 失败对用户可见」定下，如需推翻请直接改本节并同步下文：

1. `event_id` 只从事件 `header` 读取；为空即 fail-fast，不退化用 `comment_id + reply_id` 拼去重键。
2. 采用评论 reaction 作为执行中的 ack（收到即加、回复完成即删），这是评论表面唯一低噪音的进度信号。
3. 不支持的评论形态（全文评论、已解决评论）不再静默忽略，而是新建一条全文评论说明「暂不支持，请改用局部评论」。这是可见的拒绝，不是替代执行的 fallback。

## 2. 目标与非目标

### 2.1 第一阶段目标

用户体验：

1. 用户在 `docx` 中选中文本，新建一条局部评论并 `@Jarvis Bot`。
2. Jarvis 读取评论落点、评论卡片内已有讨论和文档基本信息。
3. Jarvis 执行与飞书私聊相同的 cc-connect Agent。
4. 最终答案出现在原评论卡片中。
5. 用户在同一评论卡片再次 `@Jarvis Bot` 时，续跑同一个 cc-connect Session。

### 2.2 第一阶段不做

- 不接入 Jarvis Todo/Task 流水线，不新增 Jarvis 表、状态或 API。
- 不监听未 `@Bot` 的全部评论。
- 不支持全文评论、已解决评论、旧版 `doc`、Sheet、Slides、Base 或普通 Drive 文件。
- 不支持评论图片、附件、卡片和流式过程消息；reaction 只用于 ack，不做用户表情交互。
- 不自动修改文档正文；Agent 仍按现有权限与审批策略决定是否调用工具。
- 不承诺进程崩溃后的事件 exactly-once；可靠性边界与当前 cc-connect IM 事件一致。

两条会直接影响用户预期的限制必须写在产品说明里，而不只是实现细节：

- **适用文档范围有限**。能力只在 Jarvis Bot 已获得访问权的文档上生效；对于普通用户创建的存量文档，需要逐篇通过“添加文档应用”授予 Jarvis 访问权（见 §9.1 实测结论）。
- **回复以纯文本呈现**。飞书评论内容只支持 `text_run` / `docs_link` / `person`，Agent 输出的 Markdown 不会被渲染。第一阶段不做 Markdown 转换，改为在 Agent 输入里要求纯文本短回答（见 §6）。

全文评论和已解决评论在飞书 API 中不能向原卡片追加回复。第一阶段不采用“转私聊”或“另建评论继续执行任务”作为隐式 fallback，但也不静默丢弃：这类事件不启动 Agent，并按 §8.2 回一条可见的拒绝说明。

## 3. 当前事实基线

截至 2026-08-05：

- 本机运行的是 `cc-connect v1.4.1`；Jarvis cc-connect 项目使用的 Feishu App/profile 由各机器配置，不在设计文档中固化个人实例值。
- 实时事件连接由 cc-connect 独占；Jarvis 不再保存同 app 的 event profile 或启停开关，这个边界必须保持。
- cc-connect 当前 Feishu adapter 已在同 App 的多个项目间共享一条 WebSocket，并将 IM 事件 fan-out 给 sibling platform；当前没有注册 `drive.notice.comment_add_v1`。
- 飞书官方事件列表包含 `drive.notice.comment_add_v1`，含“新增评论”和“新增回复”通知。事件体字段为 `comment_id`、`reply_id`、`is_mentioned` 与 `notice_meta.{file_token,file_type,notice_type,from_user_id,to_user_id}`；`event_id` 与 `create_time` 在 `header` 里，不在事件体里。
- Bot 身份的用户云文档事件订阅通过 `drive user subscription` 完成，状态用 `drive user subscription_status --event-type` 查询，粒度是「用户云文档事件」而非单篇文件。文件级的 `is_subscribe` 是另一组接口，两者不要混用。截至目前当前 App 的 Bot 身份尚未订阅。
- 局部评论回复是官方接口 `POST /open-apis/drive/v1/files/:file_token/comments/:comment_id/replies`（lark-cli `drive file.comment.replys create`），支持 Bot 身份，写入目标由 `file_token + file_type + comment_id` 唯一确定。
- 评论 reaction 是官方接口 `drive file.comment.reply.reactions update_reaction`，按 `reply_id` 加减表情。
- 全文评论没有对应的「向原卡片追加回复」接口，只能新建评论；已有的同类实现也是这样分流的。

飞书侧参考：

- [事件列表：添加评论、回复通知](https://open.feishu.cn/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-list)
- [事件详情：drive.notice.comment_add_v1](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/notice/events/comment_add)
- [事件订阅概述](https://open.feishu.cn/document/server-docs/event-subscription-guide/overview?lang=zh-CN)
- [订阅云文档事件](https://open.feishu.cn/document/server-docs/docs/drive-v1/event/subscribe?lang=zh-CN)
- [官方用例：Agent 回复文档评论](https://open.larkoffice.com/document/mcp_open_tools/feishu-cli/use-cases/agent-replies-to-doc-comments)

## 4. 总体架构

```mermaid
flowchart LR
    C["飞书局部评论<br/>@Jarvis"] --> E["drive.notice.comment_add_v1"]
    E --> WS["cc-connect 已有 Feishu WebSocket"]
    WS --> F["评论事件过滤与去重"]
    F --> Q["读取评论卡片与文档元数据"]
    Q --> M["构造 core.Message<br/>评论级 SessionKey"]
    M --> A["cc-connect 原有 Agent Engine"]
    A --> O["仅最终文本"]
    O --> R["回复原评论卡片"]
```

这里没有新增 Jarvis 后端组件。对 cc-connect core 而言，评论和 IM 都是 `core.Message`；差异只由 Feishu adapter 负责：如何收事件、如何构造上下文、如何把最终结果送回原表面。

### 4.1 为什么不能另起监听进程

飞书同一个 App 的多条长连接是集群消费，不是广播。同一事件只会落到其中一个连接。若再运行一个 `lark-cli event consume drive.notice.comment_add_v1`，事件可能被 sidecar 或 cc-connect 随机拿走，无法保证触发 Agent。

因此唯一正确的接入点是 cc-connect 当前 WebSocket 的 `EventDispatcher`。若未来需要让 Jarvis 流水线也消费同一 App 的事件，应由 cc-connect 作为唯一连接所有者再做本机 fan-out，而不是恢复第二条长连接。

## 5. 入站协议与过滤

### 5.1 事件接入

在 Feishu dispatcher 上注册 custom event：

```text
drive.notice.comment_add_v1
```

adapter 只投影程序硬消费的字段，同时保留原始事件供 debug。注意字段分属两层，取错层会静默拿到空串：

```text
header.event_id          # 去重键，不在 event 里
header.create_time       # 事件时间

event.comment_id
event.reply_id
event.is_mentioned
event.notice_meta.file_token
event.notice_meta.file_type
event.notice_meta.notice_type
event.notice_meta.from_user_id
event.notice_meta.to_user_id
```

`event_id` 必须从 `header` 读。已有的同类实现从事件体读 `event_id`，实际长期取到空值、去重形同虚设；这里明确不重复该错误。

不把这个结构提升成 cc-connect core 的公共评论 DTO；它只属于 Feishu adapter。

### 5.2 触发条件

按以下顺序 fail-fast 过滤：

1. 事件字段完整，`header.event_id`、`comment_id`、`file_token` 非空。`event_id` 为空视为协议异常，记 error 并丢弃，不猜测替代去重键。
2. `notice_type` 是 `add_comment` 或 `add_reply`。
3. `file_type=docx`。
4. `is_mentioned=true`。
5. `to_user_id.open_id` 必须存在且等于当前 Bot open_id。缺失即丢弃，不放行。
6. `from_user_id` 不能是 Bot 自己，避免回复触发自身形成循环。
7. 发送者必须通过该 cc-connect 项目的既有 `allow_from`。
8. 评论必须是未解决的局部评论，能够向原卡片追加 reply。

前 7 项只看事件本身，可以在任何 API 调用之前判完；任一不满足都不启动 Agent，也不给用户任何回复——这些事件要么不是给本 Bot 的，要么发起人无权使用，静默是正确行为。

第 8 项需要 §6 的评论查询结果（`is_whole`、`is_solved`）才能判定，因此发生在 reaction ack 之后。它也不启动 Agent，但事件确实是 principal `@Bot` 的，只是形态不支持，按 §8.2 给可见拒绝。

日志必须带结构化 `ignore_reason`，但默认不记录评论正文。

### 5.3 同 App 多项目路由

cc-connect 允许多个项目共享同一 Feishu App。IM 可以继续按 chat/user 过滤，但评论事件没有 `chat_id`，无法可靠判断应落到哪个项目。

新增平台配置：

```toml
[projects.platforms.options]
document_comments = true
```

硬约束：同一个 `app_id` 最多只能有一个 sibling platform 开启 `document_comments`。配置冲突时启动失败并报出冲突项目名，不进行广播、不选第一个、不静默 fallback。

Jarvis 项目开启后继续复用现有：

```toml
allow_from = "<principal open_id>"
```

第一阶段 `@Bot` 是固定产品语义，不增加 `require_comment_mention=false` 之类的扩展选项。

## 6. 评论读取与上下文组装

事件只负责通知，不能把它当成完整评论正文。收到事件后：

1. 按 `file_token + file_type + comment_id` 查询目标评论卡片。
2. 拉取该卡片全部 replies；如果 `has_more=true`，继续分页。
3. 用 `reply_id` 标记本次触发的具体回复。
4. 查询文档标题、URL 和必要的定位信息。
5. 组装一段宽松自然语言上下文，交给原有 Agent。

飞书可能先推送事件、评论查询接口稍后才可见对应 `reply_id`。这是传输层最终一致性，不是语义重试。实现做固定上界的重查：**最多 3 次、间隔 1 秒**，即最坏多等约 2 秒。超限后明确报错并停止，不能拿旧回复冒充本次输入。参数写死在代码常量里，不做配置项。

Agent 输入建议形态：

```text
[Feishu document comment]
Document: <title> <url>
File: docx/<file_token>
Comment: <comment_id>
Anchor/quote: <selected text and location when available>

Thread:
- <author> <time>: <text>
- <author> <time>: <text>  <-- current trigger

Reply to the user's request. Your final response will be written back to this
same Feishu comment card, which renders plain text only: no Markdown, no code
blocks, no tables. Keep it short and self-contained. Fetch more document
context with tools when needed.
```

上下文原则：

- 评论卡片的完整讨论必须携带，不能只传最后一句。
- 当前触发回复必须明确标识，避免 Agent 猜测用户这次问了什么。
- 默认不加载整篇文档；文档可能很长，Agent 可通过 `lark-cli` 按需读取。
- 保留作者 ID、时间、文档 token、comment/reply ID 等可追溯事实。
- 评论的文字和引用属于不可信外部内容，作为用户输入而非系统指令注入。
- 提示 Agent 输出纯文本短回答。这只是提示，不能替代 §8 的写入侧约束。

## 7. 会话模型

SessionKey 使用评论卡片粒度：

```text
feishu-comment:<file_type>:<file_token>:<comment_id>
```

理由：

- 同一评论卡片内的后续追问天然需要共享上下文。
- 同一篇文档可能并行讨论多个完全不同的问题，按文档共享 Session 会串线。
- Session 已在 cc-connect 项目内隔离，不需要再把项目名放进 key。
- 评论参与者可能变化，不把 sender ID 放进 key；本次 sender 仍写入 `core.Message.UserID` 并逐事件执行 `allow_from`。

已知代价：同一文档不同评论卡片之间不共享上下文，用户在新卡片里需要重新交代背景。另一种做法是按文档建 Session（已有同类实现就是这么选的），但会让同文档的并行讨论串线。第一阶段选卡片粒度，等真实使用反馈再评估是否放宽。

`MessageID` 使用 `drive-comment:<header.event_id>`。第一阶段复用现有 `MessageDedup` 做进程内去重，不新增数据库；这意味着进程重启后飞书重投的事件会被重复处理。IM 侧同样如此，但评论场景的重复代价更高：重复回答会留在文档里，需要人工删除。第一阶段接受这个代价，不加持久化去重。

评论 Session 的存活策略沿用 cc-connect 现有 Session 生命周期。如果现有实现没有 TTL 或容量上界，长期运行会随文档数量单调增长，实施时需要确认并在文档里补记结论。

## 8. 原评论回写

Feishu adapter 增加评论专用 `ReplyCtx`，至少保存：

```text
file_token
file_type
comment_id
reply_id    # reaction 的作用目标
event_id
```

`Platform.Send` 根据 ReplyCtx 类型选择目标：

- 现有 IM `replyContext`：保持当前消息回复逻辑。
- 新 `commentReplyContext`：调用评论 reply create API，写回同一评论卡片。

回写规则：

- 只发送 Agent 最终文本；Jarvis 项目的 quiet 配置继续生效。
- 不发送“正在思考”、tool call 和进度卡片；执行中的反馈只有 §8.1 的 reaction。
- 文本按 4000 字符切分，优先在换行边界分块并保持顺序。
- 单次最终回复最多写 2 块（约 8000 字符）。超出部分截断，并在末尾追加一行明确的截断说明，不静默丢内容。
- 写入前按 `&` → `<` → `>` 的顺序做实体转义。先转 `&` 是必需的，否则会二次转义。
- 任一分块失败即返回明确错误并停止后续分块，不伪装完成。
- 局部评论回帖失败（含 `1069302`）不自动改走新建全文评论；这是错误，不是可替代路径。
- 不自动改用 IM 或其他表面。

### 8.1 执行中的 ack

评论表面没有 typing 指示，而一次 Agent 执行常常几十秒到几分钟。没有任何反馈时用户会认为没生效并重复 `@`。

因此使用评论 reaction 作为唯一 ack：

- 在 §5.2 前 7 项过滤通过后立即添加，早于评论查询与 Agent 启动。
- 通过 `drive file.comment.reply.reactions update_reaction` 对触发事件的 `reply_id` 添加 `OK`。
- 最终回复写入完成（成功或失败）后移除该 reaction。
- reaction 的添加与移除都是 best-effort：失败只记日志，不影响主流程，也不阻塞 Agent 启动。

这是刻意选择的最低噪音方案，不引入“正在处理”文本回复。

### 8.2 不支持形态的可见拒绝

命中 §5.2 第 8 项（全文评论、已解决评论）时，不启动 Agent，但要让用户看见：新建一条全文评论，说明该形态暂不支持、请改用局部评论。

约束：

- 拒绝文案是固定字符串，不经过 Agent。
- 同一 `comment_id` 的重复触发只拒绝一次，避免刷屏；进程内记录即可。
- 拒绝写入失败只记 `comment_reply_failed`，不重试、不换表面。

## 9. 飞书侧一次性配置

实施需要同时完成三层配置，缺一不可：

1. **应用权限**：Bot 身份至少具备评论读取、评论创建/回复和文档元数据读取所需权限；以实际 API 返回为验收，不只看控制台勾选。
2. **事件配置**：开发者后台使用长连接接收事件，并添加 v2.0 `drive.notice.comment_add_v1`。
3. **订阅与文档权限**：以 Bot 身份订阅 `drive.notice.comment_add_v1`；目标文档通过“更多 → 添加文档应用”加入 Jarvis，使应用对该文档具备接收通知和读写评论所需权限。

一次性订阅可用当前 CLI 明确执行：

```bash
lark-cli --profile <jarvis_lark_profile> \
  drive user subscription \
  --data '{"event_type":"drive.notice.comment_add_v1"}' \
  --as bot --format json
```

订阅状态用 `drive user subscription_status --event-type drive.notice.comment_add_v1 --as bot` 复核。这是用户级订阅，和文件级的 `is_subscribe` 不是一回事，排障时不要互相引用。

cc-connect 不在每次启动时静默修改飞书订阅状态。

### 9.1 订阅粒度对照实验结论

2026-08-05 的真实链路对照结果如下：

- Jarvis Bot 有访问权的 `docx`：principal 在局部评论中 `@Jarvis Bot` 后，长连接收到 `drive.notice.comment_add_v1`，cc-connect 能读取评论、添加/移除 `OK` reaction，并在原评论卡片回复；同卡片再次 @ 时复用同一 Session。
- principal 通过同一应用的用户身份创建、但未执行“添加文档应用”的 `docx`：评论事件未投递给 Bot；以 Bot 身份调用评论列表接口返回 Feishu `1069303 forbidden`。

因此，用户级 `drive.notice.comment_add_v1` 订阅不等于获得所有文档的访问权。第一阶段按明确边界交付：普通用户创建的目标文档需要逐篇添加 Jarvis 文档应用；cc-connect 遇到无权文档时不降级到私聊，也不尝试绕过权限。

## 10. cc-connect 代码改动范围

建议保持改动集中在 Feishu adapter：

| 位置 | 改动 |
|---|---|
| `platform/feishu/feishu.go` | 读取 `document_comments`；在已有 dispatcher 注册 custom event；共享 WS 内只路由到唯一启用项目；`Send` 识别评论 ReplyCtx |
| `platform/feishu/comment.go` | Feishu 私有事件结构、过滤、评论读取、上下文组装、SessionKey、评论回写、reaction ack、不支持形态的拒绝文案 |
| `platform/feishu/comment_test.go` | 单元测试与 fake API 测试 |
| cc-connect Feishu 配置文档 | 选项、权限、事件订阅、局部评论限制与验收步骤 |

预计不需要修改：

- cc-connect Agent engine 和 Session store；
- Jarvis Go 服务、数据库、M2/M3/M5；
- Jarvis prompts；
- 独立事件守护进程。

如果实现中发现必须修改 core 公共协议，先暂停并重新论证；当前 `core.Message + ReplyCtx + SessionKey` 已足以承载 MVP。

## 11. 错误、并发与可观测性

### 11.1 并发

- 同一个评论 SessionKey 复用 cc-connect 现有 session busy/串行行为。
- 不同评论卡片可以并行执行。
- 同一评论中短时间连续出现多个 @ 事件时，以 event_id 去重并按会话顺序处理，不合并用户原文。

实施前必须先确认 cc-connect 现有 busy 语义到底是排队还是丢弃，并把结论写回本节。这在评论表面比 IM 严重：IM 里用户至少能看到提示，评论卡片上什么都没有。如果现有行为是丢弃，则被丢弃的事件必须给用户可见反馈（复用 §8.1 的 reaction 或 §8.2 的固定文案），不能静默。

### 11.2 日志

至少记录以下结构化事件：

```text
comment_event_received
comment_event_ignored     reason=<...>
comment_agent_dispatched
comment_reply_sent
comment_reply_failed
```

公共字段：`event_id`、`comment_id`、`file_type`、sender、project、duration。正常日志不记录评论正文、文档正文和密钥。

### 11.3 Fail-fast 边界

- 配置冲突、事件字段缺失、评论查询失败、评论状态不支持、reply 写入失败都返回真实错误。
- `header.event_id` 为空按协议异常处理：记 error、丢弃事件，不用其它字段拼去重键。
- 不用字符串匹配把“没权限、未订阅、数据尚未可见”压成同一种错误。
- 只对“事件已到但指定 reply 暂不可见”做传输层短重查（3 次 / 间隔 1 秒）；权限和参数错误不重试。
- unsupported 事件不启动 Agent，也不寻找替代评论；只按 §8.2 发一条固定拒绝文案。

## 12. 测试与验收

### 12.1 自动化测试

必须覆盖：

- custom event JSON 解析与必填字段缺失。
- `event_id` 从 `header` 读取；事件体里带同名字段时不会被误用。
- `header.event_id` 为空时丢弃事件，且不回退到其它去重键。
- `is_mentioned=false`、@其他人、`to_user_id` 缺失、自身回复、非 allow_from、非 docx 的过滤。
- 同 App 多个 `document_comments=true` 启动失败。
- event_id 去重。
- 不同评论生成不同 SessionKey；同一评论后续回复复用 SessionKey。
- 评论卡片分页、`reply_id` 定位和短暂延迟可见（含重查 3 次后仍不可见时报错）。
- 全文评论、已解决评论走 §8.2 拒绝路径，不启动 Agent，且同一 comment_id 只拒绝一次。
- ReplyCtx 正确路由到评论 API，不影响现有 IM ReplyCtx。
- 文本转义（`&` 先于 `<` `>`，无二次转义）、分块顺序、超过 2 块时截断并标注、中途失败行为。
- reaction 添加与移除被调用，且其失败不影响 Agent 执行和最终回复。
- Bot 自己回写产生的事件不会再次启动 Agent。

至少运行：

```bash
go test ./platform/feishu/...
go test ./core/...
go test ./...
```

### 12.2 真实飞书验收

在专用测试 `docx` 上逐项验证：

1. 选中文本，新建局部评论 `@Jarvis Bot 请解释这里`。
2. 事件到达后评论上很快出现 `OK` reaction，最终回复写入后 reaction 消失。
3. cc-connect 只启动一次 Agent，最终答案进入同一评论卡片。
4. 在同一卡片再次 `@Jarvis Bot 再简短一点`，续跑同一 Session。
5. 在另一处新建评论，得到不同 Session，不携带上一评论对话。
6. 全文评论里 `@Jarvis Bot`，得到固定的「暂不支持」说明，且重复 @ 不刷屏。
7. 不 @Bot 的评论不触发。
8. 非 principal 用户 @Bot 不触发。
9. 未「添加文档应用」的文档按 §9.1 实验结论表现一致。
10. Bot 回写不形成循环。
11. 重复投递同一 event_id 不重复回答。
12. 私聊和群聊原有 cc-connect 行为不回归。

完成标准不是“日志收到事件”，而是“真实评论 @ → 出现 reaction → Agent 执行 → 原卡片出现最终回复 → 同卡片追问续上上下文”。

## 13. 发布与回滚

建议双轨交付：

1. 在 cc-connect 源码上实现通用 Feishu 评论能力并向 upstream main 提 PR。
2. 本机先基于当前稳定版 `v1.4.1` 做最小补丁构建，版本标识带 commit/本地后缀，避免把 beta 的其他变化一起带入生产。

前置条件：本机目前只有二进制（`~/.local/bin/cc-connect`，`v1.4.1` / commit `5d4c96dd`），没有源码树。开工前必须先确定源码来源并把工作副本固定到 `5d4c96dd`，否则「基于 v1.4.1 打最小补丁」不成立，会退化成基于 main 构建、把无关变更一起带进生产。

本机发布步骤：

1. 备份当前 cc-connect 二进制、版本信息和 `~/.cc-connect/config.toml`，记录 SHA-256。
2. 完成飞书权限、事件和 Bot subscription 配置。
3. 在 Jarvis 项目平台选项中设置 `document_comments=true`。
4. 安装测试通过的补丁二进制。
5. 当前对话由 cc-connect 托管时使用一次性延迟重启，不能在活跃 turn 中直接杀进程。
6. 验证 9810/9820、Feishu 私聊回归和文档评论完整闭环。

回滚：恢复备份二进制和原配置后重启。评论事件 subscription 可以保留但不会被处理；若要完全撤回能力，再显式取消 Bot subscription 和开发者后台事件，不做自动清理。

## 14. 后续阶段

第一阶段真实稳定后，再分别评估：

- Sheet、Slides、Base 等其他局部评论定位协议。
- 评论中的图片、附件和富文本。
- Agent 完成文档修改后自动 resolve 评论。
- 未 @Bot 的“文档内常驻助手”模式。
- 全文评论除「可见拒绝」之外的正式产品交互。
- 长回答的呈现方式：Markdown 降级、超长内容改写入文档或私聊并在评论里给链接。
- 同文档跨评论卡片的记忆，以及持久化去重（跨进程重启不重复回答）。
- 把评论事件额外 fan-out 给 Jarvis M2 作为世界状态证据。

这些都不是 MVP 的兼容项，也不应预埋 silent fallback。
