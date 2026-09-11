# Principal 通知卡片与 notice-principal

状态：已实现；运行服务需重建后使用新接口。

## 使用与展示

```bash
jarvis-tools notice-principal --payload-file notice.json
# 或通过 stdin：
jarvis-tools notice-principal --payload - < notice.json
```

`notice.json` 示例：

```json
{
  "type": "Notice",
  "content": "MR #139 评审完成，需要修改。已在 Codebase 提交修改意见，主要涉及请求路由和图表交付。编译和测试通过，待作者修复后复审。",
  "links": [
    {"label": "查看 CR", "url": "https://code.byted.org/tiktok/llm_agent_core/merge_requests/139"}
  ],
  "details": "完整问题、验证过程和其他需要展开阅读的说明。",
  "extra": {"下一步": "待作者修复后复审"},
  "task_id": 1592,
  "idempotency_key": "jv-t1592-review-result-v1"
}
```

卡片为 Card 2.0、compact 宽度；去掉彩色大标题栏和独立标题字段，类型与正文都使用 notation 小字号（12px）。正文开头自然概括事情，后接必要说明，再显示链接，最后按需显示默认折叠的「详情」「补充」。只有阅读、展开和链接跳转，不含审批或回答回调。

| 字段 | 契约 |
| --- | --- |
| `type` | 开放展示文字，省略时为 `Notice`；参考词 `Notice`、`Update`、`Reminder`、`Alert`、`Brief`，也可自由命名。不是业务分类枚举，不控制流程或颜色。 |
| `content` | 必填 Markdown 消息，建议 1–3 句；开头自然概括事情，不单列标题、加粗标题或“标题：”标签。 |
| `links` | 可选链接数组，每项为 `label`、绝对 HTTP(S) `url`；用于 CR、文档、原消息等业务材料；任务详情由 `task_id` 自动生成，无需手动拼接。 |
| `details` | 可选长 Markdown 说明，默认折叠。 |
| `extra` | 可选自由文本或开放 JSON；对象按字段名和值展示，数组按列表展示，不规定业务字段清单。 |
| `task_id` | 可选的任务关联，用于核验引用、留痕和自动生成任务“查看详情”链接；不改变任务状态或版本。CLI 默认从 `JARVIS_TASK_ID` 补充。链接优先使用统一的 `server.public_base_url`，未配置时使用服务实际监听地址；回环链接需在打开链接的设备上运行服务或端口转发；`details` 折叠正文不依赖本地网页。 |
| `idempotency_key` | 必填、最多 50 字节；同一次通知重试复用。 |

正文的建议长度只是给模型的写作指导。工具不静默截断内容；超过卡片传输大小限制时明确报错。完整输入含扩展字段写入日志，显示结构只消费以上字段；要展示自拟栏目就放入 `extra`。

## 所有权与链路

`jarvis-tools` → `POST /api/notices/principal` → `internal/notice` → 已有 `larkcli` 客户端 → Principal 的 Bot 私聊。

- 工具目录和 CLI help 维护参数、类型参考词与写作说明。
- M5 rules 维护何时通知、是否需要单独回执、去重与原话题送达口径。
- 发送服务只负责卡片排版、固定收件人、发送、读回和留痕。各阶段均可使用该工具，不按阶段或类型分流。
- `cardask` 继续负责 `needs_human + question`；普通通知不暂停或恢复任务。
- 原会话业务回复、其他受众消息及图片文件仍使用 `feishu-send-message` Skill。Principal 的主动通知与动作回执统一交给新工具。

成功返回 `verified`、`message_id`、`chat_id`、消息链接及 `effect`。服务读回验证消息存在、会话正确、未撤回且为卡片；不依赖飞书把卡片完整转换回原始 JSON。M5 把返回的 `feishu_message` effect 原样纳入本轮 `ExecutionRun.effects`。

完整请求和发送结果逐次追加到 `var/log/principal-notices.jsonl`，发送前先记录请求，发送后记录结果和原始发送返回。现有 `task_event` 对 `(task_id, task_version)` 唯一，通知不消耗任务版本，因此不复用该表写通知日志。没有新通知表、队列、状态机或自动完成通知钩子。

发送或读回失败时，HTTP 错误仍携带已取得的消息 ID 和发送返回；CLI 错误保留整个响应。失败可能已经产生消息，模型应先核验已知消息、日志和历史 effects，再决定重试。飞书的幂等窗口有限，不能保证严格 exactly-once。

## 与原 CR 消息的关系

发生真实外部动作后，确保 Principal 收到结果；在 Principal 参与的原话题回复并真实 @ Principal，已算送达，无实质新增信息不再重复私聊。仅在 Codebase 留 review 不自动等同于飞书已送达，需要通知时发送带 CR 链接的卡片。没有原消息锚点时可以直接通知，不为寻找可能存在的线程扩大调查。

同一事项本轮的多个动作尽量合成一次回执。通知本身不再触发一条「已通知」回执。类型是 `Notice` 等展示文字，「回执」只描述发送目的，不强制成为卡片分类。需要 Principal 回答或批准并暂停等待时，仍使用原问题卡。

## 验证

测试覆盖自定义类型、可选内容和折叠详情、链接校验、完整文字与大整数保留、Bot/Principal 目标、部分成功凭据、读回失败、发送前留痕失败，以及有任务关联时状态和版本不变。CLI 测试覆盖文件载荷、换行与 Shell 特殊字符、任务关联及错误凭据保留；最终 M5 输入测试检查通知工具、原消息 Skill 和审批卡规则没有互相冲突。

2026-09-10 按 Principal 要求，通过临时验收程序调用同一套 notice.Service，实际发送 5 张测试卡片（基础通知、链接、详情、补充、自定义类型），全部读回确认成功。凭据在 `var/notice-preview/*-receipt.json`，完整留痕在 `var/log/principal-notices.jsonl`。主服务接口仍需重建加载，客户端视觉效果由 Principal 检查。

根据实际卡片反馈，已改为普通小字正文和自然开场句；原 5 张测试卡片已原地更新并读回确认成功，没有额外发送重复消息；更新凭据位于 `var/notice-preview-update/`。
