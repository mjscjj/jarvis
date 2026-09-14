# OKR 文档导出使用当前网页登录人

导出按钮只创建一篇飞书文档，返回链接。文档归属当前网页登录人，保留飞书默认密级与分享权限；分享由用户在飞书中操作。

## 最终调用链

`POST /api/biz-okr/feishu-documents` → `RequireOKRIdentity` 从 HttpOnly session 取用户 → `UserTokens.Ensure(open_id)` 读取现有授权并按需刷新 → 同一登录应用的 App ID + 该用户 access token → 单个 CLI 子进程 `docs +create --as user`。

请求只接受标题和 Markdown，用户身份不接受前端传参。每个子进程单独设置凭证环境，不修改服务器全局环境或 CLI profile；没有用户授权时拒绝导出，不回退到默认用户或机器人。CLI 输出中的用户 token 做脱敏。通知机器人继续负责原有通知，不参与文档创建身份选择。

删除创建后的密级查询、密级更新、分享权限授权检查、权限更新和读回步骤，消除“文档创建成功但修改分享权限失败导致整个导出报错”的路径。旧 `export_secure_label` 配置只兼容严格 YAML 解析，不再生效。

授权失效返回 401 并提示重新登录；用户未授予文档权限返回 403 并提示重新授权；应用未开通文档权限提示联系管理员。一般导出错误显示简短提示，CLI 诊断留在服务日志。

## 验证

- `go test ./internal/larkcli ./internal/api ./internal/okrworkspace/auth ./internal/config ./cmd/jarvis-server` 通过。
- `go test -race ./internal/larkcli ./internal/api ./internal/okrworkspace/auth -run 'TestCreateMarkdownDocument|TestOKRDocument|TestUserTokens'` 通过。
- session A/B 分别选取 A/B 的 token；刷新一次后复用并保存轮换后的 refresh token；未登录、空凭证、错配用户、缺 scope 均有覆盖。
- 子进程并发身份隔离、继承凭证覆盖、错误脱敏与仅执行创建命令有覆盖。
- 前端 typecheck、181 项测试通过。
- Playwright 导出专项：`OKR_BROWSER_EXPORT_ONLY=1`，覆盖 Review 和周报生成链接、授权失效提示、失败后清除旧链接和恢复按钮。使用隔离 DB、真实 HTTP handler 与模拟飞书创建返回，共 30 次 OKR API 请求。
- 全量旧浏览器工作流在与本次导出无关的人员搜索步骤失败：其 fixture 未配置 `Directory`，返回 503。此处未修改产品人员搜索逻辑，导出验证使用上述专项入口。
- 只读汇总现有 149 份网页登录授权，全部记录了文档 import/create/write scope。未借用任何真实用户身份创建测试文档，实际飞书创建仍须由用户点击导出完成验收。

无产品数据结构迁移，无历史文档权限变更。
