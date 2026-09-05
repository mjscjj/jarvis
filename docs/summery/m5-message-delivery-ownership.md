# M5 飞书消息工具化与结果协议解耦方案

## 结论

普通飞书消息是 M5 为完成真实任务而选择的一种外部动作，不是执行结果协议的隐式投影。

目标状态是：

- **M5 决定**是否需要发消息、发给谁、发到哪个会话、是否开话题、是否真实 `@`、发送什么内容以及发送失败后如何继续。
- **Prompt 约束行为边界**：什么时候值得通知、什么时候保持安静、发送成功和 Task 完成是什么关系。
- **`feishu-send-message` Skill 负责操作方法**：根据目标和来源场景选择现有 `lark-cli` 命令，校验真实 `message_id`，并告诉 M5 如何在最终结果中如实申报 message-specific `effects`。
- **运行时代码不再解释消息语义**：不从来源快照推断目标、不根据最终状态自动发送、不替 M5 更新旧消息、不为来源类型增加分支。
- **CC Connect 不在本方案范围内**。它与 M5 是独立交互入口，本方案不改变其 session、route claim 或消息回流。
- **审批卡片继续是机器协议**：是否需要审批由 M5 判断；runtime 只在持久化 `awaiting_approval + proposal + Task version` 后投递可验证卡片。

这不是新增一套消息服务，而是启用 M5 已经拥有的 Skill 与 `lark-cli` 能力，并删除现有 `user_message` 隐式发送链路。

## 背景与根因

### 当前最终生效输入

M5 每个 execution/apply run 的有效输入由以下内容组装：

1. `conf/prompts/m5-system-prompt.md`：M5 阶段角色、目标、停止条件和输出协议；
2. `conf/rules/m5.md`：principal 的稳定执行偏好；
3. `conf/prompts/m5-approval-policy.md`：审批判断尺度；
4. execute 阶段 Skill catalog：命中任务时读取对应 Skill；
5. `internal/toolcatalog`：声明 `jarvis-tools`、`lark-cli`、`bytedcli` 和 `git` 等工具能力；
6. Task context、shared memory、current world 和 previous runs。

`feishu-send-message` 已对 execute 阶段启用，M5 可以通过 `jarvis-tools get-skill --name feishu-send-message` 读取完整操作说明，再直接调用 `lark-cli`。真实 Run 873 已验证“给 principal 直发”这条路径能够成功返回飞书 `message_id`、由 M5 写入 `effects`，并在 `user_message` 为空时正常完成。助手群创建和群线程回复仍需要单独做真实验收，不能把 Run 873 当成全部场景已经验证。

### 当前错误所有权

当前系统同时存在两条消息路径：

| 路径 | M5 决定 | 代码决定 | 后果 |
|---|---|---|---|
| `user_message` 自动投递 | 文案和空/非空 | 回复哪条来源消息、是否线程回复、创建或更新消息、发送时机、失败如何改变 Task 状态 | 输出字段实际变成隐藏命令，绕过 Skill 的场景判断 |
| `feishu-send-message` Skill | 是否发送、目标、会话形态、内容和命令 | 飞书 API 的机器校验 | 符合 Agent-first，但当前只允许用于“来源会话以外”的例外场景 |

`user_message` 路径的问题不是“发送失败没有被吞掉”这么简单。它把一项需要理解上下文的动作拆成了两半：M5 只写内容，Go 根据来源快照替它选择收件人与交互形态。

这会产生四类错误：

1. **来源不等于正确目标。** 私聊来源可能需要创建包含 principal、对方和 Bot 的助手群，而不是让不在私聊中的 Bot 回复原消息。
2. **消息形态需要语义判断。** 群内答复可能应在特定消息下开话题并真实 `@` 某人，也可能应通知整个群，不能由 `chat_mode` 机械决定。
3. **结果状态不等于通知策略。** `completed`、`failed`、`waiting`、`observing` 都可能需要发或不发；状态名不能替模型作决定。
4. **最终输出不应产生隐藏副作用。** 模型应先执行并验证真实发送，再报告本轮结果；不能先输出一个字符串，再由 finalizer 执行它没有显式选择完整参数的动作。

