---
name: summarize-person-day
description: Build and persist a concrete, principal-centered Chinese daily report. Use when the user asks for 日报、个人总结、今天做了什么、今天和谁沟通了什么、会议总结、每日工作回顾, or regeneration of one date. Report daily counts, meetings, two-way communication, project progress, completed items, topics needing discussion, next plans, and useful open-ended discoveries. Store Markdown under data/personal-daily/YYYY-MM-DD.
---

# Write My Daily Report

Reconstruct one natural day around the principal. First lay out the day clearly;
then draw only the conclusions the facts support.

Write like a sharp personal assistant who knows the work, not a consultant
producing an abstract management memo.

## Load Only What Is Needed

Read [references/storage-and-report.md](references/storage-and-report.md) for
the workspace, exact report structure, metric definitions, and writing style.

Use the other references progressively:

- Read [references/channel-methods.md](references/channel-methods.md) when
  assigning collectors.
- Give each collector only the relevant sections of
  [references/context-and-capabilities.md](references/context-and-capabilities.md).

## Resolve the Day

Resolve the principal identities, `YYYY-MM-DD` date, timezone, and cutoff. Use
`[local 00:00, next local 00:00)` for a finished day and stop at the current
cutoff for an in-progress day.

Use tools to resolve discoverable identity or binding facts before asking the
user. Background and prior goals guide relevance; they do not prove same-day
work.

## Initialize the Workspace

Run:

```bash
bash <skill-dir>/scripts/init-day.sh <YYYY-MM-DD> [workspace-root]
```

Store the day under:

```text
data/personal-daily/YYYY-MM-DD/
```

The caller has already written the deterministic Jarvis evidence file for this
run. Record the Run ID, cutoff, and the three current evidence paths in
`00-context.md`.

## Collect Once in Parallel

Use exactly two collector subagents:

1. one Feishu collector;
2. one engineering collector.

Start them concurrently. A collector must not spawn another agent. Give each
collector the compact Jarvis seed, its source boundary, the current window,
and one exclusive Markdown output path. Do not give it the full conversation
or ask it to write the report.

The Feishu collector must return:

- daily counts for messages, people who explicitly @mentioned the principal,
  meetings and meeting duration, and documents created or substantively edited;
- one record for every meeting, including conclusions, decisions, plans,
  action items, unresolved questions, and the principal's role;
- meaningful inbound and outbound communication grouped by person and topic.

The engineering collector must return:

- daily counts for authored commits, MR/CR activity, code reviews, delegated
  Agent runs, tests, deployments, and releases where discoverable;
- concrete project and artifact state changes tied to the principal.

Use exact counts only when the query scope covers the day. Otherwise write
`至少 N` or `未知` and name the missing scope. Do not turn a partial search into
an exact zero.

Use published results and final Agent outputs directly. Do not recompute
datasets, replay full transcripts, scan every repository, or enumerate every
CLI capability.

Target about two minutes per collector and three to five minutes for the whole
run. A slow or inaccessible source becomes an explicit coverage gap.

There is no second research wave and no separate claim-checking agent. Once the
two collectors finish, proceed to synthesis. Do not create extra collector,
review, or other workers.

## Write From Facts Outward

The main agent reads the current Jarvis, Feishu, and engineering evidence files
once, then:

1. build the top statistics table, preserving exact, lower-bound, and unknown
   qualifiers;
2. write every meeting separately;
3. group messages by person and topic, separating who contacted me from whom I
   contacted;
4. merge repeated messages, meetings, Tasks, Runs, commits, and documents that
   belong to the same work item;
5. summarize project progress and durable output;
6. extract completed items without repeating the full project narrative;
7. separate topics that still need discussion or a decision from executable
   next steps;
8. write the next plan with owners and known timing;
9. add cross-item connections or unexpected findings only when they are useful.

Use a person's real name, a project's real name, the concrete event, and the
current state. If a fact is unknown, say exactly what is unknown in one short
phrase. Do not repeatedly explain evidence theory.

Compose the full report in the main agent and write `99-report.md` once. Do not
create intermediate work-item, insight, draft, or claim-check files. Keep an
existing `99-report.md` untouched until the replacement content is ready.

Use the title and nine headings from
[references/storage-and-report.md](references/storage-and-report.md). Preserve
those navigation headings, but freely add or omit smaller subheadings according
to the day. Write in first-person Chinese.

Keep evidence IDs out of normal prose. Use normal links where useful and put a
compact evidence index in `数据说明`. A daily report should be easy to read,
not look like an audit log.

## Regeneration

For regeneration, keep prior evidence files append-only and assign one fresh
Feishu file and one fresh engineering file. Reuse immutable facts by reference;
collect the current state once for mutable items. Synthesize from the current
run's files instead of rereading every historical file in the day directory.

## Finish

Before finishing:

- ensure `99-report.md` belongs to this Run ID and date;
- ensure counts expose their scope and incomplete counts are not presented as
  exact;
- ensure every discovered meeting appears once;
- ensure meaningful inbound and outbound communication is covered without
  copying chat transcripts;
- ensure completed items have a finished result, discussion items require
  human alignment or a decision, and plans contain concrete next actions;
- remove duplicate narrative, empty filler, and unsupported causal language;
- record cutoff, coverage, permission errors, truncation, and unresolved gaps;
- never send or publish externally unless the user explicitly asks.

Fail visibly on an invalid identity, time window, storage path, or missing
required evidence file. A localized source failure may produce a visibly
partial report.
