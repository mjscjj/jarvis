# 总结与洞察

> Status: current
> Authority: normative module guide
> Last verified: 2026-09-11
> Code source: `internal/dailydigest/`, `internal/morningbrief/`, `internal/insight/`

本模块把已持久化的工作事实整理为面向 principal 的只读产物。语义调查由 Skill 和 Agent 完成，Go 只保证日期边界、调度、幂等、产物路径和最小展示投影。

## 1. 个人日报

个人日报由 `summarize-person-day` Skill 主控，覆盖 Jarvis 内部事件、飞书工作和工程执行三类证据。顶层 Agent 负责归并结论，外部 collector 只采集证据，不各自撰写报告章节。

产物真源是本地 Markdown：

```text
data/personal-daily/YYYY-MM-DD/
├── 00-context.md
├── 10-evidence-jarvis.md
├── 20-evidence-feishu*.md
├── 30-evidence-engineering*.md
└── 99-report.md
```

`daily_digest` 表只保存生成状态、覆盖信息和供 API/UI 使用的摘要投影。重算可以覆盖当前 `99-report.md` 和数据库投影，但不伪造未覆盖数据，也不把历史未闭环事项算成当天发生的事实。

个人日报按配置时区的自然日运行。定时入口只生成 principal 的日报；页面允许手动生成或重算。进程重启会处理遗留的生成状态，失败不会静默标成完成。

## 2. 关键群总结

关键群总结复用 `feishu-group-daily-summary` Skill。数据库消息只提供调查入口，Agent 按需补拉完整时间窗、线程、文档、commit、MR 和相关链接。

群总结与个人日报共用 `daily_digest` 展示投影，但不共享一套固定内容 schema。无候选数据、权限不足或外部查询失败必须保留覆盖边界。

## 3. 晨间作战简报

晨间简报由 `summarize-morning-brief` Skill 生成，面向当天计划而不是回顾昨天。它读取日历容量、未闭环承诺、隔夜变化和当前 Task 状态，选择最多三个今日结果。

Go 侧只负责：

- 按 `morning_brief.schedule` 调度并在启动后最多补跑一次；
- 区分手动运行和定时投递；
- 校验当天正式稿存在；
- 避免同一天重复投递；
- 通过只读 API 展示归档。

产物真源是：

```text
data/morning-brief/YYYY-MM-DD/99-brief.md
```

手动 `-morning-brief-once` 默认只写文件；`-morning-brief-deliver` 使用定时投递语义。生成失败时不发送残缺简报。

## 4. 会议回顾

会议回顾不拥有专用执行流水线。会议事实通过统一 clue 入口进入 M2→M3→M5；只有 M5 明确产出 `meeting_summary` 时，回顾页才展示正文。

`internal/insight/MeetingReviewService` 只做投影：

- 按配置时区把会议线索归入自然日；
- 关联已有 Todo、Task、ExecutionRun 和 effects；
- 展示当前处理状态与明确的会议总结产物；
- 不从普通 Task summary 猜测会议纪要。

## 5. 展示与边界

工作台和回顾页读取上述产物及投影，不成为第二份正文真源。统计只能按实际可达证据描述；覆盖不完整时使用下界或未知，不把采集成功等同于业务结论正确。

这些总结都不直接创建业务 Task。报告中发现的新事实或行动需要继续推进时，仍通过普通对话、clue 或 Task 入口进入现有链路。
