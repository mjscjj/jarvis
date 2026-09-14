# OKR 稳定身份实施与验证

实施分支：`codex/okr-stable-identity-20260914`；隔离 worktree：`jarvis-okr-stable-identity`。按要求先验证代码、合并当前分支，再迁移历史产品数据。

## 最终行为

- OKR 通知和登录应用固定为 `cli_a96a2422f03bdbd7`，显式 profile `jarvis-notify-audit`；不受个人助手默认 profile 变化影响。
- 业务人员以完整企业邮箱识别；姓名用于显示，可选 union_id 用于核验。旧裸 open_id 请求在写入前被拒绝。只有邮箱域名统一小写，避免擅自改变邮箱本地部分。
- 当前通知 Bot 尚缺通讯录读取权限，开发者后台在此环境受可信设备策略拦截。显式过渡目录固定 `cli_a96a0c8d82b85cb1` 的同名 profile/user，只读返回邮箱与精确匹配头像；发送完全不依赖它的 open_id。获得应用通讯录权限和数据范围后，删除三个 `directory_*` 过渡设置即可启用已实现的同应用 Bot 目录。不能把过渡状态表述为“所有能力已统一到一个应用”。
- 评论事务同时保存收件意图；发送结果持久化。成功不重发，有消息 ID 的未知结果只回读；无回执的未知结果不盲重试。页面仅显示简洁状态，详细 CLI 错误留在日志。编辑和历史迁移不触发通知。
- 世界模型只对实际具备相同 union_id 的 Owner 建立人物关联，不再把不同应用的 open_id 混用。过渡目录未提供 union_id 时不推断人物关系；已有显式项目关系与 Principal 根关系保留。

## 验证

- `go test ./...` 全量通过；目录分页/缓存失败、显式 profile 环境隔离、通知回执核验不重发、邮箱派生身份不随姓名改变等回归测试通过。
- 前端类型检查通过，181 项测试通过。
- Python 迁移测试覆盖幂等、冲突回滚、正文与作者留痕不变、旧进程写入拦截。
- 用 `./scripts/jarvis-deploy --skip-pull --config conf/config.identity-validation.yaml` 启动独立 18824 实例，使用 SQLite 一致性副本与独立运行数据库。`healthz/readyz` 正常；实际目录查询返回冯程的完整企业邮箱与头像，Q3 看板只输出 email Owner。
- 在迁移副本上再次运行 dry-run，所有变更计数均为 0。
- 没有向任何同事发送测试消息，也没有重发历史评论提醒。

## 历史迁移依据

95 个不同旧业务 ID 中，80 个可在明确来源 profile 下直接精确解析；14 个文档导入 ID 通过同一文档 `DAF4dqfploOMAkxW9Ucm1elPyqg` 的原版本 `17962`、同一表格行与 @ 引用位置对应到旧主应用 ID，再精确取得邮箱。证据保留原始 ID、profile/App、邮箱、名称快照及文档引用。

`Jiaxin Cao` 的一个历史 KR Owner，原应用精确查询及离职人员查询均为空。保留姓名、清空业务 ID、邮箱为空；相关催填快照不可通知，不按同名猜邮箱。原始 ID 留在迁移证据和迁移前备份。

演练变化：342 条 KR Owner、314 条 Point Owner、3 条评论 mentions、17 条 Follow-up owners、6 份催填快照。评论正文、作者 ID、原始导入 source_payload、业务内容与结构保持原样；Owner 显示名归一到目录名称。评论提醒记录不回填、不补发。

## 生产交付结果

- 代码 `c1cc30c8`、共享开发兼容补充 `94b84522` 均已 fast-forward 合并到 `codex/jarvis-okr-mvp`，合并完成后才操作正式数据。
- 2026-09-14 09:59 UTC 确认主服务无执行中 Task，并停掉主服务及共享数据库的开发进程；`fuser` 确认数据库没有活动使用者后执行迁移。
- 一致性备份、完整迁移映射、原文档版本证据：`/data00/home/chujiejie.1/workspace-local/jarvis-okr-mvp/var/identity-migration/20260914T095928Z`；`okr-before.db` 是迁移前备份，`okr-after.db` 是交付快照。
- 正式迁移计数与演练一致；新建区域需求/决定的三个人员列也被检查和加入旧 ID 写入拦截，目前无需转换。二次 dry-run 所有计数为 0，SQLite integrity_check 为 ok。
- 主服务（18802）和开发容器（18812）分别使用 `./scripts/jarvis-deploy --skip-pull` 完成构建与重启，health 检查通过。主服务 readyz 正常，生产评论 API 已回读确认截图中的 @冯程 为 `fengcheng.charles@bytedance.com`。
- 开发 worktree 的区域评审未提交改动予以保留；通过三方合并同步邮箱/通知兼容改动、修复区域 POC 与评论边界，Go 测试与前端 181 项测试通过。没有将这些未提交区域功能并入主分支；原始变更备份位于 `var/identity-migration/dev-before.patch` 和 `dev-merge/before/`。
- 开发网关同步支持精确目录读取，保持个人消息等能力不可用；禁止通过通知 profile 静默切换到主目录 profile。
- 独立验证服务已停止并取消开机启动；worktree 与验证副本保留，便于复查。
- 新通知账本为 0 条，历史迁移未生成发送意图或补发任何消息。权限未开通与 1 条未绑定历史 Owner 仍按上文明确保留。
