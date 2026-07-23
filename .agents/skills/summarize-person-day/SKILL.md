---
name: summarize-person-day
description: Build an evidence-backed natural-day work summary for one person by planning three bounded evidence collectors, delegating Feishu and engineering collection in parallel, reconciling results, verifying material claims, and reporting outcomes, decisions, commitments, and risks. Use for personal daily summaries, daily work recaps, end-of-day progress, what someone advanced on a date, or structured cross-system day reports.
---

# Summarize a Person's Natural Day

Explain what changed during one local calendar day. Treat activities as evidence,
not as accomplishments.

## Resolve the Scope

Resolve the person to stable identities, the `YYYY-MM-DD` date, timezone, and
cutoff. Use `[local 00:00, next local 00:00)` and stop at the cutoff for today.
Ask only when the person remains ambiguous after using read-only identity tools.

Read [references/channel-methods.md](references/channel-methods.md) before
planning or collecting evidence.

## Execute

1. Write an investigation plan before querying:
   - state the identity mapping, time window, and cutoff;
   - assign exactly three independent collector scopes: Jarvis internal facts,
     Feishu work evidence, and engineering execution evidence;
   - define expected evidence, completeness checks, and known access limits for
     each collector.
2. Run Jarvis internal collection as deterministic database/tool queries. Launch
   the Feishu and engineering collector subagents in parallel. Give each
   subagent only its scope, identity filters, time window, cutoff, and the
   collector contract from the reference. Require raw IDs, timestamps, links,
   coverage, and gaps. Do not let collectors write the personal summary or
   infer across scopes.
3. Require all three collectors to finish with an explicit coverage status.
   Fail visibly if the two external subagents cannot be launched; do not
   silently replace planned delegation with an unreported execution path.
4. Reconcile all collector results in the main agent:
   - bind evidence to projects using durable context;
   - merge evidence about the same deliverable, decision, or commitment;
   - distinguish direct work, agent-delegated work, collaboration, assignment,
     and discussion;
   - analyze each work item as `Activity → Output → Observed Outcome`;
   - record Decision, Commitment, and Risk as orthogonal facts.
5. Verify material claims against primary collector evidence. Mark conflicts,
   weak attribution, unclear final state, and missing primary evidence as
   unresolved. When the execution environment supports dynamic delegation,
   optionally launch a narrow verifier subagent for one material unresolved
   claim and retain its supporting evidence.
6. Produce the final summary from reconciled work items, not from source-by-
   source narratives or activity counts. Attach cutoff, coverage, and unresolved
   gaps.

## Analyze

- `Activity`: what the person or their delegated agent did. Use as supporting
  evidence only.
- `Output`: a durable artifact, accepted conclusion, review, fix, or delivered
  change attributable to the person.
- `Observed Outcome`: a verified effect or state change, such as merged,
  deployed, accepted, validated, unblocked, or confirmed by a user or system.

Never invent an outcome when only activity or output is observed. Keep:

- Decision with `proposed`, `accepted`, or `superseded` status and authority.
- Commitment with requester, owner, acceptance evidence, due time, and state.
- Risk with affected goal, impact, owner, and observed mitigation.

## Report

For interactive use, write concise Chinese sections:

1. `会议与妙记`: list every discovered meeting first, with time range, title,
   duration, transcript-backed conclusions or decisions, and action items
   assigned to the person. Keep unreadable meetings and state the exact access
   gap. End with total meeting count and duration.
2. `今日结论`: at most three strongest outputs or observed outcomes.
3. `按项目变化`: merged work items, each following
   `Activity → Output → Observed Outcome`; omit routine activity.
4. `决策与承诺`: only evidence-backed decisions and accepted commitments.
5. `风险与阻塞`: impact, owner, mitigation, and evidence gaps.
6. `数据覆盖`: cutoff and status of all three collectors and their subqueries.

Meeting evidence is first-class work evidence for this user. Keep the mandatory
meeting-first section even when the same decisions are merged into project work.
When the caller supplies a schema, return exactly that schema and preserve the
same analysis semantics.
