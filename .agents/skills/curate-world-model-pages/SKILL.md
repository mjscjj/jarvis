---
name: curate-world-model-pages
description: 巡检并改写 Jarvis 世界模型的实体长期事实页（principal / person / project / key_matter / group / resource）：补空页、用新事实淘汰过期结论、把超限页压回上限。适用于「世界模型巡检」「事实页维护」「长期事实页补全」「把事实流沉淀成当前状态」。只读事实、只写页面，不发消息、不建 Task、不改代码。
---

# 世界模型事实页巡检

你在这条任务里是 Jarvis 世界模型的图书管理员。事实流（`list-facts`）在持续增长，但真正被 M3
准入判断和 M5 执行读到的是每个实体的**长期事实页**。你的产出就是这些页面本身，不是一份巡检报告。

## 0. 硬边界

- 只读事实、只写页面（`update-page`）。不发飞书消息、不建 Task、不改代码、不申请权限。
- 开始维护前先完整读取一次 `jarvis-tools get-page-guidance`，所有页面共用这份内容契约；不按实体类型加载不同模板。
- 只写本轮真正读到的证据支持的内容。查不到就不写，不推演、不用"应该是"补齐。
- 单页上限 8000 字（`get-page` 返回 `char_count` / `max_chars`）。写满不是目标，**让人读完能直接判断和行动**才是。
- 一轮最多改 12 页。改不完留给下一轮，不为清空队列降低单页质量。
- 页面是长期状态，不是日志。任何"本轮巡检做了什么"都不写进页面。

## 1. 建候选队列

```bash
jarvis-tools list-pages                  # 活跃实体页索引：type / id / name / index_line / char_count / last_progress_at
jarvis-tools list-pages --over-limit     # 超限页，本轮必须处理
jarvis-tools list-pages --stale-days 14  # 两周未变动的页
```

按这个顺序取：

1. 超限页（`char_count` 超上限）——写不回去会一直失败，先压缩；
2. 空页（`char_count` 为 0）且该主体近期有活动；
3. 页面 `last_progress_at` 早于该主体最新事实的；
4. 空页且有历史事实；
5. 其余按 `last_progress_at` 从旧到新。

判断"近期有活动"：

```bash
jarvis-tools list-tasks --date <昨天>     # 昨天推进过的 Task 指向哪些项目和事项
jarvis-tools list-facts --subject-type project --subject-id 44 --limit 100
```

principal、活跃项目、未关闭的 key_matter 是常驻高价值主体：即使不在上面的队列里，也要抽查它们
是否已经和现实脱节。

## 2. 每页的动作

读现状 → 读证据 → 整页重写 → 带乐观锁写回。

```bash
jarvis-tools get-page --type project --id 44
jarvis-tools list-facts --subject-type project --subject-id 44 --limit 100
```

需要控制字段（状态、负责人、仓库、群绑定）时读对应的 `get-project` / `get-person` /
`get-key-matter` / `get-group` / `get-resource`。`list-facts` 返回的是证据索引，不是可直接采信的
知识：判断页面结论是否过期时，按 `source_kind` / `source_id` 继续读取原始材料——`message` 用
`get-message`，`todo_event` 用 `get-todo-event`，`task_event` 用 `get-task-event`，
`execution_run` 用 `get-task-run`，`resource` 用 `get-captured-resource`。原始材料与索引
`description` 冲突时以原始材料为准。

写回：

```bash
jarvis-tools update-page --type project --id 44 \
  --if-unchanged-since "<get-page 返回的 updated_at>" --content - <<'EOF'
<整页新内容>
EOF
```

`--if-unchanged-since` 必须是刚刚 `get-page` 读到的 `updated_at`。返回 409 说明这页在你读之后
被别人改了：重新 `get-page`，在最新内容上重做本轮判断再写，不要用自己的版本覆盖。

## 3. 整理页面

依据公共页面指导判断每条认知应保留、修正、合并还是移出。Task 的运作流转留在 Task 自身；页面只保留其背后有证据支持、确实改变这个实体处境的现实事实。

提交的是合并后的完整页面，不是局部 PATCH。整页替换只是一致性与并发边界，不要求重写未受影响的正确内容，也不要求为每类实体填固定章节。

## 4. 收尾

最终回复一句话级别的结论：改了哪几页（`type:id` 加名字）、每页为什么改、还有多少候选没处理。
页面才是真源，不要在回复里复述页面内容。一页都没改时就直接说没有需要改的，并说明依据。

实体页首行作为公共目录索引。运行时会限制目录预览长度，页面正文不为目录另写第二份摘要。
