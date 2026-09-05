# fact 降级为证据索引

配套 [design-entity-summary.md](design-entity-summary.md)。那份文档把「现在是什么」搬到了实体页，
这份文档处理剩下的一半：`fact` 表在页面上线之后该承担什么。

## 1. 问题

`fact` 当初是世界模型的主载体：模型把每条认知写成一段自然语言，读取时由程序推送进上下文。
实体页上线后这个角色已经被拿走了——M3 上下文里只剩条数，`Todo.content.capture` 不再带 fact 明细，
rollup 整层删除。但 `fact` 的**契约没有跟着改**，于是它停在一个中间状态：既不再是知识的家，
又仍然按知识的标准被书写和阅读。

具体的失配有四处，都是实测：

**指针在结构上填不出来。** `fact` 有 `source_kind` + `source_id` 两列指向原始材料，但 factengine
写的 480 条里只有 3 条填了。原因不是模型偷懒，是它拿不到可填的值——材料渲染只给飞书的
`message_id` 字符串，而 `source_id` 是数据库自增 id：

```686:704:internal/factengine/store.go
func renderMessages(rows []messageRow, location *time.Location) string {
	blocks := make([]string, 0, len(rows))
	for _, row := range rows {
		content := strings.ReplaceAll(strings.TrimSpace(row.Content), "\r\n", "\n")
		content = strings.ReplaceAll(content, "\n", "\n    ")
		at := time.UnixMilli(row.CreateTime).In(location).Format(time.RFC3339)
		meta := fmt.Sprintf("message_id=%s time=%s sender_open_id=%s sender_name=%q sender_type=%s message_type=%s render_ok=%t",
			row.MessageID, at, row.SenderOpenID, row.SenderName, row.SenderType, row.MessageType, row.RenderOK)
```

**指针填了也走不通。** 没有任何入口能按数据库 id 取一条原始消息。`query-messages` 查的是本地
`message` 表（`GET /api/messages`，与 lark-cli 无关），过滤条件只有 chat、sender、keyword、
时间窗和条数。

**指针语义有歧义。** `source_kind` 的取值是 `message` / `todo` / `task` / `factengine`，但 factengine 的
todo、task 两个游标读的是 `todo_event` 和 `task_event` 表，`LastID` 是事件 id 不是 Todo/Task 的 id。
库里现有数据两种都有（`source_kind=task` 的 `source_id` 最大 690，而 `task` 表最大 id 是 399）。
`factengine` 更是根本不指向任何表——它是生产者名字，不是材料。

**`page_revision` 借住在这张表里。** 它没有原始材料可指，`occurred_at` 填的是写入时刻而不是业务时间，
于是每个读取点都必须记得排除它。现在需要记得的地方有四处——M3 计数（`internal/extract/worker.go:276`）、
HTTP API 的 `exclude_source_kind` 参数、前端 `FactTimeline.tsx:76`、以及 `jarvis-tools list-facts`。
前三处记得了，第四处忘了，而它正是 factengine 和图书管理员唯一的下钻入口：`occurred_at` 是写入
时刻，排序又是 `occurred_at DESC`，所以每条旧页面必然压在当轮真实证据之上。实测项目 44 取 4 条
事实，前两条是页面自己的上两版。上一轮写进页面的结论以「事实」身份回到同一个 agent 面前，
成为支持同一结论的证据——判断错了会自己给自己作证。

四个消费者里三个记得、一个忘了，这个比例本身就说明默认是错的。修法不是给第四处补过滤，
而是让不合格的东西一开始就不进这张表。

## 2. 判断依据：为什么是降级而不是删除

先确认原始数据里哪些实体归属可以推导，哪些不能。按库里实际数据：

| 归属 | 原始数据能否推导 | 覆盖率 |
|---|---|---|
| group → 消息 | `message.group_id` | 100% |
| person → 他发的消息 | `message.sender_open_id = person.open_id` | 100% |
| person → 提到他的消息 | `mentions_json` | 0%（4608 条全空） |
| project → 消息 | 经 `feishu_group.project_id` 绕一跳 | 50%（60 个 related 群只有 5 个绑了项目） |
| key_matter → 消息 | 无关联字段 | 0% |
| resource → 消息 | `resource.source_message_id` | 有 |

