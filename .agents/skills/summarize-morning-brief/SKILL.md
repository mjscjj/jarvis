---
name: summarize-morning-brief
description: 每个工作日开工前生成晨间作战简报。读日历容量、未闭环承诺、隔夜变化和当前 Task 状态，选出最多三个今日结果，写本地 Markdown，并在定时触发时只给 Principal 本人发一条飞书私聊。适用于晨报、开工简报、今天先做什么、morning brief。
---

# 晨间作战简报

你在这条任务里扮演开工对齐助手。产出一份面向今天的计划建议，不是昨天复盘，也不是把所有
未完成事项再列一遍。

## 1. 解析本轮硬边界

从当前任务指令读取：

- 自然日 `YYYY-MM-DD`
- 时区
- `trigger`：`schedule` 或 `manual`
- 当前时间

再确定投递策略：

- `schedule`：生成后投递；若当天目录已有 `delivered:` 记录，则只更新文件、不再投递
- `manual`：只更新文件，一律不投递（想连投递一起验就用 `-morning-brief-deliver`，它按 `schedule` 走）

## 2. 确认身份与目录

```bash
jarvis-tools get-principal
```

工作目录：

```text
data/morning-brief/YYYY-MM-DD/
├── 00-context.md
├── 10-evidence-*.md
└── 99-brief.md
```

```bash
mkdir -p "data/morning-brief/<YYYY-MM-DD>"
```

若当天已有 `00-context.md`，先读它。看到 `delivered:` 时记住：本轮默认不再发飞书。

## 3. 确定隔夜增量窗口

证据窗口终点 = 本轮当前时间。

起点：

1. 找上一份晨报目录（按日期往前找最近一个有 `00-context.md` 的目录）；
2. 读其中的 `- cutoff_at: ...`；
3. 找不到就退化到前一自然日本地 `00:00`，并在覆盖状态里标
   `缺少上一份晨报基线`。

不要用个人日报的 `cutoff_at` 当窗口起点。

本轮自己的 `cutoff_at` 写成终点时间。

## 4. 取证（只读）

可并行，但每条 lane 写自己的证据文件，不互相覆盖。

### 4.1 Jarvis 内部

用 `jarvis-tools`：

- `list-tasks`：重点看 `awaiting_approval`、`needs_human`、`waiting`、`executing`、未闭环状态
- `list-todos`：未闭环线索
- `list-scheduled-tasks`：已到或将到恢复时间的等待
- `list-projects` / `get-context`：活跃项目背景
- `list-facts` / `list-relations`：必要时补背景
- `query-messages`：本地已采集消息（不完整时不假装完整）

必须读**当前**状态。主动巡视刚创建或正在执行的 Task，写「已在推进」，不要再建议「现在开始」。

### 4.2 飞书

用 `lark-cli`（`--as user`）：

- 今天日历与会议：第一场会议、冲突、连续会议、完整工作窗口、准备材料
- 隔夜窗口内的 P2P 与群内显式 @Principal
- 只展开会改变今天计划的关键线程/文档/会议材料

这些只读，不落库、不 `append-clue`。读不到就标 `partial/error`，写原始错误。

### 4.3 工程状态

只围绕今日候选事项和活跃 Task 查：

- 关联仓库 Commit / MR/CR / Review
- CI、测试、部署、运行验收
- 当前可达范围内用「至少 N」口径，不扫全部仓库

### 4.4 昨天背景（可选）

若存在 `data/personal-daily/<昨天或最近一天>/99-report.md`，可读作压缩背景。读不到标
`partial`，不因此失败。

## 5. 写 `00-context.md`

先写或更新本轮上下文，至少包含：

```text
# Morning brief context — YYYY-MM-DD

## Run
- run_id: morning-brief-<本地时间戳>
- trigger: schedule|manual
- window_start: <RFC3339>
- cutoff_at: <RFC3339>
- timezone: Asia/Shanghai

## Coverage
- Jarvis: complete|partial|empty|error|unavailable
- Feishu: ...
- Engineering: ...
- Calendar: ...

## Delivery
- delivered: <message_id> at <RFC3339>   # 仅在真正发出后写入；未投递则写 none 或省略
```

Coverage 行格式必须保持 `- <Lane>: <status>`，status 只能是
`complete/partial/empty/error/unavailable`。

## 6. 选择今日结果

顺序：

1. 先看今天固定日程和可连续工作窗口；
2. 再选最多三个今日结果；
3. 容量不足就主动减少，不要硬凑三个；
4. 日程 `partial/error` 时，正文必须声明「今天日程未取全，以下结果未经容量校验」，
   不得编造时间窗口，但仍可发简报；
5. 最后给出第一个动作，并统计「另有 N 项今天排不进去」。

判断信号（不要打分表）：

- 是否今天截止或有会议/发布节点
- 是否来自 Principal 承诺、leader 交办或会议结论
- 是否阻塞他人或下游
- 外部条件是否具备
- 不做的真实代价
- 今天容量内能否取得可验证结果
- 是否已有等价 Task / 已在执行
- 是否属于活跃项目和 Principal 核心职责

每项必须带**项目名**、为什么今天、卡在哪、建议下一步。写结果，不写「看一下 MR」这类
空动作。

## 7. 写完整版 `99-brief.md`

标题：

```text
# 晨间作战简报 · YYYY-MM-DD
```

完整版可含：

1. 今日一句话
2. 今日必达（最多三个，带项目与证据）
3. 今日固定安排与容量
4. 隔夜变化
5. 延续项 / 阻塞 / 风险
6. 建议的第一个动作与被延后项
7. 数据说明（覆盖状态、窗口、错误原文）

证据 ID、Task/Todo ID、链接放完整版，不塞满飞书正文。

在替换旧 `99-brief.md` 前先在内存里写完；不要留下半截正式稿。

## 8. 飞书一屏投影

飞书正文硬约束：

- 不超过约 800 个中文字符
- 只含五块：今日一句话 / 今日必达 / 今天的安排 / 隔夜变化（最多三条）/ 建议动作+被延后项
- 每项必达结果带项目名
- 结尾必有「另有 N 项今天排不进去」（N 可为 0）
- 给出完整版路径：`data/morning-brief/YYYY-MM-DD/99-brief.md`
- **禁止**出现「回复即可推进」类承诺

轻量日（无会议、无隔夜变化、无未闭环事项）仍发极短开工简报。

## 9. 投递

需要投递时，先读：

```bash
jarvis-tools get-skill --name feishu-send-message
```

然后：

```bash
jarvis-tools get-principal
lark-cli im +messages-send \
  --user-id "<principal open_id>" \
  --markdown "<飞书正文>" \
  --as bot
```

硬约束：

- 收件人只能是 Principal 本人
- 成功后把 `message_id` 和时间写入 `00-context.md` 的 `delivered:` 行
- 失败时保留完整版，在 Coverage/数据说明写原始错误，不换渠道静默重试
- 生成失败时不发送任何看似成功的简报
- 已有 `delivered:` 且本轮未要求重投时，不重复发送

## 10. 结束检查

完成前确认：

- `99-brief.md` 属于本轮 `run_id` 和日期
- 今日结果 ≤ 3，且带项目名
- 覆盖状态显式；日历缺失时有未经容量校验声明
- 隔夜变化基于窗口增量，不重播上一份晨报已说过的事
- 结尾有被延后计数
- 正文无虚假交互承诺
- 最终回复只简短报告完成状态、是否投递、路径；文件才是真源
