---
name: summarize-person-day
description: Summarize everything a specified person advanced during one natural day by collecting and reconciling evidence across Jarvis messages, Todo and Task state, project context, Feishu/Lark documents, calendar, meetings and minutes, code reviews and commits. Use when asked for a personal daily summary, daily work recap, what someone did on a date, end-of-day progress, or a structured cross-channel natural-day report.
---

# Summarize a Person's Natural Day

Produce an evidence-backed account of what one person advanced during one local
calendar day. Treat activity counts as evidence, not as the summary itself.

## Inputs

Resolve before analysis:

- Person identity: name plus stable IDs available in each system, such as Feishu
  `open_id`, code author identity, and email.
- Natural day: `YYYY-MM-DD` and timezone.
- Evidence cutoff: current time for today; `23:59:59` for a completed past day.
- Output mode: human-readable Markdown by default, or the caller's explicit
  machine-readable contract.

Infer missing identifiers with available read-only tools. Ask the user only when
the person cannot be resolved unambiguously.

## Workflow

1. Define the half-open window `[local 00:00, next local 00:00)` and cutoff.
2. Read [references/channel-methods.md](references/channel-methods.md).
3. Collect every available channel independently. Record each channel as
   `ok`, `empty`, or `error`; preserve real error details.
4. Normalize evidence into: timestamp, source, project, subject, action,
   result/state change, artifact/link, owner/assigner, and raw evidence.
5. Resolve project from the frozen Todo/Task context or bound group first.
   Infer only when evidence supports it; otherwise label it unassigned.
6. Merge evidence about the same deliverable or decision. Prefer the strongest
   result evidence: deployed/merged/completed > produced/reviewed > discussed >
   mentioned. Keep corroborating sources without repeating the work item.
7. Separate events that happened that day from older open work. Use older open
   Todo/Task only for commitments, risks, or next actions; never claim it was
   advanced that day without dated evidence.
8. Build the report in this order:
   - `核心推进`: concrete progress grouped by project.
   - `关键产出与决策`: artifacts, links, decisions, releases, reviews.
   - `任务与承诺`: completed items, new assignments, explicit commitments,
     and materially advanced ongoing work.
   - `风险与阻塞`: failures, waiting conditions, access gaps, conflicting
     evidence, and overdue commitments.
   - `下一步`: only explicit unfinished tasks, commitments, meeting actions,
     or follow-ups supported by evidence.
9. Attach source coverage and the evidence cutoff. Never hide partial coverage.

## Quality Rules

- Say what changed and where it reached, not merely what tools the person used.
- Preserve the distinction between work performed by the person, work assigned
  to the person, and work discussed around the person.
- Do not infer completion from a message saying work will be done.
- Do not infer personal contribution from meeting attendance alone.
- Do not count a document as edited by the person when the API only proves
  ownership or last editor; state the observed scope.
- If sources disagree, prefer durable artifacts and state the conflict.
- Never invent an outcome, deadline, blocker, or next action.
- If no progress evidence exists, say so and still report source coverage.

## Output

For interactive use, output the five sections as concise Chinese bullet points,
followed by `数据截至` and `来源覆盖`.

When the caller provides a schema, return exactly that schema. Keep the five
section headings inside the summary and expose per-source `status`, `count`, and
`note`. Do not add code fences or surrounding explanation.