另外两件曾经被归给 `fact` 的能力，原始数据本来就有：**业务时间**（`message.create_time` 是飞书发送
时间，`created_at` 是采集时间，这两列本身就是业务时间与认知时间的分离）；**吸收进度**
（factengine 的三个游标就是「处理到哪了」）。

所以剥掉之后，`fact` 唯一不可替代的是**语义归属**：把一条材料归给项目、关键事项，或归给不是
发送者的某个人。项目缺 50%、关键事项缺 100%、「提到某人」缺 100%——这是索引层必须存在的
全部理由，也是它的全部理由。

成本侧也支持降级。有指针的 126 条 fact 与其原始消息对比：断言平均 142 字，原文平均 355 字，
只有 2.5 倍。算上一条断言常概括一个 thread，实际约 3–4 倍。而读 fact 是低频动作
（日常增量维护读的是原始材料游标，不读 fact；fact 只服务补页和核查），低频动作多花 3 倍
token 换一手材料是划算的——二手断言会带偏，原文不会。

## 3. 目标：三层各就各位

| 层 | 载体 | 回答 | 可变性 |
|---|---|---|---|
| 原始材料 | `message` / `todo_event` / `task_event` / `execution_run` / `resource` | 原话是什么 | 不可变 |
| 证据索引 | `fact` | 关于这个实体的材料在哪、大概是哪件事、什么时候发生 | 只增 |
| 实体页 | 六张表的 `summary` | 现在是什么 | 就地改写，有上限 |

`fact` 的新契约一句话：**它是带主体标签的检索索引项，不是迷你知识条目。** 它的职责是把读者
带到现场，不是替代现场。

这带来一个连带收益：**索引项重复无害。** 之前 238 条重复之所以严重，是因为它们被当知识读，
重复的知识会误导判断；当它们只是索引时，重复只是噪音。所以提示词里「写前查询避免重复」
那条要求可以松掉，factengine 每轮的写入成本随之下降。

## 4. 已拍板的设计决定

1. `fact` 表继续用，不建新表替代。要改的是契约和约束，不是载体；`subject_type` / `subject_id` /
   `occurred_at` / `source_kind` / `source_id` 这五列本来就是索引层该有的形状。
2. **单指针，不做多指针数组。** 指针的职责是「带我到现场」，落在正确的 chat 和时间点就够了，
   读者可以就地读周围的窗口。等观察到真实需要再加，不提前设计。
3. `page_revision` 迁出 `fact`，进一张独立小表。它不是事件，留在这张表里就要求每个读取点
   永远记得过滤。
4. **不迁移历史数据。** 2825 条旧 fact 保持原样，指针缺失的那些自然稀释。校验只对新写入生效。
5. 不给 fact 加状态、分类、枚举或原因码。

## 5. 契约层

### 5.1 指针必填且可解析

新增来源注册表，形状照抄现有的 `factSubjectModel`（`internal/progress/service.go:327`，它已经在
用同一个模式校验 subject 存在性）：

```go
// factSourceModel maps a source_kind that names raw material to its table.
// A kind in this registry must carry a source_id, and that row must exist:
// an index entry that cannot be followed is worse than no entry.
func factSourceModel(sourceKind string) (any, bool) {
	switch sourceKind {
	case "message":
		return &domain.Message{}, true
	case "todo_event":
		return &domain.TodoEvent{}, true
	case "task_event":
		return &domain.TaskEvent{}, true
	case "execution_run":
		return &domain.ExecutionRun{}, true
	case "resource":
		return &domain.Resource{}, true
	}
	return nil, false
}
```

`prepareFact` 与 `AppendFact` 的校验变成：

- `source_kind` 必填（现在可为 nil）。
- `source_kind` 命中注册表时，`source_id` 必填，且该行必须存在——复用 `requireParent`，
  和实体页的引用校验同一个思路：**编造 id 直接拒**。
