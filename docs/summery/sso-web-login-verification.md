# Jarvis 网页 SSO 接入核验（2026-09-11）

## 结论

当前完整测试服务的 CLI 登录实现不能满足纯网页登录体验。`bytedcli auth login --begin` 会进入服务账号绑定；`--session --session-method qr` 在实际用户处被公司登录策略拒绝。历史上的一点进入复用了服务器身份，不能据此确认浏览器访客身份。

仓库、测试实例运行配置和服务环境未找到可复用的独立字节 SSO 网站应用配置。发现字节云官方提供前端个人 JWT 接入，适用于非字节云网站，可免去自行设计 CLI 授权流程。尚未完成该方案代码实现或真实登录验收。

## 官方方案

- 前端使用 `@bytecloud/common-lib` 的 `bytecloudAsyncJwtService.getServiceByPartition('cn')` 获取当前浏览器用户的个人 JWT；未登录时由 SDK 跳转 SSO，完成后返回当前页面。
- 后端依据官方认证协议核验 JWT 签名、有效期及身份声明；只接受个人身份，然后核对已有用户白名单，建立 Jarvis 会话。
- 保持 OKR 自身登录和本机后台工具原有行为。
- 前置条件是 JWT 跨域域名登记，不能用后端代理或关闭浏览器校验来代替。

## 实测边界

- 正式域名 Origin 的 JWT GET 探测曾返回 HTTP 403，未返回允许跨域的响应头；另有请求超时。403 原因尚未由平台确认，不能单凭该结果断言域名未登记。
- 测试域名对应 JWT 地址存在连接超时及 IPv6 解析失败，浏览器请求超时。
- 未用服务器身份冒充访客完成登录，也未提交域名申请工单。
- 本次未改动主服务或替换当前测试服务登录代码。

## 域名登记申请草稿

用途：Jarvis 内部网站识别当前员工浏览器身份，服务端核验后仅放行 chujiejie.1 与 claire.li；不使用服务账号绑定流程。

Partition：CN。

用户已确认本机使用域名，待确认并登记的精确 Origin（不带末尾斜线）：

- `https://emily.bytedance.net`

2026-09-11 实测此域名的 `/healthz` 正常，`/api/auth/status` 返回 `enabled:false`，对应当前线上服务。先前的 `n199-199-203.byted.org:18813` 只是独立 worktree 临时测试入口，不作为用户指定的接入域名。

需要平台确认已有登记状态、域名是否满足内网解析要求，以及测试环境 HTTP Cookie 限制。测试应优先配备独立 HTTPS 域名。

工单入口：<https://bpm.bytedance.net/apply?cid=3034>。

## 来源

- [前端接入字节云个人 JWT](https://cloud.bytedance.net/docs/bytecloud/docs/63c4c6df7e9d2a021ec21002/6530dd13edc2c702f6a9661c)
- [字节云 JWT CORS 配置域名安全规范及操作指引](https://cloud.bytedance.net/docs/bytecloud/docs/63c4c6df7e9d2a021ec21002/6530d3576a54bb0310fcefb4)
- [JWT 接入概览](https://cloud.bytedance.net/docs/bytecloud/docs/63c4c6df7e9d2a021ec21002/6440b460e22f2f0253c7c7f4)

## 验收条件

真实浏览器完成个人登录并返回；白名单用户获得会话；非白名单用户被拒绝；过期或伪造 JWT 被拒绝；退出后私有接口重新拦截；OKR 登录不受影响。打开 SSO 首页、显示授权链接或模拟身份单测均不算真实登录完成。
