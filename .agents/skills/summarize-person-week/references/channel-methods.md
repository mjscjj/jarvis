# Weekly Evidence Collection, Follow-up, and Reconciliation

## Contents

- Source lanes
- Initial parallel wave
- Evidence Markdown contract
- Follow-up waves
- Reconciliation
- Self-check
- Context management
- Completion rules

## Source Lanes

### Jarvis internal facts

Collect deterministic weekly state changes and carried commitments:

- Todo and Task events;
- linked TaskEvent, ExecutionRun, and ProjectEvent records;
- frozen context snapshots;
- group/project and repository/project bindings;
- principal, colleague, leader, and project facts needed for attribution;
- meeting ingestion records and locally available transcripts;
- week-start open commitments, risks, and follow-ups that had movement or
  contact during the week;
- current state only for items touched during the week or explicitly carried as
  an outstanding commitment or risk.

Do not treat an old open item or project background as weekly progress.

### Feishu work evidence

Use lark-cli with the current user identity where personal visibility is
required. Collect relevant:

- authored messages, replies, threads, mentions, requests, and feedback across
  the seven-day window;
- substantive document or Wiki changes;
- calendar events as meeting-discovery and attendance context;
- ended meetings, VC records, Minutes, Notes, transcript-backed decisions, and
  action items;
- tasks, approvals, OKRs, or mail only when a Seed, weekly interaction, or
  material analysis question makes them relevant.

Resolve every discovered ended meeting to the best available primary artifact.
Keep unreadable meetings with the exact permission, readiness, or lookup gap.
Attendance, ownership, last editor, or message volume alone does not prove
personal contribution.

### Engineering execution evidence

Collect attributable:

- Codex or other agent sessions initiated for the principal's work;
- changed files, tests, commits, and explicit handoff state;
- authored or reviewed MR/CR records with exact revision, action, state, and
  URL;
- deployment, release, configuration, monitoring, and runtime acceptance
  evidence;
- internal knowledge or issue records needed to explain a material result.

Separate work directly performed by the principal from work delegated to an
agent. A session title, commit subject, branch, or MR update does not by itself
prove completion or deployment.

Use wide time ranges where the source supports them. `git log --since`,
bytedcli MR/commit queries, and CI history are usually cheaper and more
complete as one weekly query than seven isolated day queries.

## Initial Parallel Wave

Create a concise Seed from deterministic facts before external collection:

- identity aliases;
- week window, ISO week, Monday, Sunday, timezone, and cutoff;
- authored message IDs and chat IDs;
- meeting IDs and local capture state;
- Todo, Task, Run, and Session relations;
- relevant repositories, commits, MR links, documents, and projects;
- week-start commitments or risks that remain open or had contact this week.

Start the three source lanes concurrently. Inside Feishu, parallelize
independent message/thread, meeting/minutes, document/task, and per-day queries
when practical. Inside engineering, parallelize Session, Git/MR,
deployment/runtime, per-repository, and per-project queries.

Collectors must investigate only their lane. They may connect records within
that lane, but they must not infer across lanes or write final conclusions.

## Evidence Markdown Contract

Use loose Markdown with a small stable envelope:

```markdown
# <lane, per-day file, or follow-up title>

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

## Evidence

### <stable evidence ID> — <short subject>
- Source kind:
- Source ID:
- Occurred at:
- Observed at:
- Actor:
- Project binding:
- Relation to principal:
- Artifact URL or local reference:
- Observed facts:
- Raw excerpt or diagnostic reference:

## Follow-up leads
- <stable ID or URL>: <why it may matter>

## Gaps
- <exact unresolved access, pagination, attribution, or state gap>
```

Do not require every semantic field when the source cannot support it. Preserve
stable IDs, timestamps, actor, source reference, and raw facts. Use `unknown`
instead of inventing a value.

Evidence IDs must be unique within the weekly workspace and readable, for
example:

- `JARVIS-task-84-event-3`
- `FEISHU-2026-07-21-meeting-<meeting-id>`
- `FEISHU-2026-07-24-msg-<message-id>`
- `ENG-mr-<repo>-<iid>-<revision>`
- `ENG-commit-<repo>-<short-sha>`
- `FOLLOWUP-deploy-<run-id>`

## Follow-up Waves

After the first barrier, identify material open questions rather than declaring
collection complete forever.

Good follow-up triggers include:

- a Feishu message names an MR, document, Task, system, or person whose state
  affects the conclusion;
- an engineering Session cites a meeting decision or document;
- a Task says work finished but no output or verification is visible;
- a meeting creates a commitment whose acceptance or current state is unclear;
- cross-source timestamps or attribution conflict;
- a candidate weekly theme depends on facts not yet collected;
- a repeated mention, group, project, or deadline signal may change the
  principal's next attention.

