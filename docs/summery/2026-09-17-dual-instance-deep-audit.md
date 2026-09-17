# 主实例与开发实例深度审查报告

> 提交前复核（2026-09-17）：本文保留下列基线时的调查记录，不代表当前全部仍未修复。后续提交 `8c5e26f5` 已加入 OKR 数据库 readiness 探针；本次提交统一了 OKR 浏览器写接口身份。实际运行库是主 worktree 的 `jarvis-okr-mvp/data/okr/okr.db`，开发 worktree 的同名文件被容器挂载覆盖；不能据开发副本缺表推断运行库倒退。此次已从实际共享库通过 SQLite backup 获取提交快照，四张 Objective owner / 产品反馈表均存在。本文中的停服、目录隔离、凭证轮换等建议是历史审查建议，并非本次授权或已执行操作；其余运行现场结论须重新验证。

> 审查日期：2026-09-17（UTC）
> 开发实例基线：`codex/emily-development` / `4654840f900e93973d6fb5a72e7739a2568b88d9`
> 主实例基线：`codex/jarvis-okr-mvp` / `59a081edad6931b166cfa1e948be3adf4dcdfb79`
> 审查方式：13 个并行只读方向，加主线程交叉验证
> 本轮边界：只收集问题、验证证据并提出方案；未修复、未重启、未清理产品数据。

## 1. 结论

两边的常规 Go 与前端测试均通过，代码不是整体不可维护，OKR 核心数据的 SQLite 完整性检查也通过。但目前不能把系统判断为“可放心继续迭代”：双实例共享数据的边界、部署原子性和 Task 状态恢复存在几处高风险缺口，而且开发实例此刻已经出现了一个真实的部分故障。

最需要先处理的不是大规模重构，而是四件事：

1. 把主实例 OAuth token 移出开发实例可读写的共享目录，并轮换已经落入该边界的凭证。
2. 定位并恢复当前 `/dev/` OKR 卡死；把 OKR 数据库查询加入 readiness。
3. 明确共享 SQLite 的唯一迁移者、唯一通知 worker、schema 兼容协议和 Git 产品数据真源。
4. 把部署前置检查、staging、回滚和真实外层入口 smoke 补齐，避免“拒绝部署但前端已经换掉”或“健康通过但产品不可用”。

复杂度判断：

- OKR 业务代码有几个增长热点，但适合用窄 helper、纯映射和行为测试渐进整理，不建议现在拆大包或重写。
- 真正已经复杂过头的是“双 worktree、两个版本、一个可写数据库、两个迁移器、两个通知 worker、两个 tracked DB blob”这一运行模型。它制造了代码以外的隐式状态和发布耦合。
- 主实例的 Task/调度恢复链路总体方向正确，但若干崩溃窗口会使 Task 卡住、提前完成或继续产生副作用，应优先修状态闭包，而不是扩功能。

## 2. 证据等级与优先级

报告使用三种证据等级：

- **已复现**：本轮通过只读运行探针重复观察到。
- **代码证明**：沿生产者、持久化、消费者或发布链路可确定触发窗口；未对正式数据执行破坏性复现。
- **产品边界待确认**：代码行为明确，但是否构成缺陷取决于产品承诺。

优先级：

- **P0**：立即止血，涉及当前不可用或实例隔离失效。
- **P1**：下一轮开发前处理，存在数据、外部副作用、状态机或部署事故风险。
- **P2**：安排专项修复，通常是明确的功能竞态、恢复缺口或维护风险。
- **P3**：随迭代收敛的体验、测试或代码卫生问题。

## 3. 当前真实故障

### P0-1：开发实例 OKR API 卡死，但健康检查仍为绿色

证据等级：**已复现**。

2026-09-17 最终复测：

