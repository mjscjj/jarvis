## 1.0 什么是 Jarvis 的世界观

### 先讲一个具体场景

2026 年 8 月 19 日下午，一个叫「Jarvis 开发」的飞书群里，储节节发了一条消息：

> 「主动式 agent 那篇设计文档，你帮我再过一遍，把世界观那节补具体点。」

这条消息被采集层原封不动存进了 SQLite。紧接着，准入阶段（M3）被唤醒。它要做的第一件事不是动手，而是**装载世界观**——搞清楚「我是谁、谁在说话、在哪个群、围绕什么项目、有哪些未闭环的事、相关材料在哪」。

下面就是它实际看到的东西。

---

### 世界观里装了什么

世界观不是一个塞进提示词的大文本包，而是六类实体页加一层证据索引。准入阶段按当前会话的范围，有选择地把相关实体的 `summary` 页拼进上下文，事实只给条数不给内容。

| 维度 | 实体类型 | 在这个场景里具体是谁/什么 | 加载方式 |
|---|---|---|---|
| 我是谁 | `principal_profile` | 储节节本人，Agent 的代表对象 | 每次必载，整页 summary |
| 谁说的 | `person` | 储节节（同时也是 principal，自己给自己派活） | 按发言者 open_id 解析，整页 summary |
| 在哪个群 | `feishu_group` | 「Jarvis 开发」群，围绕 Jarvis 项目本身 | 按 chat_id 绑定，整页 summary |
| 什么项目 | `project` | Jarvis——一个主动式数字分身系统 | 群绑定项目，整页 summary；其他项目只给一行索引 |
| 相关事项 | `key_matter` | 「主动式 agent 设计文档完善」等未闭环事项 | 按项目关联，列表摘要 |
| 相关资源 | `managed_resource` | 代码仓库、设计文档、文章草稿 | 按项目关联，列表摘要 |
| 历史底账 | `fact` | 每个实体名下发生过什么 | 只给今日/近 7 天条数，需要时按主体+日期下钻 |

---

### 逐页展开：每一页长什么样

#### 我是谁（Principal）

Principal 是 Agent 的代表对象——它在群里说话时代表的那个人。这是单行表，只有一条记录。

```text
open_id=ou_xxx name="储节节" department="智能创作" title="研发工程师"
leader_open_id=ou_yyy leader_name="唐建科"

储节节是 Jarvis（主动式数字分身）项目的唯一开发者和使用者，同时负责公会 Agent 基建项目（project:44）
中 DDL/DML 审批收口、上线评估等工作。技术偏好：Go 后端、SQLite、提示词即真源、fail-fast 不做静默
fallback。对 Agent 输出风格的要求：讲清楚点，别用太多术语，不要 AI 味。
```

这一页回答了三个对准入判断至关重要的问题：我代表谁做事、我的领导是谁（领导的软话也是硬指令）、我稳定的偏好是什么。

#### 谁说的（Person）

在这个场景里，说话的人恰好就是 principal 自己。但在更多场景里，说话的是同事——这时 Person 页提供的是「这个人和我是什么关系、历史上有过什么承诺和协作」。

以项目里另一位真实人物为例，Person 页长这样：

```text
陈炜超是公会 Agent 基建（project:44）和 Agent Runtime 项目的核心开发，负责沙箱、工具网关、
出网网关和技能发布等基础设施。

当前负责：
- 沙箱加固与执行平面：已调整沙箱可见域名范围；待办包括沙箱支持 seccomp 过滤器。
- 沙箱出网网关（agent_egress_gateway）：PSM 已注册，egress.enabled 仍为 false。
- 技能发布范围管控：feat/skill-publish-scope-dimension 分支已补齐 scope 维度但尚未合入 master。

协作状态：
- 与袁小轩协作沙箱文件上传等 native 工具方向、MR !56 审批。
- 2026-08-17 接手排查 ppe_follow_stream_retro 泳道 skill content not found 问题。

关联 project:44。
```

这一页不是简历。它的第一行是索引行——一句话说清「这个人是谁、和我什么关系」；后面是当前状态和历史承诺。Agent 判断「这条消息值不值得行动」时，说话人的角色和历史承诺是关键权重。

#### 在哪个群（Group）