Run 872 是该问题的直接证据：M5 已完成调查并输出结果，runtime 却尝试让 Bot 回复其不在场的真人私聊，飞书返回 `230002 Bot/User can NOT be out of the chat`，最终覆盖了业务执行结论。Run 873 则代表目标形态：M5 自己选择消息动作，成功拿到 `message_id`，记录 effect 后完成。

## 语义所有权

| 语义 | 唯一所有者 | 真源 |
|---|---|---|
| 是否需要发普通业务消息 | M5 | `m5-system-prompt.md` |
| principal 对通知频率、公开范围、助手群、群话题、真实 `@` 和表达的稳定偏好 | M5 rules | `conf/rules/m5.md` |
| 查找/创建助手群、发送/回复命令、mention 语法、幂等与回执检查步骤 | 消息 Skill | `.agents/skills/feishu-send-message/SKILL.md` |
| 飞书发送能力与参数校验 | `lark-cli` | 工具自身及其 schema/Skill |
| Task 结果、状态和审计摘要 | M5 结果协议 | execution output schema |
| 是否需要审批 | M5 | `m5-approval-policy.md` |
| 审批 proposal、Task version、按钮授权回调 | runtime 机器协议 | `internal/execute`、`internal/cardapproval` |
| CC 原生对话/session | CC Connect | 不属于本方案 |

普通消息的判断不进入 Go；查询、命令和参数细节不复制到系统 Prompt 或 rules；审批卡片不进入普通消息 Skill。

## 目标行为

### 统一决策流程

M5 在每轮任务中按同一流程处理消息：

1. 先完成必要调查，确认真实目标和当前结果；
2. 判断现在是否有人需要收到一条消息，以及这条消息是否会推进真实目标；
3. 不需要通知时，不调用消息 Skill；
4. 需要通知时，先确定准确目标、位置和完整文案，再按 `m5-approval-policy` 判断这次具体发送是否需要审批；
5. 需要审批时不读取消息 Skill、不发送、不另发文字提醒，只返回包含准确目标和完整文案的 proposal；
6. 不需要审批，或已经进入 apply 阶段执行获批 proposal 时，读取 `feishu-send-message` Skill；
7. 根据受众和上下文选择明确的目标、锚点、会话形态、mention 和内容；
8. 调用 `lark-cli`，使用稳定的 idempotency key；
9. 只在命令成功、返回唯一真实 `message_id` 且读回消息成功后认定发送成功，由 M5 在最终 `effects` 中如实申报；
10. 再根据整个任务是否真正达标返回 `completed`、`waiting`、`needs_human`、`observing` 或 `failed`。

发送一条消息本身不自动等于 Task 完成；但真实目标包含“把结果告诉某人”时，未发送成功就不能返回 `completed`。

### 各结果状态下的消息判断

| M5 outcome | 默认行为 | 可以发送的情况 |
|---|---|---|
| `completed` | 不按状态自动播报 | 有人明确等待答案，或通知本身是完成标准的一部分 |
| `failed` | 不自动暴露内部执行失败 | 已确认的失败结论对目标受众有直接价值，且发送能帮助下一步 |
| `waiting` | 静默等待 | 需要让相关人知道明确的等待条件或已发生的中间变化 |
| `observing` | 默认静默 | 调查形成了值得同步、但当前不需要继续行动的结论 |
| `needs_human` | 向 principal 提出一个最关键问题 | 发送成功后再暂停；已经在本轮发送过同一问题时不重复 |
| `needs_approval` | 不使用普通消息 Skill 发审批提醒 | 只返回完整 proposal，由 runtime 投递审批卡片 |

表中所有“可以发送”的情况仍须服从审批策略。给 principal 本人发飞书私聊属于已有免审批项；发给其他个人或群，只有命中明确豁免时才能直接执行，否则先走 proposal。

### 消息场景

交互形态是 principal 的稳定偏好，保留在 M5 rules；Skill 负责把它们翻译成可执行命令：