| 探针 | 结果 |
|---|---|
| 主实例 `/healthz`、`/readyz` | 200 |
| 主实例 `/api/biz-okr/scope` | 200，约 0.50 秒 |
| 主实例 `/api/biz-okr/core-board` | 200，约 0.94 秒 |
| 开发实例 `/dev/healthz` | 200 |
| 开发实例 `/dev/readyz` | 200，约 1.31 秒 |
| 开发实例 `/dev/api/biz-okr/scope` | 8 秒超时，0 字节 |
| 开发实例 `/dev/api/biz-okr/core-board` | 8 秒超时，0 字节 |

开发容器内 Supervisor 的 ingress 和 jarvis-server 都显示 `RUNNING`。jarvis-server PID 508 同时持有：

- `/opt/jarvis/data/okr/okr.db-journal`；
- 共享 `okr.db` 的 POSIX write lock；
- 多个阻塞中的代理连接。

这与一个未结束的 SQLite 写事务或写路径卡死一致，但本轮没有足够证据确认是哪一个请求触发，不能把“SQLite 写事务”写成最终根因。

为何 health/ready 没发现：当前 readiness 只探测通用数据库连接，未对独立的 OKR 数据库执行真实查询，见 [health.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/api/health.go:84)。

止血方案：

1. 保存 goroutine、锁、journal、最近请求和进程日志证据后，再做受控恢复。
2. 用数据库副本做 `integrity_check`、journal 恢复和触发请求定位，不直接拿正式文件做实验。
3. readiness 增加有超时的 OKR 只读查询，并区分主 DB 与 OKR DB。
4. 增加 `/dev/` 真实入口的 OKR scope smoke，而不仅是进程存活。

### P0-2：开发容器能读写主实例 OAuth token

证据等级：**代码与运行配置证明**。

主实例把 token 配置在共享业务目录：

- [okr-module.runtime.yaml](/data00/home/chujiejie.1/workspace-local/jarvis-okr-mvp/conf/okr-module.runtime.yaml:6)：`identity.token_dir: data/okr/feishu-tokens`。

开发容器又把整个主实例 `data/okr/` 以读写方式挂到 `/opt/jarvis/data/okr`：

- [emily-dev](/data00/home/chujiejie.1/workspace-local/emily-development/scripts/emily-dev:76)。

Docker 现场确认该挂载为 `rw=true`，容器可见 token 目录；审查没有读取 token 内容。产品数据共享已经越界成身份状态共享，开发代码或容器内进程可接触主实例用户 token。

止血方案：

1. 将主实例 `identity.token_dir` 迁到 `var/okr/feishu-tokens` 或另一实例私有状态目录。
2. 将已有 token 按已跨边界处理并轮换。
3. 开发容器只挂载明确的产品数据库和资源，不再挂载整个 `data/okr/`。
4. 增加挂载边界测试，禁止 token、cookie、CLI profile 和其他实例身份状态出现在共享树。

## 4. 跨实例、数据和部署问题

### P1

