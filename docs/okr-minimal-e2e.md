# OKR 最小闭环验收

这套验收只使用当前 OKR 开发工作树，但运行数据与现有 18802 预览完全隔离：

- 18803：最小 E2E 服务；
- `var/okr-e2e/okr.db`：1 个季度 O、1 个 KR、2 个拆解点和两周进展；
- `var/okr-e2e/runtime.db`：Task、ExecutionRun 和其它 Jarvis 运行态；
- `.okr-e2e-conf/`：由脚本生成的本机配置，不提交 Git；
- 18802 及 `data/okr/okr.db`：不读写。

## 测试数据

季度固定为 `2026-Q3`，因为系统的 OKR 主周期就是季度。周报是季度 OKR 下的周度观察：

- `2026-W35`：两条基线进展；
- `2026-W36`：先开启为空周，用于验证催填；随后填写两条进展；
- 负责人：运行者显式提供的一个真实 `ou_` open_id；
- Report C：读取 W35 和 W36，产物只留在 Task 结果中。

## 运行

先设置唯一测试负责人：

```bash
export JARVIS_OKR_E2E_OWNER_OPEN_ID='ou_xxx'
export JARVIS_OKR_E2E_OWNER_NAME='你的名字'
```

准备并构建：

```bash
scripts/okr-minimal-e2e prepare
scripts/okr-minimal-e2e build
```

在一个终端前台启动：

```bash
scripts/okr-minimal-e2e serve
```

在另一个终端依次执行：

```bash
scripts/okr-minimal-e2e seed
scripts/okr-minimal-e2e verify-empty
reminder_task_id=$(scripts/okr-minimal-e2e run-reminder)
scripts/okr-minimal-e2e fill
scripts/okr-minimal-e2e verify-filled
scripts/okr-minimal-e2e cas
task_id=$(scripts/okr-minimal-e2e run-report-c)
scripts/okr-minimal-e2e verify-agents
scripts/okr-minimal-e2e status
```

`verify-empty` 证明 W36 已开启但没有进展，并且只催唯一负责人一次；`verify-filled` 证明文档、图片、评论和进展均能回读，填写完成后催填归零；`cas` 证明 Agent 的单条进展写入使用自己的版本锁，不会改变稳定 KR 版本；最后一个 Task 证明 M5 能从两个周次生成内部 Report C。

若要从头重跑，先停止 18803，然后明确删除 `var/okr-e2e` 与 `.okr-e2e-conf`。脚本不会自动删除或覆盖已有测试数据。
