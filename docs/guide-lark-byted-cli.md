# Lark CLI 与 ByteD CLI 使用手册（Jarvis 视角）

Jarvis 在本地可信环境里，通过子进程调用两类 CLI 补全飞书侧与研发侧信息：

| CLI | 用途 | Jarvis 里谁在用 |
|-----|------|----------------|
| **`lark-cli`** | 飞书/Lark 原生能力：消息、文档、日历、通讯录、任务、妙记等 | M1 采集、M3/M4/M5 agent、背景页 resolve |
| **`bytedcli lark`** | 与 `lark-cli` **同一套能力**，经 ByteD CLI 统一入口包装 | 进度页工作日志、agent 侧飞书查询（与 lark-cli 等价，命令形态略有不同） |
| **`bytedcli codebase`** | 字节 Codebase：跨仓 MR、commit、仓库、Issue 等 | 进度页「项目代码」、每日总结里的 MR 收集 |

完整架构见 [`docs/00-overview.md`](00-overview.md)。本文只讲**怎么装、怎么登、怎么调、返回长什么样、有哪些坑**——内容来自 Jarvis 各模块设计文档与**逐条实跑验证**，不是凭记忆写的。

---

## 0. 三者关系：先搞清楚用哪个

```
                    ┌─────────────────────────────────────┐
                    │            bytedcli                  │
                    │  auth / codebase / insearch / …     │
                    └──────────────┬──────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
              ▼                    ▼                    ▼
      bytedcli lark          bytedcli codebase    bytedcli insearch …
      (包装 lark-cli)        (Codebase OpenAPI)   (内网搜索等)
              │
              ▼
         lark-cli 二进制
    (im / docs / calendar / contact / …)
```

**选型原则：**

1. **飞书消息/文档/日历/人** → `lark-cli …` **或** `bytedcli lark …`（二选一，能力等价）。
   - Jarvis **Go 后端**（M1/M2/背景）直接调 **`lark-cli`**，封装在 `internal/larkcli/`。
   - **agent / 脚本 / 进度页 worklog** 可调 **`bytedcli lark`**（统一走 ByteCloud 登录态）。
2. **代码仓库 / MR / commit** → 只用 **`bytedcli codebase`**（lark-cli 没有这块）。
3. **不确定命令** → 先 `--help`，再 `--dry-run`（lark 部分子命令支持），**别猜**。

**命令形态对照（同一能力两种写法）：**

| 能力 | lark-cli（Jarvis M1 常用） | bytedcli lark（包装层） |
|------|---------------------------|-------------------------|
| 拉群消息 | `lark-cli im +chat-messages-list --chat-id oc_xxx --as user` | `bytedcli lark im message list --chat-id oc_xxx --as user` |
| 搜人 | `lark-cli contact +search-user --query "张三" --as user` | `bytedcli lark contact search-user --query "张三" --as user` |
| 读文档 | `lark-cli docs +fetch --doc <url>` | `bytedcli lark docs fetch --doc <url>` |
| 搜文档 | （部分子命令用 `drive +search`） | `bytedcli lark docs search --query "…" --filter '…' --as user` |
| Raw API | `lark-cli api GET /open-apis/…` | `bytedcli lark api GET /open-apis/… --as user` |

> lark-cli 老命令多用 `+子命令` 前缀；bytedcli lark 用空格分隔子命令。以本机 `lark-cli --help` / `bytedcli lark --help` 为准。

---

## 1. 安装与登录

### 1.1 lark-cli

```bash
# 本机路径（Jarvis 附录实测）
# /Users/bytedance/.local/bin/lark-cli  (1.0.72)

lark-cli config          # 配置 app_id / app_secret
lark-cli login           # OAuth 设备流授权用户
lark-cli auth status     # 看当前 user/bot 授权状态
```

- **`--as user`**：以「我」的身份操作（读我的消息、建我的日程、搜我的人脉）。M1 采集、resolve、日历建会都用 user。
- **`--as bot`**：以应用机器人身份（发交互卡片、收 bot 事件流）。M4 确认卡片、M5 对外发群消息常用 bot。
- **高风险写操作**（发消息、建会、改文档等）属于 `high-risk-write`，需加 **`--yes`** 才会真执行；先用 **`--dry-run`** 预览。
- 机器可读输出：加 **`--format json`**（lark-cli）或外层用 **`bytedcli --json lark …`**。

### 1.2 bytedcli

