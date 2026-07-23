# 工具路径

这里只固定可靠的能力入口。每次执行仍以本机当前 CLI 的内置 Skill 和 `--help` 为准，不把返回 JSON 结构写死。

## 1. 认证与能力说明

在调用飞书命令前读取与当前 CLI 同版本的说明：

```bash
lark-cli skills read lark-shared
lark-cli skills read lark-im
```

读取飞书文档前再执行：

```bash
lark-cli skills read lark-doc references/lark-doc-fetch.md
```

读群消息使用 `--as user`，因为跨群搜索和用户可见历史依赖用户身份。命令退出码为 0 但结果含 `ok: false` 也视为失败。

## 2. 定位群

已知 `chat_id` 时直接使用。只有群名时搜索：

```bash
lark-cli im +chat-search \
  --as user \
  --query "<群名>" \
  --chat-modes group,topic \
  --format json
```

对结果做精确名称和成员/上下文核验。不能唯一确定时停止并请用户选择。

## 3. 计算自然日窗口

使用带时区偏移的 ISO 8601 半开区间：

```text
start = YYYY-MM-DDT00:00:00+08:00
end   = next-day T00:00:00+08:00
```

若接口把 `--end` 解释为闭区间，使用下一日零点前的最小可表达时间，或拉取后按消息时间再次过滤。最终只保留 `start <= create_time < end` 的消息。

## 4. 拉取完整消息

优先用空查询覆盖整个群和时间窗口，并自动翻页：

```bash
lark-cli im +messages-search \
  --as user \
  --query "" \
  --chat-id "<chat_id>" \
  --start "<start>" \
  --end "<end>" \
  --page-size 50 \
  --page-all \
  --page-limit 40 \
  --no-reactions \
  --format json
```

用会话消息列表交叉核对时间顺序和数量；按 `page_token` 继续，直到没有下一页：

```bash
lark-cli im +chat-messages-list \
  --as user \
  --chat-id "<chat_id>" \
  --start "<start>" \
  --end "<end>" \
  --order asc \
  --page-size 50 \
  --no-reactions \
  --format json
```

需要展开单个话题时：

```bash
lark-cli im +threads-messages-list \
  --as user \
  --thread "<om_xxx_or_omt_xxx>" \
  --order asc \
  --page-size 500 \
  --no-reactions \
  --format json
```

也可批量补取最多 50 条消息并展开其线程回复：

```bash
lark-cli im +messages-mget \
  --as user \
  --message-ids "<om_a,om_b>" \
  --no-reactions \
  --format json
```

消息数量超过自动分页上限时继续分段拉取，或在输出中明确覆盖缺口，不能静默截断。

## 5. 读取关联材料

### 飞书文档

结构未知时先看目录，再按章节或关键词局部读取：

```bash
lark-cli docs +fetch \
  --as user \
  --doc "<文档 URL 或 token>" \
  --scope outline \
  --max-depth 3 \
  --doc-format markdown \
  --format json
```

```bash
lark-cli docs +fetch \
  --as user \
  --doc "<文档 URL 或 token>" \
  --scope keyword \
  --keyword "<与群内事项相关的关键词>" \
  --doc-format markdown \
  --format json
```

只有确实需要全文时才省略 `--scope`。

### Codebase commit 和 MR

URL 已带仓库和 MR 时可直接读取：

```bash
bytedcli --json codebase mr get "<MR URL>"
```

已知仓库和 commit SHA 时：

```bash
bytedcli --json codebase commit get \
  -R "<namespace/repo>" \
  --revision "<sha>"
```

本地已有正确仓库时，可用 `git show --stat --oneline <sha>` 和 `git show <sha> -- <相关路径>` 精读实际改动。先核对 remote 和 revision，不能用本地当前分支替代消息中引用的版本。

### 其他材料

- 飞书消息附件：按 `lark-im` 内置 Skill 的资源下载能力按需下载。
- 妙记、表格、多维表格、画板：从文档或消息提取 token 后，切到对应 `lark-*` 能力。
- 普通网页：优先使用已认证、能保留原 URL 的读取方式；权限不足时记录未读取原因。

## 6. 证据账本建议

可在临时工作目录保存机器可读中间结果，但不要把临时文件当最终产物。每条证据至少保留：

```text
topic
message_id / material_url
sender
create_time
claim_supported
evidence_kind = primary_message | linked_material | background
read_status = read | partial | failed
```

最终总结不必暴露完整账本，但核心结论必须能从账本追溯。
