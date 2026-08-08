# 飞书能力只读审计

## 所有权与边界

`$install-jarvis` 在选定 lark-cli Profile、完成 user OAuth 后执行一次能力审计，并把结果写入当前安装运行的 `evidence/feishu-capabilities.md`。`$bootstrap-jarvis-world-model` 复用这份结果，不重复建立另一套权限清单。

这一步只回答“当前 Profile 实际能做什么”，不是权限申请流程：

- 不运行 `auth login`，不发起增量授权，不修改开发者后台权限。
- 不因为缺少高级权限反复引导用户申请。
- scope 列表只用于定位；最终以只读 API 的 `ok=true`、返回字段和可见范围为准。
- 原始成功和错误分别保存，删除 token、device code、授权 URL 和 app secret。

## 能力分级

### 核心读取能力

完整世界模型初始化需要这些能力。缺失时不申请权限，保留原始错误，并将 `install.feishu-capabilities` 及相应世界模型项保持未勾选；服务底座仍可继续安装，但整体安装不能把相关阶段描述为完成。

| 能力 | 推荐只读探针 | 通过标准 |
|---|---|---|
| 当前用户与 Bot 身份 | `auth status --json --verify` | user、bot 均验证成功，user open_id 与选定 Profile 一致 |
| 本人基础通讯录 | `contact +search-user --user-ids me --as user` | 返回本人基础资料；字段为空与调用失败分开记录 |
| 文档搜索 | `drive +search` 搜索本人创建的 OKR 文档和最近 7 天本人编辑的文档 | 调用成功；零结果不等于缺权限 |
| 文档正文 | 有候选时对一个当前可读文档运行轻量 `docs +fetch` | 返回文档内容；没有候选时记录未实测，单文档无权属于资源边界，不代表全局 scope 缺失 |
| 会话与群成员 | `im +chat-list`，再对一个候选群运行 `im +chat-members-list` | user 身份调用成功 |
| 最近消息 | `im +messages-search` 限定最近 7 天 | 调用成功；零结果不等于缺权限 |
| CC Connect Bot 读取 | 以 bot 身份运行群列表；有候选群时再运行群成员和群消息轻量探针 | API 调用成功；零群表示 Bot 尚无可见群，不伪装成权限失败，留给端到端验收覆盖 |

命令参数、时间窗口和候选选择由 Agent 根据机器现状调整；所有读取显式带本次 `--profile` 和正确的 `--as user|bot`，不要依赖默认 Profile。

### 可选组织信息增强

以下高级通讯录能力不作为安装门禁，也不在安装或世界模型阶段申请：

- `contact:user.department:readonly`：部门 ID、直属上级等组织字段。
- `contact:user.employee:readonly`：职务等受雇字段。
- `contact:user.department_path:readonly`：完整部门路径。

审计时同时检查 scope 和用户详情实际返回字段。缺少任一权限或字段时：

1. 在证据中记录“未授权、租户未开放、API 未返回或数据本身为空”中当前能确认的最窄结论；无法区分时保持未知。
2. 世界模型继续使用 OKR 文档、近期文档和多条消息寻找职责或协作关系证据。
3. 只有多源证据足够时才以相应置信度写入；否则把直属上级、职务或部门路径保留为未知项。
4. 不把单条称呼或单个群名当作直属上级证据。

### 按配置启用的能力

当 CC Connect 的 `document_comments=true` 时，只读审计还要检查：

- `validate-binding` 确认文档评论开关、同一 App/Profile 和唯一 WebSocket 所有者。
- Bot 身份的 `drive.notice.comment_add_v1` 订阅状态为 `is_subscribe=true`。

`is_subscribe=false` 是功能配置缺口，不是权限申请结果；保持条件能力未完成，并由安装 Agent 把订阅配置作为 CC Connect 绑定的一部分处理。执行前先用 `lark-cli schema drive.user.subscription` 读回当前命令协议。订阅成功不代表 Bot 自动拥有全部文档。目标文档仍需逐篇“添加文档应用”；没有目标文档时不要为了测试制造评论写入。评论回复的真实写入能力留给用户明确参与的端到端验收，不能用 scope 勾选代替。

### 明确不使用

企业策略下不加载 `lark-okr`、不调用 OKR API，也不申请 OKR scope。OKR 证据只来自本人原始创建且当前身份可读的文档。

## 记录与打勾

`evidence/feishu-capabilities.md` 至少写清：Profile、审计时间、每项探针的身份、命令目的、成功/失败、实际可见范围、缺失字段、原始证据文件定位，以及 `permission_requests_started: false`。

满足以下条件后才勾选 `install.feishu-capabilities`：

1. 核心读取 API 探针全部成功；没有可读候选的分支已经明确记录为覆盖不足而不是权限错误；
2. 可选组织信息已经检查，存在缺口时已明确记录为非阻塞未知项；
3. 已启用的条件能力通过只读配置和订阅检查；
4. 审计过程中没有发起权限申请。
