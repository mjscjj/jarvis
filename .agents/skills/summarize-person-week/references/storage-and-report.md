# Weekly Markdown Workspace and Report

## Contents

- Storage root
- File layout
- Per-day evidence splitting
- Write ownership
- Context index
- Evidence files
- Derived files
- Final report

## Storage Root

Store one week in one directory named by that week's Monday:

```text
<workspace-root>/data/personal-weekly/YYYY-MM-DD/
```

`YYYY-MM-DD` is the Monday that starts the target week. Record the ISO week
(`YYYY-Www`), Monday, Sunday, local timezone, window, and cutoff in
`00-context.md`.

Resolve `<workspace-root>` from the explicit caller argument or the current Git
repository. Do not silently write to a home-directory fallback.

All persisted artifacts are Markdown. Do not store the report's semantic
content in a parallel JSON DTO.

## File Layout

```text
data/personal-weekly/YYYY-MM-DD/
├── 00-context.md
├── 10-evidence-jarvis.md
├── 20-evidence-feishu.md
├── 20-evidence-feishu-<YYYY-MM-DD>.md
├── 30-evidence-engineering.md
├── 30-evidence-engineering-<YYYY-MM-DD|repo>.md
├── 2x-evidence-followup-<topic>-<sequence>.md
├── 40-work-items.md
├── 60-insights.md
├── 90-report-draft.md
└── 99-report.md
```

The unsuffixed evidence files are the lane indexes and summaries. Per-day,
per-project, and per-repository evidence files are optional and should be
created when parallel collection would otherwise collide or overload context.
Follow-up evidence filenames are intentionally open-ended. Use a short topic and
monotonically increasing sequence or time.

## Per-day Evidence Splitting

A week should be collected as a week, not by concatenating seven daily reports.
Use the full seven-day window for wide queries when that is cheaper and more
complete, then split writes by day or project when concurrency or reviewability
benefits.

Good split patterns:

- Feishu message, thread, and meeting workers may each own
  `20-evidence-feishu-<YYYY-MM-DD>.md`.
- Engineering workers may own `30-evidence-engineering-<YYYY-MM-DD>.md` for
  day-based local history or `30-evidence-engineering-<repo>.md` for repository
  and MR state.
- Follow-up workers always write a fresh `2x-evidence-followup-*.md`, even
  when the lead came from a day-specific file.

The main agent consolidates only at barriers. Do not make workers rewrite
lane summaries while other workers are still running.

## Write Ownership

- The main agent owns `00-context.md`, `40-work-items.md`, `60-insights.md`,
  and both report files.
- Each initial collector owns exactly one evidence file or one family of
  disjoint per-day/per-project evidence files assigned before collection.
- Each follow-up collector receives a new `2x-evidence-followup-*.md`.
- Never let concurrent workers edit one file.
- Keep evidence append-only.
- Rewrite derived files when evidence changes, but list the supporting evidence
  IDs.
- Keep an existing `99-report.md` until the new draft passes its self-check.

## Context Index

Keep `00-context.md` concise:

```markdown
# Weekly context — YYYY-Www (YYYY-MM-DD–YYYY-MM-DD)

## Scope
- Principal:
- Identity aliases:
- Timezone:
- ISO week:
- Monday:
- Sunday:
- Window:
- Current cutoff:
- Report kind: preview | final | regeneration

## Durable context
- Active goals:
- Project bindings:
- People relationships:
- Repository and group bindings:

## Run log
### <run timestamp>
- Trigger:
- Started:
- Evidence files:
- Current stage:

## Coverage
- Jarvis:
- Feishu:
- Engineering:

## Shared leads
- <stable ID or URL>: <target lane and question>

## Open material questions
- <question>: <relevant evidence IDs>

## Current synthesis
- <short summary of important supported weekly changes and gaps>
```

Update this file only at barriers: after the Seed, after a collection wave,
after reconciliation, after delta refresh, and after publication.

## Evidence Files

Use the loose evidence format in
[channel-methods.md](channel-methods.md). Preserve source IDs, timestamps,
actors, links or local references, raw observed facts, coverage, exact gaps,
and the source file that owns the record.

Do not duplicate large source bodies when a stable local file or URL exists.
Store a concise excerpt and a precise reference. Include enough primary text to
confirm the fact even if a generated summary is later unavailable.

Evidence IDs must be unique within the weekly workspace. Prefix them by lane
and durable source, for example `FEISHU-2026-07-21-msg-<id>`,
`ENG-jarvis-mr-123-r2`, or `FOLLOWUP-deploy-<run-id>`.

