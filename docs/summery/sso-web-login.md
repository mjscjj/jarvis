# Jarvis 网页 SSO 登录接入

> 状态：当前部署仍使用 CLI 网页授权；已完成 CN / i18n 授权 API 对比，个人 JWT SDK 方案未实施，真实账号完整登录仍待验收。
> 更新日期：2026-09-14。本文统一维护网页登录方法、授权域名和排障证据；实际路由与配置仍以代码和运行时配置为准。

## 目标与当前状态

网站入口为 **https://emily.bytedance.net**。浏览器通过公司 SSO 确认当前访客身份，Jarvis 后端核验身份后匹配白名单，再建立自己的浏览器会话。

2026-09-11 记录的线上状态为 `enabled:false`；截至 2026-09-14，当前实例已启用外层登录与白名单。`internal/authn` 使用独立 bytedcli profile 为网页访客发起授权，OKR 访客仍使用飞书登录。后文个人 JWT SDK 是此前提出、尚未实施的方案，不能当作当前实现。

## 浏览器会话跨重启保留

已完成 SSO 的 Jarvis 会话保存在实例私有运行主库的 `browser_auth_session` 表中，不再使用进程内存。浏览器继续使用原有 `jarvis_session` Cookie；服务重启后，只要 Cookie 和会话未过期，就直接恢复登录，不重复授权。

- 有效期仍为登录后 12 小时，重启不会刷新有效期。
- 只保存随机 Cookie 的 SHA-256 摘要、已验证用户名/邮箱和到期时间，不保存 SSO 令牌，不写入随 Git 提交的 OKR 数据库。
- 每次验证仍匹配当前 `auth.principals`；退出登录删除对应会话，重启不会恢复已退出的会话。写库失败不会签发 Cookie，也不会将退出失败显示为成功。
- 首次升级前的会话只存在旧进程内存，无法自动迁移，需要重新登录一次。升级后的登录才会跨重启保留。
- 尚未完成授权的临时流程仍沿用现有自动恢复逻辑；此改动不替换 SSO 入口或 OKR 飞书登录。

验证覆盖数据库关闭后重开、原 Cookie 恢复身份、退出后再次重开、原到期时间、白名单变更与数据库故障；不以这些测试代替真实账号的上游授权验收。

## 当前 CLI 授权域名与超时排查（2026-09-14）

**本次登录慢的直接证据是：当前服务器请求 CN 授权 API 时频繁发生 TLS 握手超时；改用 `https://cloud.byteintl.net` 后，创建授权约需 0.8 秒。应先修正授权 API 地址，不能把十几秒的超时当作正常登录耗时，再单靠延长等待或增加重试处理。**

### 实际创建授权的对比

测试使用同一台部署服务器、bytedcli `0.144.0` 和其内置授权 runtime `0.0.36`，每次使用新的独立 profile。两组命令均使用 `--site cn`，只改变 `BYTECLOUD_CLI_API_BASE_URL`，每个域名测试六次，实际调用：

```text
POST /api/v1/ai_auth/ai/auth/service_account_app/cli_registration
```

| 授权 API 域名 | 创建授权成功 | TLS 握手超时 | 耗时 |
| --- | --- | --- | --- |
| `cloud.bytedance.net`（CN） | 1 / 6 | 5 / 6 | 失败约 10.7–10.8 秒；唯一成功约 0.879 秒 |
| `cloud.byteintl.net`（i18n BD） | 6 / 6 | 0 / 6 | 成功 0.797–0.847 秒，中位数 0.821 秒 |

另用 `cloud.byteintl.net` 创建一个新授权后，连续两次完成接口轮询均正常返回 `pending`，耗时分别为 0.77 秒和 0.80 秒，无 TLS 超时。这里的 `pending` 表示等待用户确认，不是网络失败。

直接请求同一路径的连接对比另测六轮，连接超时统一为 10 秒：CN 握手成功 4 / 6，`cloud.byteintl.net` 成功 6 / 6，`cloud-i18n.bytedance.net` 成功 2 / 6。收到 HTTP 401 只计为握手成功，不计为授权成功。**`cloud-i18n.bytedance.net` 与 `cloud.byteintl.net` 不是同一个地址，本次前者也有明显超时，不应混用。**

### 正确设置实际请求地址

Jarvis 部署通过 `conf/config.runtime.yaml` 的以下字段配置网页登录 API：

