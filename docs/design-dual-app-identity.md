# 双飞书应用身份设计

> Status: current
> Authority: normative design
> Last verified: 2026-09-02 @ `68c8fa4`

## 目标

Jarvis 同时承担两件性质完全不同的事，需要两个飞书应用分别承载：

1. **替 owner 本人干活。** Jarvis 是 principal 的数字分身，要能读他的私聊、发消息、看日历、查妙记、处理审批。这需要极敏感的授权（`im:message.p2p_msg:get_as_user` 这类"以该用户身份读私聊"），但只服务**一个人**，凭证也只在本机。
2. **把 OKR 页面分享出去让别人填。** 页面要发给同事，每个访客用**自己的**飞书账号登录，Jarvis 只需要知道"这是谁"以及以他的身份读他有权限的文档。让外部访客去授权一个能读私聊和邮箱的应用是不可接受的。

一个应用无法同时满足这两条：权限面必须按受众切开。所以主应用保持最大权限、只给 owner 用；OKR 模块单独用一个低敏应用面向所有登录人。

## 两个应用

| | 主应用 | OKR 登录应用 |
|---|---|---|
| App ID | `cli_a96a0c8d82b85cb1` | `cli_a96a2422f03bdbd7`（Emily Pro） |
| 定位 | Jarvis 本体，owner 的对等 Lark 身份 | OKR 页面对外分享时的登录入口 |
| 受众 | 仅 principal 一人 | 所有被分享到页面的人 |
| 凭证持有者 | `lark-cli`（本机登录态） | 主服务，密钥在 `JARVIS_OKR_EMILY_APP_SECRET` |
| user scope 数 | 223，23 个域 | 85，7 个域 |
| 敏感能力 | 有：消息、邮箱、通讯录、日历、任务、会议、妙记、审批 | 无 |
| 授权方式 | 本机一次性登录，长期复用 | 每个访客用自己的账号走 device flow |

OKR 应用的 85 个 scope 是主应用的**严格子集**，域分布为 `base` 39、`docs` 23、`wiki` 13、`drive` 5、`docx` 3、`profile` 1、`offline_access` 1。它完全没有 `im`、`mail`、`contact`、`calendar`、`task`、`approval`、`vc`、`minutes` —— 也就是说，即使这个应用的密钥泄露，也读不到任何人的消息、邮件或日程。

主应用独有的敏感能力包括 `im:message`、`im:message.p2p_msg:get_as_user`、`mail:user_mailbox.message.body:read`、`contact:user:search`、`calendar:calendar.event:*`、`approval:instance:write`、`minutes:minutes.transcript:export` 等约 100 项，这些**不会**出现在 OKR 应用上。

## OKR 登录当前做什么、预留什么

登录功能本身只用到 2 个 scope：

- `profile:user_profile:read` —— 拿 `open_id`、`union_id`、姓名、头像
- `offline_access` —— 换 refresh token，让登录态能续期而不用反复扫码

剩下 83 个是云文档、云空间、知识库和多维表格的读写权限，**当前链路并不调用**。保留它们是因为已经在同一次授权里拿到了：后续要做"用户往页面里贴一个飞书文档链接，Jarvis 以他自己的身份打开"这类功能时，不需要让所有人重新授权一遍。一次授权覆盖到位比日后追加更省事，而且这些域相对不敏感。

现阶段登录的产出只服务两条链路：**信息展示**（页面上显示"当前登录人是谁"）和**评论署名**（知道这条评论是谁写的）。不做行级权限，不做可见性过滤，其余 OKR/周报写操作仍按 `Jarvis` 记账。

## 为什么用 union_id 认人

`open_id` 是**按应用隔离**的：同一个人在不同应用下拿到的 `open_id` 完全不同。`union_id` 在同一开发者名下的所有应用间保持一致。

实测同一个人（储节节）在两个应用下的标识：

| | 主应用 | Emily Pro |
|---|---|---|
| `union_id` | `on_94b5aa46ca92b7aecd01031e5b2f0dc4` | 相同 |
| `open_id` | `ou_cfd9e106436c46adf20aaf9fe076c65d` | `ou_0f7d5f75fe23c57c1cf98c5123404f63` |

所以只要登录应用还可能变（换应用、加应用、合并应用），身份就必须锚在 `union_id` 上。`union_id` 由 `profile:user_profile:read` 一并返回，不需要额外 scope。代码在 `internal/okrworkspace/auth/feishu.go` 里对缺失 `union_id` 的响应直接报错——拿不到稳定标识就不建立会话，不存一个日后没人认得出的人。

`union_id` 落地在三处：`AuthSession.UnionID`（会话）、`StoredToken.UnionID`（磁盘 token 文件）、`PageComment.AuthorUnionID`（评论署名）。

一个已知限制：企业没放开通讯录高级权限，`lark-cli contact +get-user` 只返回姓名，拿不到 `union_id`。所以**无法**用 lark-cli 把登录人反查回通讯录。这不影响当前功能——姓名和头像在登录时就由飞书直接返回并存进会话，登录侧是自足的。

## 隔离边界

换 OKR 登录应用不会波及主应用，依据是：

- **lark-cli 不读这里的配置。** 它用自己的本机登录态（`lark-cli auth status` 仍是 `cli_a96a0c8d82b85cb1`），头像搜索、人员搜索、发消息一律走主应用。`identity.app_id` 只被 OKR 登录流程和 chat sidecar 使用。
- **没有跨应用的 open_id join。** `updated_by`、`created_by`、`opened_by`、`author_open_id` 都是只写不 join 的审计字段，既不参与鉴权也不与 KR 负责人（主应用命名空间）做匹配。换应用后新记录进入新命名空间，不会与既有数据冲突。
- **chat sidecar 按值传递配对凭证。** `GET /api/okr/feishu-identity` 在同一个响应里返回 `app_id` 和 token 文件路径，sidecar 原样拼进 `LARKSUITE_CLI_APP_ID=… LARKSUITE_CLI_USER_ACCESS_TOKEN=… lark-cli … --as user`。app_id 与 token 始终成对，不存在拿主应用 id 去配 OKR 应用 token 的可能。

## 真源

| 内容 | 权威来源 |
|---|---|
| OKR 登录应用 ID、开关、session TTL | `conf/okr-module.yaml` 的 `identity` |
| OKR 登录申请的 scope 清单 | `conf/okr-feishu-scopes.txt` |
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