```bash
# 全局安装（推荐）
NPM_CONFIG_REGISTRY=http://bnpm.byted.org npm install -g @bytedance-dev/bytedcli@latest

# 登录与状态
bytedcli auth login      # 首次
bytedcli --json auth status
```

`auth status` 里 `data.authenticated=true` 且 `bytecloud_auth.identity.username` 是你本人即可。Codebase / Lark 域里「我」统一写 **`@me`**，CLI 用当前登录态解析，不必手填 `chujiejie.1` 或邮箱。

### 1.3 JSON 输出约定（解析时别搞混）

| 来源 | 成功判断 | 典型结构 |
|------|----------|----------|
| `lark-cli --format json` | `ok == true` | `{"ok":true,"data":{...}}` |
| `bytedcli --json lark …` | `ok == true` | 同上 |
| `bytedcli --json codebase …` | `status == "success"` | `{"status":"success","data":{...}}` |

**全局参数位置**：`bytedcli` 的 `--json` 放在 domain **前面**：`bytedcli --json codebase search mr …`。

### 1.4 Jarvis 里的调用约定

- Go 后端：`exec.CommandContext` 调 CLI，**fail-fast**——非 0 退出、超时、JSON 解析失败、`ok:false` 直接返回 error，不静默兜底。
- 限流：M1 `internal/larkcli` 有令牌桶 + 并发信号量（默认 2–3 个同时在跑的 lark-cli）。
- 子进程可能返回 `{ "ok": false }` 但退出码仍为 0；封装层要**同时校验退出码和 `ok` 字段**。

---

## 2. lark-cli：Jarvis 各模块怎么用

### 2.1 M1 消息采集（`internal/capture/`）

**发现会话：**

```bash
lark-cli im +chat-list --as user --types=p2p,group --sort active_time --format json
```

**拉单群增量消息：**

```bash
lark-cli im +chat-messages-list --as user --chat-id oc_xxx \
  --start "2026-07-22 00:00" --end "2026-07-22 23:59" \
  --order asc --page-size 50 --page-token <token> --format json
```

- 时间范围 `--start/--end`、分页 `--page-token`、排序 `--order asc|desc`。
- 去重键：**`message_id`**（`om_` 前缀），不是 `event_id`。
- 系统消息：`msg_type=system` 时 sender 为空，M1 用占位 `__system__` 落库。

**（可选）Bot 事件流低延迟：**

```bash
lark-cli event consume im.message.receive_v1 --as bot
# NDJSON  stdout；去重仍用 message_id
```

### 2.2 背景 / 人员解析（`internal/background/`）

按姓名搜 open_id（用户身份）：

```bash
lark-cli contact +search-user --query "张三" --as user --format json
lark-cli contact +search-user --queries "alice,bob,张三" --as user
lark-cli contact +search-user --query "张三" --has-chatted --exclude-external-users
```

按 open_id 反查：

```bash
lark-cli contact +search-user --user-ids "ou_xxx,ou_yyy" --as user --format json
lark-cli contact +get-user --user-id ou_xxx --as bot --format json
```

返回字段：`open_id`、`name`、`department`、`avatar_url`、`p2p_chat_id` 等。`has_more=true` 且无法唯一确定时 **fail-fast**，让用户消歧，不猜。

Jarvis 配置里「我」的 open_id：`conf/config.yaml` → `extract.principal_open_id`（如 `ou_cfd9e106436c46adf20aaf9fe076c65d`）。

### 2.3 M5 执行：发消息 / 建会 / 改文档

**发群消息（bot）：**

```bash
lark-cli im +messages-send --as bot \
  --chat-id oc_xxx --text "内容" \
  --idempotency-key "task-123-apply" \
  --dry-run    # 先预览
# 确认后加 --yes 真发
```

**回复消息：**

```bash
lark-cli im +messages-reply --as bot --message-id om_xxx --text "回复"
```

**建日程（user，需先 resolve 参会人 open_id）：**

```bash
lark-cli calendar +suggestion --as user --start … --end …   # 找空档
lark-cli calendar +room-find --as user …                    # 找会议室
lark-cli calendar +create --as user \
  --summary "会议标题" --start 2026-07-22T15:00:00+08:00 --end 2026-07-22T16:00:00+08:00 \
  --attendee-ids "ou_aaa,ou_bbb" --dry-run
```

**读/写文档：**

```bash
lark-cli docs +fetch --doc <url-or-token> --as user
lark-cli docs +create --title "标题" --as user
# 具体更新子命令见 lark-cli docs --help
```

