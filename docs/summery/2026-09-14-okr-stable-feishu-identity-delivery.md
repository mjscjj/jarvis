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

生产合并、备份、迁移与重启结果将在完成后追加。