- `source_kind` 不在注册表时，不要求 `source_id`。这条不是豁免，是另一类生产者：程序自身的
  状态跃迁（立项、项目状态变更、归档、关键事项闭环）没有外部材料，主体加断言就是完整记录。
  这四处写入用 `source_kind="system"`，且不由 agent 写。

错误消息必须能指导模型自救，例如：

```
source_kind=message 的事实必须带 source_id，指向材料里给出的 ref。
本轮材料的可引用 ref 见每条 MATERIAL 行首。
```

### 5.2 kind 名对齐表名

`internal/factengine/store.go` 的常量改值，消除 event 表的歧义：

```
SourceTodo = "todo"  →  "todo_event"
SourceTask = "task"  →  "task_event"
```

**这里有个必须一起做的动作，漏了会炸。** 常量同时是 `fact_source_cursor` 的主键，而三个来源里
只有 `SourceMessage` 带 `StartAtPresent: true`（`cmd/jarvis-server/main.go:216-218`）。
改名后 `todo_event` / `task_event` 找不到自己的游标行，又不会从当前位置播种，会**从 0 开始重放
全部 3251 条历史事件**，每条都过一遍 LLM。

所以改常量的同一步要改游标行的 key：

```sql
UPDATE fact_source_cursor SET source = 'todo_event' WHERE source = 'todo';
UPDATE fact_source_cursor SET source = 'task_event' WHERE source = 'task';
```

这不是历史数据迁移，是控制状态改键——游标记的是「读到哪了」，值（`last_id` 1742 / 1509）
完全不变。执行一次，跑之前先确认这两行存在。

另一个选择是让游标名和 `source_kind` 彻底解耦（游标是内部调度状态，`source_kind` 是指针的目标
表名，本来是两件事）。但那要多引入一个概念，而现在改两行 key 就够，先不做。

`factengine` 这个 `source_kind` 取消：它是生产者名而不是材料，按新契约不合法。历史行不动。

### 5.3 description 是锚点

`description` 加软上限 200 字，在 `prepareFact` 校验。当前平均 142 字，所以这不是紧箍咒，
是把职责写死：**一句话说清是哪件事、涉及谁，让读者判断要不要打开原文。** 结论和当前状态
属于实体页，不写进事实。

超限的错误消息要说清它是锚点不是知识：

```
description 有 N 字，上限 200。事实是索引项不是知识条目：写一句话说清是哪件事，
结论写进实体页（update-page），细节留在原始材料里靠 source 指针追溯。
```

### 5.4 `page_revision` 迁出

新建表 `page_revision`：`id` / `page_type` / `page_id` / `old_text` / `changed_at`。

保留它的理由是页面按设计有损——压缩会主动扔掉细节，旧版是唯一记录被扔掉了什么的地方，
而压缩每天都在发生。

`internal/background/page.go` 的归档改写成写这张表。库里的历史 `page_revision` fact 按同一理由
迁进新表（实际迁移时 210 条 / 423KB），再从 `fact` 删除。

搬走之后 `page_revision` 的过滤逻辑整条链一起删，四个消费者各删一处：

- `internal/progress/service.go`：`FactSourcePageRevision` 常量、`FactFilter.ExcludeSourceKind`
  字段与两处 `Where`（`ListFacts` 和 `CountFacts`）。`ExcludeSourceKind` 只为它存在，没有第二个用途。
- `internal/api/progress_events.go:71`：`exclude_source_kind` 查询参数。
- `internal/extract/worker.go:265-287`：两处 `ExcludeSourceKind: &pageRevision`。
- `web/src/api.ts` 的 `excludeSourceKind` 选项与 `web/src/world/FactTimeline.tsx:76` 的传值。

删掉这四处是这次改动的主要收益之一：一个语义不再依赖四个消费者各自记得。

页面历史要不要在前端露出，本次不做——先确认有人真的会看。

## 6. 材料层：让指针填得出来

这是根因修复，必须排在契约校验之前——否则校验一上线，factengine 每轮都会因为填不出指针而失败。

### 6.1 每条材料行带可引用的 ref