| 编号 | 问题与证据 | 影响 | 最小整改方向 |
|---|---|---|---|
| X1 | 两个不同版本进程都会对同一 SQLite 执行 `AutoMigrate`，没有 schema version、兼容范围或唯一迁移者，见 [migrate.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/migrate.go:128) | 同时启动可锁冲突；旧代码可能错误解释新数据；非加法迁移会直接破坏兼容 | 数据库增加显式 schema/protocol version；只允许一个实例迁移；双版本期只准向后兼容迁移并 fail-fast 检查 |
| X2 | 两边通知 worker 消费同一 delivery queue，但开发版新增 owner 通知和 stale `sending` 恢复，主版仍是旧语义，见 [开发 comment_delivery.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/comment_delivery.go:35) 与 [主 comment_delivery.go](/data00/home/chujiejie.1/workspace-local/jarvis-okr-mvp/internal/okrworkspace/comment_delivery.go:24) | 相同评论因入口或竞争 worker 不同而产生不同收件人/恢复结果 | 指定唯一外部副作用执行实例；producer/consumer 加 payload version；主 worker 先升级再允许新 producer |
| X3 | 两个 worktree 都跟踪各自的 `data/okr/okr.db`，但运行时开发容器使用主 worktree 的 DB 覆盖自身文件 | 本地检查与真实运行不一致；合并二进制冲突时可能把产品库回滚 | 明确 OKR-MVP 为唯一 DB blob 决策者；提交/部署前校验 canonical path 和 hash；禁止通用选边 |
| X4 | `jarvis-deploy` 在检查 executing Task 前已经运行 Vite build 并覆盖 `web/dist`，见 [jarvis-deploy](/data00/home/chujiejie.1/workspace-local/emily-development/scripts/jarvis-deploy:90) 和 [同文件](/data00/home/chujiejie.1/workspace-local/emily-development/scripts/jarvis-deploy:170) | 命令拒绝重启时，实际已经形成新前端+旧后端半部署 | 所有运行态写入前完成防护；构建到 staging；验收后原子切换 |
| X5 | `emily-dev --deploy` 先停止、删除旧容器，之后新容器内才做 executing Task 防护，见 [emily-dev](/data00/home/chujiejie.1/workspace-local/emily-development/scripts/emily-dev:130) | 推荐重建流程可直接中断正在执行的 Task | 在旧容器销毁前 fail-closed 查询 Task；显式透传强制中断开关 |
| X6 | 部署过程中 Git 产品 DB 切换、pull 或 checkout 可能发生在旧服务仍持有 SQLite 时 | 写入可落到旧 inode 或被版本切换覆盖；Git blob 与运行文件分叉 | 数据文件不随普通代码切换；部署前停写/快照/校验 inode；产品数据提交采用单独流程 |
| X7 | 两实例均无完整的实例级部署互斥与 last-known-good rollback | 并发部署、启动失败或 readiness 失败会留下不明确版本 | 增加实例级 `flock`；保留上一版二进制和前端目录；失败自动回切 |
| X8 | 主实例实际启动链约 21.8 秒：旧进程退出、首次身份目录授权失败、systemd 5 秒重试后成功；部署健康窗口约 15 秒 | 正常可恢复启动被报告为部署失败，易诱发重复操作 | 修正首次启动授权瞬态；健康窗口覆盖真实 restart policy 并输出阶段耗时 |

### P2

| 编号 | 问题 | 最小整改方向 |
|---|---|---|
| X9 | ingress 把上游硬编码为 `127.0.0.1:18812`，绕过 `server.addr` 真源，见 [supervisord.conf](/data00/home/chujiejie.1/workspace-local/emily-development/deploy/emily-dev/supervisord.conf:20) | 从解析后的实例配置生成上游，或服务直接监听 Unix socket |
| X10 | 部署只验直连 `API_BASE`，未验 Unix socket、主站 `/dev/`、静态 base、cookie path 和一项真实 OKR API | 两边部署末尾都增加通过最终公开入口的只读 smoke |
| X11 | 两分支已有各自独占提交和真实冲突，“代码完全相同、只差配置”的文档约束已失真 | 先做一次有所有权表的代码汇合；之后 CI 检查除允许路径外两树相同 |
| X12 | Supervisor 和 systemd 日志缺完整轮转；主 `api-requests.jsonl` 当前约 175 MiB，且持久化完整请求正文 | 加大小/时间轮转、正文上限与字段级脱敏；默认仅保留必要审计摘要 |
| X13 | CC Connect 当前存在 Jarvis 专用 daemon 和旧 `cc-connect.service` 两个 daemon，旧实例占用 9810/9820 | validate 必须识别 unit、PID、端口、binary、config 的同一归属；清理旧服务需另行批准 |
| X14 | 主 CC unit 的 `After=` 依赖固定旧 Jarvis unit，已与当前实例化 unit 漂移 | 从当前 instance descriptor 生成依赖关系，并加 unit 渲染回归 |

## 5. 开发实例 OKR 功能走查

### 5.1 已确认的功能问题

