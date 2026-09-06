---
name: weekly-report-reminder
description: 每周检查周报模块本周未填写项，生成可审计催办快照，并通过 Jarvis Bot 给有真实 open_id 的缺失负责人发送一条幂等提醒。仅用于已显式启用的周报催填 ScheduledTask。
module: agency-okr
---

# 周报每周催填

这是一项有外部消息副作用的定时执行。每轮先确定时间和缺失范围，再逐个发送，最后回读批次；不得根据姓名猜 open_id，不得给已填写完成的人发消息。

## 1. 读取消息模板

定时节奏由通用 ScheduledTask 负责，本 Skill 不实现星期门禁或补偿调度。读取催填模板并验证响应 `code=0` 且正文非空：

```bash
scripts/agency-okr-tools text-config --key weekly_report_reminder_template
```

## 2. 读取当前范围和预览

通过模块自有工具读取，不在指令里硬编码季度、周次或 HTTP 路由：

```bash
scripts/agency-okr-tools scope
scripts/agency-okr-tools reminder-preview
```

工具默认读取当前仓库基础配置与运行时覆盖中的服务地址；仅在明确操作其它实例时设置 `JARVIS_API_BASE`。必须验证响应 `code=0`，并记录 `quarter`、`week`、待提醒人数、缺失 KR 数。预览失败就结束为失败，不能绕过模块工具查询数据库或自行猜测。

`open-week` 虽然是可用原子工具，但本催填行动不隐式开启新周。没有已开启周时报告缺口；只有本次 Task 或绑定 Prompt 明确要求开周，才单独调用 `open-week`，并在开周后重新读取范围。

## 3. 生成审计批次

仅在至少一名负责人需要提醒时创建一次批次：

```bash
scripts/agency-okr-tools create-reminder-batch --quarter '<quarter>' --week '<week>'
```

批次是本轮预览的不可变快照，不代表已经送达。

## 4. 逐人发送

发送飞书消息是具体外部副作用，由 M5 根据统一审批策略和当前任务上下文判断是否需要先请示。不得读取业务配置中的 `approval` 或 `mode` 字段替代该判断；尚未获准时保留批次并等待，不得发送。

只处理同时满足以下条件的 recipient：

- `needs_reminder=true`；
- `can_remind=true`；
- `owner_open_id` 以 `ou_` 开头；
- `message` 非空。

发送前读取 `feishu-send-message` Skill，使用 Jarvis Bot 身份。消息内容以 `weekly_report_reminder_template` 为模板，只替换预览能够直接提供的 `owner_name`、`week` 和 `missing_items`；模板中不存在的事实不得补猜。`missing_items` 使用 `missing_krs` 的真实标题逐行生成，不添加链接或截止时间：

```bash
lark-cli im +messages-send \
  --user-id '<owner_open_id>' \
  --text '<message>' \
  --idempotency-key 'okr-reminder-<week>-<owner_open_id>' \
  --as bot
```

幂等键不得超过 50 字符；超长时把 open_id 部分换成稳定短哈希。同一周同一负责人重跑必须复用同一键。单人发送失败不重发已经成功的收件人；继续处理其余人，并在最终结果逐项保留失败原因。

## 完成检查

- 输出范围、应提醒、成功、失败、跳过四个计数和对应人员；
- 用 `scripts/agency-okr-tools reminder-batches` 回读并确认本轮批次存在；
- 没有给无 open_id、未分配、已填完或重复收件人发送；
- 没有修改 OKR、Meego 或飞书文档；
- 所有真实发送都有 lark-cli 成功回执，部分失败必须如实标记 `partial`。
