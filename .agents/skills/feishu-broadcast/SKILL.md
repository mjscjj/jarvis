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
- 去重后的收件人稳定身份、姓名和最终文案；OKR 使用已核验的完整企业邮箱；其他调用的 `union_id` 必须确认适用于通知应用；
- 收件人范围的证据，不能按姓名猜 `open_id`；
- 哪些收件人共享完全相同的内容，哪些是个性化内容；
- 重跑查重依据，以及每条个性化消息的稳定幂等键。

只有 `on_` 开头的真实 `union_id`、非空企业邮箱，或能够明确来源 App并解析出其中之一的 `open_id`，才能进入发送计划。空文案、重复收件人、范围不明确的目标先排除并记录原因。需要请示时不执行任何发送，只返回包含准确受众和完整文案的 `needs_human` 问题。

### 跨 App 身份归一

OKR 的 `owners.email` / `mentions.email` 已由固定人员目录核验，直接作为发送地址；不再依赖旧主应用做运行时 ID 转换，也不按姓名或邮箱前缀猜收件人。

其他来源若只有裸 `open_id`，必须先查明其来源应用，在该应用下精确取得邮箱；来源未知时停止该收件项。不能把一个应用的 ID 交给另一个应用解析。
## 2. 固定并核验通知机器人身份

通知应用的稳定身份是：

- App ID：`cli_a96a2422f03bdbd7`
- Bot 名称：`Jarvis通知机器人`

OKR 调用从当前实例 `conf/okr-module.yaml` 及 runtime 覆盖读取 `feishu.app_id / cli_profile`（当前 `jarvis-notify-audit`）。先用 `lark-cli profile list` 与 `auth status --verify` 核验 profile 对应实际应用；显式绑定不匹配就停止，不使用全局默认 profile。其他广播调用也必须明确绑定通知应用。

后续每条命令都显式带上同一个 `--profile` 和 `--as bot`。发送前执行：

```bash
lark-cli --profile '<broadcast profile>' auth status --json --verify
lark-cli --profile '<broadcast profile>' api GET '/open-apis/application/v6/scopes' --as bot --format json
lark-cli --profile '<broadcast profile>' api GET '/open-apis/application/v2/app/visibility' \
  --params '{"app_id":"cli_a96a2422f03bdbd7","user_page_size":1000,"department_page_size":100}' \
  --as bot --format json
```

只有 Bot `status=ready`、`verified=true`、App ID 和 Bot 名称完全匹配，并且权限中包含 `im:message:send_as_bot`（只有使用批量接口才额外要求 `im:message:send_multi_users`） 时才继续。应用可用范围必须覆盖全部目标；范围外失败原样记录，不能通过拉群绕过。

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