群页记录「这是什么群、围绕什么、群语境是什么」。以报警群为例：

```text
chat_id=oc_critical name="[Critical]报警-公会-Agent基建" is_key_group=true project_id=44

[Critical]报警-公会-Agent基建 是 Argos 平台报警通知和 RCA 分析群，覆盖公会 Agent 基建相关服务
（llm_backstage_tool、backstage_agent、agent_backstage、llm_tool_gateway 等）的 Critical 级别报警。
小小虾（Argos RCA 分析 bot）在此群对每次报警发布 RCA 分析结论。Bax 巡检评测报告也在此群发布，
群内讨论关联 project:44。

近期报警模式（2026-08 月）：
- GPU 使用率过高（agent_backstage）：多次触发，原因包括评测入口与线上 GPU 服务共用……
- 下游失败率过高（llm_backstage_tool）：连续六次 thrift marshal 类型不匹配报警……
```

群的 `description` 字段是飞书群公告的同步，属于外部事实；`summary` 是 Agent 自己维护的群语境。两者分开。

#### 什么项目（Project）

项目页是六类实体页里信息密度最高的。以一个真实运行中的项目页为例（节选自 project:44 的实际 summary）：

```text
公会 Agent 基建是公会业务的 Agent 基础设施，覆盖工具网关、Runtime、Skill、沙箱、评测、
自进化和多租户执行，支撑 project:48、project:49 等 Bax/Backstage 业务接入。

当前阶段：新 Runtime 已正式上线。08-13 对外版已在 101354 公会试点上线，08-17 储节节在攻坚群
明确确认上线，并开始推动上线后评估流程。

关键决定和约束：
- 正式 bot 只由线上被动回调持有，BOE/OpenClaw 禁用正式 App 的 Feishu WS。
- Agent 不能改线上 TCC；沙箱被 bwrap 隔离。
- 08-17 要求 DDL/DML 审批收口，收口人：唐建科、储节节、周文华。
- 灰度方向：08-11 唐建科决定「不保留历史会话」，17:47 改口「需求迁移」。

当前主要风险：
- 工具参数非法 JSON 生成，报错后循环，用户侧无返回。
- thrift marshal 类型不匹配已发生六次，表明 llm_backstage_tool 的 CallTool 公共参数构造
  有系统性问题……

关键人：唐建科 person:59、周文华 person:99、陈炜超 person:105……
主要讨论群：group:127（研发攻坚群）、group:115807（调优&评测群）。报警群：group:36。
关键资源：resource:8、resource:7、resource:20、resource:26、resource:31、resource:36。
```

注意页内的 Markdown 引用：`person:59`、`group:127`、`resource:36`。写入时这些引用会被逐个校验目标存在，编造 ID 直接拒绝。反查靠 `LIKE` 扫六张表——全库实体只有一两百行，够用。

#### 相关事项（KeyMatter）

KeyMatter 是「一件需要长期记住并定期回看，但不构成项目、也不是一次可执行动作的事」。比如：

```text
id=12 title="主动式 agent 文章世界观章节补充" status="待完善"
project_id=1（Jarvis） due_at=2026-08-22
summary: 文章《主动式数字分身》第二章「让分身看见世界」需要补充具体场景例子，
展示 Jarvis 处理一件事时世界观里加载了哪些信息。储节节 08-19 在群里交办。
```

KeyMatter 和 Task 的区别是硬的：Task 是「现在去做点什么」，M5 执行完就进入终态；KeyMatter 是「一直记着」，只被读取和更新，永不交给 M5 执行。一个 KeyMatter 的生命周期内可能派生 0 到 N 个 Task。

#### 相关资源（Resource）

资源分两类：`resource` 是采集层从消息里自动提取的（文件、链接、妙记、飞书文档），`managed_resource` 是人在后台手工维护的。在这个场景里，相关资源可能是：

```text
id=36 type=doc url="https://bytedance.larkoffice.com/docx/..."
name="Bax(for Ops) Agent Runtime 迁移计划" project_id=44

id=41 type=repo url="https://code.byted.org/chujiejie.1/jarvis_bot"
name="Jarvis 主仓库" project_id=1 link_principal=true
```

---

### Fact 长什么样：带时间、来源、证据

