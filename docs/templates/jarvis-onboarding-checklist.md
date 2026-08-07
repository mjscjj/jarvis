# Jarvis 首次安装与初始化清单

- 运行：`{{RUN_ID}}`
- 开始时间：`{{STARTED_AT}}`
- 仓库：`{{REPO_ROOT}}`
- lark-cli Profile：`{{PROFILE}}`

使用说明：完成一项后把 `[ ]` 改成 `[x]`，并在同一行或下一行附上验证结果。没有执行、暂时阻塞或不适用的项目保持 `[ ]`，必须写明“未做 / 阻塞 / 不适用”的原因。不要只凭命令退出码打勾，要以读回或真实验收结果为准。

## A. 安装与运行底座（`$install-jarvis`）

- [ ] <!-- id:install.machine --> 已检查机器、旧数据库和已注册服务；已决定复用或新装策略。
- [ ] <!-- id:install.dependencies --> Go、Node、jq、git、lark-cli、Lark Skills、Agent CLI、补丁版 CC Connect 和 Qdrant 已安装，`validate-dependencies` 返回 `ok=true`。
- [ ] <!-- id:install.lark --> 已选择一个 lark-cli Profile，用户 OAuth 与 Bot 均验证成功。
- [ ] <!-- id:install.identity --> Principal open_id、Profile 和 Git author 已写入本机 runtime config 并读回。
- [ ] <!-- id:install.cc --> CC Connect `jarvis-codex` 已绑定同一个 App/Bot；Agent 每轮先读取 Jarvis context；绑定校验通过。
- [ ] <!-- id:install.services --> 补丁版 CC Connect 与 Jarvis 主服务已从当前 checkout 启动。
- [ ] <!-- id:install.acceptance --> `jarvis-install validate` 已通过；记录 daemon、端口、`/healthz` 与 `/readyz` 结果。

## B. 世界模型（`$initialize-jarvis`）

- [ ] <!-- id:init.existing-data --> 已检查存量 Principal、项目、人物、重点事项、资料与人工群背景；如有存量，已取得合并、补充或重建决定。
- [ ] <!-- id:init.identity-evidence --> 已读取本人身份、部门、职位和直属上级；未知字段保留原因，没有猜测。
- [ ] <!-- id:init.documents --> 已搜索并按需读取本人创建的 OKR 文档，以及最近 7 天本人撰写或编辑的文档；记录分页、权限和覆盖边界。
- [ ] <!-- id:init.messages --> 已读取最近 7 天必要的消息、群元数据和成员信息；没有把历史消息灌入正常线索流水线。
- [ ] <!-- id:init.inference --> 已形成“人、事、物、群、重点事项”的世界模型工作稿；高影响歧义已请用户决定，其余未知项已明确保留。
- [ ] <!-- id:init.entities --> Principal、Project、Person、ManagedResource 与 KeyMatter 已逐项写入并逐项读回。
- [ ] <!-- id:init.groups --> 候选群已发现；选中的监听群已按 chat_id 更新、首次扫描并读回 checkpoint。
- [ ] <!-- id:init.relations --> 必要的关系和有真实时间的基线事实已写入并读回；没有重复已有结构化关联。
- [ ] <!-- id:init.acceptance --> `jarvis-init validate` 与语义抽查已完成；数量、覆盖不足和所有未解决项已记录。

## C. 真实端到端验收

- [ ] <!-- id:e2e.message --> 用户已在一个监听群发送新消息，Jarvis 已从正常 M2 链路读回该消息。
- [ ] <!-- id:e2e.cc --> 用户已通过绑定的 Jarvis Bot 发起一次 CC Connect 对话，Agent 已读取当前 Jarvis context 并正常回复。

## 未完成、未做或不适用

在这里逐项写明对应 checklist id、现状、原因和下一步。最终交付时这一节必须存在，即使内容为“无”。

- 无

## 最终结果

- 安装验收：待填写
- 世界模型验收：待填写
- 端到端验收：待填写
- 下一步：待填写
