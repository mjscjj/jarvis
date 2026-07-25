# Daily Markdown Workspace and Report

## Storage Root

Store one natural day in one directory:

```text
<workspace-root>/data/personal-daily/YYYY-MM-DD/
```

Resolve `<workspace-root>` from the caller argument or the current Git
repository. Do not silently write to a home-directory fallback.

Markdown files are the semantic source of truth. The database may keep only the
API/UI projection.

## Minimal File Layout

```text
data/personal-daily/YYYY-MM-DD/
├── 00-context.md
├── 10-evidence-jarvis.md
├── 11-evidence-jarvis-refresh-<timestamp>.md
├── 20-evidence-feishu.md
├── 21-evidence-feishu-refresh-<timestamp>.md
├── 30-evidence-engineering.md
├── 31-evidence-engineering-refresh-<timestamp>.md
└── 99-report.md
```

The first run uses `10`, `20`, and `30`. A regeneration keeps old evidence and
uses one fresh file per lane. Do not create stage files merely to restate the
same evidence.

## Write Ownership

- The main agent owns `00-context.md` and `99-report.md`.
- The Feishu collector owns exactly one `20/21` evidence file.
- The engineering collector owns exactly one `30/31` evidence file.
- The caller owns the current `10/11` Jarvis evidence file.
- Never let concurrent workers edit the same file.
- Keep evidence append-only. Prepare the report in memory and write the
  canonical file once.

## Context Index

Keep `00-context.md` compact:

```markdown
# Daily context — YYYY-MM-DD

## Scope
- Principal:
- Identity aliases:
- Timezone:
- Window:
- Current cutoff:
- Report kind: preview | final | regeneration

## Durable context
- Active goals:
- Project bindings:
- People relationships:
- Repository and group bindings:

## Run log
### <run id>
- Started:
- Current evidence files:
- Current stage:

## Coverage
- Jarvis: complete | partial | empty | error | unavailable
- Feishu: complete | partial | empty | error | unavailable
- Engineering: complete | partial | empty | error | unavailable

## Current synthesis
- <short supported picture and material gaps>
```

Update it after the current collection finishes and after publication. Do not
turn it into a second report.

## Evidence Files

Use the loose evidence format in
[channel-methods.md](channel-methods.md). Preserve stable source IDs,
timestamps, actors, links or local references, observed facts, coverage, and
exact gaps.

Do not copy large source bodies when a stable local file or URL exists. Keep a
concise excerpt and precise reference. A lifecycle with multiple internal
events should normally be one evidence item with a compact timeline.

## Final Report

Use first-person Chinese and this stable navigation:

```markdown
# 我的日报 · YYYY-MM-DD

## 今日数据
## 今天的会议
## 消息与协作
## 项目与工作进展
## 已完成事项
## 待讨论事项
## 后续计划
## 关联、洞察与其他发现
## 数据说明
```

Keep only these nine top-level headings fixed. Use smaller subheadings when
they help and omit empty optional subsections.

### 今日数据

Open with a compact Markdown table:

```markdown
| 项目 | 数量 | 说明 |
|---|---:|---|
| 我发出的消息 | 18 | 当前已采集的工作消息 |
| @我的人 | 4 人 / 7 次 | 去重人数 / @消息次数 |
| 会议 | 3 场 / 2小时 | 已结束并涉及我的会议 |
| 新建或实质编辑文档 | 2 篇 | 新建 1，编辑 1 |
| Commit | 至少 3 个 | 覆盖 2 个已绑定仓库 |
```

Prefer these metrics when available:

- messages authored by the principal;
- explicit @mention events and unique human senders;
- meaningful direct-message counterpart count;
- meetings and total scheduled or actual duration;
- documents created and documents substantively edited;
- authored commits;
- MR/CR created, updated, reviewed, merged;
- delegated Agent runs and their succeeded/failed/pending split;
- tests, deployments, and releases.

Do not pad the table with meaningless zeroes. Keep a useful zero, such as
`会议 0`, when it answers an obvious daily question. Use `至少 N` for bounded
coverage and `未知` when the source cannot support a number.

Count definitions:

- `@我的人` is unique human senders who explicitly mentioned the principal;
  also show the number of mention messages.
