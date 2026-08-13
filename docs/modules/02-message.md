# M2 消息与线索采集模块

> Status: current
> Authority: normative module guide
> Last verified: 2026-08-02 @ `89fa24b`
> Code source: `internal/capture/`, `internal/domain/capture.go`

M2 把外部事实可靠写入 SQLite，并按 chat 唤醒 M3。它不分类、不下结论、不决定重试策略。

## 1. 当前入口

```text
飞书会话发现 + principal activity + related chat 增量轮询
    -> feishu_group / checkpoint / message / resource / scan_record

外部 Skill / 定时任务
    -> POST /api/clues
    -> clue:<source> 伪会话 + message

新增 message -> pipeline.Coordinator -> M3
message -> factengine（旁路）-> fact
```

Jarvis Bot 的事件连接由 CC Connect 独占；`jarvis-server` 不启动 `lark-cli event consume`。M2 依赖会话发现与增量轮询，按 checkpoint 推进恢复水位；未来若需要实时事件，只能由 CC Connect 通过明确的本机 fan-out 接口转发。

## 2. 机械职责

- 全量发现会话，但首次只从当前时刻建立 checkpoint，不回溯历史。
- 扫描 `related_group=1` 会话并在新增消息后唤醒 M3；tier 只用于展示。
- 每次成功扫描后，连续 5 天没有新消息的会话退出监听；扫描失败时保留监听状态。
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

轮询消息按飞书 message ID 幂等。clue 使用 `(source, external_id)` 幂等，并生成 `clue:<source>:<external_id>` 形式的 message ID。

## 4. 通用线索入口

```bash
scripts/jarvis-tools append-clue
```

它调用 `POST /api/clues`。新来源等于一个新 `source` + 一份 Skill/定时任务，不等于一个新 Go 模块。会议巡扫同时投递已结束会议的 `feishu_meeting` 事实和未来会议日程的 `calendar` 事实；录制/妙记/权限/会议相关性/准备时机/等待语义都由 M3/M5 判断。

## 5. 调度与运维

| Job | 基线配置 | 当前行为 |
|---|---|---|
| discover | `capture.discover_schedule` | 会话元数据、内部 P2P Top-N |
| scan | `capture.scan_schedule` | principal activity + related 会话增量轮询 |
| meeting sweep | `meeting_sweep.schedule` | 已结束会议 + 未来 24 小时会议日程，通过 clue 唤醒 M3 |

```bash
./bin/jarvis-server -config conf/config.yaml -discover-once
./bin/jarvis-server -config conf/config.yaml -scan-chat <chat_id>
./bin/jarvis-server -config conf/config.yaml -set-related-groups <ids>
./bin/jarvis-server -config conf/config.yaml -open-p2p
```

## 6. 当前限制

- 编辑已有消息会更新内容，但不会按“新增消息”重新唤醒 M3。
- Resource 的通用下载、解析和内容哈希复用未闭环。
- 采集轮询错误和投递为业务 evidence 的 clue 是两类不同留痕。