1. **给 principal 本人**：Jarvis Bot 按 principal `open_id` 直发。
2. **给其他个人**：查找并严格核验已有的私有助手群；只有人类成员和 Bot 成员都符合约束时才复用，否则创建包含 principal、对方和 Jarvis Bot 的新助手群，再在群里真实 `@` 对方。
3. **回复已有群消息中的个人**：在相关原消息下 `--reply-in-thread`，真实 `@` 对方。
4. **回复来源群所有人**：有明确原消息锚点时在该消息下开话题；没有锚点的主动群公告才直接发到 `chat_id`。

消息发送和回复始终使用 Bot 身份。联系人解析、群搜索和助手群创建按 Skill 使用 principal 的 user 身份；使用前必须核对当前 user identity 就是 principal。禁止的是把消息发送 fallback 成 user 身份。任一步失败都原样交回 M5，不自动切换发送身份、目标或会话形态。

## 改造范围

### 一、Prompt：把普通消息恢复成 M5 动作

修改 `conf/prompts/m5-system-prompt.md`：

- 删除“`user_message` 是来源会话唯一对外文案”的整段机制说明；
- 删除“来源会话结果只写 `user_message`、来源外才调用 Skill”的分流；
- 增加统一原则：普通消息是 M5 可选择的工具动作，与其它调查、写文档、提 MR 等动作同级；
- 明确 M5 自己判断是否通知，不按 outcome 自动发状态播报；
- 明确 M5 在完整确定目标、位置和文案后，必须先按审批策略判断这次发送；需要审批时只出 proposal，不发送业务消息或文字审批提醒；
- 明确发送成功以工具返回真实 `message_id` 为准；
- 明确如果消息是完成标准的一部分，必须先发送成功再返回 `completed`；
- 保留 `summary`、`progress_summary`、`effects` 等结果字段的审计职责；
- 保留 `needs_approval` 的现有机器协议边界。

### 二、Rules：只保留 principal 稳定偏好

修改 `conf/rules/m5.md`：

- 删除 `user_message` 与来源会话的特殊路径；
- 保留“只在必要时打扰 principal”“避免重复发送”“代表 principal 发言时真实 `@`”等稳定偏好；
- 保留交互形态偏好：principal 本人可以 Bot 直发；其他个人进入包含 principal、对方和 Bot 的助手群；已有群聊中的跟进留在准确原消息的话题下；除私聊 principal 本人外，发到其参与的会话或代表其发言时真实 `@` principal，面向具体个人时同时真实 `@` 对方；
- 明确 `needs_human` 在暂停前通过消息 Skill 私下提出一个具体问题；
- 删除查群、建群、发送、回复等命令级操作细节，把它们全部归还 Skill；
- 不改变审批判断尺度，不把普通消息当成审批替代品。

### 三、Skill：成为普通飞书消息唯一操作真源

修改 `.agents/skills/feishu-send-message/SKILL.md`：

- 明确所有 M5 普通业务消息都使用本 Skill，不区分是否来自“来源会话”；
- 开头明确：是否需要审批只服从 M5 注入的审批策略；需要审批时不得调用本 Skill，也不另发文字提醒；
- 优先使用上下文已有的目标 `open_id`；缺失时才用联系人搜索，重名、多匹配或身份不确定时 fail-fast，不猜测；
- 用 `jarvis-tools get-principal` 读取 principal `open_id`，用 `lark-cli auth status --json` 核对当前 user `openId` 等于 principal、Bot identity ready，并确认使用的是 Jarvis App/Profile；不一致时停止，不能用错误身份建群或发送；
- 查找助手群时，先按 principal 与对方的成员 ID 搜索候选，再逐个读取完整成员列表；只有人类成员、Bot 成员和群用途唯一匹配时才复用，多个候选或成员列表被截断时不能猜；
- 没有合格候选才由 principal 的 user 身份创建助手群，把对方加入 users、Jarvis App 加入 bots；创建后重新读取成员核验；建群成功是独立副作用，由 M5 申报 `feishu_chat` effect；
- 真正的消息发送和回复一律使用 Bot 身份；principal 直发失败时删除当前 Skill 的自动建群 fallback，把原始错误交回 M5；
- 要求发送前检查是否已有等价消息，避免重复；群内回复必须使用 M5 已语义选定的准确锚点，不机械选择最新消息；
- 为每条逻辑消息生成不超过 50 字符的稳定幂等键，例如 `jv-t${JARVIS_TASK_ID}-<稳定语义槽>-<内容短哈希>`；同一动作在 retry/resume 中复用同一个 key，有意发送第二条不同消息才换语义槽；明确飞书幂等窗口只有一小时，不能承诺 exactly-once；
- 捕获 `lark-cli` JSON 和退出码；退出码非零、无消息 ID 或返回多个不同消息 ID 都视为未确认；拿到唯一 `om_...` 后用 `im +messages-mget --as bot` 读回，成功后才认定发送完成；
- M5 申报 message effect 时只使用结果 schema 允许的 `kind/title/url/target/preview/extra`；`message_id`、`chat_id`、`anchor_message_id`、`operation` 和 `idempotency_key` 放进 `extra` JSON 字符串，不发明顶层字段；
- 发送失败时原样暴露错误，由 M5 结合任务目标决定下一步；
- 禁止消息发送 fallback 成 user 身份；
- 保留“审批通知不由本 Skill 发送”。

