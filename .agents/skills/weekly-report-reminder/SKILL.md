---
name: weekly-report-reminder
description: 按最终 OKR 催填 Prompt 定位当周会议，在会前两个时点实时检查完整 Board，先逐人私聊，再按 O-KR 维度群内提醒。仅用于已显式启用的 OKR 催填任务。
module: biz-okr
---

# OKR Review 两轮催填与进展评审

这是一项有外部消息副作用的执行。业务判断的唯一真源是 `okr_agent_weekly_reminder` Prompt；本 Skill 只规定如何取得实时事实、渲染消息和可靠送达。不得调用旧的进展预览或历史快照代替完整 Board 判断。

## 1. 读取最终 Prompt 和消息模板

先读取最终业务 Prompt 和消息模板，并验证响应成功且正文非空：

```bash
scripts/okr-agent-tools prompt --key okr_agent_weekly_reminder
scripts/biz-okr-tools text-config --key weekly_report_reminder_template
```

四类问题的定义、负责人聚合规则和发送口径全部以最终 Prompt 为准。本 Skill 不复制或扩展另一套业务判断。

## 2. 定位当周会议并按时恢复

周期入口由通用 ScheduledTask 负责，本 Skill 不再判断“今天是否周一”。初始执行时读取 `lark-calendar` Skill，使用 principal 的 user 身份与 Prompt 中的稳定参会人 ID，搜索本周完整时间窗内标题完全一致的日程：

```bash
lark-cli calendar +search-event \
  --query '<meeting title>' \
  --start '<monday 00:00:00+08:00>' \
  --end '<next monday 00:00:00+08:00>' \
  --attendee-ids '<calendar owner open_id>' \
  --page-size 30 --as user
```

查完所有分页，只保留标题完全一致的未取消会议实例。结果必须恰好一个；否则按 Prompt 停止。日期边界与两轮时间必须用显式 `Asia/Shanghai` 时区的程序或系统命令计算，不依赖宿主机默认时区。

当下一轮目标时间在未来时，使用：

```bash
jarvis-tools yield-until --at '<RFC3339 +08:00>' --reason '<event_id、当前阶段、已完成轮次和下次目标>'
```

返回 `waiting` 前核对工具返回的 `scheduled_task_id`、`wake_at` 和理由。恢复后先重新搜索并核对会议实例、取消状态和开始时间，再按 Prompt 决定发送、重新等待或跳过。第一轮结束后，无论全部成功、部分失败还是无问题，只要会议仍有效且第二轮在未来，都必须继续为第二轮创建恢复等待。

## 3. 读取目标周和上一有效周

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

## 4. 完成第一轮 AI 进展质量评审

第一轮还必须读取 AI 进展评审 Prompt，并验证正文非空：

```bash
scripts/okr-agent-tools prompt --key okr_agent_progress_review
```

按最终催填 Prompt 的范围，对本轮已读取的完整 Board 做语义评审。评审口径来自 `okr_agent_progress_review`，但对象层级、固定候选负责人、按一级 KR Owner 路由、问题标签和消息格式仍来自 `okr_agent_weekly_reminder`；不得把 AI 评审 Prompt 的单次界面输出格式或“只返回报告”当成本任务的发送口径。

逐项覆盖有内容的 O、一级 KR 和其下 `strategy` / `product` Point，以稳定 ID 留痕。上一周比较使用第 3 节读取的上一有效周 Board，只比较同一稳定对象；没有上一有效周时如实记录覆盖缺口。每条待改建议必须能回指当前 Board 的具体内容，但对外以一级 KR 为单位汇总，综合其 O 与全部 Point，每个一级 KR 最多一句修改建议。数据完整、与上周有实质差异且业务价值写清的 KR 直接通过，不生成建议。不用固定分数或关键词规则代替 Agent 判断。

## 5. 形成第一轮逐人广播计划

严格按最终 Prompt 判断填写完整性问题和 AI 评审问题。填写完整性仍取 KR Owner 与 Point Owner 并集；AI 评审只取 Prompt 固定候选人中与问题所属一级 KR Owner 精确匹配的人。每类按稳定 ID 去重；同一个 KR 可以进入多类。两部分都没有问题的人不进入计划。

