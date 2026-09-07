---
name: feishu-broadcast
description: 使用“Jarvis通知机器人”向一批已确定的公司内收件人直接发送一对一飞书通知，支持同文案批量广播和个性化逐人分发，并核验范围、幂等与发送回执。适用于 OKR/周报催填、全员通知和批量提醒；不用于群聊回复、普通一对一沟通或审批问题卡。
---

# 飞书广播

本 Skill 只负责把一份已经确定的收件人快照和最终文案送达。谁该收到、每个人收到什么以及本次发送是否需要审批，仍由调用它的 Task、业务 Prompt 和 M5 审批策略决定。

广播一律由“Jarvis通知机器人”直接投递到个人私聊，不搜索、复用或创建助手群，不改用 principal 的 user 身份，也不在失败后切换到默认 Jarvis Bot。

## 1. 发送前冻结广播计划

在执行任何写命令前，先形成可审计的广播计划：

- 广播目的和来源 Task；
- 去重后的收件人稳定身份、姓名和最终文案；优先使用跨应用稳定的 `union_id`，没有时使用精确回读的企业邮箱；
- 收件人范围的证据，不能按姓名猜 `open_id`；
- 哪些收件人共享完全相同的内容，哪些是个性化内容；
- 重跑查重依据，以及每条个性化消息的稳定幂等键。

只有 `on_` 开头的真实 `union_id`、非空企业邮箱，或能够明确来源 App并解析出其中之一的 `open_id`，才能进入发送计划。空文案、重复收件人、范围不明确的目标先排除并记录原因。需要请示时不执行任何发送，只返回包含准确受众和完整文案的 `needs_human` 问题。

### 跨 App 身份归一

`open_id` 按飞书 App 隔离，不能把主 Jarvis App 下的 `ou_...` 直接交给通知 App。调用方已经提供 `union_id` 时直接使用；只提供主 App `open_id` 时，先找到 App ID 为 `cli_a96a0c8d82b85cb1` 的唯一 lark-cli profile，并核验 Bot 是 Jarvis Bot、user 是 principal，再用 principal 的只读人员查询精确解析企业邮箱：

```bash
lark-cli --profile '<main Jarvis profile>' auth status --json --verify
lark-cli --profile '<main Jarvis profile>' contact +search-user \
  --user-ids '<main-app open_id>' \
  --as user --format json
```

响应必须恰好返回一人，`open_id` 必须与输入完全相同，姓名需与广播计划一致或有明确中英文别名证据，并取得唯一的 `enterprise_email` 或 `email`。查不到、多人、身份冲突或没有邮箱时跳过并报告身份缺口；不得按姓名到通知 App 猜另一个 `open_id`。主 Jarvis profile 在这里只做身份只读归一，绝不用于发送广播。

## 2. 固定并核验通知机器人身份

通知应用的稳定身份是：

- App ID：`cli_a96a2422f03bdbd7`
- Bot 名称：`Jarvis通知机器人`

先运行 `lark-cli profile list`。优先使用环境变量 `JARVIS_BROADCAST_LARK_PROFILE` 指定的 profile；未指定时，只能选择 App ID 与上面完全一致的唯一 profile。找不到或存在多个无法区分的 profile 时停止，不得改用当前默认 profile。

后续每条命令都显式带上同一个 `--profile` 和 `--as bot`。发送前执行：

```bash
lark-cli --profile '<broadcast profile>' auth status --json --verify
lark-cli --profile '<broadcast profile>' api GET '/open-apis/application/v6/scopes' --as bot --format json
lark-cli --profile '<broadcast profile>' api GET '/open-apis/application/v2/app/visibility' \
  --params '{"app_id":"cli_a96a2422f03bdbd7","user_page_size":1000,"department_page_size":100}' \
  --as bot --format json
```

只有 Bot `status=ready`、`verified=true`、App ID 和 Bot 名称完全匹配，并且权限中包含 `im:message:send_as_bot` 与 `im:message:send_multi_users` 时才继续。应用可用范围必须覆盖全部目标；范围外失败原样记录，不能通过拉群绕过。