`renderMessages`（`internal/factengine/store.go:686`）的 meta 行加 `ref=message:<db id>`。
`messageRow` 已经有 `ID` 字段（`buildUnit` 拿它拼 `Key`），只是没渲染。飞书的 `message_id`
保留，它对模型理解 reply 链有用。

todo、task 两个来源的 body 同样加 `ref=todo_event:<id>` / `ref=task_event:<id>`。

### 6.2 unit 头部声明可引用范围

`SourceUnit.Prompt()`（`internal/factengine/source.go:70`）在 `MATERIAL_KEY` 之后加一行
`MATERIAL_REFS`，列出本 unit 覆盖的 ref 区间。模型写事实时从材料行里挑具体那一条，
而不是猜一个区间端点。

## 7. 工具层

### 7.1 新增 `get-message --id`

指针必须可跟随，否则整层白做。而现在没有任何入口能按数据库 id 取一条原始消息：
`query-messages` 打的是本机 `GET /api/messages`（本地 `message` 表，与 lark-cli 无关），
而 `toolquery.MessageFilter` 只有 `ChatID` / `SenderOpenID` / `Keyword` / `From` / `Until` / `Limit`。

按仓库已有的成对形状实现，不在列表上挂 id 参数：

```
h.GET("/api/captured-resources", ListCapturedResources(toolQueries))       ← 已有
h.GET("/api/captured-resources/:resource_id", GetCapturedResource(...))    ← 已有
h.GET("/api/messages/:message_id", GetToolMessage(toolQueries))            ← 新增
```

CLI 对应 `get-message --id N`，与 `get-captured-resource --id` 同形。

选 get-one 而不是给列表加过滤，决定性理由是 fail-fast：指针指向不存在的行必须报 404。
列表过滤只会返回空数组，而空数组是个弱信号——agent 很可能当成「这条消息没什么内容」继续走。

其余来源已经可跟随：`todo_event` / `task_event` / `execution_run` 通过 `get-todo` / `get-task`
（含 `--include-run-output`）；`resource` 通过 `get-captured-resource --id`。

**不要回飞书取原文。** 原文在 M2 采集时已落盘（`content` 与 `content_raw` 两列都在），
回飞书是多一次网络往返和限流风险，且撤回或删除的消息在飞书侧取不到而本地副本仍在。
指针指向本地行。

### 7.2 `append-fact` / `append-facts-batch`

`--source` 从可选改为必填，`--source-id` 在 source 命中注册表时必填。CLI 层先拦一次，
错误消息与服务层一致。

### 7.3 `list-facts` 输出指针

`FactView` 已经带 `source_kind` / `source_id`，CLI 透传 `.data` 也已经包含。确认输出里不裁剪即可，
不需要改动。

### 7.4 `internal/toolcatalog/catalog.go`

第 37 行那句补一层语义，仍然不点具体参数：

```
- 实体的长期事实用 get-page / update-page 读写，索引用 list-pages。
  list-facts 返回的是证据索引：一句锚点加指向原始材料的指针，需要原话时沿指针取原文。
```

## 8. 维护层

### 8.1 `conf/prompts/fact-extract-system-prompt.md`

改第 17 行那一步（每轮动作顺序的第 5 步）和「长期事实页」一节，写清三件事：

- 每条事实必须带 `source`，指向材料行首给出的 `ref`。填不出指针说明这条不该写成事实。
- `description` 是锚点：一句话说清是哪件事、涉及谁。结论和当前状态写进页，不写进事实。
- 不再要求为避免重复而反复查询现值。索引项重复无害；页面的正确性靠改写页保证，
  不靠事实流去重。

「主次」一节不变——改写页仍然是必做动作，追加事实仍然是附带。

### 8.2 `.agents/skills/curate-world-model-pages/SKILL.md`

第 2 节「每页的动作」补一句：`list-facts` 返回的是索引，判断页面里某个结论是否已经过期时，
沿 `source_kind` / `source_id` 取原文核实，不要只凭 `description` 下结论。

原文与断言冲突时以原文为准。

## 9. 消费者影响