| 优先级 | 问题 | 证据/触发 | 方案与应补测试 |
|---|---|---|---|
| P1 | 删除仍被 `RegionalAlignment.PlanID` 引用的 Plan，会使对应季度区域页面持续不可读，且无恢复 API | 删除链路见 [plans.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/plans.go)；区域读取见 [regional_alignment.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/regional_alignment.go) | 删除前返回引用冲突，或先显式解绑/选择替代 Plan；补“被区域引用的 Plan 不可直接删除”测试 |
| P1 | Plan Objective 创建为多步写入；子节点失败会留下半成品，重试仅按 Objective ID+PlanID 返回成功，不补齐缺失结构 | [plan_objectives.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/plan_objectives.go) | 遵循项目无事务默认：先写可恢复 intent，子项使用确定性 ID 幂等 upsert，重试补齐，最后标 ready；补逐步故障注入 |
| P1 | Core/Biz KR 创建依次写 KR、owners、tags，失败可能残留部分数据并在重试时重复创建 | [service.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/service.go:1335) | 优先使用确定性 request key、状态标记、幂等补齐与补偿删除；若业务必须强一致，再由用户批准小范围事务设计 |
| P1 | autosave debounce 尚未执行时离开 SPA，会在 cleanup 中清 timer 丢失最后一次编辑 | [store.tsx](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/okr/emily/store.tsx:410)、[planStore.tsx](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/okr/emily/planStore.tsx:186) | 路由切换前 flush，浏览器退出用 `pagehide`/`visibilitychange` 保护，并显示 pending/failed；补编辑后立即导航测试 |
| P1 | 区域/季度快速切换时，旧异步响应可以覆盖新选择，后续保存使用错误 scope | [RegionalAlignmentApp.tsx](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/okr/emily/RegionalAlignmentApp.tsx:434) | AbortController 或 request generation；只接受最新 scope 响应；补乱序 promise 测试 |
| P1 | 通用 OKR quarter 加载同样缺最新请求保护，失败时可能保留旧 board 与新 route | [OKRPluginPage.tsx](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/okr/OKRPluginPage.tsx:190) | 把 route scope、loading、data 作为一个状态快照；错误时清除不匹配数据；补 Q1→Q2 乱序测试 |
| P2 | 单 KR GET 没有完整验证 opened week，可能读取软删除周或生成非法 scope 投影 | [service.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/service.go) | 复用 board 的 scope 验证真源；补软删除周和跨季度读取测试 |
| P2 | 区域 recap 重排没有 expected version/order，两个编辑者可静默覆盖或混合排序 | [regional_alignment.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/okrworkspace/regional_alignment.go) | 请求带当前 order revision；冲突返回可理解错误；补两客户端并发重排测试 |
| P2 | Plan selector 改变当前 Plan 时未同步 route `plan_id`，刷新或分享会回到旧 Plan | [planStore.tsx](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/okr/emily/planStore.tsx) | 选择器和 route 使用一个状态真源；补选择→刷新→分享测试 |
| P2 | OKR logout 依赖 audit middleware 调 `AuthenticateRequest` 的副作用清理 SSO | [okr_module_routes.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/api/okr_module_routes.go) | 身份 service 显式拥有 logout/session clear；audit 只记录，不改变认证状态 |
| P2 | 开发实例插件详情深链不被识别为实例本地对象，`main_workbench_accounts` 用户可能被送回主站 | [instanceNavigation.ts](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/instanceNavigation.ts) | 用路由元数据声明实例归属，不再靠零散字符串判断；补深链刷新/后退测试 |

### 5.2 数据与产品语义待处理

