# OKR 旧负责人兼容与原始 API 请求日志

针对 Q4 Plan 旧页面提交 `owners[].open_id` 被新接口拒绝的问题，按用户确定的两项范围实现并部署。没有增加浏览器草稿、保存队列、版本历史或恢复平台；未回填或修改正式 Plan 内容。

## 行为

1. OKR API 的 POST/PUT/PATCH 在严格解码前，将负责人中的旧 OpenID 按已核验迁移映射换成邮箱与规范姓名；未命中直接移除该负责人，正文继续正常校验保存。覆盖 Plan 更新的 `objective` 包装、O/KR/Point 嵌套及根级 owners；email 已存在时优先保留。其它非法字段、数据版本冲突等继续报错。
2. Hertz 通用中间件在认证、代理、兼容和业务校验之前将完整原始请求写入 `var/log/api-requests.jsonl`，请求结束追加同 logid 的 HTTP 状态和耗时。所有 `/api/` 与配置的开发代理 API 前缀均覆盖；不采样、不截断，非 UTF-8 原文用 Base64 保存。关闭 multipart 预解析，保证上传的原始字节可回读。Cookie、Authorization 等请求头不写入；原始 URI、正文完整保留。

负责人映射唯一运行时来源是 `data/okr/legacy-owner-identities.json`，由本次事故调查中已核验的迁移证据整理，包含 94 个旧 ID。不同 Bot 的已知 ID 分别精确映射，不依赖姓名猜测，不在线遍历其他 Bot。源文件保留应用和必要文档行证据。数据库和响应仍仅使用 email；这是对先前“旧请求一律拒绝”迁移策略的明确调整。

## 验证与部署

- `go test ./internal/observability ./internal/api ./internal/okrworkspace/... ./cmd/jarvis-server` 通过。
- Plan 实际路由 + 内存 SQLite 回归覆盖现代请求、旧 Owner 创建、目标 PATCH、未知 Owner 清空后的正文保存及邮箱回读。
- 请求日志覆盖未知字段 400、损坏 JSON、未认证/不存在路由、长正文、二进制、并发追加、不改请求内容，以及兼容前旧字段的完整保留。
- `./scripts/jarvis-deploy --skip-pull` 部署主服务成功；首页、healthz、readyz 验收通过。
- 线上对不存在的 Plan/O 做安全验证：已知与未知旧 Owner 请求均通过严格解码后返回业务对象不存在 404；额外非法字段仍返回 400。原始 body 与提交内容逐字一致。
- 线上向不存在的上传路由提交二进制 multipart，返回 404；日志 Base64 解码后与完整上传字节一致。
- 线上验证证据：`var/incident-wu-tuo-20260914/compat-delivery/live-smoke.json`。当前产品数据库一致性快照 integrity_check 为 ok，随改动提交，保留本次工作开始前已有的数据修改。

## 边界

推送前 review 补充：默认 Hertz Recovery 原先位于日志中间件外层，处理器 panic 时会先记录默认 200，再恢复为 500。已改为请求日志包住 Recovery，并增加 panic 回归，确保日志中的最终状态与实际 HTTP 500 一致。

日志只覆盖到达 Hertz 中间件的请求；HTTP 解析失败或超过现有请求体大小限制的报文不会进入中间件。记录只追加、不自动轮转或清理；磁盘写入失败会进入服务错误日志，但不阻塞业务保存。新日志不能补录历史失败请求或浏览器尚未发送的内容。正文仍可能因版本冲突、其它校验或数据库错误保存失败，届时可以按 logid 回读原始提交。
