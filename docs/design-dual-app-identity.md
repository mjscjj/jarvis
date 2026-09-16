# 双飞书应用身份设计

> Status: current。“Jarvis通知机器人”的登录 user scope 仍未收敛到设计要求；其 Bot 身份已用于广播
> Authority: normative design
> Last verified: 2026-09-07（含真实 Bot 身份、广播权限与可用范围核验）

## 目标

Jarvis 同时承担三件性质不同的事，由两个飞书应用按受众和 token 身份分别承载：

1. **替 owner 本人干活。** Jarvis 是 principal 的数字分身，要能读他的私聊、发消息、看日历、查妙记、处理审批。这需要极敏感的授权（`im:message.p2p_msg:get_as_user` 这类"以该用户身份读私聊"），但只服务**一个人**，凭证也只在本机。
2. **把 OKR 页面分享出去让别人填。** 页面要发给同事，每个访客用**自己的**飞书账号登录，Jarvis 只需要知道"这是谁"以及以他的身份读他有权限的文档。让外部访客去授权一个能读私聊和邮箱的应用是不可接受的。
3. **给一批同事发送系统通知。** 这只需要专用应用的 Bot 身份直接发送，不需要取得收件人的 user token，也不应让主 Jarvis Bot 为每个人创建助手群。

主应用与面向同事的应用必须按受众切开：主应用保持最大权限、只给 owner 用；“Jarvis通知机器人”以低敏 user scope 承载 OKR 登录，同时用独立的 tenant Bot scope 承载广播。两种 token 的权限边界分别核验，不能因为 Bot 要发通知就给登录用户开放消息读取权限。

## 两个应用

| | 主应用 | OKR 登录与通知应用 |
|---|---|---|
| App ID | `cli_a96a0c8d82b85cb1` | `cli_a96a2422f03bdbd7`（Jarvis通知机器人） |
| 定位 | Jarvis 本体，owner 的对等 Lark 身份 | OKR 登录入口 + 公司内通知广播 Bot |
| 受众 | 仅 principal 一人 | 所有被分享到页面的人 |
| 凭证持有者 | lark-cli 默认 profile | 主服务持有登录 secret；lark-cli 独立 profile 持有 Bot 凭证 |
| 后台开通的 user scope | 223，23 个域 | 155，19 个域（**应为 85，见下节**） |
| 代码请求的 scope | 不适用，由 lark-cli 管理 | 85，7 个域 |
| 广播所需 tenant scope | 不适用 | `im:message:send_as_bot`、`im:message:send_multi_users` |
| 授权方式 | 本机一次性登录，长期复用 | 每个访客用自己的账号走 device flow |

设计意图是让 OKR 登录 user token 只拿 85 个低敏 scope：`base` 39、`docs` 23、`wiki` 13、`drive` 5、`docx` 3、`profile` 1、`offline_access` 1，完全不含 user 级 `im`、`mail`、`contact`、`calendar`、`task`、`approval`、`vc`、`minutes`。`conf/okr-feishu-scopes.txt` 只表达这部分意图；通知 Bot 的 tenant 发送 scope 不在该文件中。

## 授权范围的真正生效点是开发者后台，不是 scopes 文件

**实测结论：飞书 device flow 忽略请求里的 scope 子集，按应用后台已开通的全集签发 token。**

2026-09-02 首次实测、2026-09-03 复测时，旧授权仍带有敏感权限。开发者后台完成收窄后，2026-09-09 再次核验：

- 最近两天的 112 份新授权均为 **86** 个 scope，`im`、`mail`、`contact`、`calendar`、`task`、`approval`、`vc`、`minutes`、`search` 敏感域全部为零。
- 九位 OKR 管理与打标用户的现有授权也均为 86 个 scope，可以安全用于页面登录。
- principal 的一份历史授权仍保留 155 个 scope，并会在 refresh 时沿用旧授权范围；历史授权不会因为后台收窄自动降权，应退出并重新授权或撤销旧授权。

`conf/okr-feishu-scopes.txt` 仍只表达登录 user scope 意图；真正的权限上限是开发者后台。名单按 `union_id` 识别，仅控制“管理与打标”入口是否显示，不限制 Plan、Review 和周报协作。

