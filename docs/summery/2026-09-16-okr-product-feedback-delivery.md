# Emily 建议反馈交付

## 结果

Emily 各页面右上角现有统一的「建议反馈」入口。入口打开全局右侧抽屉，登录用户可以提交问题、查看待解决与已解决问题、回复、幂等地 +1/取消 +1，并看到反馈人、回复人和 +1 用户的姓名与头像。反馈创建者和现有 OKR 管理人员可以标记解决或重新打开。

## 语义所有权

- 产品反馈是 Emily 全局协作数据，不属于季度、周次、Plan 或区域对齐，因此没有复用 `okr_workspace_comment`。
- 登录身份仍由 OKR 飞书登录提供；稳定身份优先使用 `union_id`，其次是规范化企业邮箱和登录应用 `open_id`。
- 姓名、邮箱和头像 URL 在反馈、回复、+1 时保存为身份快照，历史展示不依赖会过期的浏览器 Session；图片加载失败回退姓名首字。
- 前端由 `ProductFeedbackProvider` 唯一持有抽屉、未解决数量和当前页面来源上下文，各页面 Header 只消费 `ProductFeedbackTrigger`。

## 持久化与接口

Biz OKR migration 新增：

- `okr_product_feedback`
- `okr_product_feedback_reply`
- `okr_product_feedback_plus_one`

公开业务接口位于 `/api/biz-okr/feedback`，读取和写入均经过现有 OKR 身份中间件。+1 使用 `(feedback_id, actor_key)` 联合主键和幂等 PUT/DELETE；解决状态使用 `expected_version` 乐观并发。

## 验证

- `go test ./internal/okrworkspace ./internal/api`：通过，覆盖双用户回复、幂等 +1、取消 +1、解决权限、版本冲突、历史列表和重新打开。
- `npm run typecheck`：通过。
- `npm test`：200 项通过。
- `npm run build`：通过。
- `./scripts/jarvis-deploy --skip-pull`：实例已重新构建并启动，`healthz`、`readyz` 正常，新表已在 `data/okr/okr.db` 建立。
- 使用飞书实时核验的短时身份走当前实例真实 API：反馈人头像、+1 头像、回复、解决、历史列表和重新打开全部通过；测试反馈及短时 Session 已清理。
- 浏览器脚本 `web/test/okrProductFeedback.browser.mjs` 已覆盖桌面和窄屏入口及完整交互，但当前容器 Chromium 缺少 `libglib`、NSS、X11 等系统动态库，未能实际启动浏览器。因此构建成功和真实 API 验证不能表述为已查看页面效果。

## 环境说明

当前 checkout 的 `.git` 文件仍指向宿主机不存在的 worktree 元数据路径，`git status` 和提交不可用。本次保留了工作区原有改动；`data/okr/okr.db` 的新增产品表需要在 Git 元数据恢复后与源码一并提交。
