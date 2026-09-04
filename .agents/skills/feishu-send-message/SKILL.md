---
name: feishu-send-message
description: 使用 lark-cli 通过 {{AGENT_NAME}} Bot 给个人或群聊发送飞书消息。适用于通知、提醒、回复、总结、图片和文件。
---

# 飞书发消息

所有 M5 普通业务消息都使用本 Skill，不区分消息是否回到 Task 的来源会话。消息发送和回复始终使用 {{AGENT_NAME}} Bot；联系人查询、群搜索和助手群创建使用 principal 的 user 身份。禁止把发送失败 fallback 成 user 身份、另一个目标或另一种会话。

## 0. 先判断审批，未获授权不要写

是否需要审批只服从 M5 当前注入的审批策略。调用本 Skill 之前，M5 必须已经确定准确目标、会话位置或原消息锚点、mention 对象和完整文案，并对这一次具体发送完成审批判断。

- 需要审批：不执行本 Skill 的任何写命令，不另发文字提醒；只返回包含准确目标和完整文案的 proposal，审批卡片由 {{AGENT_NAME}} runtime 发送。
- 不需要审批，或当前是 apply 阶段执行已经批准的 proposal：继续下面步骤。

### 审批通知不由这个 Skill 发送

不要用本 Skill 发送审批卡片或纯文字提醒。{{AGENT_NAME}} runtime 会先持久化提案和 `awaiting_approval`，再发送绑定当前 Task version 的确认／拒绝卡片。本 Skill 只负责任务本身需要的普通业务消息。

## 1. 固定并核验 {{AGENT_NAME}} 身份

先读取 principal 的世界模型资料：

```bash
jarvis-tools get-principal
```

结果必须提供 principal `open_id`。Task 可能在任意业务仓库执行，不要从当前 cwd 猜 Jarvis 配置；从 `jarvis-tools` 的真实路径定位 Jarvis workspace，再用现有只读配置命令读取合并后的生效身份：

```bash
JARVIS_TOOLS_REAL="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$(command -v jarvis-tools)")"
JARVIS_REPO_ROOT="$(cd "$(dirname "$JARVIS_TOOLS_REAL")/.." && pwd)"
(cd "$JARVIS_REPO_ROOT" && go run ./cmd/jarvis-config show-principal --config conf/config.yaml) \
  | jq -c '{principal_open_id}'
```

这个结果的 `principal_open_id` 必须与 `jarvis-tools get-principal` 完全相同。命令缺失、配置读取失败或两个 principal 不一致都停止，不能改读 Task 仓库里的同名文件。所有飞书命令使用 lark-cli 当前默认身份，先显式核验：

```bash
lark-cli auth status --json --verify
```

只有同时满足以下条件才继续：

- `identities.user.status=ready`，且 user `openId` 与 principal `open_id` 完全相同；
- `identities.bot.status=ready`，Bot 是 {{AGENT_NAME}} Bot；
- 顶层 `appId` 是当前 {{AGENT_NAME}} App，后续建群时把它作为 `--bots` 参数。

任一项缺失或不一致都停止并把原始结果交回 M5，不能用错误身份查群、建群或发送，也不能临时切换到另一个 profile。

## 2. 解析目标，不猜人

优先使用 Task 上下文中已经给出的目标 `open_id`。只有缺少 ID 时才搜索：

```bash
lark-cli contact +search-user \
  --query "<姓名或邮箱>" \
  --as user
```

搜索结果必须唯一且身份信息与上下文一致。重名、多匹配、没有匹配或身份仍不确定时 fail-fast，不猜 `open_id`、不发送。

## 3. 选择准确会话

### 给我（principal）发消息 / 主动 ping

当任务是 `notify_principal`，或需要把有用信息主动告知我本人时，直接给我发一条清晰、有结论的飞书消息。

给我本人发单聊最简单：{{AGENT_NAME}} Bot 与我已有私聊关系，直接用我的 open_id 发，不用建群：

```bash
lark-cli im +messages-send \
  --user-id "<principal open_id>" \
  --markdown "<消息内容>" \
  --idempotency-key "<稳定幂等键>" \
  --as bot
```

如果 `--user-id` 直发失败，把原始错误交回 M5；不要自动创建群、改用 user 身份或更换目标。

写给我的消息要点：说清是什么、为什么值得我知道、我可能要做什么；只发真正有用的，不制造噪音。

### 给个人发消息

除 principal 本人外，不让 Bot 直接私聊对方。先用 principal 和对方的 open_id 搜索已有私有助手群；`+chat-search` 需要按返回的 page token 查完所有页：

```bash
lark-cli im +chat-search \
  --member-ids "<principal open_id>,<target open_id>" \
  --search-types private \
  --chat-modes group \
  --is-manager \
  --page-size 100 \
  --as user
```

若响应的 `page_token` 非空，就把它作为下一次相同查询的 `--page-token`，直到返回空 token；不得只检查第一页。

对每个候选读取完整成员集合：

```bash
lark-cli im +chat-members-list \
  --chat-id "<candidate chat_id>" \
  --member-types user --member-types bot \
  --page-all --page-limit 0 \
  --as user
```

只有 `users` 恰好是 principal 与对方、`bots` 恰好是当前 {{AGENT_NAME}} App、没有 `truncations`，且群用途确实是该助手群时才算合格。唯一合格候选才复用；多个候选、成员不完整或用途不确定时停止，不能猜。

没有合格候选时，由 principal 的 user 身份创建私有助手群。principal 作为创建者已在群中，`--users` 加入对方，`--bots` 加入刚核验的 {{AGENT_NAME}} `appId`：

```bash
lark-cli im +chat-create \
  --name "{{AGENT_NAME}} - <对方姓名>" \
  --users "<对方 open_id>" \
  --bots "<{{AGENT_NAME}} app_id>" \
  --type private \
  --chat-mode group \
  --as user
```

