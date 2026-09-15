# M5 首轮上下文简单压缩方案

> Status: proposed
> Date: 2026-09-14
> Scope: 先做通用、机械的上下文去重；不做消息相关性判断、摘要或来源专用分支。

## 1. 目标与口径

本方案降低 M3 冻结现场及 M5 新 Session 首轮请求的输入量，同时保持原始消息可追溯。

- `input_tokens` 指模型首轮请求的实际输入，不是整个 Agent run 多轮累计值。
- 字符数是 Jarvis 本地序列化结果；Token 数使用 `o200k_base` 对已落盘文本估算。模型服务的最终记账还包含 TraeCode 运行时、工具 Schema 和协议封装，因此只有 rollout 里的首轮 `input_tokens` 是精确值。
- 不修改 `message` 数据库表。数据库中的 `chat_id`、`chat_mode` 和 `source_url` 是采集、查询和审计真源，本身不占模型上下文；只压缩 Todo/Task 冻结快照和模型读取投影。

## 2. 已确认的简单改动

### 2.1 前序消息上限从 50 改为 25

将 `extract.context_messages` 从 50 改为 25。

普通会话继续只读取 24 小时内的前序消息；Thread 继续不设时间下限。最终冻结消息数为：

```text
本轮同一 conversation unit 的新消息
+ 最多 25 条前序消息
+ 缺失时补入的 reply/root 锚点
```

单条新消息触发的普通场景通常由最多 51 条降到最多 26 条；锚点仍可使总数超过 26。

### 2.2 会话字段只在 group 保存一次

一个 M3 conversation unit 只属于一个会话。新冻结快照在 `capture.group` 保留：

```json
{
  "chat_id": "oc_xxx",
  "chat_mode": "p2p"
}
```

从每条 `capture.messages[]` 删除重复的：

```text
chat_id
chat_mode
```

消息仍保留 `message_id`、发送者、正文、时间、mentions 及 reply/root/thread 关系。

### 2.3 source_url 不再逐条冻结

新冻结快照不再把 `source_url` 复制到每条 `capture.messages[]`。任务页、问题卡和其它展示消费者统一使用会话级 `chat_id` 与 `source.trigger_message_id` 机械生成跳转链接：

```text
https://applink.feishu.cn/client/chat/open
  ?openChatId=<URL 编码后的 chat_id>
  &openMessageId=<URL 编码后的 message_id>
```

Go 实现使用 `url.Values`：

```go
"https://applink.feishu.cn/client/chat/open?" + url.Values{
    "openChatId":    {chatID},
    "openMessageId": {messageID},
}.Encode()
```

只在 `trigger_message_id` 非空、属于 `source_message_ids`，且 capture 中存在该消息时生成链接；不按证据顺序猜测锚点。数据库继续保留飞书返回的原始 `message.source_url`。旧 Todo/Task 继续读取已经冻结的 `source_url`，不批量迁移。

### 2.4 coverage 只保留范围，不重复完整 ID 列表

`messages[].message_id` 已表达实际冻结消息，删除重复的 `coverage.shown_message_ids`；`evidence_refs` 不再嵌套复制整份 `coverage`。

`coverage` 保留：

```json
{
  "scope": "conversation",
  "loaded_count": 26,
  "history_limit_messages": 25,
  "history_window_minutes": 1440,
  "omitted_message_ids": [],
  "missing_anchors": []
}
```

`evidence_refs` 只保留运行审计入口，例如 `run_directory` 和工具返回文件说明。

## 3. 暂不做的细粒度优化

- 不用模型筛选“相关消息”，也不新增摘要步骤。
- 不根据场景动态选择 5、10 或 25 条历史。
- 不把 sender 改成 participant 数字索引。
- 不把时间戳改成相对时间。
- 不删除 sender、mentions、reply/root/thread 等原始证据字段。
- 不改变 M5 evidence 的 30,000 字符总预算。
- 不为飞书、会议、CC Connect 等来源增加专用数据结构或分支。

## 4. 真实样本收益

