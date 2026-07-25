---
name: summarize-person-week
description: Build and persist an evidence-backed, principal-centered weekly panorama. Use when the user asks for 周报、个人周报、我的本周全景、what I did this week, a weekly recap, or regeneration of one week. Creates a Markdown workspace under data/personal-weekly/<Monday>, collects Jarvis, Feishu, and engineering evidence in parallel across the week, permits later targeted evidence waves during analysis, reconciles self-collected evidence, and writes the final Chinese report.
---

# Build the Principal's Weekly Panorama

Act as the principal's digital extension. Reconstruct what changed around the
principal during one local week; do not merely concatenate daily activity.

Before planning, read:

1. [references/context-and-capabilities.md](references/context-and-capabilities.md)
   for the principal background and the lark-cli/bytedcli capability map.
2. [references/storage-and-report.md](references/storage-and-report.md) for the
   Markdown workspace and final report structure.
3. [references/channel-methods.md](references/channel-methods.md) for evidence
   collection, follow-up research, and reconciliation.

## Resolve the Week

Resolve the principal to stable identities, the target Monday `YYYY-MM-DD`, ISO
week, timezone, week window, and cutoff. Use `[Monday 00:00, cutoff)` in local
time. For a finished week, cutoff is the following Monday 00:00. For the
current week, cutoff is the current time; the regular Friday 17:00 run reports
Monday through that trigger time.

Use tools to resolve identity, projects, people, repositories, groups, and
history before asking the user. Ask only when a missing choice cannot be
discovered and would materially change the report.

Treat profile, project background, goals, relationships, and prior commitments
as context. They do not prove that work happened this week.

## Initialize the Markdown Workspace

Resolve this skill's directory, then run:

```bash
bash <skill-dir>/scripts/init-week.sh <Monday YYYY-MM-DD> [workspace-root]
```

The default workspace root is the current Git repository. Store all artifacts
under:

```text
data/personal-weekly/YYYY-MM-DD/
```

Do not overwrite existing evidence. Reuse the week's directory, append a new
run entry to `00-context.md`, and create new per-day, per-project, or follow-up
files as needed.

## Plan the Investigation

Write the identity mapping, time window, cutoff, known bindings, initial source
lanes, expected evidence, and known limits into `00-context.md`.

Start with three independent lanes:

- Jarvis internal facts;
- Feishu work evidence;
- engineering execution evidence.

These are default lanes, not a closed evidence list. Split work inside a lane
when independent queries can run in parallel, especially by day, project, or
repository. Add narrow follow-up lanes later when analysis discovers material
unanswered questions.

Assign one output file to each concurrent worker. Never let two workers edit the
same Markdown file.

## Collect in Parallel

Run the three initial lanes concurrently:

- collect Jarvis facts with deterministic database or `jarvis-tools` queries;
- delegate Feishu investigation to bounded workers;
- delegate engineering investigation to bounded workers.

Give each worker only:

- the principal identity filters;
- the week window and cutoff;
- relevant Seed IDs and durable bindings;
- its source boundary;
- its assigned output file;
- the required coverage and evidence conventions.

Do not give collectors the full conversation or ask them to write the report.
Require raw IDs, timestamps, URLs or local references, observed facts, coverage,
gaps, and promising follow-up leads.

Allow slow but progressing collectors to continue. When the runtime exposes
timeouts, request at least 60 minutes for the whole skill, allow up to 25
minutes for an initial external collector, and up to 12 minutes for a targeted
follow-up. Poll long-running commands instead of blocking silently for more than
60 seconds.

## Share Evidence at Barriers

Keep initial collectors independent. Share the Seed before collection, but do
not stream one collector's interpretations into another collector.

After the first wave finishes:

1. record every lane's coverage and gaps in `00-context.md`;
2. read the evidence files;
3. extract only stable cross-domain leads such as message IDs, meeting IDs,
   document tokens, Task/Run/Session IDs, repository paths, commit SHAs, and
   MR/CR URLs;
4. dispatch narrow follow-up queries in parallel for material leads;
5. write each follow-up to a new `2x-evidence-followup-*.md` file.

Repeat targeted evidence waves whenever analysis exposes a material gap. Do not
force a fixed number of waves. Stop when material claims are supported,
rejected, explicitly unresolved, or blocked by a recorded access gap.