| 消费者 | 影响 | 动作 |
|---|---|---|
| factengine 增量维护 | 无。它读原始材料游标，从来不读 fact | 无 |
| 图书管理员 skill | 需要沿指针核实 | §8.2 |
| M3 上下文 | 只用条数，且已排除 page_revision | `ExcludeSourceKind` 删除后同步简化 |
| `dailydigest` | 它独立采集 authored_messages / todo_events / task_events / execution_runs 四个原始源，fact 只是第五个，且已标注 `Relation to principal: context only; actor unknown`、`description` 截到 500 字 | 无。后续可沿指针增强，不在本次范围 |
| Web `FactTimeline` / `FactsPanel` | 现在自己传 `excludeSourceKind: 'page_revision'` | 删掉该选项（§5.4）。把指针渲染成链接是可选项 |

## 10. 执行顺序

依赖是硬的，不能调换前两步：

1. **材料层带 ref**（§6）——不做这步，指针在结构上填不出来。
2. **`get-message --id`**（§7.1）——不做这步，指针填了也跟随不了。
3. **契约校验**（§5.1 / §5.2 / §5.3）——指针必填、来源注册表、kind 改名、description 上限。
   kind 改名与游标改键必须同一步落地，否则 factengine 会重放 3251 条历史事件。
4. **`page_revision` 迁表**（§5.4）——建表、改写归档、删掉四处过滤逻辑、删那 2 条历史行。
   这一步跨 Go 与前端，`web/src/api.ts` 的选项和 `FactTimeline.tsx` 的传值要一起改，
   否则前端会给 API 传一个已被删除的参数。
5. **提示词与 skill**（§8）。
6. **CLI 与 toolcatalog**（§7.2 / §7.4）。
7. 前端指针链接（可选，可不做）。

## 11. 验收

`go build ./...`、`go test ./...` 全绿，`gofmt` 无残留，改了 `web/` 则 `npx tsc --noEmit`。
重启走 `./scripts/rebuild-server.sh`。

单测至少覆盖：

- `source_kind` 缺失被拒；命中注册表但 `source_id` 缺失被拒；`source_id` 指向不存在的行被拒
  （错误消息含 kind 与 id）。
- `source_kind="system"` 允许无 `source_id`。
- `description` 超 200 字被拒，且错误消息说明它是锚点。
- 材料渲染含 `ref=message:<id>`，且 id 是数据库 id 而非飞书 `message_id`。
- `get-message --id` 能取到指定行；id 不存在时返回 404 而不是空列表。
- `page_revision` 写入 `page_revision` 表而不是 `fact`；`fact` 表不再出现该 `source_kind`；
  `update-page` 连续两次改写留下两条版本记录，内容不变时不留。
- 全仓库不再出现 `ExcludeSourceKind` / `exclude_source_kind` / `excludeSourceKind`。

重启后必须核对一次：`fact_source_cursor` 里是 `message` / `todo_event` / `task_event` 三行，
`last_id` 分别是 4609 / 1742 / 1509（或更高），且首轮 `world_maintenance` 的 `units` 不是几千。
出现大批量 units 说明游标改键漏了，立刻停服务修键，不要让它跑完。
- 提示词要求事实带 source 指针，且不含具体命令参数（沿用现有的提示词与工具目录分离规则）。

上线后看三个数字判断机制是否生效：

- **新写入 fact 的指针填充率**，目标 100%（对比现在 factengine 480 条里 3 条）。
- **新写入 fact 的 description 平均长度**，应稳定在 200 以内（现在 142）。
- **`fact` 表里 `page_revision` 行数**，应为 0。

## 12. 范围之外

- 不加多指针数组（§4.2）。
- 不补 `mentions_json` 采集，也不批量给 related 群绑项目。那会提高原始层的可推导比例，
  但不改变 key_matter 这类归属永远需要判断的事实，属于另一件事。
- 不改 `dailydigest` 的证据组装。
- 不迁移 2825 条历史 fact。接下来一段时间 `list-facts` 返回的是新旧混合形态：
  新写入带指针，旧行不带。