样本 Task 1905 的 M5 新 Session 首轮实际为 46,156 input tokens。原始 `TASK_CONTEXT` 为 33,847 字符，按 `o200k_base` 估算约 13,847 tokens。

对该快照机械模拟上述四项后：

| TASK_CONTEXT 内容 | 优化后字符 | 估算 Token |
|---|---:|---:|
| 26 条消息及必要字段 | 5,548 | 2,307 |
| World overview | 5,927 | 2,342 |
| 其它现场字段 | 2,451 | 877 |
| coverage | 146 | 34 |
| evidence_refs | 226 | 59 |
| Task/history 等顶层字段 | 260 | 84 |
| 合计 | 约 14,628 | 约 5,716 |

`TASK_CONTEXT` 预计减少约 8,131 tokens，因此同等运行时条件下，首轮预计由 46,156 降到约 38,000 tokens。该值需要实施后通过新 rollout 的首轮 usage 回归确认。

## 5. 压缩后仍然存在的 Token 大头

真实首轮的静态消息、模型执行配置和最小工具差分实验已经可以把总账闭合。以下分块用同一个 `o200k_base` tokenizer 估算；原始首轮 `46,156 input_tokens` 及工具差分使用服务端实测。各块估算相加与服务端计数只差约 105 tokens，可归入消息边界和协议封装。

| 层级 | 原样首轮 | 简单压缩后 | 性质 |
|---|---:|---:|---|
| Jarvis M5 固定 Prompt（不含 TASK_CONTEXT） | 约 11,791 | 约 11,791 | Jarvis 可直接优化 |
| TraeCode developer 注入 | 约 8,937 | 约 8,937 | CLI/harness 注入 |
| TraeCode session base instructions | 约 4,370 | 约 4,370 | CLI/harness 注入 |
| model profile base instructions | 约 4,081 | 约 4,081 | 模型执行配置，文本被上一项完整包含 |
| TASK_CONTEXT | 约 13,847 | 约 5,716 | 本方案压缩对象 |
| 默认工具 Schema | 约 1,401 | 约 1,401 | 最小差分实测 |
| 最终输出 Schema | 约 885 | 约 885 | Jarvis 结构化结果合同 |
| AGENTS/工作区附加说明 | 约 739 | 约 739 | 项目级注入 |
| 消息边界和协议封装 | 约 105 | 约 105 | 按总账反推 |
| 合计 | **46,156** | **约 38,025** | 压缩后待新 Task 实测 |

因此，原先笼统估成 19K–20K 的“TraeCode/harness 固定底座”可以进一步拆成：

```text
两层 base instructions  约 8.45K
developer 注入          约 8.94K
默认工具 Schema         约 1.40K
消息/协议封装           约 0.11K
合计                    约 18.90K
```

若只把“最终 session 系统词”算一次，固定底座会少算约 4.08K。真实首轮总账表明 `model profile base instructions` 也进入了服务端 Token 计数：不计它时缺口为约 4,186 tokens，计入后只剩约 105 tokens。

### 5.1 Jarvis M5 固定 Prompt：约 11.8K tokens

| 固定模块 | 字符 | 估算 Token |
|---|---:|---:|
| 工作规则 | 4,739 | 3,137 |
| 输出协议 | 3,412 | 2,099 |
| Skill 目录及当前内联 Skill | 4,271 | 1,945 |
| M5 系统提示词 | 2,323 | 1,382 |
| 执行规则 | 1,939 | 1,276 |
| 审批策略 | 1,864 | 1,196 |
| phase + shared memory | 710 | 354 |
| Jarvis 工具目录 | 822 | 404 |

这一层是压缩后最大的 Jarvis 自有单块。明显存在“送达、请示、状态停止条件、输出字段”在系统提示、工作规则、审批策略和输出协议间重复表达的可能，但下一步应先做语义去重审计，而不是直接截短。

### 5.2 TraeCode developer 注入：约 8.9K tokens

