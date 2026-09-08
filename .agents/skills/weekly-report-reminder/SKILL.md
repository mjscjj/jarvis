---
name: weekly-report-reminder
description: 按最终 OKR 催填 Prompt 检查目标周与上一有效周的完整 Board，聚合每位负责人的 Review 填写问题，并通过 feishu-broadcast 逐人发送幂等私聊提醒。仅用于已显式启用的 OKR 催填任务。
module: biz-okr
---

# OKR Review 催填

这是一项有外部消息副作用的执行。业务判断的唯一真源是 `okr_agent_weekly_reminder` Prompt；本 Skill 只规定如何取得实时事实、渲染消息和可靠送达。不得调用旧的进展预览或历史快照代替完整 Board 判断。

## 1. 读取最终 Prompt 和消息模板

先读取最终业务 Prompt 和消息模板，并验证响应成功且正文非空：

```bash
scripts/okr-agent-tools prompt --key okr_agent_weekly_reminder
scripts/biz-okr-tools text-config --key weekly_report_reminder_template
```

四类问题的定义、负责人聚合规则和发送口径全部以最终 Prompt 为准。本 Skill 不复制或扩展另一套业务判断。

## 2. 读取目标周和上一有效周

定时节奏由通用 ScheduledTask 负责，本 Skill 不实现星期门禁或补偿调度。优先使用 Task 明确给出的 `quarter`、`week`；没有给出时才读取当前已开启范围：

```bash
scripts/biz-okr-tools scope
```

使用确定的季度和周次读取目标周完整 Board：

```bash
scripts/biz-okr-tools board --quarter '<quarter>' --week '<week>'
```

验证响应成功，并从目标周 Board 的 `previous_week` 取得上一有效周。存在 `previous_week` 时再读取上一周完整 Board：

```bash
scripts/biz-okr-tools board --quarter '<quarter>' --week '<previous_week>'
```

两次读取都必须固定同一季度，并以稳定 KR/Point ID 对照。没有上一有效周时，只判断不依赖上周的类别，并把“无法判断跟上周一样”作为覆盖缺口报告；不得猜一个自然周。Board 读取失败就 fail-fast，不绕过模块工具查询数据库。

`open-week` 虽然是可用原子工具，但催填不隐式开启新周。没有已开启周时报告缺口；只有本次 Task 明确要求开周，才单独调用 `open-week`，随后重新读取范围和完整 Board。

## 3. 形成逐人广播计划

严格按最终 Prompt 判断“进展缺失、核心数据缺失、一级 KR 评分缺失、跟上周一样”，再取 KR Owner 与 Point Owner 并集并按负责人聚合。每类按稳定 KR ID 去重；同一个 KR 可以进入多类。没有问题的人不进入计划。

使用 `weekly_report_reminder_template` 渲染每个人的完整最终文案：

- 替换 `owner_name`、`week`、`fill_url`；
- 按固定顺序渲染 `missing_progress_section`、`missing_core_section`、`missing_score_section`、`unchanged_section`；
- 零项区块替换为空字符串，非零区块写真实数量和 KR 标题；
- 填写链接必须指向本次季度、周次的周报填写页；没有可靠的对外地址时报告缺口，不编造链接；
- 不增加 mention、“同步：”、截止时间或模板以外的话术。

把所有收件人与正文先冻结成一份广播计划，每项包含来源 `owner_open_id`、`owner_name`、四类去重 KR ID、完整文案和稳定幂等键。不得根据姓名猜 `open_id`。

## 4. 通过广播 Skill 逐人直发

发送飞书消息是具体外部副作用，由当前执行 Agent 按统一审批策略和任务上下文判断是否需要先请示。发送前读取 `feishu-broadcast` Skill，使用其中写死的“Jarvis通知机器人”和个性化广播路径；不得搜索、复用或创建助手群，也不得切换为默认 Jarvis Bot。

来源 `owner_open_id` 属于主 Jarvis App，必须先按 `feishu-broadcast` 的跨 App 身份归一步骤精确核验身份并取得企业邮箱，再由通知 App 逐人直发；不得把来源 `open_id` 原样交给通知 App。

幂等键不得超过 50 字符，同一季度、周次、负责人重跑必须复用同一键。单人发送失败不重发已经确认成功的收件人；继续处理其余人，并逐项保留真实 `message_id` 或原始失败原因。

## 完成检查

- 报告目标周、上一有效周，以及四类去重 KR 数；
- 报告广播计划人数、成功、失败、跳过人员和消息回读结果；
- 报告无上一有效周、无可靠填写链接或身份不可解析等覆盖缺口；
- 没有给无问题、无可验证身份或重复收件人发送；
- 没有修改 OKR、Meego、飞书文档或任何催填快照；
- 所有真实发送都有 lark-cli 成功回执，部分失败如实标记 `partial`。
