---
name: feishu-meeting-clue
description: 采集飞书已结束会议，把每场会议作为一条线索投递给 M2，交由 M3 做 Task 准入。当任务是「会议线索采集」「扫描已结束的飞书会议」「把今天开过的会交给流水线」时使用。只报会议本身，不拉妙记、不判断有没有录制、不生成总结——这些后续处理由 M5 完成。
---

# 飞书会议线索采集

你在这条任务里扮演的是**采集者**，不是分析者。你要做的只有一件事：**找出最近结束的会议，把「这场会开完了」连同会议本身的客观信息投递成线索**。

## 边界（重要）

会议开完了，本身就是一条值得 M3 看一眼的线索——不管它有没有录制、有没有妙记、有没有结论。所以：

- **不要**去拉妙记、逐字稿、AI 总结、会议 Todo。
- **不要**判断这场会「有没有开录制」「妙记生成好了没」「有没有权限」。
- **不要**因为一场会看起来「没什么内容」就跳过它。
- **不要**自己写会议总结或待办。

M3 拿到线索后只判断是否值得启动 M5，不拉妙记、不申请权限、不等待产物。上面的会后产物调查和处理全部交给 M5；采集者多做一步，就是把下游职责写死在采集层。

## 步骤

### 1. 确认自己是谁

```bash
jarvis-tools get-principal
```

拿到 `open_id`，下一步按参会人搜索。

### 2. 搜出时间窗内我参加过的会议

默认回看**最近 24 小时**，除非任务指令里另有说明。

```bash
lark-cli vc +search \
  --participant-ids "<principal open_id>" \
  --start "<窗口起点，如 2026-07-27T00:00+08:00>" \
  --end "<窗口终点>" \
  --page-size 30 \
  --as user
```

结果分页时用 `--page-token` 翻完，不要只取第一页。

`vc +search` 只用于发现 `data.items[].id`，它不返回可用于判断结束状态的完整时间和参会人。对每个 `meeting_id` 读取会议事实：

```bash
lark-cli vc meeting get \
  --meeting-id "<meeting_id>" \
  --with-participants \
  --user-id-type open_id \
  --as user
```

只保留 `.data.meeting.status=3` 且 `end_time` 已过去的会议；`1` 是呼叫中，`2` 是进行中。`start_time` / `end_time` 是 Unix 秒字符串，投递前必须按本机时区转换成 RFC3339，禁止直接当 RFC3339 复制或手算。正在进行中的会先不投递，下一轮再说。

`host_user.id` 与 `participants[].id` 是参会人的 open_id。需要姓名时一次批量解析：

```bash
lark-cli contact +search-user \
  --user-ids "<逗号分隔的 open_id，最多 100 个>" \
  --as user
```

超过 100 个 open_id 时按 100 个一批查询。解析不到姓名就保留 open_id 并写“姓名未返回”，不要猜。

### 3. 每场会议投递一条线索

一场会一条，`--external-id` 用 `meeting_id`。服务端按 `(source, external_id)` 幂等，所以**重复投递是安全的**，不需要自己记哪些投过了。

```bash
jarvis-tools append-clue \
  --source feishu_meeting \
  --external-id "<meeting_id>" \
  --title "会议结束：<会议主题>" \
  --occurred-at "<会议结束时间，RFC3339>" \
  --content - <<'TXT'
会议主题：<topic>
会议 ID：<meeting_id>
会议号：<meeting_no>
开始时间：<start_time>
结束时间：<end_time>
主持人：<host 姓名与 open_id>
参会人：<逐个列出姓名与 open_id；能用 lark-cli contact 解析出姓名的就解析>
会议链接：<app_link>
TXT
```

`--content` 只写 `vc meeting get --with-participants` 和联系人解析直接返回的客观字段。字段缺失就如实留空或写「未返回」，不要编，也不要为了补齐它去调 `vc +detail`、妙记或其他会后产物接口。

### 4. 汇报

任务结束时说清楚：时间窗、搜到几场会、投递了几条线索、其中几条是新的（`inserted=true`）、几条是重复。逐场列出会议主题和 meeting_id。

某场会投递失败就直接报错说明是哪一场、错误原文是什么，不要吞掉继续跑。
