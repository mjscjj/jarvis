# 实体长期事实：summary 单页化

> Status: proposed
> Authority: `docs/00-overview.md` 是总纲；本文只提议实体长期事实页的收敛方式
> Baseline: 2026-08-15

## 1. 问题

世界模型现在只有事件流在工作，画像那一半饿死了。实测：

| 实体 | 画像字符数 | fact 条数 |
|---|---|---|
| 公会 Agent 基建 | 422（8 天未变） | 1089 |
| Agent Runtime | 77 | 227 |
| Bax AM 助手 | 25 | 72 |

根因是**世界模型没有写读同构的单位**。写入单位是「一条 fact」或「一个实体的一个字段」，读取单位是 `renderUserPrompt` 运行时拼出来的投影，两者从不是同一个东西。后果：

- Agent 写下一条 fact 时无法预演读取者看到什么，没有反馈回路 —— 累积 238 条重复；
- 「整理」缺少对象（不存在「关于项目 44 我们现在知道什么」的完整视图），维护动作只能退化成追加；
- 读取时必须由模型重新归纳，上下文膨胀到 `prompt_chars=99332` 后 `signal: killed`；
- 为对抗膨胀另建的日压缩层按自然日锚定，而 fact 是回填的（08-13 的 144 条在 08-14 才写入），结构性漏掉大部分事实，08-12/13/14 三天 rollup 产出为 0。

## 2. 目标

给每个世界实体一个**有名字、有边界、被整体读写**的长期事实页，字段名统一为 `summary`。

- 有名字 → 重放天然幂等，不依赖模型「写前查询」的自觉；
- 有边界 → 压缩成为写入路径上绕不过去的动作，不再需要独立的压缩作业；
- 整体读写 → 写的人能预演读的人看到什么。

`fact` 表保持不变，职责重新划清：**summary 答「现在是什么」，fact 答「发生了什么」**。fact 的无损、按业务时间查询、可聚合、并发安全这四项能力是 wiki 形态给不了的，必须保留。

## 3. 已拍板的设计决定

实施时以此为准，不要另作取舍。

1. **单字段，不设第二个索引字段。** summary 的第一行即索引行，约定为一句话说明「这是什么」。不保留 `description` / `relation` 之类的并行短字段——两个字段就是两个陈旧源，且要求模型每次同步维护两处。
2. **索引不无条件注入上下文，只做工具。** 过滤后仍有约 91 个活跃实体，一行一个约 7000 字符，不值得每轮固定开销。M3 的主体范围本来就由当前 chat 决定。
3. **fact 必须撤出默认上下文。** 这一条不做整个方案失效：只要 M3 还无条件推送几十条 fact，追加就仍是更省事的路径，summary 会和现在的画像一样饿死。
4. **不迁移历史字段内容。** 废弃列直接删除，summary 从空开始。
5. **旧版 summary 写进独立的 `page_revision` 表。** 页面按设计有损，压缩会主动扔掉细节，旧版是唯一记录被扔掉了什么的地方。它不进 fact 表：fact 答「世界发生了什么」，旧版答「我们自己的笔记被改过」，混在一起会让上一轮写下的结论以证据身份回到维护 Agent 面前。见 [design-fact-as-evidence-index.md](design-fact-as-evidence-index.md) §5.4。
6. **引用不建表。** 正文内 Markdown 链接 + 写入时校验目标存在 + `LIKE` 反查。全库实体几百行，够用。
7. **`relation_fact` 整表废弃。** 页内引用取代它，25 条内容不迁移。
8. **上限用常量不进 config。** 合适值需观察一周才知道，不为想象中的调优提前建配置项。

## 4. 数据层

### 4.1 六张表统一加 `summary`

`internal/domain/models.go`，字段声明统一为：

```go
// Summary is this entity's long-term truth: what it is and where it stands.
// It is read and written whole. Line one is the index line. Detail history
// lives in Fact rows; drill down with list-facts.
Summary *string `gorm:"column:summary"`
```

