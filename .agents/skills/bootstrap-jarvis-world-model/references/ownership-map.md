# 世界模型建立阶段的数据所有权

| 语义 | 唯一所有者 / 写入入口 | 初始化阶段的做法 | 禁止做法 |
|---|---|---|---|
| Principal 的 app-scoped open_id | `extract.principal_open_id`，本机 `conf/config.runtime.yaml` | `$install-jarvis` 已配置；初始化只读 | 写进 Profile payload 或共享记忆冒充配置 |
| lark-cli 用户 profile | `lark_cli.profile`，本机 `conf/config.runtime.yaml` | `$install-jarvis` 已配置；所有读取显式使用 | 依赖机器默认 profile |
| Jarvis Bot App/Profile 绑定 | `$install-jarvis` + `integrations/cc-connect/` | 初始化只验证运行底座已经通过 | 初始化阶段再选择 Bot、配置或重启 CC |
| Bot 飞书长连接 | CC Connect `jarvis-codex` | 补丁版 CC Connect 独占连接，向 Jarvis 转发 localhost 审批回调 | Jarvis 与 CC Connect 同时消费相同 App 长连接 |
| Principal 的 Git author 模式 | `dailydigest.git_author`，本机 `conf/config.runtime.yaml` | `$install-jarvis` 已配置；初始化用于证据关联 | 写死用户名，或写入 Profile 冒充程序配置 |
| Principal 身份与简介 | M1 `PrincipalProfile` | `update-principal` | 写进 rules、prompt 或 Skill 正文 |
| 项目、重点事项、人物、资料 | M1 对应服务 | 现有 `jarvis-tools` CRUD | 建 onboarding 专用表或 DTO 链路 |
| 实体之间的重要语义关系 | M1 `RelationFact` | `create-relation`，随后 `list-relations` 读回 | 重复已有 `project_id`/`person_id` 结构化关联，或把关系写进 rules |
| 有日期的决策、交付、阻塞和方向变化 | M1 `Fact` | `append-fact --source initialization`，随后 `list-facts` 读回 | 把静态简介或对象创建动作重复写成事实 |
| 群的发现字段 | M2 capture | 触发 discovery 后只读 | 由 M1 创建群、覆盖 chat_id/name/tier |
| 群的背景与监听选择 | M1 Group curated columns | `update-group` | 直接改 checkpoint 或 message 表 |
| 群扫描水位与消息 | M2 capture | 通过 related_group 触发现有扫描并读回 | 把初始化历史灌入 M2/M3 |
| 稳定行为偏好 | 适用阶段 rules | 默认只记录在工作稿，另行确认后再改 rules | 混进 Principal 事实或工具手册 |
| 整体安装状态页 | `$install-jarvis` 的 `var/install/<run-id>/INSTALL_CHECKLIST.md` | 由整体安装调用时只更新世界模型 E 区 | 独立初始化伪造或接管整张安装清单 |
| 世界模型证据与工作稿 | 安装时复用 `var/install/<run-id>/`；独立重建用 `var/onboarding/<run-id>/` | 持续更新 `evidence/` 和 `world-model.md` | 用工作稿替代 M1/M2 业务真源 |

初始化 Skill 是流程真源，不拥有业务事实。它只调用现有所有者，并在每次写入后读回。Group→Project、KeyMatter→Project、ManagedResource→Person/Project/Principal 已有结构化字段时，以对应实体为唯一所有者，不再额外创建同义 RelationFact。
