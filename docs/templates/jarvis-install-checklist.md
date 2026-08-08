# Jarvis 整体项目安装清单

- 安装运行：`{{RUN_ID}}`
- 开始时间：`{{STARTED_AT}}`
- 仓库：`{{REPO_ROOT}}`
- lark-cli Profile：`{{PROFILE}}`

这是从完整仓库 checkout 到可交付 Jarvis 的唯一安装状态页。完成一项后把 `[ ]` 改成 `[x]`，并在同一行或下一行附上实际读回或验收结果。没有执行、暂时阻塞或不适用的项目保持 `[ ]`，必须写明“未做 / 阻塞 / 不适用”的原因。不要只凭命令退出码打勾。

## A. 仓库与安装决策（`$install-jarvis`）

- [ ] <!-- id:install.checkout --> 完整仓库已 checkout，当前分支/commit 和远端已记录，Agent 已加载 repo-local `$install-jarvis` 与 `$bootstrap-jarvis-world-model`。
- [ ] <!-- id:install.run --> 本安装运行目录、清单和 `evidence/` 已创建；后续所有阶段持续更新本页。
- [ ] <!-- id:install.existing --> 已检查旧数据库、已有 Jarvis/Qdrant/CC Connect 服务及其 checkout 归属；复用、迁移、新装或替换策略已决定。

## B. 工具链与全部依赖（`$install-jarvis`）

- [ ] <!-- id:install.machine --> macOS/架构、Go、Node/npm、jq、git、CGO、C 编译器和 Xcode Command Line Tools 已验收。
- [ ] <!-- id:install.lark-cli --> 官方 lark-cli 与 Lark Agent Skills 已安装，版本和 Skills 路径已读回。
- [ ] <!-- id:install.agent-cli --> 配置选择的 Agent CLI 已安装并登录；仓库基线使用 traex 时已完成 SSO 并读回状态。
- [ ] <!-- id:install.cc-binary --> 仓库固定版本和补丁的 `bin/cc-connect-jarvis` 已构建，版本与 patch commit 已验收；此时尚未启动 daemon。
- [ ] <!-- id:install.qdrant --> Qdrant 已安装或确认复用，healthz 正常。
- [ ] <!-- id:install.dependencies --> `jarvis-install validate-dependencies` 返回 `ok=true`；在此之前没有启动 CC Connect 或 Jarvis 主服务。

## C. 飞书身份与一体化绑定（`$install-jarvis`）

- [ ] <!-- id:install.lark-profile --> 已选择一个 lark-cli Profile；该 Profile 的用户 OAuth、Bot 和 token 均验证成功。
- [ ] <!-- id:install.feishu-capabilities --> 已完成飞书能力只读审计；核心文档、消息、群和 Bot 读取 API 已验收，无候选数据时已记录覆盖边界，已启用的条件能力已检查；高级组织字段缺失已记录为非阻塞未知项，过程中没有发起权限申请。
- [ ] <!-- id:install.identity --> Principal open_id、Profile 和 Git author 已写入本机 runtime config 并读回。
- [ ] <!-- id:install.cc-binding --> CC Connect `jarvis-codex` 已绑定同一个 App/Bot；Agent 每轮先读取 Jarvis context；`validate-binding` 通过。

## D. 服务启动与运行底座验收（`$install-jarvis`）

- [ ] <!-- id:install.cc-service --> 补丁版 CC Connect daemon 已从当前 checkout 启动，9810/9820 和实际 launchd program 已验收。
- [ ] <!-- id:install.server --> Jarvis 主服务已通过签名安装脚本从当前 checkout 启动；没有裸 `go build` 覆盖运行 binary。
- [ ] <!-- id:install.runtime --> `jarvis-install validate` 已通过；Qdrant、Jarvis `/healthz`、`/readyz`、配置权限和同一 App/Profile 绑定均正常。

## E. 世界模型建立（`$bootstrap-jarvis-world-model`）

- [ ] <!-- id:world-model.existing-data --> 已检查存量 Principal、项目、人物、重点事项、资料与人工群背景；如有存量，已取得合并、补充或重建决定。
- [ ] <!-- id:world-model.identity-evidence --> 已读取本人基础身份和当前可见的部门信息；职位、直属上级和部门路径已尝试取证，缺失时保留原因并继续，没有申请高级权限或猜测。
- [ ] <!-- id:world-model.documents --> 已搜索并按需读取本人创建的 OKR 文档，以及最近 7 天本人撰写或编辑的文档；记录分页、权限和覆盖边界。
- [ ] <!-- id:world-model.messages --> 已读取最近 7 天必要的消息、群元数据和成员信息；没有把历史消息灌入正常线索流水线。
- [ ] <!-- id:world-model.inference --> 已形成“人、事、物、群、重点事项”的世界模型工作稿；高影响歧义已请用户决定，其余未知项已明确保留。
- [ ] <!-- id:world-model.entities --> Principal、Project、Person、ManagedResource 与 KeyMatter 已逐项写入并逐项读回。
- [ ] <!-- id:world-model.groups --> 候选群已发现；选中的监听群已按 chat_id 更新、首次扫描并读回 checkpoint。
- [ ] <!-- id:world-model.relations --> 必要的关系和有真实时间的基线事实已写入并读回；没有重复已有结构化关联。
- [ ] <!-- id:world-model.acceptance --> `jarvis-world-model validate` 与语义抽查已完成；数量、覆盖不足和所有未解决项已记录。

## F. 真实端到端验收（`$install-jarvis`）

- [ ] <!-- id:e2e.message --> 用户已在一个监听群发送新消息，Jarvis 已从正常 M2 链路读回该消息。
- [ ] <!-- id:e2e.cc --> 用户已通过绑定的 Jarvis Bot 发起一次 CC Connect 对话，Agent 已读取当前 Jarvis context 并正常回复。
- [ ] <!-- id:e2e.final-status --> `jarvis-install status` 已读回；所有未勾选项均已写明“未做 / 阻塞 / 不适用”的原因，最终结果已交付。

## 未完成、未做或不适用

在这里逐项写明对应 checklist id、现状、原因和下一步。最终交付时这一节必须存在，即使内容为“无”。

- 无

## 最终结果

- 依赖验收：待填写
- 身份与绑定验收：待填写
- 飞书能力审计：待填写
- 服务验收：待填写
- 世界模型验收：待填写
- 端到端验收：待填写
- 总体结论：待填写
- 下一步：待填写