| 类别 | 现场 | 建议 |
|---|---|---|
| owner 迁移保护 | 真实产品 DB 缺少防旧 `open_id` 回退 trigger | 先确认 trigger 是永久约束还是一次迁移；在副本验证后补 migration gate |
| fresh checkout 漂移 | tracked DB 缺 Objective owner 和 product feedback 新表，首次启动会改写 tracked DB | 产品 DB 提交前运行真实迁移两次、clean-check 和 schema snapshot 比对 |
| delivery ledger | 有 1 条 orphan `unknown`，无法 retry/verify | 加人工可审计的 resolve/retry 路径；不要直接删记录 |
| 图片资源 | 315 个资源中约 65 个未被当前 DB 引用，约 49.5 MiB | 先做引用报告、保留期和 dry-run GC；本轮不得直接删除 |
| activity JSONL | 被 Git 忽略，但承载第二持久化面；只恢复 DB 会丢操作记录 | 明确它是审计日志还是产品真源；若需恢复一致性，应与 DB 快照同批归档 |
| weekly comment | target 可不存在或跨季度 | 需要产品确认它是“当前实体引用”还是“历史 snapshot”；两种语义测试不同 |
| 空 metric | Plan 中有 14 条空 metric，其中一条 KR 下两条完全空白 | 由产品确认是合法占位还是污染；确认前不自动清理 |

### 5.3 身份边界待确认，不直接按安全漏洞定级

外层认证明确把 `/api/okr/`、`/api/biz-okr/`、`/api/okr-chat/` 整体视为模块自管的公开入口，见 [http.go](/data00/home/chujiejie.1/workspace-local/emily-development/internal/authn/http.go:108)。但许多写路由没有 `RequireOKRIdentity`，未登录请求会以默认 actor `jarvis` 执行。

这不是依据当前“本地单用户可信 MVP”原则就能直接定为 P0；需要先确定产品边界：

- 若公开域名承诺“只有完成 OKR 登录的人可写”，则这是严重写权限缺口，应统一给 mutation 路由加模块身份门禁。
- 若这些 API 明确只面向可信内部入口，则当前可保留，但必须在架构文档写清信任边界，并增加边界测试，避免以后误暴露。

评论 ownership 同理：当前是否允许管理员/可信内部 actor 代操作，需要产品语义确认后再决定。

## 6. 主实例功能、Task 流水线与 CC Connect

### 6.1 Task、M2/M3/M5 与调度

| 优先级 | 问题 | 影响 | 最小整改方向 |
|---|---|---|---|
| P1 | close 一个 executing Task 只改持久化状态，不停止活跃 agent | Task 已显示 done，agent 仍可能继续外部副作用和写回 | close 先请求 executor cancel，记录 cancel outcome，再以版本条件完成状态转换；超时显式标记 |
| P1 | supplement executing Task 会递增 Task version，活跃 executor 最终 transition 使用旧 version 而失败 | Task 可永久卡在 executing，执行结果丢失 | supplement 写独立 revision/append-only context，不抢执行状态 version；或让 executor显式吸收新版本 |
| P1 | waiting/needs_human claim 后、新 run 建立前失败会卡 executing；stale recovery 可能参考历史 run 误判 failed | 可恢复任务变成假失败或永久卡住 | claim 建立 durable resume intent；启动失败可幂等退回原状态；恢复只看绑定 source run + claim generation |
| P1 | scheduled occurrence 在 claim 和 Task/receipt 落库之间崩溃 | 可能丢执行，或有 Task 无 occurrence 关联 | occurrence key 先持久化为 dispatch intent；Task 创建和 receipt 通过幂等键反复 reconcile |
| P1 | needs_human 状态已落库后，飞书卡发送失败无 durable 补偿 | 用户看不到问题，Task 永久等人 | 建 durable notification outbox，失败重试并允许 UI 回读/补发 |
| P2 | resume 没有重新注入最新 shared memory | 长时间等待后的继续执行使用旧背景 | 恢复时重新组装可变 memory，同时保留冻结 evidence；补等待期间 memory 更新测试 |
| P2 | `model_api` 路径对单条超长 new message 会裁掉全文；当前生产 Codex 路径不受影响 | 备用 M3 backend 切换后可能违反完整原文原则 | 长内容转 evidence reference/section，不裁掉唯一原文；补单条超长输入测试 |
| P2 | observing Task 中“更新原 Todo 并重开”是不可达旧分支，与当前“新证据创建新 Todo”语义冲突 | 注释、测试构造与真实生产链路不一致 | 决定唯一语义；若沿用新 Todo，删除不可达分支并补 clue→M3→materializer E2E |
| P2 | one-shot 参数定义、互斥计数和 dispatch 三处分离，漏了 meeting sweep 与两个 morning brief 参数 | 组合参数会静默执行前一个并忽略后一个 | 用单一声明表驱动选择、名称和执行；补全两两组合测试 |

