# Biz OKR Plan 飞书文档导出

## 结果

- Biz OKR Plan 的全量 AI 评审按钮旁新增「导出OKR Plan」。
- 导出继续使用当前网页登录人的飞书授权和既有 `/api/biz-okr/feishu-documents` 接口，不增加身份、权限或数据库真源。
- 文档按 KR 的业务方向拆成多张表，每张表固定为 O、KR、优先级、策略具体KR、产品具体KR 五列；优先级展示为 Focus item、P1、P2 或未标注。
- 同一个 O 跨其所属 KR/具体 KR 行纵向合并；同一个 KR 跨策略/产品具体 KR 行纵向合并。
- 没有业务方向的 KR 和尚未拆出 KR 的 O 进入「未标注业务」表格。
- Plan 导出不包含负责人、指标、其它标签、评分、进展、评论或 AI 评审结果。

## 实现

飞书文档导入仍使用 Markdown 通道，表格部分使用该通道支持的 XML 扩展标签。`<td rowspan="...">` 表达合并单元格，正文文本在进入 XML 单元格前独立转义。

Review、周报与 Plan 共用同一个前端飞书导出组件，统一处理导出中状态、成功链接、转换警告、失败重试和范围切换后的旧结果清理。Review 与周报原有 Markdown 内容保持不变。

## 初次实施验证记录

- `npm --prefix web test`：206 项通过。
- `npm --prefix web run typecheck`：通过。
- `go test ./internal/api ./internal/larkcli`：通过。
- `git diff --check`：通过。
- `PATH=/opt/go/bin:$PATH ./scripts/jarvis-deploy --skip-pull`：构建、重启和 HTTP 200 健康检查通过，开发实例地址为 `http://127.0.0.1:18812`。
- 发布静态产物已回查到「导出OKR Plan」、五列表头及 `rowspan` 生成逻辑。

初次实施时未定位到可用的 Playwright/Chromium，因此当时只写入浏览器专项断言，未实际执行；也没有点击真实按钮创建外部飞书文档。提交前复验结果见下文。

## 提交前 Review 与复验（2026-09-17）

- 修复混合业务分类时，其他业务的 O 被误投影为「未标注业务」空行的问题，并增加分类隔离回归。
- 浏览器 fixture 统一剥离实例 `/dev/` 前缀，避免真实请求路径与断言、代理路径不一致。
- 使用机器已有的 Playwright 和 Chromium 完成导出专项浏览器测试：Plan、Review、周报返回文档链接，Plan 合并表格内容正确，授权失效可重试；共 55 次隔离后端 API 请求。飞书文档服务为 stub，不代表真实飞书创建通过。
- 完整浏览器工作流通过：14 组检查、303 次隔离后端请求，覆盖 Plan、区域、Review、周报、并发冲突与 Agent 操作。修正测试的 KR Owner 定位、头像图片响应和分享 URL 尾斜杠，并为首个 Plan 列表等待加超时。
- 前端单测 207 项、类型检查、`go test ./...` 均通过。
- `go test -race ./internal/api ./internal/authn` 通过；前端生产构建输出到临时目录并通过，仍有现有的大 chunk 提示。
- 随本次改动统一业务写身份，并验证所有注册写路由拒绝匿名浏览器请求，登录作者落库正确，本机 CLI、无效和过期会话行为符合约定。
- 数据库提交使用实际共享库的 SQLite backup 快照，完整性检查为 `ok`；254 处图片引用对应资源均存在，无身份会话或 token 表。
- 本次提交前未重新部署，也未执行真实飞书登录、评论通知或文档创建验收；前文部署记录属于先前实施阶段。
