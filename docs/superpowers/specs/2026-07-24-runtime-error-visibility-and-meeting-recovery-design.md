# 运行错误可见性与会议权限恢复设计

> Status: implemented-history / partially obsolete
> Authority: non-normative implementation record
> Last verified: 2026-08-02 @ `89fa24b`
> Warning: 运行错误可见性已经实施；`meeting_ingest`、M4 和会议专用权限链路已退役。当前会议通过 Skill + `/api/clues` 接入通用 M2→M3→M5 流水线。

日期：2026-07-24

## 1. 目标

本次改动解决两个问题：

1. 会议妙记无权限时，系统生成一条可审批的权限申请 Todo，不因缺少妙记正文而停止闭环。
2. M3、M4、M5 的实时执行错误在后台可见，并在侧边栏提示；系统不依赖 M3 为自己的失败生成 Todo。

本设计保留当前 M2 → M3 → M4 → M5 分工，不引入新的告警平台、事件总线或数据库表。

## 2. 设计原则

### 2.1 当前状态可变，原始证据不可变

`meeting_ingest` 保存会议采集的当前状态，可以覆盖为最新值：

- `status`
- `attempt_count`
- `last_attempt_at`
- `next_retry_at`
- `last_error`

`message` 保存进入 M3 的原始证据。已经被 Todo 引用的消息不得覆盖，否则 `source_quote` 可能与来源消息失配。

重复的同类失败只更新 `meeting_ingest`，不重复生成消息。状态发生有意义的变化时，系统生成新的不可变证据。例如：

```text
permission_denied → permission_requested → imported
```

### 2.2 业务动作进入 Todo，运行故障进入运行状态页

Todo 表达需要推进的业务动作，例如：

- 申请某份妙记的查看权限
- 联系妙记所有者授权
- 根据会议内容完成明确交办

运行状态页表达系统故障，例如：

- M3 模型返回非法 JSON
- M3 调用 mem0 超时
- M4 决策执行失败
- M5 Codex 进程失败

M3 不能负责报告自己的执行失败。否则 M3 失败时，错误 Todo 也无法产生。

## 3. 会议权限恢复

### 3.1 首次无权限

M2 首次读取妙记得到 `permission_denied` 时：

1. 更新 `meeting_ingest` 当前状态。
2. 写入一条不可变的 `meeting_capture_result` 消息。
3. 通知 M3 处理该消息。

消息保留以下事实：

- meeting_id
- meeting topic
- minute_token
- 会议链接
- 错误原文
- 建议申请的权限类型

### 3.2 M3 生成权限 Todo

M3 必须区分两类 meeting 消息：

- `meeting_minutes`：会议内容证据，只提取明确落到 principal 身上的业务行动。
- `meeting_capture_result`：采集阻塞证据。`permission_denied` 本身就是可处理的行动线索，不要求会议正文存在。

抽取 Prompt 显式携带已有的 `message_type`。对于 `meeting_capture_result + permission_denied`，M3 生成一条 `manual_followup` Todo：

```text
标题：申请“07-24 | 公会基建Agent 日会”妙记查看权限
目标：minute_token=obmyzu55trffd4424v42p97e 的 view 权限
动作：调用 lark-cli minutes +apply-permission
```

Todo 继续使用现有语义指纹去重。相同 meeting_id、minute_token 和权限类型的重复失败不得生成第二条 Todo。

### 3.3 审批与继续采集

权限申请属于外部写操作：

1. M4 在上下文充分时可以自动创建 Task；`auto` 只代表 Todo 无需补充，不代表批准外部写入。
2. M5 形成申请方案并进入 `awaiting_approval`。
3. 用户批准后，M5 发起权限申请。
4. 会议采集按现有调度继续读取。
5. 权限生效后，M2 导入妙记正文；M3 再从正文提取会议业务 Todo。

系统不因创建权限 Todo 停止采集重试，也不自动批准权限申请。

## 4. 运行错误可见性

### 4.1 复用现有页面

不新增一级 Tab。现有“调试”菜单改名为“运行状态”，保留以下子页：

- 健康与积压
- 模块运行
- 报错时间线
- 采集流水
- 抽取水位
- 最近 Todo
- 最近 Task
- 运行日志

“报错时间线”作为默认子页。

### 4.2 补齐日志解析

当前解析器只识别：

```text
<module>-cron ... status=error
```

新解析器同时识别：

```text
pipeline ... stage=m3 ... status=error
pipeline ... stage=m4 ... status=error
pipeline ... stage=m5 ... status=error
```

