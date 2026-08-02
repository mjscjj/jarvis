# M2 消息与线索采集模块

> Status: current
> Authority: normative module guide
> Last verified: 2026-08-02 @ `89fa24b`
> Code source: `internal/capture/`, `internal/domain/capture.go`

M2 把外部事实可靠写入 SQLite，并按 chat 唤醒 M3。它不分类、不下结论、不决定重试策略。

## 1. 当前入口

```text
飞书 im.message.receive_v1 长连接
    -> message（message_id 幂等）
    -> related chat 立即唤醒 M3

飞书会话发现 + principal activity + related chat 轮询补偿
    -> feishu_group / checkpoint / message / resource / scan_record

外部 Skill / 定时任务
    -> POST /api/clues
    -> clue:<source> 伪会话 + message

新增 message -> pipeline.Coordinator -> M3
message -> factengine（旁路）-> fact
```

事件流由 `jarvis-server` 内的单个 `lark-cli event consume` 子进程持有；启动必须等到官方 ready marker，退出先关闭 stdin、必要时只发 SIGTERM。事件不推进轮询 checkpoint，扫描仍会看到同一 `message_id`、完成幂等去重并推进恢复水位。

## 2. 机械职责

- 全量发现会话，但首次只从当前时刻建立 checkpoint，不回溯历史。
- 实时保存 Bot 收到的原始消息事件；已关联会话提交后立即唤醒 M3。
- 扫描 `related_group=1` 会话作为补偿；tier 只用于展示。
- 按活跃度自动纳入内部真人 P2P Top-N，排除服务号 P2P。
- 搜索 principal activity，发现本人发言的群聊/话题并维护独立 checkpoint。
- 原样保存消息和外部 clue；资源只登记引用元数据，不通用下载正文。
- 成功新增后推进 checkpoint 并唤醒 M3。
- 扫描错误写 ScanRecord/日志；外部 Skill 需要把失败作为业务证据时，主动通过 clue 投递原始错误。

## 3. 表与幂等

| 表 | 作用 |
|---|---|
| `feishu_group` | 会话目录、related 标记、项目归属和展示字段 |
| `chat_checkpoint` | 每会话增量扫描水位 |
| `principal_activity_checkpoint` | principal activity 搜索水位 |
| `message` | 原始消息/线索真源 |
| `resource` | 附件、文档、妙记等引用元数据 |
| `scan_record` | 采集尝试的追加审计 |

事件和轮询消息共用飞书 message ID 幂等；事件保存完整 lark-cli 事件 JSON 到 `content_raw`，但不移动轮询 checkpoint。clue 使用 `(source, external_id)` 幂等，并生成 `clue:<source>:<external_id>` 形式的 message ID。

## 4. 通用线索入口

```bash
scripts/jarvis-tools append-clue
```

它调用 `POST /api/clues`。新来源等于一个新 `source` + 一份 Skill/定时任务，不等于一个新 Go 模块。会议 Skill 只投递会议事实，录制/妙记/权限/等待语义由 M3/M5 判断。

## 5. 调度与运维

| Job | 基线配置 | 当前行为 |
|---|---|---|
| event consume | `capture.event_enabled/event_profile` | 长连接实时接收 `im.message.receive_v1` |
| discover | `capture.discover_schedule` | 会话元数据、内部 P2P Top-N |
| scan | `capture.scan_schedule` | principal activity + related 会话增量轮询补偿 |

```bash
./bin/jarvis-server -config conf/config.yaml -discover-once
./bin/jarvis-server -config conf/config.yaml -scan-chat <chat_id>
./bin/jarvis-server -config conf/config.yaml -set-related-groups <ids>
./bin/jarvis-server -config conf/config.yaml -open-p2p
```

## 6. 当前限制

- 编辑已有消息会更新内容，但不会按“新增消息”重新唤醒 M3。
- `im.message.receive_v1` 不携带 sender display name；实时行以 open_id 为硬身份，已维护 Person 由 M3 按 open_id 补全，轮询仍保留飞书渲染名。
- Resource 的通用下载、解析和内容哈希复用未闭环。
- 采集轮询错误和投递为业务 evidence 的 clue 是两类不同留痕。