已确认正确的部分：通用 clue 入口按 `(source, external_id)` 幂等；Todo、Event、水位在同一提交路径；冻结证据沿 `Todo.content → Task.source_payload → M5 evidence` 完整传递；没有为会议/邮件/群聊新增 Go 专用流水线。

### 6.2 主前端

| 优先级 | 问题 | 方案 |
|---|---|---|
| P2 | RuntimeSettings 首次请求失败后只剩永久 Spinner | 渲染明确错误和重试按钮；区分 loading/empty/error |
| P2 | executing Task 详情页状态不会可靠跟随列表完成 | 轮询选中详情或在列表状态变化后刷新详情；用 generation 防旧响应 |
| P2 | 插件授权轮询遇一次瞬时错误后永久停止 | 错误可见但继续带退避轮询；终态才停止 |
| P2 | Chat 搜索、Task/Scheduler polling、WorldMap selection 均有旧响应覆盖新选择的窗口 | 统一 AbortController/request generation helper，逐页补乱序测试 |
| P3 | 移动端无插件入口 | 若移动端需管理插件，补同一菜单模型；否则产品文档明确不支持 |
| P3 | unknown route 保留坏 URL 却静默显示 Chat | 显式 404/纠正 route，避免用户误以为链接有效 |

### 6.3 CC Connect 与安装诊断

| 优先级 | 问题 | 方案 |
|---|---|---|
| P1 | exec session 修复只判断 transcript 文件存在，异步 resume 真失败仍持续复用 stale session | 将 resume 结果纳入 session 健康；失败后清理映射并新建 session，保留审计 |
| P1 | relay 路径绕过 `SessionIDValidator` | 所有 session 入口统一经过 validator，补 relay 恶意/过期 ID 测试 |
| P1 | install/validate 可把磁盘新 binary、旧运行 PID、另一个 daemon 端口和不同 config 拼成假成功 | 验证 unit→PID→exe hash→config→listen socket→live version 的单链归属 |
| P1 | patch 安装没有 build→受控 restart→live version/config/protocol 验证闭环 | 安装产物只在验收后切换；重启目标必须唯一；失败回滚旧 binary |
| P2 | `jarvis-install doctor` 硬编码 `var/jarvis.db`，会漏掉真实 `var/jarvis-lixiaolin.db` | 从实例配置读取 DB 路径，禁止 doctor 再维护第二真源 |
| P3 | 3500+ 行 CC 定制是单个 patch，任一 upstream 冲突阻塞全部能力 | 保持 pinned upstream，但按 auth/agent/feishu/web 拆有序 patch；逐片 check 和测试 |

## 7. 复杂度审查

### 7.1 需要整理，但不建议大重构

1. `okrworkspace.Service` 把 core、Biz、weekly、owner、tag、comment cleanup 集中在少数大方法。先在公开入口内部抽 `definition`、`biz metadata`、`removed child cleanup` 三类窄 helper，保持 package 和 API 不变。
2. [Chat.tsx](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/Chat.tsx:81) 同时负责 session、SSE、draft、附件、恢复和 compact/full 两套 UI。按 `useChatSessions`、`useChatStream`、`useDraftPersistence` 渐进抽 hook，每一步恢复 effect 依赖检查并补竞态测试。
3. [OKR api.ts](/data00/home/chujiejie.1/workspace-local/emily-development/web/src/okr/emily/api.ts) 有多套 KR/Plan KR wire-domain 映射，Meego preview 已出现完全重复转换。先抽纯映射 helper，再按 core/weekly/meego/feedback 分文件，保留 barrel export。
4. CC Connect 定制 patch 是升级冲突单元过大，不需要改 vendor 策略，只需按能力拆 patch。

