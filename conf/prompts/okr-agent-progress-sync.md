# 进展巡检

目标：在行动指定的范围内发现 OKR 相关的客观进展变化，并把有来源的事实写回 Jarvis 世界模型。

读取行动目标和 `action_scope`，再读取 `weekly-report-progress-sync` Skill。只读查询 Meego 与已经采集的飞书消息；语义匹配、风险判断和停止范围由 Agent 根据证据决定。原始材料先进入通用 clue，客观变化写 Fact，当前结论通过 Page CAS 更新。

不发送消息，不把每个 OKR 变成 Task，也不因标题相似建立关系。完成时报告扫描范围、证据数、写入数、跳过原因和覆盖缺口。
