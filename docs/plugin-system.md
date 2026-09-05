# 插件系统

Jarvis 插件只负责外部能力的接入，不拥有事件判断或任务决策：

```text
Plugin lifecycle -> Scheduled Task -> Collector Skill -> /api/clues -> M2 -> M3 -> M5
```

## 内置插件

Codebase、Meego、Oncall 随仓库安装，但对新用户默认关闭。用户在“插件”
页面主动开启；需要额外身份时先完成对应授权，授权成功后创建周期采集任务并
立即执行一次。

“插件”入口始终显示；某个插件开启后才在左侧“插件”下显示同名二级入口，
关闭后立即移除。插件自己的配置和运行详情只放在该二级页面，不塞进管理列表。

关闭插件会停止后续采集并从 Agent 可用 Skill 中隐藏对应采集能力。已经进入
M2 的原始线索、Todo、Task 和执行记录继续保留，作为历史证据和审计记录。

## 所有权

- `internal/plugin`：Manifest、启停状态、授权探测、调度绑定和 Skill 门禁。
- `.agents/skills/*-clue-collector`：各来源的查询步骤和原始线索投递方法。
- `internal/scheduledtask`：通用时间触发和 Task 固化，不理解插件类型。
- `internal/capture`：统一接收 `/api/clues`，不理解 Codebase、Meego 或 Oncall。
- M3/M5：结合上下文判断线索是否值得行动，以及如何执行。

插件不建立来源专用业务表，不把外部对象转换成固定 Go DTO，也不绕过通用
M2 → M3 → M5 流水线。

## 持久化

`plugin_installation` 只保存程序必须消费的状态：

- `plugin_id`
- `enabled`
- `revision`
- `config`（插件自有的宽松 JSON）
- `scheduled_task_id`
- 创建和更新时间

Manifest 和 Skill 文件是仓库真源；外部原始事实保存在现有 clue 消息中。

## 授权

授权提供者使用代码内固定命令，不从 Manifest 或模型输出执行命令：

- Codebase：`bytedcli` SSO Session
- Meego：Meego 官方设备授权
- Oncall：`lark-cli` 当前默认用户的飞书 IM 授权

启用与授权是两个独立状态。插件可以处于“已开启、待授权”，但此时不会创建
可运行的采集计划。

Oncall 默认按 `oncall`、`值班` 匹配当前用户可见群的名称和描述。用户可以在
插件页面增删 `config.search_terms`；保存后配置进入同一个定时 Task 的自然语言
指令，由 `oncall-clue-collector` 解释并通过通用 clue 入口投递原始群消息。
