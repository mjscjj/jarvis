---
name: weekly-report-progress-sync
description: 只读巡检已启用周报模块的人工进展、Meego 工作项和已采集飞书消息，经通用关系与 Page/Fact/WorldProgress 工具写回世界模型。用于周进度同步和定时巡检；不发送消息、不修改正式周报，也不把 OKR 物化成 Task。
module: biz-okr
---

# 周报进度巡检

这项工作是一次独立执行单元：读取现状、收集证据、更新世界模型，然后结束。`Task` 只承载本次执行，不给 Task 增加 OKR/Project/KeyMatter 外键，也不为每个 OKR 自动创建常驻 Task。

## 硬边界

- 外部系统只读。Meego 使用 `bytedcli` 查询；飞书消息只读本地已采集数据，必要时使用 `lark-cli` 查询，但不调用任何发送、更新或删除命令。
- 不发送真实飞书消息，不催办，不修改 Meego 工作项。
- 不为来源新建 Go 专用流水线。原始 Meego 证据通过 `jarvis-tools append-clue` 进入统一证据流；确定的实体进展通过 `update-page` 和 `append-fact` 写回；对 O、KR、Point 的周期判断通过 WorldProgress 原子工具维护。
- 没有直接证据就不更新状态。工具输出不完整时记录覆盖缺口，不把“查不到”写成“没有进展”。
- 只处理未闭环 OKR。已闭环 OKR 的项目仍可独立维护，但不得新建或移动项目到已闭环 OKR。
- 人工 KRProgress 和 WeeklyKRCore 只作为本轮判断输入，不默认逐条复制成 Clue；只有其中包含尚未采集且值得进入通用证据流的独立外部事实时才投递。
- 本固定行动的 Prompt 只允许写通用 Clue、Fact、Page、WorldProgress 和 Meego 观察快照，因此本轮不调用 `open-week`、周进展增删改或 `confirm-meego-progress`。这些工具可供其它明确要求写周报的 Prompt 使用，不能因为工具存在就扩张本轮目标。

## 1. 建立本轮范围

从模块工具读取本周产品真源，再从通用关系读取已经确认的世界映射：

```bash
scripts/biz-okr-tools scope
scripts/biz-okr-tools board --quarter <quarter> --week <week>
jarvis-tools list-relations --source-type okr_kr --source-id <kr_id> --limit 100
jarvis-tools list-relations --target-type okr_kr --target-id <kr_id> --limit 100
```

关系只存事实成立的一个方向，因此对 O、KR、Point 都要按 source 和 target 两端读取，再沿 `maps_to`、`advances` 等已确认关系寻找现实锚点。没有已确认关系时，读取 `okr-world-projector` Skill 建立或审阅映射；锚点不明确时跳过并报告，不做全租户宽泛搜索。

## 2. 读取 Meego 证据

先以当前安装版本的帮助为准发现只读命令：

```bash
bytedcli --json --all-help | rg -i 'meego|work.?item'
```

只运行查询/list/get/search 类命令。每条候选证据至少保留：工作项稳定 ID、标题、当前状态、负责人、最近更新时间、来源链接（若返回）以及本轮查询时间。不要只凭标题相似就关联；至少还要有 Project 代号、明确的 OKR/项目引用、负责人一致或已有 summary 链接之一。

每个页面已明确绑定的工作项，都把本轮只读结果写成模块观察快照。查询失败也要写 `fetch_error`，但不要编造 remote 字段：

```bash
scripts/biz-okr-tools record-meego-observation --payload - <<'JSON'
{
  "point_id": "<具体 KR 点 ID>",
  "work_item_id": "<Meego 工作项 ID>",
  "week": "<YYYY-Www>",
  "observed_at": "<RFC3339>",
  "remote": {"title":"<标题>","status":"<状态>","progress":"<进展>","updated_at":"<RFC3339 或空>"},
  "fetch_error": ""
}
JSON
```

这个接口只保存工具已经读到的观察值并生成 UI 差异，不会自己调用 bytedcli。未在页面绑定的候选工作项不写模块快照。

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

进入关联步骤前先读取目标关系和实体 Page。标题相似、同负责人或同一群都不能单独作为自动关联依据；新的映射必须使用 `create-relation` 保存来源证据。

消息只有在明确提及结果、风险、决定、交付物或下一里程碑时才构成进展证据。闲聊、转述、自动机器人通知和仅有“收到/在看”的消息不更新 OKR。

## 4. 汇总并写回世界模型

