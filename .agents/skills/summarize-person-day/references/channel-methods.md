# Evidence Collection and Reconciliation Contract

Use this contract for planning, collector prompts, reconciliation, verification,
and coverage reporting.

## Source Boundary

Collect only these three evidence domains.

### Jarvis internal facts

Collect deterministic Jarvis facts needed to reconstruct same-day state change:

- Todo and Task events created or transitioned inside the window;
- linked TaskEvent, ExecutionRun, and ProjectEvent records;
- frozen context snapshots, group/project bindings, and repository mappings
  needed for attribution;
- the current state of an item only when it was touched inside the window.

Do not treat an old open Todo or Task as same-day progress. Use an older record
only when same-day evidence directly references it or when reporting an explicit
outstanding commitment or risk. Do not use project background as proof that work
happened.

### Feishu work evidence

Collect attributable work evidence from Feishu:

- replies, thread context, and linked material around person-authored messages
  already captured by Jarvis; do not emit the same authored message twice;
- document revisions or substantive changes attributable to the person;
- ended meetings involving the person, their minutes/transcripts, decisions,
  and action items;
- calendar records only as discovery and attendance context.

Discover every ended meeting in the window. Resolve each to its available
`minute_token` or `note_id`, then read the original transcript for every
readable artifact. Use summaries, chapters, and AI Todo only as navigation or
secondary evidence. Keep a discovered meeting in coverage when its artifact
cannot be read, and record the exact permission, readiness, or lookup error.
Never convert that error into `empty`.

Emit exactly one `source_kind=meeting` EvidenceCard for every discovered
meeting, including unreadable meetings. Use the stable `meeting_id` as its
source identity, merge transcript/minutes into that card, and make the
`meetings_minutes` coverage count equal the number of meeting cards.

Do not treat message volume, calendar presence, meeting attendance, document
ownership, or `last_editor` alone as personal contribution.

### Engineering execution evidence

Collect attributable engineering execution:

- Codex or other agent sessions initiated for the person's work, including
  resulting files, tests, commits, and explicit handoff state;
- authored or reviewed MRs/CRs with exact revision, actor action, state, and URL;
- commits by mapped author identity across relevant repositories;
- test, deployment, release, and runtime acceptance evidence linked to the work.

Separate work directly performed by the person from work delegated to an agent.
Do not infer completion from a session title, commit subject, local branch, or
MR update alone. Prefer exact remote state and observed verification results.

## Collector Result

Return one object with this shape. Do not wrap it in a `collector` key:

```text
domain: jarvis_internal | feishu_work | engineering_execution
identity_filters: [...]
window: {start, end, cutoff, timezone}
status: complete | empty | partial | error | unavailable
coverage:
  - {scope, query_or_cursor, status, count, truncated, error}
evidence: [EvidenceCard...]
gaps: [...]
```

For the runtime JSON contract:

- `domain`, `identity_filters`, and `window` are immutable control-plane values
  owned and injected by the main controller; collector echoes are accepted only
  for schema compatibility and are never trusted as the execution plan;
- the top-level `status` is derived by the main controller from per-scope
  coverage and is not trusted from the collector echo;
- for the unambiguous success states, the main controller canonicalizes
  zero-count coverage to `empty` and positive-count coverage to `complete`;
  `partial`, `error`, and `unavailable` remain collector-owned and require their
  diagnostic error;
- `coverage` must contain exactly the scopes assigned by the caller, with no
  extra discovery scopes;
- `query_or_cursor` is one diagnostic string, not an array or object;
- `raw_reference` is one compact string, not a nested object;
- `gaps` is an array of diagnostic strings, not structured objects;
- every field named in the caller schema uses the caller's exact scalar type.

Collector-owned evidence remains strict. In particular, every
`attribution=direct` card must carry an `actor_identity` present in the
controller's identity mapping; the controller must reject mismatches rather
than rewriting or weakening attribution.