### 7.2 不应做的“整理”

- 不因文件大就拆 `Service` 成多个互相调用的 service。
- 不增加新的实例状态枚举或为 OKR/会议/邮件建立专用 Go 流水线。
- 不用下游前端补丁掩盖数据库、身份或导航真源错误。
- 不为了所有多步写入统一引入事务；先用顺序、幂等、补偿和恢复完成最小闭包。确有不可分割强一致要求时再单独评审事务。

## 8. 测试现状与缺口

### 8.1 本轮已通过

- 开发实例：`go test ./...`。
- 开发实例：`go test -count=1 -race ./internal/okrworkspace ./internal/authn ./internal/api`。
- 开发实例：前端 203/203 单测、typecheck、production build。
- 主实例：`go test ./cmd/... ./internal/...`。
- 主实例：`go test -count=1 -race ./internal/store ./internal/pipeline ./internal/extract ./internal/execute ./internal/scheduledtask ./internal/api`。
- 主实例：前端 201/201 单测、typecheck、production build。
- 共享 OKR DB：`integrity_check=ok`、`quick_check=ok`，已检查的引用和 JSON 一致性正常。

这些结果证明常规单元层没有现成红灯，但不能覆盖本报告中的跨进程、部署崩溃窗口、浏览器乱序和真实迁移问题。

### 8.2 明确缺口

| 优先级 | 缺口 | 建议门禁 |
|---|---|---|
| P1 | 两个 worktree 都没有 tracked CI/merge gate，部署脚本也不跑单测 | 建立 deterministic 必过 job：Go、focused race、TS unit、typecheck、fixture browser、Python；平台/live 测试独立分组 |
| P1 | 15–17 个 browser 测试不在 `npm test`，Playwright 未声明依赖 | 锁定 Playwright/Chromium；单一命令启动 fixture、运行、回收；纳入 CI |
| P1 | tracked SQLite 没有历史快照真实升级、重复迁移和 clean-check | 将 DB 复制到临时目录，连续迁移两次，验 integrity/foreign keys/关键计数/schema/hash |
| P1 | 部署测试多为脚本文本断言 | fake service manager + HTTP server 执行失败路径；另保留 Linux/container 真实 smoke |
| P1 | integration 的 prerequisite、skip/fail 和 executed 报告不一致 | 拆 deterministic、Docker、live-model、real acceptance 四组，摘要必须列 executed/skipped/unavailable |
| P1 | 主程序 wiring 与 semantic/Qdrant 行为覆盖很低 | 临时 SQLite/Qdrant 启动完整进程，验证路由、模块开关、ready、shutdown、collection schema |
| P1 | product feedback real browser 测试会写正式数据且不清理 | 默认只跑临时 DB；真实验收必须带唯一 run ID、目标实例保护和回读清理 |
| P2 | Python 缺依赖声明、Rust 无统一入口、macOS 测试在 Linux 不自包含 | 建 hermetic Python 环境；Rust unit 独立 job；macOS runner 跑 packaging/app smoke |
| P2 | 区域前端部分测试只匹配源码字符串 | 改成 React/browser 用户行为测试 |
| P2 | 缺跨实例 schema、worker、最终 `/dev/` 入口 gate | CI 比较允许路径外两树；启动双版本 fixture；真实 proxy smoke |
| P3 | race suite 与日历依赖未固定 | 建 focused race job；fixture 注入时钟，真实验收显式传 quarter/week |

## 9. 分阶段整改方案

### Phase 0：止血与保存现场

目标：恢复开发 OKR 可用，关闭凭证和数据发布的最高风险。