## 3. 选择投递方式

### 个性化广播

每个人的正文、缺失项或链接不同时，按已核验的 `union_id` 或企业邮箱逐人直接私聊。这仍然属于一次广播，但每条消息都有独立幂等键和回执；OKR 周报催填使用企业邮箱路径。直接私聊不需要再 `@` 收件人。

```bash
BROADCAST_DATA="$(jq -nc \
  --arg recipient '<recipient union_id or enterprise email>' \
  --arg message '<final plain-text message>' \
  --arg uuid '<stable key, at most 50 characters>' \
  '{receive_id:$recipient,msg_type:"text",content:({text:$message}|tojson),uuid:$uuid}')"
lark-cli --profile '<broadcast profile>' api POST '/open-apis/im/v1/messages' \
  --params '{"receive_id_type":"<union_id or email>"}' \
  --data "$BROADCAST_DATA" \
  --as bot --format json
```

幂等键由 Task ID、广播语义槽、收件人和最终文案共同决定。同一逻辑消息在 retry/resume 中必须复用同一个键。每次成功响应必须恰好包含一个 `om_...`，并用同一 profile 回读：

```bash
lark-cli --profile '<broadcast profile>' im +messages-mget \
  --message-ids '<message_id>' \
  --as bot
```

单人失败不重发已经确认成功的收件人；继续处理其余计划项，最终逐人报告成功、失败和跳过原因。

### 同文案批量广播

至少两名收件人的最终消息完全相同、并且都已取得 `union_id` 时，可以按每批最多 200 个 `union_id` 调用批量接口。批量接口不接受邮箱；只有企业邮箱的收件人即使文案相同也走上面的逐人直发路径。当前通知机器人不使用 `department_ids`；按部门发送必须先确认额外的 `im:message:send_multi_depts` 权限。

```bash
BROADCAST_DATA="$(jq -nc \
  --argjson recipient_union_ids '["on_xxx","on_yyy"]' \
  --arg message '广播正文' \
  '{union_ids:$recipient_union_ids,msg_type:"text",content:{text:$message}}')"
lark-cli --profile '<broadcast profile>' api POST '/open-apis/message/v4/batch_send/' \
  --data "$BROADCAST_DATA" \
  --as bot --format json
```

批量接口没有客户端幂等键。调用前必须检查本 Task 的历史 runs/effects 和相同广播计划指纹；已有等价 `bm-...` 回执时不得再次提交。响应中的 `invalid_union_ids` 必须单独记录，不能把部分接受写成全量成功。

批量发送是异步的。取得唯一 `bm-...` 后，用同一 profile 查询进度：

```bash
lark-cli --profile '<broadcast profile>' api GET \
  '/open-apis/im/v1/batch_messages/<batch_message_id>/get_progress' \
  --as bot --format json
```

`success_user_ids_count` 等于本批有效且应送达人数时才算整批成功；更小则是部分成功，常见原因包括收件人不在应用可用范围。任务尚未处理完成时，由 M5 根据真实目标选择等待并续跑，不用固定次数忙轮询。

需要富文本或交互卡片时，先读取当前版本的 `lark-im` 卡片说明再构造内容；不能把 Markdown 原样塞进批量 `post`，因为该接口的富文本不支持 `md` 标签。

## 4. 完成与留痕

完成时报告计划人数、去重后人数、成功、失败和跳过人数，并保留每个 `om_...` 或每批 `bm-...`、目标、内容摘要、稳定幂等键或计划指纹以及原始错误。

- 个性化直发按已回读的消息申报 `feishu_message` effect。
- 同文案批量发送按已查询进度的批次申报 `feishu_broadcast` effect，`extra` 中记录 `batch_message_id`、计划指纹和人数快照。
- 批量接口只能证明发送进度；需要已读统计时再查询 `read_user`，不得把“已发送”写成“已阅读”。
- 任一失败都不得改成助手群、群公告、principal user 身份或另一个 Bot 作为 fallback。
