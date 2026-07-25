# One-Pass Evidence Collection and Reconciliation

## Source Lanes

### Jarvis internal facts

The caller writes deterministic same-day evidence before the Agent starts:

- authored messages already ingested by Jarvis;
- Todo and Task lifecycle events;
- linked ExecutionRuns and Agent session IDs;
- project, group, and repository bindings;
- relevant ProjectEvents as context only.

Treat this as the compact seed and relationship layer. Old open work without a
same-day event is not same-day progress.

### Feishu work evidence

Use lark-cli with the current user identity when personal visibility is needed.
Collect one bounded same-day inventory plus seed-linked detail:

- authored messages and explicit mentions needed for daily counts;
- all same-day meetings involving the principal, then the best available
  Minutes, Note, transcript, decisions, plans, and action items for each;
- same-day documents created or substantively edited by the principal;
- thread context for meaningful inbound and outbound communication;
- directly referenced tasks, approvals, people, or mail.

Do not scan every Drive item, task list, OKR, approval definition, mail folder,
or lark-cli domain. Do not bulk-export or recompute Base/Sheets datasets for a
daily report; retain the published same-day result and its source link unless
the user explicitly asked for the underlying analysis. Attendance, last editor,
ownership, and message volume alone do not prove contribution.

### Engineering execution evidence

Start from linked Task, Run, Session, repository, commit, and MR/CR identifiers.
Collect:

- authored commits and MR/CR/review activity needed for daily counts;
- actual output and terminal state for linked Agent sessions;
- exact relevant Git changes and remote MR/CR state;
- tests, deployment, release, configuration, or runtime results directly tied
  to those changes;
- one bounded author/date repository discovery only when the seed has no useful
  engineering binding.

Do not read every Codex transcript, scan every cloned repository, or query every
bytedcli engineering domain. When Jarvis already has a Run result, read the
session's final output/effects and directly linked test evidence instead of
replaying the full transcript. Separate direct work, delegated Agent work,
collaboration, assignment, and discussion.

## Parallel Assignment

Start one Feishu worker and one engineering worker concurrently. These are the
only subagents in the run.

Give each worker:

- the principal identity filters;
- natural-day window and cutoff;
- relevant seed IDs and durable bindings;
- its source boundary;
- one exclusive output file;
- the evidence format below;
- a target of about two minutes.

A worker must not spawn another agent. It may make several read-only tool calls
inside its lane, but it must stop after this collection pass and record any
unresolved or inaccessible material as a gap.

## Evidence Markdown Contract

Use a small, loose envelope:

```markdown
# <lane> evidence — YYYY-MM-DD

## Scope
- Principal:
- Window:
- Cutoff:
- Query plan:

## Coverage
- <scope>: complete | empty | partial | error | unavailable
  - Query/cursor:
  - Count:
  - Truncated:
  - Error:

## Daily counts
- <metric>: <exact N | at least N | unknown>
  - Scope:
  - Deduplication key:
  - Note:

## Evidence

### <stable evidence ID> — <short subject>
- Source kind:
- Source ID:
- Occurred at:
- Actor:
- Project binding:
- Relation to principal:
- Artifact URL or local reference:
- Observed facts:
- Raw excerpt or diagnostic reference:

## Gaps
- <exact access, pagination, attribution, or state gap>
```

Do not require a semantic field that the source cannot support. Use `unknown`
instead of inventing a value. Evidence IDs must be unique within the day and
readable, for example:

- `JARVIS-task-84`
- `FEISHU-meeting-<meeting-id>`
- `ENG-mr-<repo>-<iid>-<revision>`

For the Feishu lane, include counts for:

- principal-authored messages;
- explicit @mention messages and unique human senders;
- direct-message counterpart counts when discoverable;
- meetings and total duration;
- documents created and substantively edited.

For the engineering lane, include counts for:

- authored commits, with repository scope;
- MR/CR created, updated, reviewed, and merged;
- delegated Agent runs by terminal state;
- tests, deployments, and releases.

Use an exact number only when the recorded query covers the whole intended
scope. A seed-only or repository-bounded result is a lower bound. Do not write
zero after a failed or unavailable query.

## Reconcile Once

After both workers finish, the main agent reads the three current evidence
files and proceeds directly to the report.

Bind a project in this order:

1. frozen Todo/Task project context;
2. group/project or repository/project binding;
3. explicit project reference in the artifact;
4. evidence-backed inference;
5. `未归属`.

Merge evidence referring to the same deliverable, decision, incident,
commitment, or risk. Prefer stable Task/Run/Session, meeting, document, MR/CR,
commit, deployment, and artifact identifiers over keyword similarity.

For each material work item retain:

- what changed;
- relation to the principal;
- activities, durable outputs, and observed outcomes;
- involved projects, people, events, and artifacts;
- decisions, commitments, risks, and current state;
- evidence IDs and explicit gaps.

For every meeting retain its title, time, participants, conclusions, decisions,
plans, Todo with owners, unresolved questions, and principal impact.

For communication, group related messages into a person-topic record. Retain
who initiated the meaningful exchange, both sides' material statements, the
current conclusion, and pending discussion or Todo. Do not emit one evidence
item per routine chat message.

When collected sources conflict, report the conflict or use cautious wording;
do not start another research stage.

## Context and Time Discipline

- Keep `00-context.md` as a small run index, not a transcript.
- Pass collectors seed IDs and short excerpts, not every raw file.
- Read each current evidence file once for synthesis.
- Do not reload the whole Skill package into each worker; give it only its lane
  method and relevant capability section.
- Prefer stable URLs and local paths over copying large documents.
- Treat two minutes per collector and three to five minutes total as the normal
  target. The outer runtime timeout is only a runaway cap.

## Completion

Before writing `99-report.md`:

1. ensure the three lanes have explicit coverage;
2. ensure daily counts carry scope and exact/lower-bound/unknown semantics;
3. ensure every discovered meeting has one evidence record;
4. merge repeated message threads, lifecycle events, and duplicate narratives;
5. preserve contribution mode and uncertainty;
6. expose partial sources, access errors, pagination limits, and unresolved
   states;
7. distinguish finished results, topics requiring human discussion, and
   confirmed executable next steps.

Write the report directly. Do not create extra analysis-stage files or workers.
