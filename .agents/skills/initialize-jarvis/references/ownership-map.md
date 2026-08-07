# 初始化数据所有权

| 语义 | 唯一所有者 / 写入入口 | 初始化阶段的做法 | 禁止做法 |
|---|---|---|---|
| Principal 的 app-scoped open_id | `extract.principal_open_id`，本机 `conf/config.runtime.yaml` | Skill 自带 `jarvis-init configure` | 写进 Profile payload 或共享记忆冒充配置 |
| lark-cli 用户 profile | `lark_cli.profile`，本机 `conf/config.runtime.yaml` | Skill 自带 `jarvis-init configure` | 依赖机器默认 profile |
| Principal 身份与简介 | M1 `PrincipalProfile` | `update-principal` | 写进 rules、prompt 或 Skill 正文 |
| 项目、重点事项、人物、资料 | M1 对应服务 | 现有 `jarvis-tools` CRUD | 建 onboarding 专用表或 DTO 链路 |
| 群的发现字段 | M2 capture | 触发 discovery 后只读 | 由 M1 创建群、覆盖 chat_id/name/tier |
| 群的背景与监听选择 | M1 Group curated columns | `update-group` | 直接改 checkpoint 或 message 表 |
| 群扫描水位与消息 | M2 capture | 通过 related_group 触发现有扫描并读回 | 把初始化历史灌入 M2/M3 |
| 稳定行为偏好 | 适用阶段 rules | 默认只记录在草案，另行确认后再改 rules | 混进 Principal 事实或工具手册 |
| 运行证据与草案 | `var/onboarding/<run-id>/` | 本地文件，保留可追溯性 | 存 device_code、URL 或 token |

初始化 Skill 是流程真源，不拥有业务事实。它只调用现有所有者，并在每次写入后读回。