1. 保存当前开发进程 goroutine、锁、journal、请求日志片段和 DB 副本，定位卡死请求；随后按标准流程受控恢复。
2. 将主 token 迁出共享目录并轮换；把共享挂载缩到产品数据白名单。
3. 为 OKR DB 增加 readiness 查询；两边部署都从最终公开入口检查 OKR scope。
4. 暂停两个不同版本同时执行 migration 和 notification delivery：指定唯一 owner。
5. 部署前先检查 executing Task 和工作区/DB 状态；任何构建或 Git 切换不得先于检查。

验收：token 目录在开发容器不可见；主、开发 OKR scope 连续成功；ready 能识别 OKR 锁死；只有一个迁移者和一个 delivery worker。

### Phase 1：收拢双实例与状态恢复

目标：建立单一真源和可恢复的状态机。

1. 明确 canonical DB、schema version、compatibility range、唯一迁移流程。
2. 汇合两分支通用代码，实例差异只保留在 runtime/config/state；CI 检查树漂移。
3. 为部署增加互斥、staging、原子切换、last-known-good rollback 和全链路 smoke。
4. 修 close/supplement/resume/scheduled occurrence/needs_human notification 的 durable intent、幂等 reconcile 和补偿。
5. CC validate 建立 unit、PID、binary、config、port 的单归属链。

验收：故障注入后 Task 不会假 done、卡 executing 或丢 occurrence；部署失败自动保留旧版本；DB blob 不会被普通代码合并回滚。

### Phase 2：OKR 业务正确性与前端竞态

目标：清除会丢编辑、串 scope、产生半成品或不可恢复数据的业务问题。

1. Plan 引用删除保护，Plan Objective/KR 多步写的幂等补齐和补偿。
2. autosave flush、request generation、route-plan 单真源、recap optimistic concurrency。
3. 补 opened week、owner trigger、delivery orphan、fresh DB migration 处理。
4. 对 weekly comment、空 metric、匿名 mutation 边界取得产品决定后固化规则与测试。

验收：双客户端、快速切换、立即导航、逐步失败与重试测试全通过；正式数据只在明确迁移流程中变化。

### Phase 3：测试门禁与渐进整理

目标：让风险在合并前暴露，并降低下一次修改成本。

1. 建 CI 分层门禁和可重复 browser/integration/platform runner。
2. 增加真实 DB migration、部署行为、双实例、Qdrant 和 desktop smoke。
3. 渐进抽 OKR service helper、Chat hooks、API pure mappers；拆 CC patch。
4. 加日志轮转、正文上限和运行状态观测。

验收：每个 job 清楚报告真正执行与跳过项；关键失败窗口有回归测试；代码整理不改变 API 和产品语义。

## 10. 建议的决策顺序

需要用户先决定的只有三项，其他可以按上述最小方案推进：

1. **双实例长期模型**：推荐“同代码版本 + 不同配置 + 共享明确的产品数据 + 单迁移者/单副作用 worker”，不再允许两个长期分叉版本共同解释同一 DB。
2. **OKR 匿名写边界**：公开域名是否要求完成模块登录才能 mutation。
3. **评论/周报引用语义**：引用必须指向当前存在实体，还是允许作为不可变历史 snapshot。

若同意推荐模型，实施顺序应是 Phase 0 → Phase 1，先恢复与收边界，再处理 OKR 功能细节。不要先做 `Service`/`Chat` 大拆分。

## 11. 工作区与审查边界记录

- 开发 worktree 自身 `data/okr/okr.db` 与其 HEAD blob 一致。
- 主 worktree 的 `data/okr/okr.db` 当前为 modified；它是运行中开发容器实际挂载的产品库，修改时间与启动迁移一致。本轮未清理、回滚或提交。
- 本轮只新增本审查文档；未修改运行代码、配置或产品数据，未重启任一实例。
- 当前 `/dev/` OKR 部分故障仍在，等待用户批准 Phase 0 后处理。