直接调用 `lark-cli` 后，`effects` 仍是 M5 根据工具结果申报的展示数据，不是 runtime 独立核验的可信回执。发送成功后若 Codex 在输出最终结果前崩溃，可能出现“飞书已有消息但 effect 未落盘”的窗口；稳定幂等键、发送前查重和发送后读回只能降低重复概率，不能在纯 Prompt/Skill 方案中承诺绝对 exactly-once。这是把执行判断交给模型后的明确取舍，不用新 API 或状态机掩盖。

### 四、代码：只删除旧耦合，不新增消息决策

如果仅在 Prompt 中要求 `user_message=""`，现有 schema 仍会告诉模型该字段可以触发投递，runtime 仍保留第二条发送路径。这是用下游指令压住错误真源，不是完整修复。

因此需要做一轮机械删除：

- 从 `internal/execute/prompt.go` 的 execution result schema 删除 `user_message`，并把 `ExecutionPromptVersion` 从 `task-exec-v12-user-message` 升级为反映新契约的版本；
- 从 `internal/execute/codex_runner.go` 的结果结构删除 `UserMessage`；
- 从 `internal/execute/agent_executor.go` 删除 `ExecuteResult.UserMessage`、`runUserMessage`、`taskFeedbackResult`、`notifyTaskFeedback`、自动查找/更新结果消息和结果消息 effect 拼装；
- 删除 `routeRun` 中自动投递、投递失败改写业务 run、投递后 reload 的分支；删除 waiting、needs_human、terminal 和 awaiting-approval 返回值中的 `UserMessage` 投影；
- 从 `runResultPayload` 和 `proposalPayload` 删除 `user_message` 投影，但保留 proposal、source run、Codex session、needs_followup 和审批状态所需内容；
- 把 `TaskFeedbackNotifier` 收窄成 reaction-only 接口，只保留 `AddProcessingReaction`；删除 `ReplyResult`、`UpdateResult` 和它们的数据结构；
- 保留 execute、resume、apply 开始时的 best-effort `OnIt` reaction、精简 notifier 的 `cmd/jarvis-server/main.go` 装配、来源 `message_id` 选择和 reaction effect 记录；来源选择只需 `message_id`，删除为结果回复服务的 `ReplyInThread` 与 `chat_mode` 推断；
- `internal/taskfeedback` 只保留 reaction 命令和对应测试，删除结果 reply/update 能力和测试；不要求在本次顺手重命名包。

这部分代码不新增任何消息业务逻辑，只把已经放错所有权的逻辑删掉。不存在兼容 fallback，也不保留“旧路径备用”。历史 execution output 中已有的 `user_message` 保持只读审计数据，不迁移、不回填、不重新发送；旧 Task 的 `previous_runs` 仍可能带这个历史字段，Prompt 必须明确它只是审计证据，不能再触发发送。

### 五、明确不改

- 不修改 CC Connect、route claim、CC session 或飞书回流；
- 不新增 `jarvis-tools send-message`、HTTP 消息 API、消息表或 outbox；
- 不新增消息状态枚举、来源类型分支、目标推断器或自动重试策略；
- 不增加“编辑历史消息”能力；本方案只使用现有的新发和对指定消息回复能力；
- 不修改 `m5-approval-policy.md` 的审批判断尺度；
- 不把审批卡片改成普通文字消息；
- 不为旧 `user_message` 增加兼容读取或 fallback。