页回答「现在是什么」，Fact 回答「发生了什么」。Fact 是带主体标签的证据索引项，不是迷你知识条目。一条新写入的 Fact 长这样：

```json
{
  "id": 2847,
  "subject_type": "project",
  "subject_id": 1,
  "description": "储节节在「Jarvis 开发」群要求完善主动式 agent 设计文档的世界观章节，涉及设计文档和文章草稿两处。",
  "occurred_at": "2026-08-19T19:36:00+08:00",
  "source_kind": "message",
  "source_id": 4612,
  "created_at": "2026-08-19T19:36:03+08:00"
}
```

五个关键字段：

- **`subject_type` + `subject_id`**：这条证据归谁——项目 1、人物 105、还是群 36。归属可以是语义判断（一条消息可能归给不是发送者的某个人）。
- **`description`**：一句话锚点，说清「是哪件事、涉及谁」，上限 200 字。结论和当前状态不写在这里，写在页里。
- **`occurred_at`**：业务时间（事情什么时候发生的），不是写入时间。回填的事实落在正确的日期上。
- **`source_kind` + `source_id`**：指针，指向原始材料。`message:4612` 表示本地 `message` 表第 4612 行，可以用 `get-message --id 4612` 取到原文。填不出指针的事实不允许写入——一个无法跟随的索引比没有索引更糟。

Fact 的新契约里有一条关键设计：**索引项重复无害**。之前累积 238 条重复事实之所以严重，是因为它们被当知识读；当它们只是索引时，重复只是噪音。所以不再要求模型写前查询去重，每轮写入成本下降。

需要看证据原文时，沿指针取：

```bash
jarvis-tools get-message --id 4612
```

返回的是采集时落盘的完整消息原文——不回飞书取，不截断，撤回了的消息本地副本也在。

---

### Summary 如何综合这些信息

Summary 页不是 Fact 的拼接，而是 Agent 基于所有材料**改写**出来的当前最佳认知。它的运作方式有三个要点：

**第一，整体读写，有硬上限。** 每个实体的 `summary` 是一整段 Markdown 文本，上限 8000 字符。写超了直接拒绝，错误消息教模型自救：「合并旧明细为一句结论、把某一节改成对 fact 的引用、或删除已不重要的内容」。压缩是写入路径上绕不过去的动作。

**第二，矛盾时改写，不并列。** Agent 维护页面时三选一：新增、更新既有小节、或与既有内容矛盾。矛盾时改写并在正文留一句「此前认为 X，08-14 改判为 Y」，不静默覆盖，也不两条并列留给下一个读者猜。

**第三，旧版不丢。** 每次改写前，旧全文存进独立的 `page_revision` 表。页面按设计有损——压缩会主动扔掉细节，旧版是唯一记录被扔掉了什么的地方。它不进 Fact 表，因为「我们的笔记被改过」和「世界发生了什么」是两件事。

以项目页的一段演化为例。08-11 那天，唐建科在群里说灰度「不保留历史会话」，项目页写下了这个决定。当天 17:47 他改口「需求迁移」，页面不是追加一条，而是改成：

```markdown
- 灰度方向：08-11 唐建科决定「不保留历史会话」，17:47 改口「需求迁移」。
```

一句话保留了决策的完整轨迹，而不是两条互相矛盾的记录并排躺着。

---

### 渐进式加载：不是把所有信息塞给 Agent

世界观装载不是全量 dump。准入阶段的提示词里，世界段落的结构是这样的（节选自实际渲染逻辑）：

