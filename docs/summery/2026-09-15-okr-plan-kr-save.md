# OKR Plan 按 KR 粒度保存

> Status: delivered on branch `codex/okr-plan-kr-save`
> Date: 2026-09-15

Q4 Plan 原先把一个 Objective 作为自动保存和乐观并发的边界。同一个 O 下任意 KR 被他人保存后，旧页面继续保存该 O 都会收到 409；这会让互不相关的 KR 编辑彼此阻塞。

本次增加 Plan KR 写入接口 `PATCH /api/biz-okr/plans/:plan_id/krs/:kr_id`。KR 标题、负责人、标签、指标说明、指标和具体条目结构按 KR 自身版本保存；具体条目标题、负责人、类型、标签和 Meego 链接继续由已有 Point 接口独立保存。前端为每个 KR 分别维护脏状态和修订号，先完成待处理的 Point 写入，再保存对应 KR。

O 级接口继续兼容旧浏览器及 O 文案、KR 增删和排序。它在写入前校验请求内全部既有 KR 的版本，避免旧浏览器把 KR 级接口已保存的新内容覆盖掉。删除 Metric 或 Point 时，KR 结构令牌包含 Point 版本；因此独立 Point 更新之后，旧页面不能删除该 Point。

并发结果：

- 同一个 O 下编辑不同 KR：各自成功保存。
- 同时编辑同一个 KR：后提交者收到 409，本地内容保留，可明确重试。
- 同时编辑不同具体条目：继续各自成功保存。
- 旧浏览器提交过期 O 快照：在改写 KR 前收到 409。

验证覆盖 workspace 并发写入、API 路由、旧 `open_id` 在新 KR 请求包装中的兼容、前端冲突重试版本合并、全量前端测试和生产构建。