| Developer 子块 | 字符 | 估算 Token |
|---|---:|---:|
| 全量 Skill 清单（55 个） | 13,445 | 5,906 |
| Skill 使用规则 | 2,576 | 558 |
| Memory 使用规则及当前 memory summary | 8,933 | 2,159 |
| Plugins 使用规则 | 988 | 199 |
| Permissions | 362 | 68 |
| 其它边界文本 | - | 约 47 |
| 合计 | 26,563 | 8,937 |

最大项不是工具，而是 55 个 Skill 的全量清单。对 Jarvis 的 M5 后台执行而言，TraeCode Memory 还与 Jarvis 自己的 shared memory、World overview 和 Task 历史形成了第二套背景注入；当前 memory 仓库即使是空状态，也固定占约 2.2K。

### 5.3 两层基础指令：约 8.45K tokens

rollout 同时记录：

- `model_execution_profile.model_info.base_instructions`：19,706 字符，约 4,081 tokens；
- `session_meta.base_instructions`：21,306 字符，约 4,370 tokens。

前者是后者的完整文本前缀，后者只额外追加约 290 tokens 的 personality。两者的文本重复约 4.08K，而且首轮总账显示两层都被服务端计数。这属于 TraeCode/harness 与模型执行配置之间的固定重复，不是 Jarvis `prompt.txt` 造成的。

### 5.4 默认工具 Schema：约 1.4K tokens，不是大头

使用相同模型、相同一句话 prompt 做最小首轮差分：

| 工具配置 | 首轮 input_tokens | 相对默认减少 |
|---|---:|---:|
| 默认工具 | 17,441 | - |
| 禁用四个默认工具 | 16,040 | 1,401 |

四个工具逐个贡献约为：`apply_patch` 923、`exec_command` 261、`write_stdin` 122、`update_plan` 95 tokens。`--allowed-tool exec_command` 实测仍为 17,441，说明该参数是额外允许工具，不是 allowlist。

因此无需为了 Token 裁掉 M5 的工具能力。Jarvis `BEGIN_AVAILABLE_TOOLS` 只有约 403 tokens，介绍的是 `jarvis-tools`、`lark-cli`、`bytedcli` 和 `git` 等外部入口；runtime Schema 定义的是 TraeCode 内建工具的参数。两者只有“如何使用工具”的概念交叉，没有重复一份完整参数 Schema。

四个容易混淆的层次实际关系如下：

| 层次 | 主要内容 | Token | 与 runtime Schema 的关系 |
|---|---|---:|---|
| runtime 工具 Schema | `apply_patch`、`exec_command`、`write_stdin`、`update_plan` 的参数定义 | 约 1,401 | 唯一完整参数 Schema |
| TraeCode base/developer 指令 | 何时使用工具、如何编辑、Skill 加载规则 | 已计入 base/developer | 有少量行为语义交叉，不复制参数 Schema |
| Jarvis 工具目录 | `jarvis-tools`、`lark-cli`、`bytedcli`、`git` 的发现入口 | 约 403 | 工具集合不同，不重复 runtime 参数 |
| Jarvis Skill 块 | 领域流程目录和当前 Skill 正文 | 约 1,944 | 描述业务流程，不是工具 Schema |

因此这一项应当做“规则所有权审计”，不应按四份 Schema 相加。

### 5.5 Skill 重复：目录重复，正文没有整份重复

Jarvis `BEGIN_AVAILABLE_SKILLS` 共约 1,944 tokens：

- 11 个普通 Skill 的名称、描述和读取方式约 879 tokens；
- 当前内联的 `my-delegations-execute` 正文约 718 tokens；
- 其它目录和边界文本约 348 tokens。

这 11 个普通 Skill 全部已经出现在 TraeCode developer 的 55 个 Skill 清单中，因此 Jarvis 的这部分目录描述确实重复。严格说，已经确认的逐项重复只有这约 879 tokens；上层其余约 5.6K 是 M5 通常不需要常驻的其它 Skill 目录和通用加载规则，属于可关闭空间，不是逐字重复。内联 Skill 是当前 Task 需要的完整执行规则，上层只列名称和摘要，并没有重复整份正文。

