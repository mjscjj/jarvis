# OKR 世界模型模块

> Status: MVP implementation
> Scope: MVP

人工复核入口与未提交改动清单见 [OKR MVP 验收与交接](okr-mvp-handoff.md)。

## 1. 语义边界

OKR 是世界模型中最大的结果对象，固定层级为 `OKR → Project → KeyMatter`：

- `OKR` 表达一个周期内希望达成的结果，关联一个负责人；
- `Project` 是交付容器，通过可空 `okr_id` 归属于一个 OKR；
- `KeyMatter` 是项目中持续跟进的重要事项，继续通过 `project_id` 归属；
- `Task` 仍然只表示一次执行单元，不增加 `okr_id`、`project_id` 之外的新耦合，也不因 OKR 更新而自动物化。

OKR、Project 和 KeyMatter 的当前认知写在各自的 summary page；发生过的变化追加为 Fact。状态正文由人和 Agent 自由维护，程序只把 `closed_at` 当作闭环硬边界。

## 2. MVP 数据骨架

`okr` 只保存程序需要查询的字段：标题、周期、自由状态、负责人、闭环时间、当前 summary 与最后进展时间。负责人引用既有 Person；相关群、资料和其他人使用 summary 中的实体引用，不另建关系表。

`GET /api/okrs/:id` 返回完整 `OKR → Project → KeyMatter` 树，供后续页面直接渲染。控制字段由 `/api/okrs` CRUD 维护；进度正文复用 `/api/pages/okr/:id` 的 CAS 写入和 `/api/facts?subject_type=okr` 的历史流。

## 3. 进度来源

不为消息、Meego 或定时器新建 Go 专用流水线：

1. 消息仍由 M2 进入统一 `message → factengine` 世界维护链路；Agent 识别到确定的 OKR 变化后更新 page、追加 Fact。
2. Meego 由外围 Skill 调用 `bytedcli` 读取原始状态，完整证据通过 `POST /api/clues` 投递；M3/M5/factengine 按现有职责判断和沉淀。
3. 周期轮询和周报催办由既有 ScheduledTask 触发，每次只创建独立 Task；Task 使用工具读取 OKR、Person、Meego，完成一次动作后把结果写回世界模型。

这样新增来源只增加 Skill、定时任务和 `source`，不会把 OKR 变成第二条执行流水线。

## 4. 已完成与后续切片

- 已给 `jarvis-tools` 增加 OKR CRUD 与完整层级读取；page/fact 通用命令支持 `type=okr`；
- 已增加简洁的 OKR 页面，按负责人展示进度并可展开 Project 与 KeyMatter；
- 已在 factengine 与 proactive 的正确提示词所有者补充 OKR 识别、向上汇总、失速看护以及 Task 独立边界；
- 已用临时 SQLite、真实本地 HTTP listener 和 `jarvis-tools` 全链路验证 `OKR → Project → KeyMatter`、未关联 Meego 证据发现、最小实体写回、Fact/Page/周视图回读、关联后退出队列与零 Task 副作用；外部 runner 在测试中 fail-closed，未发送真实飞书消息。接入真实方向数据留给部署后的受控配置。
- 已增加 execute 阶段的 `okr-progress-sync` Skill：按未闭环 OKR 收窄查询范围，只读 Meego 与已采集消息，把原始版本化证据投递到 clue，再按最小实体写入 Fact/Page；Page 更新使用 CAS，外部系统保持只读。
- 已增加未关联证据只读入口：按 clue 来源或普通消息、采集时间和稳定文字锚点检索尚未成为 OKR/Project/KeyMatter Fact 来源的 Message；Agent 明确选择最小实体后再调用统一写回，系统不根据相似度自动猜关联。
- 周期巡检复用 ScheduledTask，每次触发一个独立 Task；Skill 提供 interval 配置模板和重复任务检查，不在 OKR/Project/KeyMatter 上增加 scheduler 或 Task 耦合字段。真实方向、查询锚点和巡检频率仍由部署后的受控配置提供。
