# Emily development 本轮收口（2026-09-16）

## 已完成的功能

- Review/周报、Plan、区域对齐共用的评论抽屉增加“只浏览今日评论”。
  按浏览器当地自然日和创建时间筛选，包含旧讨论今天新增的回复及今天创建的
  已解决评论；仅编辑旧内容不会被算成新增。逐条浏览保留原文上下文及前后导航。
- 页面保持打开跨过午夜、切回窗口或重新打开抽屉时更新日期；今日为空时显示
  明确空态。原来的全部讨论串浏览、历史评论开关、评论编辑等能力保留。
- 区域分享的复制失败回退、链接展示和空负责人处理，以及当前 Review 周提醒
  规则保留。这些是开发分支已有功能修复，不属于应删除的环境兼容层。

## 环境整理的交付边界

独立实例代码已完成：普通 Docker bridge 网络、原生 CLI、实例自己的配置与
HOME、本地 Chat runner、统一前端 API 前缀。删除旧出口网关、宿主 Lark 转发、
跨实例 Chat 执行和主站聊天入口覆盖。按用户最终确认的简单边界，`data/okr`
整个目录只做一次完整挂载，不分类、不迁移、不清理，也不对子目录做覆盖。
真实验收脚本不再默认使用主站/固定个人身份。

**运行迁移已完成。** 开发容器已使用普通 bridge 网络和新的
`var/container/home`；Lark 用户授权为储节节，ByteDance CLI 为
`chujiejie.1`。按用户决定，Codex 授权从主环境复制一次到开发 HOME，此后两边
各自保存，不再挂载主凭证。旧开发出口网关已停用，本次迁移没有操作主服务。

本轮没有执行人工 OKR 业务写入、发送通知、创建飞书文档或重启主服务。开发实例
按设计打开同一份 OKR 数据库；产生的当前数据库快照按仓库规则随交付提交。

## 验证证据

- 前端全量 196 项单元测试和 TypeScript 类型检查通过；评论单元测试在
  `America/Los_Angeles` 时区也通过，避免 UTC-only 测试掩盖本地日期问题。
- `/dev/` 路径下的前端生产构建和标准部署通过，运行页面返回 HTTP 200。
- 评论真实组件的 Chromium 测试通过：1400px/UTC 与 375px/Asia/Shanghai；
  覆盖新回复、已解决评论、原有浏览方式、前后导航、午夜更新、空态和重新打开。
  测试加载产品样式，检查抽屉固定布局及标题不溢出。
- 浏览器组件测试使用独立 Vite fixture，所有评论请求均为模拟 GET，禁止写请求；
  另外单独回读了运行容器的 Lark、ByteDance 与 Codex 登录状态。
- `internal/okrchat`、`internal/chat`、`internal/okrworkspace/moduleconfig`、
  `cmd/jarvis-config`、`internal/textstore` 及 5 项部署/验收入口测试通过。
- 新容器验证了两个原生 CLI、Codex、直连网络及整个共享 OKR 目录；
  OKR 目录以外的主凭证、主聊天附件/会话和旧出口 socket 均不可见。
  两项依赖宿主 Git/systemd/既有 CLI 的安装集成测试在旧容器中未通过，
  不宣称全量 Go 测试通过；与本轮相关的工具目录测试已通过。

评论浏览器检查可复跑：在 `web/` 启动 `npm run test:comments:serve`，另一个终端
设置 `OKR_BROWSER_URL` 指向测试服务并执行 `npm run test:comments:browser`。
按本机需要设置 `PLAYWRIGHT_MODULE` 和 `CHROME_EXECUTABLE`；无需产品登录凭证。
