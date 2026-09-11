# Jarvis 网页 SSO 登录接入

> 状态：方案已查证，代码接入与真实登录验收尚未完成。
> 核验日期：2026-09-11。本文统一维护网页登录方法；实际路由与配置仍以代码和运行时配置为准。

## 目标与当前状态

网站入口为 **https://emily.bytedance.net**。浏览器通过公司 SSO 确认当前访客身份，Jarvis 后端核验身份后匹配白名单，再建立自己的浏览器会话。

截至核验日期，线上 `/api/auth/status` 返回 `enabled:false`，网页可以打开不代表已验证访客身份或白名单。现有 `internal/authn` 仍使用 CLI 授权实现，不能视为本文方案已经落地。

## 采用的登录方法

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

## 域名接入条件

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

## 身份边界

| 场景 | 身份来源与职责 |
| --- | --- |
| Jarvis 网页访客 | 当前浏览器的个人 JWT；后端核验并匹配白名单 |
| 服务器后台工具 | 本机 bytedcli / lark-cli 已有授权；用于执行后台工作 |
| OKR 模块访客 | 继续使用 OKR 自己的飞书应用登录 |

本方案用于网页访客认证，不增加 Agent、stage 或 Task 之间的内部权限系统。

## 不再采用的网页登录方式

- 直接把本机 bytedcli 身份发给浏览器：只能说明服务器已授权，无法确认打开页面的人是谁。
- 用 `bytedcli auth login --begin` 为访客登录：本次实测进入 ByteCloud 服务账号创建、绑定流程，不符合网页个人登录需求。
- 强制 `--session --session-method qr` 或改成飞书唤起链接：实际用户遇到公司 SSO「不允许使用此登录方式」，更换打开链接的客户端没有解决认证方式限制。

这些结论只否定其作为本网站访客登录入口的用途，不影响 CLI 自己的正常授权流程。

## 实施与验收

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