| 表 | 动作 |
|---|---|
| `project` | 新增 |
| `person` | 新增 |
| `managed_resource` | 新增 |
| `feishu_group` | 新增 |
| `principal_profile` | 新增 |
| `key_matter` | 已有 `summary`，保持不动 |

`key_matter.LastProgressAt` 的现有语义（「moves only when Summary actually changes」）推广到全部六张表：`summary` 实际变化时才推进 `last_progress_at`，普通控制位编辑不推进。`project` / `person` / `principal_profile` 需补这个列。

### 4.2 删除的列

```
project:            description, repos, tech_stack, key_decisions, timeline, notes
person:             relation, comm_style, notes
managed_resource:   description
principal_profile:  background, preferences
feishu_group:       background_note
```

这些列没有任何程序消费点。`repos` / `tech_stack` / `key_decisions` / `timeline` 的全部引用都在 `internal/background/project.go` 的 CRUD 透传和 `views.go` 的响应映射，最终去向是 `internal/extract/prompt.go:241` 拼成一个 blob 进提示词。按 AGENTS.md §4「说不出消费点就保持宽松」，它们本就该是自由文本。`person.comm_style` 全库 0/99 填充，`project.notes` 全库 0/7 填充。

**唯一保留的短文本是 `feishu_group.description`** —— 它是 capture 同步的飞书群公告，属于外部事实，不是我们的知识。原注释已写明 "capture owns Description"。

### 4.3 整表删除

`relation_fact` 表及 `internal/background` 中对应的 CRUD、`/api/relation-facts` 四个路由、`jarvis-tools` 的 `create-relation` / `update-relation` / `list-relations`。

### 4.4 迁移

不写迁移脚本。GORM `AutoMigrate` 负责加列；删列与删表用一次性 SQL 手工执行：

```sql
ALTER TABLE project DROP COLUMN description;
ALTER TABLE project DROP COLUMN repos;
ALTER TABLE project DROP COLUMN tech_stack;
ALTER TABLE project DROP COLUMN key_decisions;
ALTER TABLE project DROP COLUMN timeline;
ALTER TABLE project DROP COLUMN notes;
ALTER TABLE person DROP COLUMN relation;
ALTER TABLE person DROP COLUMN comm_style;
ALTER TABLE person DROP COLUMN notes;
ALTER TABLE managed_resource DROP COLUMN description;
ALTER TABLE principal_profile DROP COLUMN background;
ALTER TABLE principal_profile DROP COLUMN preferences;
ALTER TABLE feishu_group DROP COLUMN background_note;
DROP TABLE relation_fact;
```

## 5. 约束层

### 5.1 上限

`internal/background/types.go`：

```go
// SummaryMaxChars forces compaction at write time. This is the whole
// mechanism: without a ceiling the agent always appends.
const SummaryMaxChars = 8000
```

在各 `Input.validate()` 与 `update-page` 入口校验。超限返回的错误必须能指导模型自救，而不只是拒绝：

```go
return fmt.Errorf("summary 有 %d 字符，上限 %d。请先压缩：合并旧明细为一句结论、把某一节改成对 fact 的引用、或删除已不重要的内容，再重新提交", n, SummaryMaxChars)
```

一个实体写失败不得影响同一轮其它实体的写入——与 `MaterializeOnce` 收集错误继续处理的做法一致，收集后 `errors.Join` 返回。

### 5.2 引用

正文内 Markdown 链接，target 为实体 URI：

```markdown
[唐建科](person:12) 08-11 决定按不保留历史会话灰度，影响 [Agent Runtime](project:45)。
根因见 [Task 393](task:393)，它更正了 [Task 390](task:390) 的结论。
```

scheme 固定：`principal` `person` `project` `key_matter` `group` `resource` `task` `todo` `fact`，后接 `:ID`。