## Derived Files

### `40-work-items.md`

Group cross-source evidence by real weekly work item, not by source and not by
day. For every material item include:

- what changed during the week;
- relation to the principal;
- project, people, events, and artifacts involved;
- activity, output, and observed outcome;
- decisions, commitments, risks, and current state;
- evidence IDs;
- unresolved questions.

### `60-insights.md`

Record candidate cross-item discoveries, supporting evidence, alternative
explanations, and whether to include them. For weekly reporting, explicitly
scan for repeated blockers, emerging themes, attention drift, new collaborators
or projects, deadline signals, and quiet but consequential ownership changes.
Do not force content.

### `90-report-draft.md`

Write the complete candidate report. It may change after follow-up waves or
delta refresh.

### `99-report.md`

Write only after the final checks. This is the canonical report for the week.
On first publication, create it only after `90-report-draft.md` passes its
self-check. On regeneration, keep the prior file untouched until its
replacement passes.

## Final Report

Use first-person Chinese. Keep three stable anchors: `本周概要`, `本周主线`,
and `数据覆盖与完整底账`. The other sections below are the default navigation;
they may grow, merge, rename, or disappear when the evidence calls for it, as
long as the report still covers the principal, projects, people, events,
artifacts, insights, next actions, and coverage.

```markdown
# 我的本周全景 · YYYY-Www（周一–cutoff）

## 本周概要
## 本周主线
## 我：推进、决策与状态
## 项目：我负责范围内的进展
## 人：协作、老板待办与相互承诺
## 事：关键事件、决策与风险
## 物：交付与资产变化
## 关联洞察与本周发现
## 下一步工作台
## 数据覆盖与完整底账
```

### 本周概要

Lead with the most consequential supported progress and the highest-value item
that needs the principal's attention or decision. Use two to four sentences.
Do not repeat the rest of the report.

### 本周主线

This is the main weekly narrative. Group by project or theme, rank by
importance, and put accomplishments before activity. Heavy projects get more
space; untouched projects do not need a placeholder. Cite evidence inline with
stable IDs, MR/commit/document/meeting references, or local evidence files.

### 我：推进、决策与状态

Cover direct work, delegated Agent work, collaboration, decisions, accepted
commitments, pending responses, who is waiting on the principal, and whom the
principal is waiting on. Preserve contribution mode. Do not infer emotion.

### 项目：我负责范围内的进展

Describe state change from week start to cutoff. Include projects with
progress, changed risk, changed priority, or meaningful lack of expected
progress. Do not allocate equal space mechanically.

### 人：协作、老板待办与相互承诺

Include leader assignments, requests, feedback, consensus, disagreement,
dependency, ownership, waiting, and mutual commitments. Do not list ordinary
conversation.

### 事：关键事件、决策与风险

Include meetings, reviews, releases, incidents, approvals, new requests, and
expected events that did not happen. Put the complete meeting ledger in the
final section; include only material meeting effects here.

### 物：交付与资产变化

Include durable documents, code, MR/CR, commits, bug fixes, systems,
configuration, data, reports, Tasks, approvals, tests, deployments, and Agent
artifacts whose state changed. Include remote state when it matters.

### 关联洞察与本周发现

After writing the main lines, actively ask what the principal might have missed:
new groups or projects, repeated mentions, a topic heating up, a commitment
approaching its deadline without movement, or a small signal worth remembering.
Keep only evidence-backed discoveries. Label incomplete but useful patterns as
hypotheses.

### 下一步工作台

Derive from unresolved commitments, risks, choices, waiting, and known due
times. Separate what the principal should do, what others owe, what to monitor,
and what can stop. Do not invent plans.

### 数据覆盖与完整底账

State window, cutoff, each evidence lane's coverage, permission or pagination
gaps, unresolved claims, and evidence-file links. Include the complete meeting
ledger and concise indexes of important messages, documents, Tasks/Runs,
commits/MRs, deployments, and other artifacts.

## Style and Deduplication

For a material item, communicate:

```text
what changed -> relation to me -> why it matters -> current state -> next attention
```

Write the full item once in its primary section. Other sections may reference
the relationship, person, or artifact without retelling the full story.

Prefer verified state change, output, accepted decision, or observed outcome
over activity volume. Use active verbs and accomplishment-first phrasing. Keep
routine activity in the ledger unless it explains a material result. Preserve
uncertainty and evidence links; write `[待补]` where a metric or state is
missing.