最简单的语义归属是：M5 保留 Jarvis 自己的 Skill 目录和按需内联正文，M5 专用 TraeCode profile 不再注入全局 Skill 清单；需要飞书 Skill 时仍按现有提示通过 `lark-cli skills list/read` 发现。这样不需要做按 Task 精细筛选。

### 5.6 TASK_CONTEXT 内部：World overview 已与消息并列

做完四项简单压缩后，样本中的两个最大动态块将变成：

```text
messages        约 2.3K tokens
world_overview 约 2.3K tokens
```

因此继续只压消息的收益已经有限。World overview 当前每轮接近 6,000 字符，是下一项可观察对象；但它是 M3/M5 共享的当前世界导航，不应为了省 Token 直接删除。若后续优化，优先考虑减少默认目录条数或字段，再由工具按需查询，不做任务相关性模型筛选。

## 6. 后续优化优先级

本轮只记录，不随本方案一起实施：

1. 为 M5 使用独立 TraeCode profile，关闭上层 Skill 注入；Jarvis Skill 目录继续作为 M5 的权威来源。TraeCode 当前支持用 `[[skills.config]] enabled = false` 逐项禁用，bundled Skill 可用 `[skills.bundled] enabled = false` 整体关闭。当前样本的上层 Skill 块共约 6.5K；是否能整体清空还取决于用户、项目和插件 Skill 的配置覆盖范围，需用新 Session 验收。
2. 在同一 profile 中关闭 TraeCode Memory 注入。M5 已有 Jarvis shared memory、World overview、Task 历史及工具查询入口，当前第二套 Memory 固定占约 2.2K。正确读取开关是 `[memories] use_memories = false`；若也不允许 M5 生成 TraeCode memory，再加 `generate_memories = false`。
3. 向 TraeCode/harness 侧确认并消除两层 base instructions 的重复计数，理论空间约 4.1K；这不是 Jarvis prompt 内可直接修复的项目。
4. 对 M5 固定 Prompt 做语义重复审计，重点检查系统提示、工作规则、审批策略和输出协议；一个语义只保留在正确所有者。当前总量约 11.8K，是最大的 Jarvis 自有固定块。
5. 观察 World overview 6K 字符预算是否长期打满；如是，机械降低默认条数或删除非必要展示字段，详情仍通过现有工具读取。
6. 暂不优化默认工具 Schema。完整四工具仅约 1.4K，保留工具能力比这一点 Token 更重要。

只计算已经量化的简单项，首轮可能依次落到：

```text
当前真实样本                         46.2K
消息快照四项机械压缩后               38.0K
再关闭 M5 上层 Skill + Memory 注入   约 29.4K
再消除两层 base instructions 重复    约 25.3K
```

最后两行是理论值，必须分别通过 M5 profile 新 Session 和 TraeCode/harness 修复后的真实 rollout 验收；不把 M5 固定 Prompt 尚未量化的语义去重收益提前算进去。

## 7. 实施与验收

实施时同步修改配置真源、快照生产者、source link 投影、contextpack 消费者、文档与测试。新格式必须版本化或由明确适配器读取旧格式，不静默误读。

至少覆盖：

- 普通群、P2P 和 Thread 的 25 条前序上限；
- 本轮多条新消息与 reply/root 锚点导致总数超过 26 的场景；
- 新格式消息不重复携带会话字段和 URL；
- 新格式链接按 chat ID + trigger message ID 正确生成；
- 无 trigger、trigger 未被引用、消息缺失时不生成链接；
- 历史快照的原始 `source_url` 仍可读取；
- `get-task/get-todo --message-id` 和 conversation/full/range 读取不丢原文；
- M5 evidence 仍优先展示直接证据和锚点；
- 新建真实 Task 的首轮 `input_tokens` 与改动前样本对比。

涉及构建或重启主服务时只使用 `./scripts/rebuild-server.sh`。
