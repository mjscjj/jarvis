---
name: okr-progress-sync
description: 只读巡检 OKR 相关的 Meego 工作项和已采集飞书消息，把可追溯证据写回 OKR、Project、KeyMatter 的 Page/Fact。用于 OKR 进度同步、Meego 轮询、周报取数和定时进展巡检；不发送消息，也不把 OKR 物化成 Task。
---

# OKR 进度巡检

这项工作是一次独立执行单元：读取现状、收集证据、更新世界模型，然后结束。`Task` 只承载本次执行，不给 Task 增加 OKR/Project/KeyMatter 外键，也不为每个 OKR 自动创建常驻 Task。

## 硬边界

- 外部系统只读。Meego 使用 `bytedcli` 查询；飞书消息只读本地已采集数据，必要时使用 `lark-cli` 查询，但不调用任何发送、更新或删除命令。
- 不发送真实飞书消息，不催办，不修改 Meego 工作项。
- 不为来源新建 Go 专用流水线。原始 Meego 证据通过 `jarvis-tools append-clue` 进入统一证据流；确定的实体进展通过 `update-page` 和 `append-fact` 写回。
- 没有直接证据就不更新状态。工具输出不完整时记录覆盖缺口，不把“查不到”写成“没有进展”。
- 只处理未闭环 OKR。已闭环 OKR 的项目仍可独立维护，但不得新建或移动项目到已闭环 OKR。

## 1. 建立本轮范围

先读取未闭环 OKR，再对候选项读取完整树和当前事实，避免重复扫描无关对象：

```bash
jarvis-tools list-okrs --limit 100
jarvis-tools get-okr --id <okr_id>
jarvis-tools get-okr-weekly-view --id <okr_id> --date <YYYY-MM-DD>
jarvis-tools get-page --type okr --id <okr_id>
jarvis-tools list-facts --subject-type okr --subject-id <okr_id> --limit 50
```

从 OKR、负责人、Project、KeyMatter 的标题、代号、summary 引用和已知链接中提取查询锚点。锚点不明确时跳过该来源并报告，不做全租户宽泛搜索。

## 2. 读取 Meego 证据

先以当前安装版本的帮助为准发现只读命令：

```bash
bytedcli --json --all-help | rg -i 'meego|work.?item'
```

只运行查询/list/get/search 类命令。每条候选证据至少保留：工作项稳定 ID、标题、当前状态、负责人、最近更新时间、来源链接（若返回）以及本轮查询时间。不要只凭标题相似就关联；至少还要有 Project 代号、明确的 OKR/项目引用、负责人一致或已有 summary 链接之一。

把每个确认相关的工作项作为幂等原始线索投递，`external-id` 使用“工作项稳定 ID + 最近更新时间”的短哈希或稳定短串；同一版本重复执行必须返回 `inserted=false`：

```bash
jarvis-tools append-clue \
  --source meego \
  --external-id "<stable_item_revision_id>" \
  --title "Meego 工作项更新：<标题>" \
  --occurred-at "<updated_at, RFC3339>" \
  --content - <<'TXT'
工作项 ID：<id>
所属 OKR：<okr_id 与标题>
关联项目：<project_id 与名称，未知则写未确认>
状态：<status>
负责人：<owner>
来源：<url 或查询命令返回的稳定引用>
证据：<本轮直接读到的客观变化>
TXT
```

## 3. 读取消息证据

优先读取本地已采集消息，按负责人、已关联群、明确关键词或日期缩小范围：

```bash
jarvis-tools query-messages --sender-open-id <owner_open_id> --keyword '<项目代号或唯一关键词>' --date <YYYY-MM-DD> --limit 100
```

进入关联步骤前，先从尚未写入任何 OKR/Project/KeyMatter Fact 的证据队列收窄候选。`source` 使用 `meego` 等 clue 来源，普通飞书消息使用 `message`；`anchor` 必须来自已读取的 OKR 标题、项目代号、事项名或稳定 ID：