## Reconcile Without Losing Raw Evidence

Keep source observations append-only. Never rewrite raw evidence to match a
later conclusion.

In the main agent:

1. bind evidence to projects using frozen context, group/repository bindings,
   explicit references, and only then evidence-backed inference;
2. merge the same deliverable, decision, commitment, incident, risk, or weekly
   theme across sources;
3. distinguish direct work, agent-delegated work, collaboration, assignment,
   discussion, and external changes that affect the principal;
4. analyze `Activity -> Output -> Observed Outcome` without inventing missing
   stages;
5. retain decisions, commitments, risks, people relationships, and durable
   artifacts as orthogonal facts;
6. write reconciled work items to `40-work-items.md`, with evidence IDs and
   unresolved questions.

Use stable IDs for joins. Keep semantic analysis as natural language or loose
Markdown, not a large rigid DTO.

## Self-check Before Writing

All evidence is self-collected, so there is no separate verification stage and
no verifier workers. The one thing to guard against is mistaking what a source
*says* for what actually happened. As you reconcile, keep a source's claim
distinct from its terminal state:

- a Feishu message, title, or generated summary reports intent or a status
  update, not proof of a merged, deployed, online, or accepted outcome;
- "completed", "merged", "deployed", "online", or "unblocked" needs
  corresponding terminal-state evidence (MR/release/deployment/Task state),
  otherwise describe it as in-progress or reported;
- when sources conflict on state or attribution, prefer the durable artifact or
  system state and note the conflict;
- only count changes inside the week window as weekly results.

When a material outcome rests only on a message and its real state matters,
dispatch one narrow follow-up (not a verifier) to fetch the terminal state.
Do not launch a separate verification wave or write a claim ledger; fold this
judgment directly into `40-work-items.md` and the report. Do not polish weak
evidence into certainty, but do not wrap every self-collected fact in
defensive "unproven" caveats either — reserve explicit uncertainty for
outcomes whose terminal state is genuinely unknown.

## Discover What Was Not Obvious

After work items exist, examine relationships across projects, people, events,
and artifacts. Look for:

- repeated blockers across projects;
- new or changing collaborator dependencies;
- decisions with effects beyond one item;
- emerging reusable assets or directions;
- attention that diverged from stated priorities;
- expected events that did not happen;
- small signals likely to grow;
- hidden contribution such as finding a root cause, preventing a wrong path,
  clarifying ownership, or unblocking others;
- new groups, new projects, repeated mentions, or approaching commitments the
  principal may have missed.

Write candidates to `60-insights.md`. Keep only evidence-backed insights in the
report. Label a useful but incomplete pattern as a hypothesis requiring further
observation. Do not force an insight when none is supported.

## Refresh Before Writing

For an in-progress week, perform one narrow delta query from the prior cutoff to
the new cutoff. Refresh mutable items already in the report candidate set:
Tasks, Runs, MRs, approvals, deployments, and explicit commitments.

Append new evidence and re-analyze only affected work items. Do not restart the
entire investigation unless the scope changed materially.

## Write One Coherent Report

Use one main writer for the final report. Do not draft report sections in
parallel.

Write `90-report-draft.md`, check every material statement against evidence,
then publish `99-report.md`. If no canonical report exists yet, create it only
after the draft passes its self-check. If one already exists, leave it
untouched until the replacement draft passes. Preserve the stable anchors from
[references/storage-and-report.md](references/storage-and-report.md), while
letting section length, subheadings, emphasis, and discoveries follow the week.

Write in first-person Chinese from the principal's perspective. Lead with
accomplishments, state change, accepted decisions, and observed outcomes before
activity. State what changed, the principal's relation to it, why it matters,
current state, and what deserves attention next. Avoid source-by-source or
day-by-day narratives and repeated facts.

## Finish Transparently

Before finishing:

- confirm every output, outcome, decision, commitment, risk, and material
  insight has evidence IDs;
- remove duplicates and unsupported causal language;
- preserve contribution mode;
- record cutoff, coverage, partial sources, permission errors, and unresolved
  claims;
- keep the previous `99-report.md` until the new draft passes its self-check;
- never send or publish the report externally unless the user explicitly asks.

Fail visibly on invalid identity, time window, storage, or an unavailable
required evidence lane. A localized unreadable meeting or document may produce
a report only when the gap is stated prominently in coverage.