### 2.4 日历 / 妙记 / 视频会议（每日总结等场景）

```bash
# 我的日程
lark-cli calendar +agenda --as user --start <day> --end <day>

# 我参与的会议
lark-cli vc +search --participant-ids <me_open_id> --start … --end … --as user

# 我拥有的妙记
lark-cli minutes +search --owner-ids me --start … --end … --as user
lark-cli minutes +get-transcript --minute-token <token> --as user
```

### 2.5 lark-cli 全域约定

| 约定 | 说明 |
|------|------|
| `--dry-run` | 打印将发出的请求，不执行 |
| `--jq '<expr>'` | 过滤 JSON 输出 |
| `high-risk-write` | 写操作需 `--yes` |
| `--as user \| bot` | 选身份；凭据由 lark-cli profile 管理 |
| Risk 标注 | 子命令 help 里会标 `Risk: read` / `write` |

---

## 3. bytedcli lark：飞书能力（包装 lark-cli）

语法：`bytedcli lark <domain> <action> [flags]`，能力与 lark-cli 对齐。不确定时：

```bash
bytedcli lark --help
bytedcli lark docs --help
bytedcli lark schema im.messages.create    # 看某 API 的参数/权限
```

**Raw API 直通**（封装命令没覆盖时）：

```bash
bytedcli lark api GET /open-apis/calendar/v4/calendars --as user
bytedcli lark api POST /open-apis/im/v1/messages \
  --params '{"receive_id_type":"chat_id"}' \
  --data '{"receive_id":"oc_xxx","msg_type":"text","content":"{\"text\":\"hello\"}"}'
```

**常用封装命令：**

```bash
bytedcli lark docs fetch --doc <url-or-token>
bytedcli lark im message send --chat-id oc_xxx --text "hi" --as user
bytedcli lark task create --summary "待办标题" --as user
bytedcli lark sheets read --url <spreadsheet-url> --range "Sheet1!A1:D10"
```

---

## 4. 文档：三种口径要分开（重要）

飞书**没有**「我最近编辑的文档列表」直取接口（`drive/v1/recents`、`drive/explorer/v2/recent` 实测 404；`view_records` 是「某文档被谁看过」，方向反了）。只能靠**搜索** `search/v2/doc_wiki/search`（bytedcli 封装为 `lark docs search`）。

### 4.1 三种维度（不要混在一个列表里）

| 维度 | 含义 | 怎么查 | 注意 |
|------|------|--------|------|
| **我创建的（owner=我）** | 文档所有者是我 | `--filter '{"owner_ids":["@me"]}'` | **服务端 filter 生效**。别人后来编辑，owner 仍是我，会出现在这里 |
| **我编辑的（最后编辑人=我）** | 最后一次保存是我 | 宽搜召回后，**本地**过滤 `edit_user_id == 我的 open_id` | **`edit_user_ids` 服务端 filter 实测无效**（会退化成普通搜索）。必须本地比对 |
| **我收到的** | 别人发我的文档链接 | 查 Jarvis 库 `resource`（M1 采集），不调飞书 | `doc_token` 非空 + 当天 `created_at` |

**owner ≠ 我写的。** 反例（实测）：

```jsonc
{
  "owner_name": "储节节",
  "edit_user_name": "孙齐浓",   // 最后编辑人是别人
  "update_time_iso": "2026-06-22T11:47:35+08:00"
}
```

这条会进「我创建的」，但不应进「我编辑的」。

### 4.2 搜索命令（bytedcli lark）

**我创建的（owner 过滤）：**

```bash
bytedcli --json lark docs search \
  --as user \
  --query " " \
  --filter '{"owner_ids":["@me"]}' \
  --page-size 20
```

**我编辑的（无可靠服务端 filter，宽搜 + 本地过滤）：**

```bash
bytedcli --json lark docs search \
  --as user \
  --query " " \
  --page-size 20
# 本地保留：result_meta.edit_user_id == principal_open_id
#           且 update_time_iso 落在目标日期窗口
```

**dry-run 看实际请求体：**

```bash
bytedcli lark docs search --query " " --filter '{"owner_ids":["@me"]}' --dry-run --as user
# → POST /open-apis/search/v2/doc_wiki/search
#   {"doc_filter":{"owner_ids":["@me"]},"wiki_filter":{"owner_ids":["@me"]},...}
```

要点：

