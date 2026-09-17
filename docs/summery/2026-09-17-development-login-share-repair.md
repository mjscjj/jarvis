# 开发实例登录与 OKR 分享故障修复

## 环境与边界

- 基线：`codex/emily-development` / `4654840f`，本轮代码变更在其上。
- 入口：`https://emily.bytedance.net/dev/`，实例 `emily-development`。
- OKR 业务目录挂载自 OKR-MVP worktree 的 `data/okr/`；会话使用开发实例的 `var/jarvis-lixiaolin.db`。
- 2026-09-17 02:07 UTC 通过容器内 `./scripts/jarvis-deploy --skip-pull` 部署。没有重启主实例。

## 结论和修复

1. 开发实例原 SSO 名单只有 `chujiejie.1`。按用户指令在本机 runtime 中增加 `ruoyizhang@bytedance.com` 和已核验账号 `lixiaolin`。三个账号可通过字节 SSO 准入，主实例名单未改。两边 SSO 登录地址相同，不能将此次问题归因于登录域名。
2. 01:35 UTC 开始的 Plan 评论请求持有 OKR 数据库唯一连接；通知上下文却通过 `service.db` 再取连接，形成等待，随后 scope、board、plans 都不能返回。健康检查读取的是另一数据库，因此仍为 200。隔离数据库以单连接和超时复现同一等待位置。修复让通知上下文及周度指标层级读取沿用调用者事务，不新增事务或扩大连接池。
3. 通用 `OnboardingGate` 错误包裹分享模块，访客触发个人世界模型初始化并收到 401。页面入口改为由模块负责自己的访客登录，个人工作台仍使用原 onboarding。浏览器回归明确断言分享入口不访问 `/api/setup/`，也不挂载 `.world-model-setup`。

## 验证

| 验证 | 结果 |
|---|---|
| `go test ./internal/okrworkspace ./internal/authn ./internal/api -timeout 120s` | 通过，新增单连接 Plan 与周报 metric 评论回归 |
| 前端类型检查、203 项单测、生产构建 | 通过 |
| `instanceNavigation.browser.mjs` | 3 个真实匿名旧分享入口、4 个模拟身份导航场景通过；模拟部分不代表本人登录验收 |
| 公网健康及业务 API | health/ready、scope、Q3 core-board、Q4 plans 返回 200；core-board 返回 116105 字节真实数据 |
| 本人真实授权后的页面导航 | 8 个 OKR 页签通过 |
| 真实 Plan 定义与评论 | 新建 O/KR、本人 Owner、评论发布/刷新/回复/编辑/解决/历史/重开/回读通过，测试 Plan 和评论已删除 |
| Review、周报导出 | 从真实页面创建飞书文档，并回读确认正文和文档所有者为本人 |

真实业务测试用本人现有授权实时核身后签发的短时会话，结束已撤销。公网代理会重写 cookie 名，测试使用代理后的名称。没有伪造他人身份，没有向同事发送测试通知。

首次 Plan 用例因旧脚本点击了新增加的 Objective Owner 控件，等待 KR PATCH 超时；已将控件定位限制到 KR article，单独重跑通过。该失败不是评论接口复发。

证据（不含凭证的摘要留在本机）：

- `var/okr-real-run.gn2BxP31/`：导航与两个导出结果及飞书回读；首次 Plan 定位失败。
- `var/okr-real-run.F9nYENuy/`：修正定位后的真实 Plan 与评论全流程。
- Review 测试文档：`https://bytedance.my.larkoffice.com/docx/V3cTdpg7KovS4sx30yKmizYlyff`。
- 周报测试文档：`https://bytedance.my.larkoffice.com/docx/TSVBdJcPmonURSxV2lpmp407y2f`。

## 未验证与保留边界

- 三人逐个完成真实字节 SSO 授权未验证，不能将准入配置或本人飞书业务测试表述为三人 SSO 均已验收。
- 若怡企业邮箱已通过目录核验，但当前可用权限未返回足以绑定其飞书 union ID 与邮箱的证据，未按姓名猜测绑定。她应通过字节 SSO 进入开发工作台；飞书登录 OKR 后再进入私人工作台可能需要重新完成 SSO。
- `lixiaolin` 的已有主工作台导航偏好保留；开发实例准入与导航偏好是不同配置。
- 未增加 OKR 数据库 readiness 探针；现有 health/ready 不应替代业务 API 验证。