## 审批边界

审批和普通消息必须分开：

1. M5 调查后判断下一步副作用需要审批；
2. M5 不执行副作用，也不调用普通消息 Skill 发“要不要批准”；
3. M5 返回 `needs_approval=true`、`outcome=needs_human` 和完整 proposal；
4. runtime 先持久化 proposal、`awaiting_approval` 与 Task version；
5. runtime 再发送带 task/version 的审批卡片；
6. 只有结构化按钮回调构成批准或拒绝。

这里 runtime 不判断“是否审批”，只执行已经由 M5 作出的语义决定，并保证授权动作可验证。这是允许保留的机器硬边界，不属于本方案要删除的输出耦合。

消息 proposal 获批后，apply 阶段继续同一个 M5 执行协议：按获批的目标和完整文案读取普通消息 Skill，发送并验证一次。同一轮可以发送与审批无关、独立有价值且本身无需审批的普通业务消息，但不得复制 proposal、催促批准，或把普通文字回复当成批准。

实施时必须明确保留 `ApprovalNotification`、`ApprovalNotifier`、proposal 持久化、`MarkAwaitingApproval`、持久化后卡片发送、task/version/idempotency callback 和 `internal/cardapproval` 测试；它们与要删除的 task result feedback 不是同一条链。

## 实施顺序

为避免新旧两条发送路径同时存在，按一个小提交内的语义闭包切换：

1. 先修改 M5 Prompt、Rules 和消息 Skill，形成完整的新有效指令；
2. 同一提交中删除 `user_message` schema 和 runtime 自动投递链路；
3. 更新现有 Prompt/Skill/execute/taskfeedback 测试：删除只验证旧自动回复行为的断言，保留并收窄 reaction 测试；
4. 更新 `docs/modules/05-execution.md`，说明普通消息由 M5 显式调用 Skill，`OnIt` 只是 best-effort 开始确认，审批卡片仍为 runtime 状态投影；
5. 把旧的 `docs/design-m5-prompt-cleanup.md` 标记为已被本方案取代，避免它继续把 `user_message` 当作现行设计；
6. 不修改 CC 文档，因为 CC 行为不变。

不建议先让模型固定输出空 `user_message`、以后再删代码。那会在两个提交之间保留冲突真源，并可能在任意一次模型执行中重新触发旧链路。

## Subagent review 结论

本方案分别经过架构所有权、Prompt/Rules/Skill 可执行性和现有代码删除链路三个视角的独立 review。三方结论一致：**方案方向合理，可以进入实施；普通消息应归 M5 显式决策，审批卡片继续归 runtime 机器协议，CC 不应进入改造范围。**

review 提出的关键问题已经吸收进正文：

- 在任何消息工具调用前增加对具体目标和完整文案的审批判断；
- 删除当前工具并不支持、MVP 也不需要的“编辑历史消息”场景；
- 补全助手群搜索、唯一匹配、创建后核验、user/Bot 身份边界和部分成功 effect；
- 明确保留 reaction-only notifier、主进程装配、来源 `message_id` 选择和 `OnIt` effect，避免误删开始确认；
- 补全 `user_message` 在 schema、prompt version、route、payload 和测试中的完整删除闭包；
- 把 effect 精确限制到当前结果 schema，并明确它是模型申报，不是 runtime 验证回执；
- 给出 retry/resume 可复用的稳定幂等键，同时明确一小时窗口和崩溃窗口；
- 将 Run 873 的证据边界收窄为 principal 直发，不把未验收场景写成已验证。

review 后仍保留两个有意接受的 MVP 边界：消息 effect 不是可信 outbox，发送后、最终结果落盘前仍有崩溃窗口；飞书消息幂等仅覆盖一小时。解决这两个问题需要新增持久化执行协议，不符合本次“Prompt + Rules + Skill 决策、不新增消息服务”的目标，因此不在本方案中引入。

## 验证