```yaml
auth:
  login_api_base_url: https://cloud.byteintl.net
```

`cmd/jarvis-server` 将配置传入 `internal/authn`，后者仅为自己的 bytedcli 子进程设置 `BYTECLOUD_CLI_API_BASE_URL`；授权创建、完成轮询与临时 profile 清理使用同一个地址，不修改宿主进程环境或后台工具的配置。留空沿用 CLI 自身配置，当前实例显式使用上面的 i18n BD 地址。

实测当前版本只设置 `--site i18n`、`--site i18n-bd` 或 `--site i18n-tt`，创建授权仍访问 `cloud.bytedance.net`。站点参数不能代替授权 API 地址配置。以下环境变量才实际改变请求目标，已用本地诊断端点确认其生效路径：

```bash
BYTECLOUD_CLI_API_BASE_URL=https://cloud.byteintl.net \
  bytedcli --json --profile YOUR_LOGIN_PROFILE auth login --begin

BYTECLOUD_CLI_API_BASE_URL=https://cloud.byteintl.net \
  bytedcli --json --profile YOUR_LOGIN_PROFILE auth login --complete YOUR_COMPLETE_TOKEN
```

开始授权和后续轮询应使用同一 profile，并保持相同的 API 地址配置。`YOUR_COMPLETE_TOKEN` 使用开始授权返回的值，文档和日志不保存真实令牌。网页登录接入此配置时应限定在对应 CLI 子进程，避免无意改变后台工具的站点配置。

API 请求目标、浏览器授权页 URL、SDK Partition 是不同配置。本次 API 切到 `cloud.byteintl.net` 后，返回的浏览器授权页仍属于 `cloud.bytedance.net`；应使用服务端原样返回的 URL，不手动替换域名。此次结果也不等于后文个人 JWT SDK 的 Partition 已完成验证。

### 当前故障与验证范围

页面停在“正在验证字节身份”的复现链路是：`/api/auth/status` 正常返回未登录 → 创建授权的 CN 上游连接超时 → `/api/auth/login` 返回 502 → 前端把自动重试继续显示成验证中的转圈，隐藏了具体错误。当前代码已修正错误显示并保留自动重试；**错误显示修复不等于上游域名配置已经修正。**

当前实例已通过 `auth.login_api_base_url` 配置 i18n BD 地址。域名对比验证的是连接、创建授权与等待确认的轮询；真人授权完成、身份归属和浏览器进入仍需完整验收。样本支持当前部署优先使用 `cloud.byteintl.net`，不把这次测量推广成所有网络环境的永久结论。

配置接入并执行标准部署后，通过 `https://emily.bytedance.net` 连续六次调用 `/api/auth/login`，全部 HTTP 200，耗时 1.206–1.527 秒，中位数 1.299 秒；两次 `/api/auth/login/complete` 轮询均正常返回 `pending`，耗时 1.257 秒和 1.136 秒。真实浏览器访问普通对话页约 2.8 秒显示授权入口，验证转圈已退出。这些是部署后测量，包含网页网关与接口开销，与前面的独立 CLI 对比数据分别记录。

## 2026-09-11 个人 JWT SDK 方案（未实施）

使用字节云官方前端个人 JWT SDK `@bytecloud/common-lib`，选择 CN Partition。个人 JWT 由当前浏览器的公司登录态取得，后端负责验证和放行。

1. 用户打开 Jarvis，点击「字节登录」。
2. 前端调用官方 SDK 获取个人 JWT；需要登录时由 SDK 跳转公司 SSO，完成后返回当前页面。认证方式由公司 SSO 决定。
3. 前端把 JWT 提交给 Jarvis 后端。
4. 后端依照官方认证协议验证签名、有效期、签发方、Partition 及必要声明，确认身份类型为 `person_account`，再读取可信用户名或邮箱。
5. 后端按 `auth.principals` 精确匹配白名单，通过后建立 Jarvis 会话；未通过则返回无访问权限。
6. 用户退出时清除 Jarvis 会话；下次访问受保护页面重新验证。

官方 SDK 调用示例（尚未接入本仓库）：

```ts
import { bytecloudAsyncJwtService } from '@bytecloud/common-lib'

const service = await bytecloudAsyncJwtService.getServiceByPartition('cn')
const jwt = await service.getJwt()
// 将 jwt 提交给 Jarvis 后端核验；不要写入 URL 或日志。
```

