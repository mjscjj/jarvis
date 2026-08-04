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

### 请我拍板：发审批卡片

当你判断某个动作要先请我批准（依据见 `conf/prompts/m5-approval-policy.md`），别发纯文字，发一张带跳转按钮的交互卡片，我扫一眼就知道是什么事、点按钮就能去后台处理。

卡片正文用大白话讲清三件事：**要做什么**、**会产生什么影响**、**你的判断**；底部一个按钮跳到后台 `http://127.0.0.1:18800/`（进去就能看到「任务 → 审批中」）。

```bash
lark-cli im +messages-send \
  --user-id "<principal open_id>" \
  --msg-type interactive \
  --content '{
  "schema": "2.0",
  "config": { "wide_screen_mode": true },
  "header": {
    "title": { "tag": "plain_text", "content": "需要你拍板：<一句话概括这件事>" },
    "subtitle": { "tag": "plain_text", "content": "项目 <项目名> · 任务 #<task_id>" },
    "template": "orange"
  },
  "body": {
    "direction": "vertical",
    "padding": "12px 12px 20px 12px",
    "elements": [
      {
        "tag": "markdown",
        "content": "**要做的事**\n<具体要执行的动作，说人话>\n\n**会产生的影响**\n<对外/对线上会发生什么，能不能回滚>\n\n**我的判断**\n<为什么要先问你>"
      },
      {
        "tag": "button",
        "text": { "tag": "plain_text", "content": "去后台处理" },
        "type": "primary_filled",
        "width": "fill",
        "behaviors": [
          { "type": "open_url", "default_url": "http://127.0.0.1:18800/", "pc_url": "", "ios_url": "", "android_url": "" }
        ]
      }
    ]
  }
}' \
  --as bot
```

按钮只是本地跳转打开后台，不回调服务端——批准/驳回在后台点，服务端照常走 `/api/tasks/:task_id/approve|reject`。卡片发送连续失败就退回 `--markdown` 纯文字通知，别卡在这。

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