### 最终 Prompt 检查

导出或从 ExecutionRun 读回完整有效 Prompt，确认：

- `user_message` 不再出现在系统 Prompt、rules、输出 schema 或工具说明中；
- 普通消息只有一个操作真源：`feishu-send-message` Skill；
- 系统 Prompt 只说明“是否发送”、审批判断顺序和完成边界，不复制命令步骤；
- rules 只保留 principal 稳定偏好，包括助手群、准确话题和真实 `@` 的交互偏好；
- Skill 包含目标选择、线程、mention、幂等、回执和 effect 记录；
- 审批卡片仍明确排除在普通消息 Skill 外。

### 自动测试

- execution result schema 的 `required` 和 `properties` 都不再包含 `user_message`；parser 仍可按既有宽松策略读取历史未知字段，不把“旧 output 能否读取”当成本次兼容发送；
- M5 execute/apply Prompt 均暴露 `feishu-send-message` Skill 和 `lark-cli`；
- non-empty/empty `user_message`、自动 reply、自动 update、结果投递完成门相关旧测试被删除；
- `internal/execute/task_feedback_test.go` 保留来源 `message_id` 选择和 reaction unavailable 不影响 run 的测试，删除 reply/thread/update/delivery-gate 用例；
- `internal/taskfeedback/notifier_test.go` 保留 reaction 命令、Bot 身份和缺失 `reaction_id` fail-fast 测试；
- `internal/skill/skill_test.go` 保留“审批卡片归 server”契约，并增加助手群查重、Bot 发送、稳定幂等、Profile 身份检查、读回和 `extra.message_id` 等关键 Skill 文案断言；
- `needs_approval` 仍先持久化状态，再发送绑定 version 的审批卡片；
- `go test ./...` 与 `git diff --check` 通过。

### 真实 M5 验收

至少执行以下场景：

1. **principal 本人直发**：Bot 直发成功，不误建助手群；失败时不自动 fallback。
2. **真人私聊来源、Bot 不在原会话**：M5 读取 Skill，创建或复用助手群并 `@` 对方；Task 不出现 230002 自动回复失败。
3. **助手群复用**：唯一成员匹配的助手群被复用；同成员但包含额外人员、多个候选或成员列表截断时不猜、不发送。
4. **助手群部分成功**：建群成功但消息失败时只申报 `feishu_chat`，不申报消息成功；重跑先找到并复用该群。
5. **身份解析**：联系人重名、多匹配或当前 lark user 不是 principal 时停止，不猜 open_id、不使用错误 Profile。
6. **群消息来源、回复某个人**：M5 选择准确请求锚点，在其下创建话题并真实 `@`；来源已经在 thread 中时继续准确 thread，不机械选择最新消息。
7. **无需通知的 observing/waiting**：M5 不调用消息 Skill，也不存在 runtime 自动状态播报。
8. **需要 principal 补充信息**：M5 私聊提出一个具体问题，发送成功后返回 `needs_human`。
9. **发送回执异常**：命令非零、退出零但无 ID、多 ID或读回失败时不虚报 effect，不把“准备发送”当成完成。
10. **恢复幂等**：schema 输出失败后重跑，同一逻辑动作复用同一个 idempotency key，不在一小时窗口内重复发送。
11. **消息需要审批**：execute 阶段只返回包含准确目标和完整文案的 proposal；apply 阶段按批准内容调用 Skill 并发送一次。
12. **普通审批**：M5 不调用普通消息 Skill 发送审批提醒；proposal 持久化后仍收到版本绑定的审批卡片。

## 完成标准

本方案完成必须同时满足：

1. M5 的最终有效输入中，普通消息决策只有 Prompt/Rules/Skill 一套一致语义；
2. M5 能用现有 `lark-cli` 完成所有目标场景，不需要新消息 API；
3. execution output 不再包含可触发发送的 `user_message`；
4. runtime 不再根据来源快照或 outcome 自动发送、回复或更新普通业务消息；
5. CC Connect 没有代码、配置和文档变化；
6. 审批判断仍归 M5，审批卡片的 task/version/callback 仍归 runtime；
7. 没有兼容层、fallback、新状态、新表或来源专用分支。
