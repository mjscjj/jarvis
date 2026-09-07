# OKR Review 催填

目标：按指定季度和 Review 周次检查填写质量，把每位负责人的问题合成一条消息，由 Jarvis Bot 发到已核验的私有助手群。

## 判断

- 从 Task 读取 `quarter`、`week`，回读目标周、上一有效周及两周完整 Biz OKR board；只按稳定 KR/Point ID 比较，不使用任务创建时的旧统计。
- 每条 KR 独立判断四类问题：`KR 进展缺失`＝名下 Point 没有有效进展；`KR 核心数据缺失`＝没有非空核心数据；`KR 评分缺失`＝一级 KR 的 `score` 为空；`KR 跟上周一样`＝两周都有有效内容，且核心数据或 Point 进展至少一部分在忽略版本、记录 ID、来源等元数据后相同。两周都空不能算相同。
- 收件人取 KR `owners` 与其 Point `owners` 的并集，必须有可回读的真实 `open_id`；按负责人聚合，每类按 KR ID 去重。同一 KR 可进入多类。

## 发送

- 使用 `weekly_report_reminder_template`。顺序固定为“进展、核心数据、评分、跟上周一样”；每个非零类别都列出数量和具体 KR 标题，零项整段省略。每人一条，只真实 `@` 该负责人；不要 `@` principal，不要添加“同步：”字段。
- 先查世界关系 `feishu_chat --okr_review_reminder_channel_for--> feishu_user`。关系只作候选，发送前仍须核验群成员恰好是 principal、负责人和当前 Jarvis Bot；没有唯一有效映射时，再按 `feishu-send-message` Skill 搜索或创建群。发送成功后用真实 `chat_id` 和 `message_id` upsert 该关系，重跑不得重复建群。
- 使用 Jarvis Bot、稳定幂等键并逐条回读消息。发送失败保留原始错误，不切换身份、收件人或会话。没有问题时不发送。

完成时报告检查的两周、四类去重 KR 数、成功/失败/跳过人员、群复用/新建数量、消息回读结果和覆盖缺口。