主应用独有且**应当**只在主应用上出现的敏感能力包括 user 级 `im:message`、`im:message.p2p_msg:get_as_user`、`mail:user_mailbox.message.body:read`、`contact:user:search`、`calendar:calendar.event:*`、`approval:instance:write`、`minutes:minutes.transcript:export`。通知应用只保留广播需要的 tenant 发送权限，不因此取得登录人的会话读取能力。

## OKR 登录当前做什么、预留什么

登录功能本身只用到 2 个 scope：

- `profile:user_profile:read` —— 拿 `open_id`、`union_id`、姓名、头像
- `offline_access` —— 换 refresh token，让登录态能续期而不用反复扫码

剩下 83 个是云文档、云空间、知识库和多维表格的读写权限，**当前链路并不调用**。保留它们是因为已经在同一次授权里拿到了：后续要做"用户往页面里贴一个飞书文档链接，Jarvis 以他自己的身份打开"这类功能时，不需要让所有人重新授权一遍。一次授权覆盖到位比日后追加更省事，而且这些域相对不敏感。

登录身份用于信息展示、评论署名、文档导出和 OKR 管理入口可见性。2026-09-16 源码增加两位既有主用户的登录互通：`internal/authn` 将已验证会话的 `union_id` 映射到主账号，仍检查实例 `auth.principals`；普通协作者不获得主模块身份。该映射不交换飞书令牌、不切换主应用凭证；详见 [登录互通改动记录](summery/2026-09-16-okr-main-login-identity.md)。

## 为什么用 union_id 认人

`open_id` 是**按应用隔离**的：同一个人在不同应用下拿到的 `open_id` 完全不同。`union_id` 在同一开发者名下的所有应用间保持一致。

实测同一个人（储节节）在两个应用下的标识：

| | 主应用 | Jarvis通知机器人 |
|---|---|---|
| `union_id` | `on_94b5aa46ca92b7aecd01031e5b2f0dc4` | 相同 |
| `open_id` | `ou_cfd9e106436c46adf20aaf9fe076c65d` | `ou_0f7d5f75fe23c57c1cf98c5123404f63` |

所以只要登录应用还可能变（换应用、加应用、合并应用），身份就必须锚在 `union_id` 上。`union_id` 由 `profile:user_profile:read` 一并返回，不需要额外 scope。代码在 `internal/okrworkspace/auth/feishu.go` 里对缺失 `union_id` 的响应直接报错——拿不到稳定标识就不建立会话，不存一个日后没人认得出的人。

广播也使用同一边界：OKR Owner 目前保存的是主应用 `open_id`，不能直接传给通知应用。已有 `union_id` 时通知 Bot 可直接使用；当前周报催填则由 `feishu-broadcast` 使用 principal 的只读人员查询按原 `open_id` 精确取得企业邮箱，再让通知 Bot 以 `receive_id_type=email` 直发，不按姓名猜另一个 App 的身份。

`union_id` 落地在三处：`AuthSession.UnionID`（会话）、`StoredToken.UnionID`（磁盘 token 文件）、`PageComment.AuthorUnionID`（评论署名）。

一个已知限制：企业没放开通讯录高级权限，`lark-cli contact +get-user` 只返回姓名，拿不到 `union_id`。所以**无法**用 lark-cli 把登录人反查回通讯录。这不影响当前功能——姓名和头像在登录时就由飞书直接返回并存进会话，登录侧是自足的。

## 隔离边界

换 OKR 登录应用不会波及主应用，依据是：