使用 `weekly_report_reminder_template` 渲染每个人的完整最终文案：

- 替换 `owner_name`、`week`、`fill_url`；
- 按固定顺序渲染 `missing_progress_section`、`missing_core_section`、`missing_score_section`、`unchanged_section`、`quality_review_section`；
- 零项区块替换为空字符串，非零区块写真实数量和 KR 标题；
- `quality_review_section` 按最终 Prompt 以 O → 一级 KR 层级渲染，每个一级 KR 仅一句综合修改建议，不重复粘贴整段原始进展；
- 填写链接必须指向本次季度、周次的周报填写页；没有可靠的对外地址时报告缺口，不编造链接；
- 不增加 mention、“同步：”、截止时间或模板以外的话术。
- 每条最终文案都必须原样保留“以上为AI评审判断，仅供参考”。

把所有收件人与正文先冻结成一份广播计划，每项包含已核验 `owner_email`、`owner_name`、四类去重 KR ID、完整文案和稳定幂等键。不得根据姓名猜 `open_id`。

同一负责人同时命中填写完整性和 AI 评审问题时合并成一条最终文案，不拆成两次通知。AI 评审固定候选人没有命中 KR Owner、没有问题或身份无法核验时，不因其出现在名单中就发送。

## 6. 通过广播 Skill 发送第一轮

发送飞书消息是具体外部副作用，由当前执行 Agent 按统一审批策略和任务上下文判断是否需要先请示。发送前读取 `feishu-broadcast` Skill，使用 OKR 配置指定的“Jarvis通知机器人”和个性化广播路径；不得搜索、复用或创建助手群，也不得切换为CLI 默认 Bot。

Owner 已使用完整企业邮箱，按当前 OKR 模块固定的通知应用配置逐人直发；不再做旧主应用 ID 转换。

幂等键不得超过 50 字符，同一季度、周次、负责人重跑必须复用同一键。单人发送失败不重发已经确认成功的收件人；继续处理其余人，并逐项保留真实 `message_id` 或原始失败原因。

## 7. 形成并发送第二轮群汇总

第二轮必须重复第 3 节的两周 Board 读取和判断，不得复用第一轮的问题快照。按最终 Prompt 的 O-KR 顺序、问题标签顺序和负责人去重规则渲染一条完整群消息；零问题时跳过。

发送前读取 `feishu-send-message` Skill，但业务 Prompt 指定的通知 Bot、群、mention 和不额外 CC principal 的口径优先于该通用 Skill 的默认示例：

1. 用同一显式通知 profile 的 Bot 身份回读 Prompt 中固定 `chat_id` 的真实群名与 Bot 成员；
2. 目标必须仍为 Prompt 指定的群，当前 OKR 配置指定的通知 Bot 必须是群成员；
3. 群接口确需临时 ID 时，先在同一通知应用内按完整邮箱精确查询，再渲染为真实 `<at user_id="...">姓名</at>`，不额外提及 principal；
4. 用当前 OKR 配置指定的通知 Bot 向固定 `chat_id` 主动发送，幂等键不超过 50 字符；
5. 取得唯一 `om_...` 后用同一 Bot 回读，确认目标群和内容。

群名不匹配、Bot 不在群内、发送或回读失败时保留原始错误，不换群、不切换 user 身份或其它 Bot 发送。

## 完成检查

- 报告会议实例、两轮计划/实际时间、目标周、上一有效周，以及每轮四类去重 KR 数；
- 报告第一轮填写完整性四类去重 KR 数、AI 评审覆盖的 O / KR / strategy Point / product Point 数、三类 AI 质量问题数、广播计划人数、成功、失败、跳过人员，以及第二轮群消息回读结果；
- 报告无上一有效周、无可靠填写链接或身份不可解析等覆盖缺口；
- 没有给无问题、无可验证身份或重复收件人发送；
- 没有修改 OKR、Meego、飞书文档或任何催填快照；
- 所有真实发送都有 lark-cli 成功回执，部分失败如实标记 `partial`。
