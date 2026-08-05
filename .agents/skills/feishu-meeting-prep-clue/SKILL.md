---
name: feishu-meeting-prep-clue
description: 扫描当前用户未来 24 小时内尚未拒绝的飞书会议日程，把每个会议实例及其标题、时间、组织者、参会人、描述和链接作为一条原始线索投递给 Jarvis。适用于未来会议巡扫、会前数据源和逐会处理判断；只采集日程事实，不判断是否需要准备、不创建 Task。
---

# 飞书未来会议线索采集

只采集未来会议的客观安排，交给 M3/M5 判断。不要研究议题、判断重要性、生成简报或创建 Task。

## 1. 确认身份与时间窗

```bash
jarvis-tools get-principal
lark-cli auth status --json --verify
```

日历必须使用 `--as user`。用系统命令计算当前 RFC3339 时间和 24 小时后的时间，禁止手算时区或时间戳。

## 2. 查询未来会议

```bash
lark-cli calendar +agenda \
  --start "<当前 RFC3339>" \
  --end "<24 小时后 RFC3339>" \
  --as user
```

只保留：

- `start_time` 晚于当前时间；
- `self_rsvp_status` 不是 `decline`；
- 有具体开始和结束时间；
- 确实是会议：组织者不是 Principal，或参会人中存在 Principal 之外的用户、群聊或会议室。仅有 Principal 自己的专注时间、占位和提醒不投递。

已取消日程由 `+agenda` 自动过滤。`accept` 和 `needs_action` 都保留；是否最终参加、准备到什么程度由下游结合上下文判断，采集层不猜。

## 3. 逐场补齐客观信息

每个候选会议读取完整日程和全部参会人：

```bash
lark-cli calendar +get \
  --calendar-id primary \
  --event-id "<event_id>" \
  --as user

lark-cli calendar event.attendees list \
  --calendar-id primary \
  --event-id "<event_id>" \
  --user-id-type open_id \
  --page-size 100 \
  --page-all \
  --as user
```

保留原始标题、描述、起止时间、时区、组织者、本人 RSVP、参会人及其 RSVP、群聊、会议室、日程链接和视频会议链接。字段没返回就写“未返回”，不要补写推断。

## 4. 每个会议实例投递一条线索

`external_id` 直接使用具体实例的 `event_id`；重复投递由服务端按 `(calendar, event_id)` 幂等。`occurred_at` 使用本轮实际扫描时间，会议开始时间放在正文中。

```bash
jarvis-tools append-clue \
  --source calendar \
  --external-id "<event_id>" \
  --title "已安排待参加会议：<标题；空标题写未命名会议>" \
  --occurred-at "<本轮扫描时间 RFC3339>" \
  --content - <<'TXT'
日程标题：<summary 或未命名会议>
日程实例 ID：<event_id>
重复日程 ID：<recurring_event_id 或未返回>
开始时间：<RFC3339>
结束时间：<RFC3339>
组织者：<display_name 和 open_id>
本人 RSVP：<accept/needs_action/...>
参会人：<逐项列 display_name、open_id、角色和 RSVP>
参会群聊：<逐项列群名和 chat_id>
会议室：<逐项列名称和 room_id>
日程描述：<原文；未返回则写未返回>
日程链接：<app_link>
视频会议链接：<meeting_url 或未返回>
TXT
```

某场详情读取或投递失败时直接报告会议标题、`event_id` 和原始错误并停止；不要跳过、改用猜测字段或换一套静默 fallback。

## 5. 汇报

报告扫描时间窗、候选会议数、投递数、新增数和重复数，并逐场列标题、开始时间、`event_id` 与投递结果。没有会议就直接说明。