创建后必须再次用 `+chat-members-list --page-all --page-limit 0` 做同样的完整核验。建群是已经发生的独立副作用：成功后在最终结果申报一条 `feishu_chat` effect；即使随后发消息失败，也不能把已建群写成没有发生。重跑时先搜索并复用这个群。

然后用 Bot 在群里发送。按 M5 rules 构造真实 mention：除私聊 principal 本人外，必须真实 `@` principal；面向具体个人时同时真实 `@` 对方。助手群因此同时 `@` 对方和 principal：

```bash
lark-cli im +messages-send \
  --chat-id "<chat_id>" \
  --markdown '<at user_id="<target open_id>"><对方姓名></at> <at user_id="<principal open_id>"><principal 姓名></at> <消息内容>' \
  --idempotency-key "<稳定幂等键>" \
  --as bot
```

### 群聊前置：确认 {{AGENT_NAME}} Bot 在这个群里

下面两种群聊发送都要求 Bot 已是群成员。先用 principal 的 user 身份读回 bot 成员：

```bash
lark-cli im +chat-members-list \
  --chat-id "<chat_id>" \
  --member-types bot \
  --page-all --page-limit 0 \
  --as user
```

`bots` 不含第 1 步核验过的 {{AGENT_NAME}} `appId` 时，用 principal 的 user 身份把 Bot 拉进这个群：

```bash
lark-cli im chat.members create \
  --params '{"chat_id":"<chat_id>","member_id_type":"app_id"}' \
  --data '{"id_list":["<{{AGENT_NAME}} app_id>"]}' \
  --as user
```

拉群后重新执行上面的成员读回确认成功，再用 Bot 发送。入群是已经发生的独立副作用，成功后申报一条 `feishu_chat` effect。失败（群限制只有群主/管理员可加人、principal 不在群里等）把原始错误交回 M5，不改用 user 身份发送、不换目标会话、不放弃发送。

### 在群聊里给某个人发消息

由 M5 根据语义选择准确的原消息锚点，不使用“最新一条消息”替代判断。在相关消息下面创建话题，并同时真实 `@` 对方和 principal：

```bash
lark-cli im +messages-reply \
  --message-id "<message_id>" \
  --markdown '<at user_id="<target open_id>"><对方姓名></at> <at user_id="<principal open_id>"><principal 姓名></at> <消息内容>' \
  --reply-in-thread \
  --idempotency-key "<稳定幂等键>" \
  --as bot
```

### 给整个群发消息

存在明确原消息锚点时仍使用 `+messages-reply --reply-in-thread`，但不因“发给整个群”而额外 `@` 无关成员；仍必须真实 `@` principal。只有没有锚点的主动群公告才直接发到 `chat_id`：

```bash
lark-cli im +messages-send \
  --chat-id "<chat_id>" \
  --markdown '<at user_id="<principal open_id>"><principal 姓名></at> <消息内容>' \
  --idempotency-key "<稳定幂等键>" \
  --as bot
```

## 4. 查重与稳定幂等键

发送前检查 `previous_runs`、目标群和准确线程，已有相同消息或等价内容就不重复发送。新发和回复都必须使用不超过 50 字符的稳定幂等键：

```text
jv-t<JARVIS_TASK_ID>-<不超过8字符的稳定语义槽>-<12位内容哈希>
```

内容哈希由 `operation + target chat/user + anchor message（没有则空）+ 完整最终文案` 共同计算。同一逻辑动作在 retry、resume 和 schema 重写后必须复用同一个语义槽和 key；有意发送第二条不同消息才换语义槽。飞书幂等窗口只有一小时，助手群创建也没有幂等参数，所以这只能降低重复概率，不能承诺 exactly-once。

## 5. 回执检查与 effects

捕获 `lark-cli` 的 JSON 和退出码，按以下顺序确认：

1. 退出码必须为 0；
2. 递归收集响应中的 `message_id`，去重后必须恰好得到一个 `om_...`；
3. 用 Bot 身份读回该消息：

```bash
lark-cli im +messages-mget \
  --message-ids "<message_id>" \
  --as bot
```

4. 读回必须确认消息真实存在，目标会话和内容与本次动作一致。

命令非零、无 ID、多 ID 或读回失败都视为“未确认发送”，不得申报成功 effect，也不得在 summary 里写“已通知”。错误原样交回 M5，由 M5 结合任务目标决定下一步。

发送成功后，M5 在最终结果中申报 `feishu_message` effect。只使用结果 schema 允许的顶层字段：`kind/title/url/target/preview/extra`；`message_id`、`chat_id`、`anchor_message_id`、`operation` 和 `idempotency_key` 写进 `extra` JSON 字符串，不发明顶层字段。例如：

```json
{
  "kind": "feishu_message",
  "title": "已向目标发送结果",
  "url": "",
  "target": "<人或群>",
  "preview": "<消息摘要>",
  "extra": "{\"message_id\":\"om_xxx\",\"chat_id\":\"oc_xxx\",\"anchor_message_id\":\"om_root\",\"operation\":\"reply\",\"idempotency_key\":\"jv-t...\"}"
}
```

effects 是 M5 根据已核验工具结果作出的展示申报，不是 runtime 独立验证的可信 outbox。发送成功后若 M5 在最终结果落盘前崩溃，可能出现飞书已有消息但 effect 未记录；恢复时依靠查重、稳定 key 和读回降低重复风险。

## 其他消息类型

根据内容把 `--markdown` 换成 `--text`、`--image`、`--file`、`--video` 或 `--audio`，审批、身份、幂等、读回和 effect 规则不变。不清楚参数时先运行对应命令的 `--help`。