```bash
jarvis-tools list-unassociated-okr-evidence \
  --source meego \
  --date <YYYY-MM-DD> \
  --anchor '<项目代号、事项名或稳定 ID>' \
  --limit 100
```

列表只证明“这条已采集消息尚未被关联”，不会推荐目标实体。Agent 必须回读 OKR 树与 Page，明确选择最小的 OKR、Project 或 KeyMatter 后再调用 `apply-okr-evidence`；标题相似、同负责人或同一群都不能单独作为自动关联依据。

消息只有在明确提及结果、风险、决定、交付物或下一里程碑时才构成进展证据。闲聊、转述、自动机器人通知和仅有“收到/在看”的消息不更新 OKR。

## 4. 汇总并写回世界模型

先判断证据属于哪个最小实体：KeyMatter 优先，其次 Project，最后才是 OKR。向上汇总时引用下级实体，不复制整段明细。

1. 仅当“当前结论”发生变化时写回。先 `get-page`，再用统一证据边界把已采集消息关联为 Fact，并把返回的 `updated_at` 作为 Page CAS 条件：

   ```bash
   jarvis-tools apply-okr-evidence --payload - <<'JSON'
   {
     "subject_type": "<okr|project|key_matter>",
     "subject_id": <id>,
     "evidence_message_id": "<append-clue 或 query-messages 返回的 message_id>",
     "description": "<这次发生的客观变化>",
     "occurred_at": "<RFC3339>",
     "content": "<合并后的完整当前结论>",
     "if_unchanged_since": "<get-page.updated_at>"
   }
   JSON
   ```

   这个边界只接受已进入本地 Message 表的证据，并自动保存数值型来源行 ID。重复相同 payload 不会重复 Fact；Page CAS 冲突时 Fact 已安全记录，响应会带当前 Page，重新合并后用新的 `updated_at` 重试，绝不覆盖远端新内容。没有改变当前结论时，只用 `jarvis-tools append-fact` 记录新事实。
2. OKR Page 第一行保持一句话结论，后续写当前状态、风险、下一检查点，并用 `[名称](project:<id>)` / `[名称](key_matter:<id>)` 引用下级实体。
3. 不调用 `create-task`、`start-task` 或任何消息发送命令。

## 5. 定时巡检

周期执行必须复用 ScheduledTask；每次触发只创建一个普通 Task。不要在业务表或 OKR 上保存 scheduler 状态。

```bash
jarvis-tools create-scheduled-task --payload - <<'JSON'
{
  "title": "OKR 只读进展巡检",
  "action_type": "agent_task",
  "instruction": "读取 okr-progress-sync Skill 并执行一次。只读 Meego 和已采集消息，更新 Page/Fact；不发送消息，不修改外部系统。",
  "context_snapshot": {"skill": "okr-progress-sync", "mode": "read_only"},
  "schedule_type": "interval",
  "interval_minutes": 360,
  "enabled": true
}
JSON
```

创建前先 `list-scheduled-tasks`，存在同名 active 任务时更新它，不重复创建。定时任务的最后结果必须包含：扫描 OKR 数、Meego/消息证据数、写入 Fact 数、更新 Page 数、跳过原因和覆盖缺口。

## 完成检查

- 每个写入都能回读到对应实体和来源；重复执行不会重复投递同一版本证据。
- 已用 `get-okr-weekly-view` 回读目标日期的完整层级与变更计数，且该读取没有创建 Task。
- 已通过 `list-unassociated-okr-evidence` 按来源、日期和稳定锚点收窄候选；关联后的同一消息不再出现在列表。
- Page CAS 冲突已经重新读取并合并；没有把历史明细覆盖掉。
- 没有产生 OKR 专用 Task 关系，也没有发送消息或修改外部系统。
- 明确列出证据覆盖时间窗、查询锚点、更新项、未确认关联和失败来源。
