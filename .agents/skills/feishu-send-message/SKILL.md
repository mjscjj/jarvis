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

当你判断某个动作要先请我批准（依据见 `conf/prompts/m5-approval-policy.md`），别发纯文字，发一张交互卡片，我扫一眼就知道是什么事。卡片正文永远用大白话讲清三件事：**要做什么**、**会产生什么影响**、**你的判断**。

按钮分两档，由你按动作的具体后果判断该给哪一档——**跟动作类型无关，只看这次改了什么**。但 callback 还有一个硬前提：有效配置里的 `card_approval.enabled=true`，且 `transport`、`profile`、`principal_open_id` 都非空。没启用就只能发「查看详情」链接卡，不能发点了没反应的同意/拒绝按钮。

- **callback 已启用，且动作简单、低风险、后果一句话说得清**（回个"收到"、改个注释、跑个只读查询…）→ 给 `[同意]` `[拒绝]` `[查看详情]` 三个按钮。我在飞书里点一下就直接落地，不用进后台。同意/拒绝是 callback 按钮，`value` 里带 `action` 和本任务的 `task_id`。
- **高风险、对外承诺、删改线上、或后果说不清** → 只给 `[查看详情]` 一个跳转按钮，让我进后台看全貌再定，别让我在卡片上盲点。

**带同意/拒绝的卡片**（简单动作）：

```bash
lark-cli --profile "<card_approval.profile>" im +messages-send \
  --user-id "<card_approval.principal_open_id>" \
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
        "tag": "column_set",
        "flex_mode": "flow",
        "horizontal_spacing": "medium",
        "columns": [
          {
            "tag": "column",
            "width": "weighted",
            "weight": 1,
            "elements": [{
              "tag": "button",
              "text": { "tag": "plain_text", "content": "同意" },
              "type": "primary_filled",
              "width": "fill",
              "behaviors": [
                { "type": "callback", "value": { "action": "jarvis_approval", "decision": "approve", "task_id": <task_id> } }
              ]
            }]
          },
          {
            "tag": "column",
            "width": "weighted",
            "weight": 1,
            "elements": [{
              "tag": "button",
              "text": { "tag": "plain_text", "content": "拒绝" },
              "type": "danger",
              "width": "fill",
              "behaviors": [
                { "type": "callback", "value": { "action": "jarvis_approval", "decision": "reject", "task_id": <task_id> } }
              ]
            }]
          },
          {
            "tag": "column",
            "width": "weighted",
            "weight": 1,
            "elements": [{
              "tag": "button",
              "text": { "tag": "plain_text", "content": "查看详情" },
              "type": "default",
              "width": "fill",
              "behaviors": [
                { "type": "open_url", "default_url": "http://127.0.0.1:18800/", "pc_url": "", "ios_url": "", "android_url": "" }
              ]
            }]
          }
        ]
      }
    ]
  }
}' \
  --as bot
```

**只给查看详情的卡片**（高风险/说不清）：去掉上面两个 callback 按钮，只留最后那个 `[查看详情]` 的 open_url 按钮（`type` 设 `primary_filled`、加 `"width": "fill"` 撑满成强焦点）。若 `card_approval.enabled=true`，继续用独立 `profile` 和该 app 的 `principal_open_id` 发送；若 callback 未启用，按本 Skill 前面的普通 principal 私聊路径发送（不加 `--profile`，使用 `jarvis-tools get-principal` 返回的原 Jarvis Bot open_id）。

`task_id` 必须填成本任务真实的数字 ID；`<card_approval.profile>` 和 `<card_approval.principal_open_id>` 必须逐字使用有效配置值，不能省略。`transport=cc_connect` 时二者就是当前 Jarvis Bot 的 profile/open_id，CC Connect 继续独占该 app 的长连接；`transport=standalone_app` 时二者才是独立审批 app 的值。发卡片与接收 callback 必须属于同一个 app。发送成功后，从 lark-cli 原始返回中取真实 `message_id`，在本轮 `effects[]` 的 `feishu_message.extra` 里原样记录 `{"message_id":"om_..."}`；服务端用它把点击绑定到当前 proposal，不能遗漏或编造。同意/拒绝的 callback 由 jarvis-server 直接落地（点一下就走已有的 approve/reject），并把卡片就地更新成「已同意/已驳回」——你不用再管后续，也不要自己再去调审批接口。`[查看详情]` 是纯本地跳转，不回调。卡片连续发送失败就退回 `--markdown` 纯文字通知（正文照样讲清三件事 + 后台入口），别卡在这。

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