- **默认 lark-cli 不读这里的配置。** 默认 profile 仍是 `cli_a96a0c8d82b85cb1`，供主 Jarvis 的头像、人员和普通会话操作使用；`feishu-broadcast` 通过显式独立 profile 选择通知应用，绝不切换默认 profile。`identity.app_id` 仍只被 OKR 登录流程和 chat sidecar 使用。
- **没有跨应用的 open_id join。** `updated_by`、`created_by`、`opened_by`、`author_open_id` 都是只写不 join 的审计字段，既不参与鉴权也不与 KR 负责人（主应用命名空间）做匹配。换应用后新记录进入新命名空间，不会与既有数据冲突。
- **chat sidecar 按值传递配对凭证。** `GET /api/biz-okr/feishu-identity` 在同一个响应里返回 `app_id` 和 token 文件路径，sidecar 原样拼进 `LARKSUITE_CLI_APP_ID=… LARKSUITE_CLI_USER_ACCESS_TOKEN=… lark-cli … --as user`。app_id 与 token 始终成对，不存在拿主应用 id 去配 OKR 应用 token 的可能。

## 真源

| 内容 | 权威来源 |
|---|---|
| OKR 登录应用 ID、开关、session TTL | `conf/okr-module.yaml` 的 `identity` |
| OKR 登录**请求**的 scope 清单（仅表达意图） | `conf/okr-feishu-scopes.txt` |
| OKR 登录**实际生效**的授权范围 | “Jarvis通知机器人”开发者后台的 user scope 勾选项，仓库内无法约束 |
| 广播 Bot 身份、权限与操作步骤 | `.agents/skills/feishu-broadcast/SKILL.md`；凭证保存在 lark-cli 独立 profile |
| 会话表所在数据库 | 主库（`sqlite.path`），不是 `data/okr/okr.db`——会话是本机状态，不进 Git |
| OKR 应用密钥 | 环境变量 `JARVIS_OKR_EMILY_APP_SECRET` |
| 主应用凭证与身份 | `lark-cli` 自己的登录态，不在本仓库 |
| device flow 与 user_info 解析 | `internal/okrworkspace/auth/feishu.go` |
| 会话与 token 存储 | `internal/okrworkspace/auth/service.go`、`tokenstore.go` |
| 登录接口与中间件 | `internal/api/okr_identity.go` |

用户 access/refresh token 不落库，按 `open_id` 写到 `identity.token_dir`（`data/okr/feishu-tokens/<open_id>.json`），同一人再登录即覆盖。该目录被 `.gitignore` 排除，不入库。

换应用时 `open_id` 命名空间随之改变，旧应用的 token 文件会变成孤立凭证，应当直接删除而不是留着。

## 验证

- `internal/okrworkspace/auth/feishu_test.go` 覆盖 device flow 正常路径，以及缺失 `union_id` 时拒绝建立身份
- `internal/okrworkspace/auth/service_test.go` 覆盖 `union_id` 写入会话与 token 文件
- `internal/okrworkspace/comments_test.go` 覆盖评论署名 `union_id` 的写入与读回

2026-09-03 在部署实例上重走的完整链路（登录开关打开后），结果如下：

| 环节 | 结果 |
|---|---|
| `GET /api/biz-okr/me` 未登录 | `configured: true, authenticated: false` |
| 未登录 `POST /api/biz-okr/comments` | 401 `40180 请先使用飞书登录` |
| 只读 `GET /api/biz-okr/comments` | 200，不需要登录 |
| device flow 发起与轮询 | 走通，`completed` |
| 会话落库 | `open_id` 为通知应用命名空间，`union_id` 为跨应用稳定值 |
| session cookie | `jarvis_okr_session`，HttpOnly，365 天（由 `identity.session_ttl_hours` 配置） |
| 已登录发评论 | 署名 `储节节` + `author_union_id`，落库并可读回 |
| token 落盘 | `<新 open_id>.json`，含 `union_id` 与 refresh token |
| `GET /api/biz-okr/feishu-identity` | 返回通知应用的 `app_id` 与配对 token 路径 |
| lark-cli 吃通知应用的 user token | 接受，`identity: user`，读到云空间 341 个文件 |
| 回归：人员搜索、头像 | 正常，返回的仍是主应用 open_id |
| 主应用身份 | `lark-cli auth status` 仍为 `cli_a96a0c8d82b85cb1`，未受影响 |
| `POST /api/biz-okr/auth/logout` | 会话失效，`me` 回到未登录，写评论重新 401 |

未通过的一项是授权范围：见上文「授权范围的真正生效点是开发者后台」。