新增 `internal/background/reference.go`：

- `ParseReferences(content) []Reference` —— 正则提取；
- `ValidateReferences(ctx, db, refs) error` —— 逐个查目标存在，不存在 fail-fast。**这是防模型编造 ID 的唯一手段**；
- `FindBacklinks(ctx, db, type, id) []Backlink` —— 六张表 `summary LIKE '%(person:12)%'`。几百行实体，LIKE 够用，不建索引表。

### 5.3 并发

factengine 与 M5 可能同时改同一个实体的 summary。`update-page` 必须带 `--if-unchanged-since <RFC3339>`，服务端与 `updated_at` 做 CAS：

- 不匹配返回 409，响应体带**当前全文**，让 agent 基于最新版本重新合并后重试；
- 不新增 version 列，复用 `updated_at`。

## 6. 工具层

### 6.1 `scripts/jarvis-tools` 新增四个通用命令

字段名统一是这四个命令能存在的前提——否则每实体一套读写，「页」只是比喻。

```
get-page --type TYPE --id N
```
返回：`summary` 全文、当前字符数与上限、`updated_at`（供 CAS 用）、出链列表、反链列表、该主体的 fact 条数（**只给数字不给内容**，逼下钻）。

```
update-page --type TYPE --id N --content - --if-unchanged-since TS
```
顺序执行：校验上限 → 校验引用目标存在 → CAS 比对 → 旧全文写进 `page_revision` 表 → 更新 `summary` 与 `last_progress_at`。任一步失败整体不写。

```
list-pages [--type TYPE] [--all] [--stale-days N] [--over-limit]
```
索引本身。每行：类型、id、名字、summary 首行、字符数、`last_progress_at`。默认只列活跃实体（`project.status='active'`、`person.is_active AND role IN ('leader','key')`、`key_matter.closed_at IS NULL`、`managed_resource.is_active`、`feishu_group.related_group AND tier<>'cold'`，约 91 个）；`--all` 放开。`--stale-days` 与 `--over-limit` 供巡检用。

```
list-backlinks --type TYPE --id N
```

### 6.2 收口写入口

`update-project` / `update-person` / `update-key-matter` / `update-resource` / `update-group` / `update-principal` **不再接受 `summary`**，只管控制位（`status` / `priority` / `code` / `role` / `tier` 这些真有程序消费的）。长期事实只有 `update-page` 一个写入口。

### 6.3 API

- 各实体 `PUT` 移除 `summary` 字段；
- 新增 `GET /api/pages`、`GET /api/pages/:type/:id`、`PUT /api/pages/:type/:id`、`GET /api/pages/:type/:id/backlinks`；
- 删除 `/api/relation-facts` 四个路由。

### 6.4 `internal/toolcatalog/catalog.go`

第 37 行改写为：实体的长期事实用 `get-page` / `update-page` 读写，索引用 `list-pages`，历史明细用 `list-facts` 按主体和日期下钻。仍不点具体参数（有测试校验提示词与工具目录分离）。

## 7. 读取层

### 7.1 `internal/extract/prompt.go`

删除 240-242 行的六字段 blob 渲染。实体段落改成两行结构：名字 + `summary` 全文。

**fact 段落改成只给条数与下钻提示**，不再推送内容：

```
# 世界事实（明细未展开）
project:44「公会 Agent 基建」今日 23 条、近 7 天 187 条 —— 需要细节用 list-facts 按主体和日期查。
```

`loadFacts` 简化为计数查询，`cfg.Extract.FactLimit` 与关键人事实主体的并集逻辑一并删除。

### 7.2 `internal/contextsnap/snapshot.go`

`Snapshot` 中实体字段收敛为 `summary`，`buildContextSnapshot` 一并冻结。

### 7.3 降级边界