先判断证据属于哪个最小实体：KeyMatter 优先，其次 Project，最后才是 OKR。向上汇总时引用下级实体，不复制整段明细。

1. 仅当“当前结论”发生变化时写回。先 `jarvis-tools get-page`，用 `jarvis-tools append-fact` 保存客观证据，再以 `updated_at` 调用 `jarvis-tools update-page`。CAS 冲突时重新读取并合并，绝不覆盖新内容。
2. 世界 Page 第一行保持一句话结论，后续写当前状态、风险、下一检查点，并引用已确认的下级实体。
3. 不调用 `create-task`、`start-task` 或任何消息发送命令。

## 5. 形成 O、KR、Point 周期判断

从 Point 开始自下而上判断，再判断 KR 和 Objective。只有现实证据、人工正式进展、指标和下级判断足以支撑对应层级结论时，才维护该层 WorldProgress。KeyMatter 是现实事项，不是进展判断本身；不能把它的 status 机械复制成 Point 进展，也不能把下级灯色按固定公式机械汇总为上级灯色。

先读取同一 Point 和周次：

```bash
jarvis-tools get-world-progress \
  --subject-type okr_point --subject-id '<point_id>' --period-key '<YYYY-Www>'
```

读取上级时分别使用 `okr_kr` 和 `okr_objective`。Point 证据不足不妨碍在 KR 或 O 存在直接证据时形成上级判断；反过来，也不能仅因一个 Point 已完成就宣告整个 KR 或 O 完成。

不存在时创建，存在时携带读到的 `version` 更新：

```bash
jarvis-tools create-world-progress --payload - <<'JSON'
{
  "expected_version": 0,
  "subject_type": "okr_point",
  "subject_id": "<point_id>",
  "period_key": "<YYYY-Www>",
  "signal": "yellow",
  "summary": "当前：...\n本周变化：...\n风险与缺口：...\n下一观察点：...",
  "evidence": {"refs": ["fact:12"], "coverage": "..."},
  "evidence_until": "<RFC3339>"
}
JSON

jarvis-tools update-world-progress --id '<world_progress_id>' --payload - <<'JSON'
{
  "expected_version": 2,
  "signal": "green",
  "summary": "当前：...\n本周变化：...\n风险与缺口：...\n下一观察点：...",
  "evidence": {"refs": ["fact:12", "task:39"], "coverage": "..."},
  "evidence_until": "<RFC3339>"
}
JSON
```

- `signal` 只选 `unknown/green/yellow/red`，完整判断写在 summary，不另造状态词。
- `evidence_until` 是证据覆盖截止时间，不是执行时间；没有新证据时不刷新。
- 相同内容不重复写；409 冲突时重新读取并根据新内容重新判断。
- 写后按 ID 回读。WorldProgress 是 Jarvis 独立判断，不创建或更新任何正式 KRProgress。
- 同一周期最多分别维护一条 O、KR、Point 判断；允许某一层暂无判断，不能为了填满页面编造结论。

## 6. 定时巡检

周期执行必须复用 ScheduledTask；每次触发只创建一个普通 Task。不要在业务表或 OKR 上保存 scheduler 状态。

```bash
jarvis-tools create-scheduled-task --payload - <<'JSON'
{
  "title": "OKR 只读进展巡检",
  "action_type": "agent_task",
  "instruction": "读取 weekly-report-progress-sync Skill 并执行一次。",
  "context_snapshot": {"skill": "weekly-report-progress-sync", "module": "biz-okr", "mode": "read_only"},
  "schedule_type": "interval",
  "interval_minutes": 360,
  "enabled": true
}
JSON
```

创建前先 `list-scheduled-tasks`，存在同名 active 任务时更新它，不重复创建。定时任务的最后结果必须包含：扫描 OKR 数、人工/Meego/消息证据数、写入 Fact 数、更新 Page 数、创建或更新 WorldProgress 数、跳过原因和覆盖缺口。

## 完成检查

- 每个写入都能回读到对应实体和来源；重复执行不会重复投递同一版本证据。
- 已用模块工具回读目标周的完整层级，且读取没有创建 Task。
- 所有跨模块映射都能通过 `list-relations` 回读，且带来源证据。
- Page CAS 冲突已经重新读取并合并；没有把历史明细覆盖掉。
- 每个写入的 WorldProgress 都已回读，证据截止时间真实；没有新证据时没有刷新判断。
- 没有产生 OKR 专用 Task 关系，也没有发送消息或修改外部系统。
- 明确列出证据覆盖时间窗、查询锚点、更新项、未确认关联和失败来源。
