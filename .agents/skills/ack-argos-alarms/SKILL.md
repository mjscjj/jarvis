---
name: ack-argos-alarms
description: 确认飞书群中主值班人包含 principal 的 ArgosDuty 报警。适用于“把群里最近几天值班有我的报警全部 ACK/确认”“确认这条 Argos 报警”等请求；只处理未确认的 Problem，并从 Argos 页面和原飞书卡片双重回读结果。
---

# 确认 Argos 报警

只完成一件事：把指定范围内“主值班人包含我”的未确认 Argos Problem ACK。不要把群回复、表情或已读状态当作 ACK，不修改报警规则，不屏蔽通知。

## 1. 确认身份和范围

```bash
jarvis-tools get-principal
lark-cli auth status --json --verify
```

两处 principal `open_id` 必须一致。按用户原话确定群名和时间窗；“最近三天”默认解释为 principal 时区内今天及前两个自然日。时间边界使用带时区的 RFC3339，不用机器默认时区。

用 user 身份精确定位群。只接受唯一的同名正常群，不选名称带“废弃”的近似结果：

```bash
lark-cli im +chat-search --as user --query "<群名>" --page-all --format json
```

## 2. 找出待 ACK Problem

```bash
lark-cli im +chat-messages-list \
  --as user \
  --chat-id "<chat_id>" \
  --start "<窗口起点>" \
  --end "<窗口终点>" \
  --order asc \
  --page-all --page-limit 50 \
  --format json
```

只选择同时满足以下条件的消息：

- `msg_type=interactive`，发送方是 Argos 报警应用；
- 正文的“主值班人”一行包含 principal `open_id`；
- 卡片仍含 `[确认问题]`，且不含 `[已确认]`；
- 正文包含 `/argos/alarm/space/...` 的问题详情 URL 和 `anomalyId`。

同一 `anomalyId` 只处理一次。先列出命中数量和摘要；用户当前请求已明确要求 ACK 时直接继续，不重复请示。用户只要求查看、统计或诊断时停在只读结果，不执行下一节。

## 3. 执行 ACK

先复用或刷新当前用户的 ByteCloud 浏览器登录态：

```bash
bytedcli --site i18n-tt --json auth login --session --auto --yes
```

然后把每个去重后的问题详情 URL 作为一个 `--url` 传给脚本；必须显式加 `--ack` 才会写入：

```bash
node .agents/skills/ack-argos-alarms/scripts/ack-problem.mjs \
  --url "<problem_url_1>" \
  --url "<problem_url_2>" \
  --ack
```

脚本只允许 Argos 协作空间 URL，逐条核对 URL 中的 `anomalyId`、详情抽屉和唯一可用的 ACK 按钮。输出状态：

- `acked`：本次已 ACK，且 Argos 活动日志出现 `Acked Problem`；
- `already_acked`：执行前已经 ACK，幂等跳过；
- 非零退出：未能可靠确认结果，保留原始错误并停止，不猜测成功。

## 4. 回读飞书卡片

对本次 `acked` 的每个 `message_id` 逐条读取：

```bash
lark-cli im +messages-mget \
  --as user \
  --message-ids "<message_id>" \
  --format json
```

必须同时看到卡片标题含 `[已确认]`，正文不再含 `[确认问题]`，并出现“确认了报警”的审计记录，才计为本次成功。短暂未同步时可重读数次；不要重复点击 ACK。最后汇报范围、命中总数、原已确认数、本次确认数、失败数和剩余未确认数。

## 失败边界

- 找不到唯一群、身份不一致、登录态无法复用、页面没有精确命中目标 Problem、ACK 按钮不唯一或双重回读失败时，立即停止并报告具体对象与原始错误。
- 不通过构造未知 Argos API、修改第三方卡片、发送群消息或添加表情来伪造确认。
- 不 ACK “主值班人”中没有 principal 的报警。