页面自带上限后世界数据体量有界，不再增加按实体类型逐级收紧的降级协议。`BuildPrompt` 超 `MaxChars` 时只裁减可舍弃的会话上下文，仍然 fail-fast，不静默截断冻结证据。

## 8. 维护层

### 8.1 `conf/prompts/fact-extract-system-prompt.md` 改写

factengine 每轮动作顺序固定：

1. `list-pages` 看索引，判断本轮材料影响哪些实体；
2. 逐个 `get-page` 读全文；
3. 三选一判断：新增、更新既有小节、或与既有内容矛盾。**矛盾时改写并在正文留一句「此前认为 X，08-14 改判为 Y」**，不静默覆盖，也不两条并列留给下一个读者猜；
4. `update-page` 写回，超限先压缩；
5. `append-facts-batch` 照常留档。

关键是把主次写死：**改写 page 是必做动作，append fact 是附带**。现在的提示词是反过来的。

### 8.2 删除 `changedFields`

`internal/background/project.go:280-312` 及 person / key_matter 的同类逻辑，连同它们生成的「更新项目资料：status、repos。」审计 fact（现存 30 条）一并删除。单字段之后该函数的答案永远是 `summary`，真正的历史已由 `page_revision` 表承担。

### 8.3 M5 的写入权

M5 执行完 Task 后可以更新相关实体的 summary——它掌握最新状态。写入走同一个 `update-page`，由 CAS 保证不互相覆盖。不为 M5 开第二条路径。

## 9. 初始重建：不做（已拍板）

`summary` 从空开始，不做批量重建。

必须正视的后果：factengine 的三个来源游标已在最新位置，它不会回头读 08-02 到 08-14 的历史消息，因此世界模型从上线当天开始积累，此前的沉淀不会自动进入 summary。加上 fact 已撤出默认上下文（§3.3），**接下来一到两周 M3 的判断质量会明显下降**，这是已知且接受的代价。

历史没有丢失：`fact` 表 2825 条一条不删，agent 随时可用 `list-facts` 按主体和日期下钻。若上线后发现质量下降不可接受，再单独评估重建方案，本次不实现。

## 10. 前端

`web/src/Background.tsx` 六个实体的编辑表单统一改成「Markdown 编辑器 + 实时字符数/上限」。删除 project 的五个输入区、person 的三个、resource 与 principal 的对应输入区，以及 relation_fact 的整块 UI。

## 11. 范围之外

- 不建 librarian 巡检 cron（`list-pages --stale-days` / `--over-limit` 已提供数据，第二批再看是否需要自动化）；
- 不动 `daily_digest`、`mem0`、`Snapshot.Memories`、`data/shared-memory.md`；
- 不改 `fact` 表结构，不删任何历史 fact；
- 不解决「什么该独立成 key_matter」的组织权问题——每实体一页不覆盖这一层，属已知局限。

## 12. 验收

`go build ./...`、`go test ./...` 全绿，`gofmt -w` 无残留；改了 `web/` 则 `npx tsc --noEmit`。重启走 `./scripts/rebuild-server.sh`。

单测至少覆盖：

- 上限校验：超限报错，错误消息含当前值与上限；
- 一个实体写失败不阻塞同轮其它实体；
- 引用校验：指向不存在实体时 fail-fast；
- 反链查询命中六张表；
- CAS：`--if-unchanged-since` 不匹配返回 409 且响应含当前全文；
- `update-page` 把旧全文写进 `page_revision` 表、不写 fact，且 `last_progress_at` 只在内容实际变化时推进；
- `update-project` 等拒绝 `summary` 字段；
- 提示词只渲染 summary 与 fact 条数，不含任何已删字段；
- `list-pages` 的默认活跃过滤、`--stale-days`、`--over-limit`。

上线一周后看两个指标判断机制是否生效：项目 44 的 `summary` 是否持续变化（对比现在 422 字符 8 天不动），M3 单轮提示词字符数是否稳定在数千（对比 99332 那次 OOM）。
