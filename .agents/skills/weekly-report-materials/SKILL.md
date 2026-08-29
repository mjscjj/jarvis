---
name: weekly-report-materials
description: 根据周报模块当前周事实生成会议或对外汇报草稿；只有 publish_report 工作流在明确目标和审批后才可创建飞书文档或发送消息。用于已显式配置的周报材料 ScheduledTask。
module: weekly-report
---

# 周报材料生成与提交

本 Skill 根据 ScheduledTask 的 `context_snapshot.workflow` 执行一个动作。支持 `meeting_summary` 和 `publish_report`；其它值必须失败，不得猜测。

## 1. 动作配置与日期门禁

读取 Task 背景中的 `action_config`：

- `timezone` 必须为 `Asia/Shanghai`；
- `weekday` 是 1-7（周一到周日）；
- `target` 是外部提交目标，可以为空；
- `approval` 决定是否允许外部写入。

按 `Asia/Shanghai` 判断星期。由调度器正常触发且当前星期不等于 `weekday` 时成功结束，输出 `no_op=weekday_gate`，不生成或发布材料。只有当前 Task 的 `occurrence_key` 明确以 `manual:` 开头时，才视为用户点击“立即运行”并绕过星期门禁；不得从指令措辞猜测手动触发。

先读取当前产品范围：

```bash
scripts/weekly-report-tools text-config --key weekly_report_cycle
scripts/okr-module-tools scope
scripts/weekly-report-tools board --quarter '<quarter>' --week '<week>'
```

所有命令必须验证响应 `code=0`。失败时停止，不直接查询 SQLite。

## 2. meeting_summary：只生成会议草稿

先读取页面可编辑的会议材料模板，再读取风险和变化并生成双周会材料投影：

```bash
scripts/weekly-report-tools text-config --key weekly_report_meeting_template
scripts/weekly-report-tools pmo-digest --quarter '<quarter>' --week '<week>'
scripts/weekly-report-tools generate-report-draft \
  --quarter '<quarter>' \
  --week '<week>' \
  --report-type biweekly_review
```

只返回内部草稿摘要，必须包含：当前周、风险数、变化数、缺失数、章节数量以及需要人工复核的内容。不得创建飞书文档、发送消息或修改 KR 进展。

## 3. publish_report：审批后提交

先根据报告类型读取页面可编辑的模板，再用 `middle_platform_weekly` 生成或读取投影：

```bash
scripts/weekly-report-tools text-config --key weekly_report_middle_platform_template
# 双周会发布改读 weekly_report_biweekly_template
scripts/weekly-report-tools generate-report-draft \
  --quarter '<quarter>' \
  --week '<week>' \
  --report-type middle_platform_weekly
```

硬门禁：

- 草稿必须返回 `saved=true`；未保存时标记 `needs_human`，不能发布临时投影；
- `action_config.target` 必须非空；不得按名称猜群、接收人或目录；
- `action_config.approval` 必须是 `require_approval`；
- 创建文档、发送消息都是外部写入，必须先生成完整 proposal 并进入 Jarvis 审批，未批准不得执行。

批准后，把已保存草稿按“标题、进展、风险、变化、来源”组织为 Markdown，并创建飞书文档：

```bash
scripts/weekly-report-tools create-feishu-document --payload - <<'JSON'
{"title":"<草稿标题>","content":"<Markdown 正文>"}
JSON
```

若 `target` 明确为群或个人，再读取 `feishu-send-message` Skill，把文档链接发送到该目标；目标只是文档目录或描述不清时，只创建文档并标记 `partial`，不得扩大收件范围。

## 完成检查

- 输出 workflow、quarter、week、日期门禁结果和实际动作；
- 草稿生成不产生外部副作用；
- 外部提交有 approval、文档 URL 和 lark-cli/API 成功回执；
- 发送逐项列出成功、失败、跳过，部分失败标记 `partial`；
- 不修改 OKR 定义、周报进展、Meego 或世界关系。