Dispatch independent follow-ups concurrently. Give each worker one question,
the relevant stable leads, and one new output file. Keep follow-ups narrow:
retrieve the missing primary evidence or state, not another broad weekly scan.

Permit additional waves while they can change a material report claim. Stop
when the claim is supported, rejected, explicitly unresolved, or access is
exhausted and recorded.

## Reconciliation

### Bind projects

Apply this order:

1. frozen Todo/Task project context;
2. group/project or repository/project binding;
3. explicit project reference in the artifact;
4. evidence-backed inference;
5. `未归属`.

Do not classify solely by keyword similarity when a durable binding exists.

### Merge work items

Merge evidence referring to the same deliverable, decision, incident,
commitment, risk, or weekly theme. Prefer:

- Task/Todo/Run/Session relations;
- meeting ID or action ID;
- document token or artifact URL;
- MR/CR ID and exact revision;
- commit ancestry;
- deployment or runtime identifier;
- explicit normalized subject only when no stable join exists.

For each work item retain, in natural language:

- what changed across the week;
- relation to the principal;
- project, people, events, and artifacts involved;
- activities;
- durable outputs;
- observed outcomes;
- decisions, commitments, and risks;
- evidence IDs;
- conflicts, confidence, and unresolved gaps.

Use messages and meetings to establish intent, discussion, and decisions. Use
durable artifacts and observed system state to prove output and outcome.

### Analyze progression

- Activity is evidence of work, not automatically an accomplishment.
- Output requires an attributable artifact, accepted conclusion, review, fix,
  or delivered change.
- Observed Outcome requires verified effect or final state.
- Weekly progression should explain state change from Monday to cutoff when the
  evidence supports it.

Keep:

- decisions with authority and whether proposed, accepted, or superseded;
- commitments with requester, owner, acceptance evidence, due time, and state;
- risks with affected goal, impact, owner, mitigation, and evidence;
- relationship changes that affect coordination or responsibility;
- durable artifacts whose state changed.

Rank by observed outcome, delivered output, accepted decision, material
milestone, explicit commitment, risk requiring action, and emerging signal.
Drop routine activity unless it explains one of these.

## Self-check (no separate verification stage)

All evidence here is self-collected, so there is no verifier fan-out and no
claim ledger. Fold one judgment into reconciliation: keep what a source *says*
distinct from what actually happened. As you write work items, ask:

- Does a durable artifact or system state back the outcome, or only a message,
  title, or generated summary?
- Do "completed", "merged", "deployed", "online", or "unblocked" have
  corresponding terminal-state evidence?
- Did the change occur inside the week window?
- Do sources conflict on state or attribution? Prefer the durable artifact and
  note the conflict.

When a material outcome rests only on a message and its real state matters,
dispatch one narrow follow-up to fetch the terminal state. Do not launch a
verification wave or write `confirmed/rejected/unresolved` records. Do not
polish weak evidence into certainty, and do not wrap every self-collected fact
in "unproven" caveats — reserve explicit uncertainty for outcomes whose
terminal state is genuinely unknown.

An insight should rest on evidence or work-item anchors; a single primary
source can establish it. Otherwise label it as a hypothesis.

## Context Management

- Keep `00-context.md` as the small run index: scope, plan, file list, coverage,
  leads, open questions, and current synthesis state.
- Give collectors the Seed and their lane instructions, not all prior evidence.
- Let every concurrent worker write a separate file.
- Prefer per-day or per-project evidence files when a lane can run many workers
  at once.
- At a barrier, summarize new evidence and open questions in `00-context.md`.
- Give follow-up workers only the evidence IDs and excerpts needed for their
  question.
- Read raw evidence files selectively with `rg` or headings; do not repeatedly
  load every file into every agent.
- Keep final ranking, deduplication, and writing in one main context.

## Timeouts and Long-running Work

Weekly collection is allowed to take longer than daily collection. When the
runtime exposes timeouts, request at least 60 minutes for the whole skill, allow
up to 25 minutes for an initial external collector, and up to 12 minutes for a
targeted follow-up.

Poll long-running commands instead of blocking silently for more than 60
seconds. If a collector is slow but showing progress, let it continue within
the relaxed budget. If it reaches the limit, record exact partial coverage and
the command or cursor needed to resume.

## Completion Rules

Before writing the report:

1. Ensure every planned lane has explicit coverage.
2. Ensure every material output, outcome, decision, commitment, risk, and
   insight points to evidence IDs.
3. Remove duplicate work items and unsupported causal language.
4. Preserve direct, delegated, collaborative, assigned, discussed, and
   externally affecting relations.
5. Expose every partial, error, unavailable source, pagination limit, and
   unresolved claim.
6. Perform a narrow delta refresh for an in-progress week.

Fail on invalid identity, time window, storage, or a completely unavailable
required lane. Permit a visibly partial report for localized access gaps only.
