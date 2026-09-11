# 产品流程插件

产品流程是内置 capability 插件（`product-management`）。页面汇总 Skills、定时任务和插件计划产生的历史 Task，任务、运行结果与计划仍以现有 Task / ExecutionRun / ScheduledTask 为真源，无独立业务表。

在「插件管理」开启后出现「产品流程」二级入口。启用本身不创建计划、不立即执行外部动作。Skills 页支持查看、直接编辑 Markdown 和启停；人和 Agent 修改的是同一份 `.agents/skills/<name>/SKILL.md`，不复制到数据库。保存使用内容版本校验，期间若 Agent 或其他人已经改过文件则拒绝覆盖并保留页面草稿；新正文从 Agent 下次读取开始生效，已运行的 Session 不热更新。提供两个 execute Skill：`product-prd-review` 负责单篇或会议批次的评审判断、Claire 高频关注点和报告要求；`product-tools` 负责群消息与文档定位、正文和评论读取、原始证据保存，以及报告交付和回执核验。Manifest 的 Skills 列表决定归属，conf/skills.yaml 决定阶段与启用。会议批次优先使用任务指定范围，未指定时定位最近一次相关产品会议；单篇评审直接读取指定文档，不要求提供群或会议。

定时任务页复用 ScheduledTasks 组件，选择 Skill，填写文档 URL 或 Resource、范围、关注点与周期。上下文保存 `plugin: product-management` 和 `skill`，其余业务背景宽松保留。Agent 通过现有 create-scheduled-task 工具可设置相同上下文；程序不按产品 Skill 名称选择执行链路。调度到点创建普通 Task，M5 读取任务背景和当前 Skills 目录，再按指定 Skill 调查执行。

这些计划同时显示在「任务 → 自动化」中，任何一处修改都是同一条计划。`GET /api/scheduled-tasks?plugin=product-management` 在服务端按归属过滤后应用 limit。最近执行按插件归属展示历史 Task 的真实状态，调度创建成功不代表 Review 已完成。详情跳回通用任务中心查看过程、报告链接、通知回执和继续处理。

关闭插件时，后续带该 plugin 标记的周期和手动触发均不创建新 Task；保留计划启用意愿与历史关联，重新开启后可继续。现有 Task 与等待续跑不被取消，运行中 Session 已读入的 Skill 不会被强制移除。该开关是产品生命周期，不是本机 Agent 权限。

对指定 skill 的计划，派发前检查该 Skill 的 execute 阶段、启用与插件可用状态。停用时跳过本轮；名称不存在时报错，不悄悄使用其它 Skill。页面不复制 Skill 正文，Agent 按 Task 背景中的 skill 名称读取当前文件。

单篇阅读或评审默认在当前对话或 Task 输出，按任务要求保存长报告；会议批次评审逐份生成飞书报告并通知 Principal。源 PRD 只读，工具方法本身不扩大外部动作授权。原始正文、评论和来源保存在 `docs/summery/` 的任务目录中，缺失材料标明未覆盖，不以评论引用代替全文。

不再单列产品 Skill 维护入口。按仓库 AGENTS.md 与通用 Skill 维护方法直接编辑上述文件，保留用户已确认的约束，并同步校验文件、配置和插件绑定；普通评审不自动修改 Skill。定时评审入口仍为 `product-prd-review`，不复制正文到计划或数据库。