```text
# 当前时间
2026-08-19T19:36:00+08:00（时区 Asia/Shanghai）

# 我的背景(principal)
open_id=ou_xxx name="储节节" department="智能创作" title="研发工程师"
[principal summary 全文……]

# 当前会话所属项目（详细）
id=1 code="jarvis" name="Jarvis" role=owner status=active priority=2
[project summary 全文……]

# 我的其他项目（精简，仅作归属参考）
id=44 code="guild-agent-infra" name="公会 Agent 基建" role=approver status=active priority=2
id=45 code="agent-runtime" name="Agent Runtime" role=contributor status=active priority=3

# 来源会话（Group）
chat_id=oc_jarvis_dev name="Jarvis 开发" is_key_group=true project_id=1
[group summary 全文……]

# 参与者
open_id=ou_xxx name="储节节" role=owner is_leader=true
[person summary 全文……]

# 相关资源
id=41 type=repo name="Jarvis 主仓库" url="https://code.byted.org/..."
id=42 type=doc name="文章草稿" url="https://bytedance.larkoffice.com/..."

# 世界事实（明细未展开）
project:1「Jarvis」今日 3 条、近 7 天 12 条 —— 需要细节用 list-facts 按主体和日期查。
person:59「唐建科」今日 0 条、近 7 天 5 条 —— 需要细节用 list-facts 按主体和日期查。

# 最近有进展的任务（仅作背景）
task_id=87 title="完善设计文档世界观章节" status=pending summary="待开始" last_progress_at=2026-08-19T19:30:00+08:00

# 已存在的未闭环 Todo（仅作背景）
todo_id=201 action_type=investigate title="审查主动式 agent 设计文档" status=extracted

# 会话记录
[new] msg_id=om_xxx time=2026-08-19T19:36:00+08:00 sender_name="储节节":
    主动式 agent 那篇设计文档，你帮我再过一遍，把世界观那节补具体点。
```

注意几层信息量的差异：

| 层级 | 给了什么 | 没给什么 | 为什么 |
|---|---|---|---|
| 当前项目 | summary 全文 | fact 明细 | 当前项目是判断的主战场，需要完整认知 |
| 其他项目 | 一行：id/code/name/role/status | summary、fact | 只需要知道「我还负责什么」，用于归属判断 |
| 群 | summary 全文 | fact 明细 | 群语境直接影响对消息的理解 |
| 参与者 | summary 全文 | fact 明细 | 谁在说话、和我什么关系，是权重判断的核心 |
| 资源 | 元数据一行 | 正文内容 | 知道在哪，需要时再取 |
| Fact | 今日/近 7 天条数 | 任何 fact 内容 | 逼模型自己决定要不要下钻，避免追加比改写更省事 |
| 最近任务 | id/title/status/summary/时间 | 执行记录、run 输出 | 只需要知道「什么在做、刚做完什么」避免重复 |
| 未闭环 Todo | id/action_type/title/status | description、context | 去重用，细节靠 get-todo 下钻 |

如果上下文超长，降级顺序也是写死的：先砍世界数据（每类留个下限），世界数据全到下限后才开始砍会话消息。理由是：世界摘要能靠工具查回来，原始对话是判断的一手证据，不可替代。

---

### 三层各就各位

整个世界观存储分三层，各有明确的职责边界：

| 层 | 载体 | 回答 | 可变性 | 体量 |
|---|---|---|---|---|
| 原始材料 | `message` / `todo_event` / `task_event` / `execution_run` / `resource` | 原话是什么 | 不可变 | 不限，不进上下文 |
| 证据索引 | `fact` | 关于这个实体的材料在哪、大概是哪件事、什么时候发生 | 只增 | 每条 ≤200 字锚点 + 指针，不进默认上下文 |
| 实体页 | 六张表的 `summary` | 现在是什么 | 就地改写，有 8000 字上限 | 每页 ≤8000 字，相关页整页进上下文 |
| 页面历史 | `page_revision` | 我们自己的笔记以前长什么样 | 只增 | 不进上下文，需要时查 |

这四层合起来回答了一个问题：**当 Agent 需要做判断时，它怎么知道自己知道什么、怎么知道自己不知道什么。**

页提供当前最佳认知，让 Agent 不用每次从零归纳；Fact 保留可追溯的证据链，让 Agent 能核实而不是盲信自己的总结；渐进式加载保证上下文体量有界——不是靠更大的窗口，而是靠「摘要进上下文、细节靠工具查」的分层。

这不是把所有信息塞给 Agent。它是有结构的：有名字的实体页让写入者能预演读取者看到什么，有上限的页面让压缩成为绕不过去的写入动作，有指针的事实让每一句断言都能追溯到原话，有层级的加载让上下文不会膨胀到 OOM。

实测对比很直接：改造前单轮提示词涨到 99332 字符后进程被 kill；改造后世界数据体量有界，单轮稳定在数千字符。而那个曾经 422 字符 8 天不动的项目画像，现在已经是一份 8000 字以内、持续更新的活文档。