阶段映射保持固定：

| 日志字段 | 页面模块 |
|---|---|
| `stage=m3` | M3 抽取 |
| `stage=m4` | M4 决策 |
| `stage=m5` | M5 执行 |

`status=queued`、`status=ok` 和 `status=stale` 不算错误。`status=error`、panic 和 fatal 进入报错时间线。

### 4.3 错误身份与恢复判断

恢复判断必须使用同一执行范围，不能用另一个成功请求掩盖当前错误：

| 模块 | 错误身份 |
|---|---|
| M3 | `stage + chat_id` |
| M4 | `stage + todo_id`；无 todo_id 时使用 `stage + trigger` |
| M5 | `stage + task_id`；无 task_id 时使用 `stage + trigger` |
| cron | `module + job` |

同一身份出现更晚的 `status=ok` 才标记为“已恢复”。其他会话、Todo 或 Task 的成功不能恢复该错误。

相同身份和错误摘要连续出现时，页面合并展示并增加次数；原始日志仍可展开查看。

### 4.4 用户提示

应用每 60 秒读取一次运行错误摘要：

- 侧边栏“运行状态”显示未恢复错误数量。
- 数量大于零时显示红色 Badge。
- 打开页面后展示错误时间、阶段、作用范围、logid、错误摘要和恢复状态。
- 已恢复错误保留在近 24 小时时间线中，但不占用红色 Badge。

MVP 使用现有日志文件和 `/api/debug/*` 接口，不新增错误表。日志轮转前的历史错误不做迁移。

## 5. API 与组件边界

### 5.1 后端

扩展现有 `insight.DebugService`：

- 统一解析 cron 与 pipeline 日志。
- 返回标准化的模块、阶段、范围 ID、logid、错误、次数和恢复状态。
- 保留原始日志行。

复用并扩展：

- `GET /api/debug/modules`
- `GET /api/debug/failures`

不新增写接口。

### 5.2 前端

扩展现有 `Debug.tsx`：

- 展示 M3、M4、M5 实时错误。
- 默认打开报错时间线。
- 展示同范围恢复状态和重复次数。

扩展现有 `App.tsx`：

- 将“调试”改为“运行状态”。
- 定时拉取未恢复错误数量。
- 在菜单项显示 Badge。

## 6. 错误处理

- 日志行格式无法识别时，跳过该行，不猜测字段。
- 已识别 `status=error` 但缺少范围 ID 时，按阶段和 trigger 聚合，并保守标记为未恢复。
- `/api/debug/failures` 读取失败时，页面显示接口错误；不把“读取失败”展示为“系统正常”。
- 权限 Todo 创建失败时，保留原始 `meeting_capture_result` 和 M3 错误，等待 reconcile 重试。

## 7. 测试

### 7.1 后端

至少覆盖：

1. 解析 M3、M4、M5 的 `pipeline status=error`。
2. 同 chat_id 的 M3 成功将错误标记为已恢复。
3. 不同 chat_id 的成功不能恢复该错误。
4. `queued`、`ok`、`stale` 不进入错误列表。
5. cron 错误解析保持兼容。
6. 首次 `permission_denied` 生成一条采集证据。
7. 重复无权限只增加 `attempt_count`，不新增证据和 Todo。
8. M3 从权限证据生成一条 `manual_followup` Todo。
9. 权限生效后导入正文，并允许 M3 生成会议业务 Todo。

### 7.2 前端

至少覆盖：

1. 未恢复错误数量显示在“运行状态”菜单。
2. 已恢复错误不占用红色 Badge。
3. 报错时间线展示 M3 的 chat_id、logid 和错误摘要。
4. 错误 API 失败时显示错误态。

## 8. 验收标准

完成后应满足：

1. 制造一条 M3 非法 JSON 错误后，60 秒内可在侧边栏看到红色数量。
2. 打开“运行状态”可定位到具体 chat_id 和 logid。
3. 另一个 chat 的 M3 成功不会把该错误误标为恢复。
4. 同一 chat 后续成功后，错误变为“已恢复”，红色数量减少。
5. 妙记首次无权限时生成且只生成一条权限 Todo。
6. 重试十次相同权限错误不会产生十条 Todo。
7. 用户批准并获得权限后，会议正文成功导入，业务 Todo 正常生成。

## 9. 非目标

本次不做：

- 自动批准或自动申请外部权限。
- 将所有运行错误转成 Todo。
- 新建通用告警平台或错误数据库。
- 迁移日志窗口之外的历史错误。
- 修改现有会议正文抽取规则。