Use:

- `complete`: every planned subquery succeeded and relevant evidence exists;
- `empty`: every planned subquery succeeded and no relevant evidence exists;
- `partial`: some planned scope is unreadable, truncated, or failed;
- `error`: the domain could not be investigated reliably.
- `unavailable`: the required tool, identity mapping, or environment capability
  is absent, so the query could not be attempted.

Never infer `complete` from a non-empty first page. Paginate to exhaustion or
mark the exact truncation. Keep real tool and permission errors verbatim enough
to diagnose.

## Evidence Card

Normalize each item as:

```text
evidence_id
domain
source_kind
source_id
occurred_at
actor_identity
actor_role
project_binding
subject
activity
output
observed_outcome
lifecycle_state
artifact_url
raw_reference
attribution: direct | delegated | collaborative | assigned | discussed
strength: primary | corroborating | contextual
```

Use `null` when output or observed outcome is not evidenced. Preserve the raw
reference, stable ID, timestamp, and attributable actor after compression.
Collectors may connect records inside their own domain but must not deduplicate
or infer across domains.

## Main-Agent Reconciliation

Perform cross-domain analysis only after all collector results arrive.

### Bind projects

Apply this order:

1. frozen Todo/Task project context;
2. group-to-project or repository-to-project binding;
3. explicit project reference in the artifact;
4. evidence-backed inference;
5. `未归属`.

Do not classify solely by keyword similarity when a durable binding exists.

### Merge work items

Merge evidence referring to the same deliverable, decision, bug, or commitment.
Prefer stable joins such as artifact URL, MR/CR ID, Task/Todo relation, document
token, meeting action ID, agent session, commit ancestry, or normalized subject.

For each merged work item retain:

```text
project
subject
activities[]
outputs[]
observed_outcomes[]
decisions[]
commitments[]
risks[]
contribution
evidence_ids[]
confidence
unresolved_gaps[]
```

Use messages and meetings to explain intent and decisions. Use durable artifacts
and observed system state to prove output or outcome. Report one work item once
while retaining all corroborating evidence.

### Analyze the progression

Apply `Activity → Output → Observed Outcome` without forcing missing stages:

- Activity alone is not an accomplishment.
- Output requires an attributable durable artifact or accepted conclusion.
- Observed Outcome requires evidence of effect or final state; planned impact is
  not an outcome.

Record these facts orthogonally:

- Decision: subject, status (`proposed`, `accepted`, `superseded`), authority,
  evidence, and resulting constraint.
- Commitment: requester, owner, acceptance evidence, due time, current state,
  and evidence. An assignment is not an accepted commitment.
- Risk: affected goal, observed condition, impact, owner, mitigation, and
  evidence. An access gap is a coverage risk, not automatically a work blocker.

Rank by observed outcome, delivered output, accepted decision, material
milestone, explicit commitment, then risk requiring action. Drop routine
activity unless it explains a material output, outcome, decision, or risk.

## Targeted Verification

Request a verifier only when a material final claim has:

- conflicting state or attribution across sources;
- an outcome supported only by a message or summary;
- an MR, release, deployment, or task whose final state is unclear;
- a meeting decision or action item missing primary transcript evidence.

Give the verifier one claim, the relevant stable IDs, and one expected answer.
Require `confirmed`, `rejected`, or `unresolved`, plus primary evidence and the
remaining gap. Do not ask the verifier to repeat broad collection or write the
summary.

## Final Checks

Before reporting:

1. Trace every output, outcome, decision, commitment, and risk to evidence IDs.
2. Remove duplicated work items and unsubstantiated causal language.
3. Preserve contribution mode: direct, delegated, collaborative, assigned, or
   discussed.
4. Confirm all three collector statuses and every partial/error gap are visible.
5. Preserve a mandatory meeting-first narrative containing every discovered
   meeting, while also merging meeting-derived decisions into project work.
