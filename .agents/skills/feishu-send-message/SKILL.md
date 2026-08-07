---
name: feishu-send-message
description: 使用 lark-cli 通过 Jarvis Bot 给个人或群聊发送飞书消息。适用于通知、提醒、回复、总结、图片和文件。
---

# 飞书发消息

消息使用 Jarvis Bot 发送。企业不支持 send as user。

## 给我（principal）发消息 / 主动 ping

当任务是 `notify_principal`，或需要把有用信息主动告知我本人时，直接给我发一条清晰、有结论的飞书消息。

先拿到我的身份（open_id、与 Jarvis Bot 的私聊 chat_id）：

```bash
jarvis-tools get-principal
```

给我本人发单聊最简单：Jarvis Bot 与我已有私聊关系，直接用我的 open_id 发，不用建群：

```bash
lark-cli im +messages-send \
  --user-id "<principal open_id>" \
  --markdown "<消息内容>" \
  --as bot
```

如果 `--user-id` 直发失败（极少见，说明还没建立私聊关系），再按下面「给个人发消息」用我的 open_id 创建 Jarvis 私有群后发。

写给我的消息要点：说清是什么、为什么值得我知道、我可能要做什么；只发真正有用的，不制造噪音。

### 审批通知不由这个 Skill 发送

当你判断下一步需要 principal 审批时，不要用本 Skill 发送审批卡片或纯文字提醒。只在 M5 最终结果里返回 `needs_approval=true` 和完整 proposal；Jarvis 会先持久化提案，再自动发送绑定当前 Task version 的确认/拒绝卡片。本 Skill 只负责任务本身确实需要发送的普通业务消息。

## 给个人发消息

先解析对方的 open_id：

```bash
lark-cli contact +search-user --query "<姓名或邮箱>" --as user
```

如果已有与对方的助手群，直接复用 chat_id。否则创建包含我、对方和 Jarvis Bot 的私有群：

```bash
lark-cli im +chat-create \
  --name "Jarvis - <对方姓名>" \
  --users "<对方 open_id>" \
  --bots "<Jarvis app_id>" \
  --as user
```

然后在群里发送消息并 @ 对方：

```bash
lark-cli im +messages-send \
  --chat-id "<chat_id>" \
  --markdown '<at user_id="<open_id>"><姓名></at> <消息内容>' \
  --as bot
```

## 在群聊里给某个人发消息

在相关消息下面创建话题并 @ 对方：

```bash
lark-cli im +messages-reply \
  --message-id "<message_id>" \
  --markdown '<at user_id="<open_id>"><姓名></at> <消息内容>' \
  --reply-in-thread \
  --as bot
```

## 给整个群发消息

```bash
lark-cli im +messages-send \
  --chat-id "<chat_id>" \
  --markdown "<消息内容>" \
  --as bot
```

## 其他消息类型

根据内容把 `--markdown` 换成 `--text`、`--image`、`--file`、`--video` 或 `--audio`。不清楚参数时运行对应命令的 `--help`。
