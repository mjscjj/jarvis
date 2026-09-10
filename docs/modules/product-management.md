# 产品管理插件

产品管理是内置 capability 插件（`product-management`）。页面汇总 Skills、定时任务和每条计划最近一次 Task 的状态，任务、运行结果与计划仍以现有 Task / ExecutionRun / ScheduledTask 为真源，无独立业务表。

在「插件管理」开启后出现「产品管理」二级入口。启用本身不创建计划、不立即执行外部动作。Skills 页查看正文和启停，正文由 Agent 维护项目文件。提供 product-doc-read、product-doc-review、product-skill-maintain 三个 execute Skill；Manifest 的 Skills 列表决定归属，conf/skills.yaml 决定阶段与启用。

定时任务页复用 ScheduledTasks 组件，选择 Skill，填写文档 URL 或 Resource、范围、关注点与周期。上下文保存 `plugin: product-management` 和 `skill`，其余业务背景宽松保留。Agent 通过现有 create-scheduled-task 工具可设置相同上下文；程序不按产品 Skill 名称选择执行链路。调度到点创建普通 Task，M5 读取任务背景和当前 Skills 目录，再按指定 Skill 调查执行。

这些计划同时显示在「任务 → 自动化」中，任何一处修改都是同一条计划。`GET /api/scheduled-tasks?plugin=product-management` 在服务端按归属过滤后应用 limit。最近执行展示各计划 last_task_id 的真实 Task 状态，调度创建成功不代表 Review 已完成。详情跳回通用任务中心查看过程、结果和继续处理。

关闭插件时，后续带该 plugin 标记的周期和手动触发均不创建新 Task；保留计划启用意愿与历史关联，重新开启后可继续。现有 Task 与等待续跑不被取消，运行中 Session 已读入的 Skill 不会被强制移除。该开关是产品生命周期，不是本机 Agent 权限。

对指定 skill 的计划，派发前检查该 Skill 的 execute 阶段、启用与插件可用状态。停用时跳过本轮；名称不存在时报错，不悄悄使用其它 Skill。页面不复制 Skill 正文，Agent 按 Task 背景中的 skill 名称读取当前文件。

产品文档 Skill 默认阅读并输出建议，不修改原文或外发消息。Task 明确授权其他动作时按统一执行规则处理。维护 Skill 根据明确的维护目标与真实 Task 案例优化文件，普通 Review 不顺带自改规则。