- **必须 `--as user`**。
- `--query` 不能省（全文搜索接口）；用空格 `" "` 做宽泛召回；`--page-size` 最大 20，要全就翻 `page_token`。
- 展示前剥掉标题里的高亮标签：`<b>` `<h>` `<hb>` 等。

**返回字段（`data.results[]`）：**

```jsonc
{
  "entity_type": "WIKI",
  "title_highlighted": "标题<h>高亮</h>",
  "result_meta": {
    "doc_types": "DOCX",
    "owner_id": "ou_cfd9e106436c46adf20aaf9fe076c65d",
    "owner_name": "储节节",
    "edit_user_id": "ou_...",
    "edit_user_name": "孙齐浓",
    "url": "https://bytedance.larkoffice.com/wiki/...",
    "token": "...",
    "create_time_iso": "2026-06-02T11:03:22+08:00",
    "update_time_iso": "2026-06-22T11:47:35+08:00",
    "last_open_time_iso": "2026-07-21T19:38:04+08:00"
  }
}
```

- 时间字段带 `+08:00`（本地时区）。
- 「今天动过」：`update_time_iso` 落当天窗口；「今天打开过」：`last_open_time_iso`。

### 4.3 我收到的文档（Jarvis 库，非 CLI）

```sql
SELECT * FROM resource
WHERE doc_token IS NOT NULL AND doc_token <> ''
  AND created_at >= <当天0点> AND created_at < <次日0点>
ORDER BY created_at DESC;
```

列：`name`、`url`、`doc_token`、`group_id`（join `group` 拿群名）、`created_at`。无 URL 时兜底：`https://bytedance.larkoffice.com/docx/<doc_token>`。

### 4.4 lark-cli 侧的文档搜索（daily digest 设计稿提及）

设计文档里还写过：

```bash
lark-cli drive +search --mine --sort edit_time --as user
```

`--mine` 表示「我拥有的」，与 `owner_ids:["@me"]` 同类；**不能**精确表示「我编辑过但 owner 是别人」。接受该能力上限，或改用 §4.2 的宽搜 + `edit_user_id` 本地过滤。

---

## 5. bytedcli codebase：代码与 MR

字节 protected branch 走 MR，所以「我的提交」以 **MR 为粒度**。跨仓必须用 `search mr`（`mr list` 必须 `-R <单仓>`）。

### 5.1 跨仓库查我的 MR（进度页 / 每日总结）

```bash
bytedcli --json codebase search mr \
  --author @me \
  --sort-by UpdatedAt --sort-order Desc \
  --updated-since 2026-07-22T00:00:00+08:00 \
  --updated-until 2026-07-23T00:00:00+08:00 \
  --page-size 100
```

| 参数 | 说明 |
|------|------|
| `--author @me` | 作者是我 |
| `--updated-since` / `--updated-until` | RFC3339，**服务端按更新时间过滤** |
| `--status` | `open` \| `closed` \| `merged` |
| `--repo-path` | 限定单仓，如 `chujiejie.1/jarvis_bot` |
| `--page-size` | 1–100 |

**返回（`data.merge_requests[]`，字段 PascalCase）：**

```jsonc
{
  "Title": "合入近期改动到 main …",
  "Status": "open",
  "URL": "https://code.byted.org/chujiejie.1/jarvis_bot/merge_requests/1",
  "TargetBranchName": "main",
  "CommitsCount": 9,
  "ChangesCount": 68,
  "CreatedAt": "2026-07-22T10:50:12Z",
  "UpdatedAt": "2026-07-22T10:50:39Z",
  "MergedAt": null,
  "CheckRunSummaryStatus": "passed"
}
```

- **仓库名从 URL 解析**：`https://code.byted.org/<owner>/<repo>/merge_requests/N` → `<owner>/<repo>`。
- 时间 UTC（`Z`），展示时转本地时区。

### 5.2 贡献统计（只有总数，无明细）

```bash
bytedcli --json codebase user-statistics --user @me --days 30
```

返回 `ContributedRepositoriesCount`、`CommitsCount`、`MergedMergeRequestsCount`、`AddedLoc`/`DeletedLoc` 等。**要明细仍用 §5.1**。

### 5.3 其它常用

```bash
bytedcli codebase mr get <number|url>
bytedcli codebase mr diff <number>
bytedcli codebase commit list -R "<owner/repo>" --revision master
bytedcli codebase repo get "<owner/repo>"
bytedcli codebase search issue --assignee @me --status todo
```