- A meeting is a non-cancelled same-day meeting involving the principal.
  Distinguish calendar presence from confirmed attendance when known.
- A document counts only when created or substantively edited by the principal
  that day. Reading, ownership, or being the last editor without a same-day
  revision does not count.
- A commit counts by author identity and commit time inside the window. State
  the repository scope when global discovery is unavailable.
- Deduplicate all counts by stable message, meeting, document, commit, MR/CR,
  Task, or Run ID.

### 今天的会议

Write one `### <time> · <meeting title>` block per meeting. Include:

- who attended or the key participants;
- what was actually discussed;
- conclusions and decisions;
- Todo, owner, and deadline if present;
- plans and remaining disagreements or unanswered questions;
- what the meeting changes for me.

Use transcript, Minutes, or Note when available. If only the calendar record is
available, keep the meeting and say `无可读纪要，结论未知`; do not invent a
summary. If there were no meetings, say so in one sentence.

### 消息与协作

Prefer these two subheadings:

```markdown
### 谁找了我
### 我找了谁
```

Group by person and topic, not message chronology. For each meaningful exchange
state:

- person and context;
- what they asked, informed, challenged, or promised;
- what I replied, requested, or committed to;
- current conclusion;
- Todo, pending discussion, or next push.

Do not repeat a bidirectional conversation twice. Put it under the direction
that started the meaningful exchange and mention both sides there. Skip
greetings, emoji, repeated status pings, and routine noise.

### 项目与工作进展

Group concrete changes by real project. Include direct work, delegated Agent
work, documents, code, MR/CR, tests, deployments, incidents, and decisions.
State the current status directly: `完成`, `待评审`, `未合并`, `待回复`,
`运行失败`, or another concrete phrase.

Do not write a generic project-health essay. If an event does not belong to a
known project, use a natural topic heading instead of forcing a binding.

### 已完成事项

List the day's finished items in a compact form. Each item must have an
observable finished result such as a delivered document, completed analysis,
passed test, accepted decision, merged change, resolved incident, or explicit
handoff.

Do not count `Agent Run succeeded` by itself as completion when the requested
artifact, merge, deployment, approval, or user-visible result is still pending.
Do not repeat the full project narrative; write `事项 → 完成结果 → 产物/状态`.

### 待讨论事项

Include only topics that still need human discussion, alignment, or a decision:

- a choice with material alternatives;
- unclear ownership or scope;
- disagreement or an unanswered question;
- a proposal waiting for approval;
- a risk that requires someone to choose a response.

Name who should participate and what needs to be decided. Do not mix ordinary
execution Todo into this section.

### 后续计划

Turn confirmed next work into concrete actions. Prefer:

```text
动作 → 负责人 → 已知时间或触发条件 → 预期结果
```

Separate what I will do from what I am waiting for when useful. Keep risks and
dependencies next to the affected action. Do not invent owners, deadlines, or
accepted commitments.

### 关联、洞察与其他发现

This is the open section. Add a useful cross-meeting, cross-person, or
cross-project connection; an unexpected pattern; a neglected issue; or
something the principal is likely to have missed. State the concrete facts
first, then the interpretation. It is valid to write one short sentence when
there is no worthwhile insight.

### 数据说明

Keep this compact. Record:

- time window and cutoff;
- per-lane coverage and exact gaps;
- which counts are exact, lower bounds, or unknown;
- a compact source index with links or evidence IDs for material items.

Do not copy the entire evidence inventory into the report.

## Writing Style

- Prefer names, verbs, numbers, titles, and current states.
- Keep paragraphs short. Use tables and bullets when they reduce reading time.
- Put the fact before the interpretation.
- Say `MR 还没合并` instead of a paragraph about evidence boundaries.
- Do not attach evidence codes to every sentence. Keep them in `数据说明`.
- Avoid consultant filler such as `进一步收敛`, `形成闭环`, `释放信号`,
  `值得关注`, `从更高维度看`, or `今天最重要的变化是` unless the next words
  state a concrete fact that cannot be said more directly.
- Do not infer emotion, motivation, consensus, completion, or causal impact.
- Write each fact once in its natural section and reference it briefly later.
