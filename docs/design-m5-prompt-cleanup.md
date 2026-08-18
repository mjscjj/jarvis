# M5 提示词整理方案

## 现状

M5 的指令由三份文件拼成一份，模型只看到拼接结果。按 AGENTS.md §1.2，它们的所有权是：

| 文件 | 应负责 |
|---|---|
| `conf/prompts/m5-system-prompt.md` | 阶段角色、停止条件、输出协议 |
| `conf/rules/m5.md` | principal 的稳定行为偏好 |
| `conf/prompts/m5-approval-policy.md` | 审批判断尺度 |

执行阶段不注入跨阶段规则，这三份就是全部。当前内容与上表有偏离，且有互相抵消的条目。

## 问题

**会改变行为的：**

1. 「凡事principal主动让jarvis干活的，不需要审批」——条件写在「事情源自 principal」上，而绝大多数 Task 都源自他，按字面能盖掉整份需审批清单。真实意图是「他直接对 Jarvis 下的指令」。
2. 「创建新文档不需要审批」和「创建…文档…需要审批」同时在两侧清单里，模型只能任选一边。
3. 「给当前上下文中已解析的 principal 不需要审批」是残句，没有谓语。
4. @ Principal 的要求三处适用范围不一致（「私聊之外的会话」／「我参与的会话」／审批策略里已删），且与 `user_message` 面向来源会话所有人的定位冲突。
5. `m5.md` 的「`notify_principal` 只表示…」引用 `action_type`，但 `TASK_CONTEXT` 不含该字段，M5 看不到，规则不可执行。两处「不按 `action_type` 分流」同理，那是代码约束。
6. 「一定一定要用结构化表达」压过同节的「简单问题用一到四个短句」，也与 `m5.md`「不用分点排版硬凑结构」矛盾。

**所有权与冗余：**

7. 说话风格、`@` 规则长在系统提示词里，应属 `m5.md`；「建议39分钟重试一次」该删，等待时长归模型。
8. `m5.md` 的会议产物处理六条带 `lark-cli` 命令、BAX 排障一条带 `bytedcli`，属于 Skill。
9. `user_message` 的写法在完成标准 6／6.1／6.2 说了三遍；「不得用私聊代替审批」两遍；「能查到的不问我」三遍。

## 改动

`m5-approval-policy.md`

- 豁免条件改成「principal 直接对 Jarvis 下的指令」：私聊指示、群里 @ Jarvis Bot、`execution_supplements`。这三种 M5 都判断得出来——快照里有 `sender_open_id`，群里 @ 机器人在正文是明文 `@Jarvis Bot`。范围限定为他指名的动作和对象，不覆盖 Jarvis 自己另外决定的副作用。现有的发消息特例并入这条。
- 「创建新文档／新分支」补边界（尚未分享给他人），需审批那条排除这两项。
- 补全 (3) 的谓语，删掉 `action_type` 说明。

`m5-system-prompt.md`

- 移出说话风格与输出前自查、`@` 规则到 `m5.md`；删「建议39分钟重试一次」和 `action_type` 分流说明。
- 合并 `user_message` 三段为一段，合并两处审批提醒。

`conf/rules/m5.md`

- 接收移入内容，`@` 规则合并成一条并统一适用范围；表达结构要求统一成一条。
- 重写 `notify_principal` 那条，不引用模型看不到的字段。
- 会议产物处理与 BAX 排障移出到 Skill。
- 合并重复的「能查到的不问我」。

搬层不减体积——`m5.md` 同样嵌在同一份 prompt 里，收益是每条规则只有一个真源。真正离开 prompt 的只有搬进 Skill 的部分。

## 待定

1. `user_message` 要不要 @ principal。建议不 @，只在代表他向来源会话以外发声时 @。
2. 「一定一定要用结构化表达」是否降级为按内容量选择。建议降级。
3. 「给 principal 不需要审批」原意是否不止发私聊消息。
4. `data/shared-memory.md` 是空文件、共享记忆块不注入，但审批策略把 `append-shared-memory` 列为免审批写入。开始用，还是摘掉。

## 验证

改完重新导出完整指令，检查：无同一动作出现在审批清单两侧、`@` 规则只出现一次、全文无 `action_type`、`user_message` 说明只有一处。跑 `internal/execute` 与 `internal/workrule` 的测试。