在 git 仓库目录内执行时，`-R` 可省略，CLI 从 `origin` 推断。

### 5.4 本地 git commit（补充，非 bytedcli）

对已 clone 的仓库：

```bash
git log --author=chujiejie.1 --since="2026-07-22 00:00:00" --until="2026-07-23 00:00:00" \
  --pretty=format:"%h %s (%ci)"
```

bytedcli **没有**「我某天在所有远端仓库的 commit 列表」全局接口；跨仓只能走 MR 搜索（§5.1）或自己维护仓库清单扫 git。

---

## 6. agent 侧：M3/M4/M5 怎么引导模型用 CLI

Jarvis 的 codex/traex agent 跑在 `danger-full-access` + 联网环境，prompt 里只给**简短指引**，让模型自己 `--help` 探索，不维护冗长命令清单：

```
可执行 shell。需要补信息时：
- jarvis-tools <子命令>   # Jarvis 自有数据（项目/人/群/记忆），输出 JSON
- lark-cli / bytedcli lark …  # 飞书侧（先 --help）
- bytedcli codebase …     # 代码仓/MR
- git                     # 本地仓库
查询结果用于推断；被引用的消息才是证据，工具结果不得当作新证据。
```

M3 抽取引擎默认 **codex（traex）** 就是为「自跑 lark-cli/bytedcli/git」设计的；kimi/model API 的 function-calling 循环**不能**自跑 shell。

---

## 7. 踩坑清单

| 坑 | 说明 |
|----|------|
| owner ≠ 编辑人 | `owner_ids:["@me"]` 是「我拥有的」，不是「我写的」；要分 Tab 展示（§4） |
| `edit_user_ids` filter 无效 | 飞书搜索服务端不认，返回里 editor 可能是任何人；编辑维度必须本地滤 |
| 无「最近文档列表」API | 只能搜索 + 时间本地过滤；召回不全就翻 `page_token` |
| JSON 信封两套 | lark 看 `ok`，codebase 看 `status`（§1.3） |
| 时区不一致 | codebase UTC；lark search 本地 `+08:00` |
| lark-cli 退出码 0 但 `ok:false` | 封装层要同时校验 |
| `message_id` vs `event_id` | 去重/撤回用 `message_id`（`om_`），不是事件投递 id |
| MR 才是提交粒度 | 字节 protected branch 不直接 push；查「我的代码」看 MR |
| `--dry-run` 再 `--yes` | 所有 high-risk-write 先预览再确认 |

---

## 8. Jarvis 代码落点

| 功能 | 代码 | 用的 CLI |
|------|------|----------|
| M1 采集 | `internal/capture/`、`internal/larkcli/` | `lark-cli im …` |
| 背景 resolve | `internal/background/resolve.go` | `lark-cli contact …` |
| M3/M4/M5 agent | `internal/extract/codexengine/`、`internal/decide/`、`internal/execute/` | agent 自跑 lark-cli / bytedcli / git |
| 进度·项目代码 | `internal/insight/worklog.go` → `GET /api/worklog/commits` | `bytedcli codebase search mr` |
| 进度·文档 | `internal/insight/worklog.go` → `GET /api/worklog/documents` | `bytedcli lark docs search` + 库 `resource` |
| 每日总结（设计） | `docs/design-daily-digest.md` | agent 自跑 §2.4 + §5.1 + git |

---

## 9. 相关文档（已并入本文，细节以本文为准）

| 文档 | 原内容 | 与本文关系 |
|------|--------|------------|
| `guide-bytedcli-larkcli.md`（已删除） | 进度页 worklog 实测命令 | **已合并** → 本文 §4–§5 |
| [`design-daily-digest.md`](design-daily-digest.md) §2.1 | 个人进度数据源（文档/日历/MR/git） | **已合并** → 本文 §2.4、§4.4、§5 |
| [`modules/01-background.md`](modules/01-background.md) §5.1 | contact 搜人 | **已合并** → 本文 §2.2 |
| [`modules/02-message.md`](modules/02-message.md) §1.1 | im 采集命令 | **已合并** → 本文 §2.1 |
| [`modules/05-execution.md`](modules/05-execution.md) 附录 A | lark-cli 能力探测 | **已合并** → 本文 §2.3、§2.5 |
| [`design-context-pipeline.md`](design-context-pipeline.md) | agent 自跑 CLI 设计 | **已合并** → 本文 §6 |