JWT 的公钥获取、声明字段和验证规则必须按官方协议实现，不能只解码 payload 就信任其中的邮箱，也不能由前端决定是否在白名单内。网站已登录后使用 Jarvis 自身的会话机制。

## 个人 JWT SDK 方案的域名接入条件

官方要求非字节云网站完成 JWT CORS 域名登记。当前需要确认登记状态的精确 Origin 为：

```text
https://emily.bytedance.net
```

使用 CN Partition，Origin 不带末尾斜线。按官方指引确认域名满足内网解析等要求，登记状态由平台核实。

- [域名登记工单入口](https://bpm.bytedance.net/apply?cid=3034)
- 申请用途：Jarvis 内部网站获取当前员工浏览器个人身份，由后端验签并限制指定员工访问。
- 当前需要放行的身份：`chujiejie.1@bytedance.com`、`claire.li@bytedance.com`。实际名单的唯一配置来源是运行时 `auth.principals`，代码不硬编码这两个人。

2026-09-11 的探测出现过 HTTP 403、缺少允许跨域响应头，以及请求超时；平台尚未确认原因，不能单凭 403 断言域名未登记。申请工单尚未提交，真实用户 JWT 尚未取得。

独立 worktree 的临时主机端口不是本次指定的网站 Origin。测试环境若使用其他 Origin，需要单独确认其登记状态和 Cookie 条件，不能通过伪造 Origin 或代理服务器身份代替访客登录。

## 个人 JWT SDK 方案的身份边界

| 场景 | 身份来源与职责 |
| --- | --- |
| Jarvis 网页访客 | 当前浏览器的个人 JWT；后端核验并匹配白名单 |
| 服务器后台工具 | 本机 bytedcli / lark-cli 已有授权；用于执行后台工作 |
| OKR 模块访客 | 继续使用 OKR 自己的飞书应用登录 |

本方案用于网页访客认证，不增加 Agent、stage 或 Task 之间的内部权限系统。

## 历史方案中的排除项

以下是 2026-09-11 对个人 JWT SDK 方案的选型记录；其中 CLI 授权尚未从当前部署移除。上面的域名测试修正了对当前超时的诊断，不代表以下方案切换已经完成。

- 直接把本机 bytedcli 身份发给浏览器：只能说明服务器已授权，无法确认打开页面的人是谁。
- 用 `bytedcli auth login --begin` 为访客登录：本次实测进入 ByteCloud 服务账号创建、绑定流程，不符合网页个人登录需求。
- 强制 `--session --session-method qr` 或改成飞书唤起链接：实际用户遇到公司 SSO「不允许使用此登录方式」，更换打开链接的客户端没有解决认证方式限制。

这些结论只否定其作为本网站访客登录入口的用途，不影响 CLI 自己的正常授权流程。

## 个人 JWT SDK 方案的实施与验收

先确认域名接入条件，在独立 worktree 接入前端 SDK 与后端 JWT 验证，复用现有白名单和 Jarvis 会话。域名路由、代码和配置经过验证后再切换线上；仅修改文档不启用门禁。

验收必须覆盖：

- 真实用户完成 SSO 并返回 Jarvis，取得的身份属于当前访客。
- 白名单用户进入，非白名单用户被拒绝。
- 过期、伪造或不符合个人身份要求的 JWT 被拒绝。
- 登录失败不发会话，退出后私有接口重新拦截。
- OKR 独立登录、本机后台工具继续正常工作。

首页可访问、出现 SSO 登录页、链接能打开或模拟身份单测通过，都不能代替真实登录和白名单验收。

## 官方依据

- [前端接入字节云个人 JWT](https://cloud.bytedance.net/docs/bytecloud/docs/63c4c6df7e9d2a021ec21002/6530dd13edc2c702f6a9661c)：SDK、浏览器登录及回跳流程。
- [字节云 JWT CORS 配置域名安全规范及操作指引](https://cloud.bytedance.net/docs/bytecloud/docs/63c4c6df7e9d2a021ec21002/6530d3576a54bb0310fcefb4)：域名登记要求与申请入口。
- [JWT 接入概览](https://cloud.bytedance.net/docs/bytecloud/docs/63c4c6df7e9d2a021ec21002/6440b460e22f2f0253c7c7f4)：个人身份声明、服务端验证与授权职责。
