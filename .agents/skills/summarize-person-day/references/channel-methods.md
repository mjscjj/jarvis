# Evidence Channels and Analysis Methods

## Channel matrix

| Channel | What to collect | Strong evidence | Known limit |
|---|---|---|---|
| Jarvis messages | Person-authored messages in the natural-day window, conversation and bound project | Explicit status, decision, commitment, shared artifact | A message is a claim until corroborated; monitored chats may be incomplete |
| Jarvis Todo | New or changed Todo, leader assignment, source quote, due time, project, frozen context | Assignment, explicit commitment, state transition | An old open Todo is not proof of same-day progress |
| Jarvis Task | Same-day transitions, current state, execution result, artifact, project | `done` plus result/artifact; `failed` plus error | `updated_at` approximates transition time unless run events exist |
| Project/group context | Bound project, project background, group purpose, relevant people | Durable ownership and attribution context | Use for classification, not as proof that work happened |
| Feishu documents | Documents edited or updated by the person in the window; title, URL, update time, change substance | Revision/change content attributable to the person | “Owned by me” or “last edited by me” is only an approximation |
| Calendar | Events intersecting the day, role, attendees, title | Scheduled context | Attendance does not prove contribution or outcome |
| Meetings/Minutes | Participant/organizer role, transcript/summary, decisions, action items | Decision and action item from readable meeting artifact | Missing permission and artifact-not-ready must be reported |
| Code reviews/MRs | Authored/reviewed MRs updated in window, state, URL, change summary | Merged/opened/reviewed state with durable URL | “Updated” may reflect another participant; distinguish authorship |
| Git commits | Commits by mapped author in every cloned repository, time, repo, hash, subject | Durable commit by the person | Local clones are not the full remote universe; run `git -C <repo>` |
| Approval/issue systems | Requests initiated, approved, rejected, or commented on when searchable | Durable state transition with link | Omit when no reliable cross-definition/date query exists |

## Collection method

For every channel:

1. Use stable person identifiers rather than display-name matching.
2. Query the exact local-day window with timezone-aware timestamps.
3. Retain raw IDs, URLs, timestamps, state, and attributable actor.
4. Count collected evidence only after date and identity filtering.
5. Mark:
   - `ok`: query succeeded and one or more relevant items remain.
   - `empty`: query succeeded and no relevant items remain.
   - `error`: query failed or evidence could not be read; include the real error.

Do not convert `error` into `empty`.

## Normalization

Normalize each item conceptually as:

```text
source, source_id, occurred_at, person_role, project,
subject, action, result, status, artifact_url, raw_evidence
```

Keep original evidence available even when the final report is compressed.

## Project attribution

Apply this order:

1. Frozen Todo/Task project context.
2. Group-to-project binding.
3. Repository-to-project mapping.
4. Explicit project reference in the artifact or evidence.
5. Evidence-backed inference.
6. `未归属`.

Do not classify solely from keyword similarity when a stronger binding exists.

## Deduplication

Merge items when they refer to the same project and deliverable, decision, bug,
or commitment. Useful keys include artifact URL, Task/Todo relation, MR URL,
document token, meeting action ID, and a normalized subject.

Within a merged item:

- Use the latest verified state as the result.
- Preserve distinct contributions such as implementation and review.
- Use messages as explanation; use durable artifacts as completion evidence.
- Count evidence sources separately, but report the work item once.

## Significance and phrasing

Rank work by:

1. User/business outcome or production change.
2. Completed deliverable or accepted decision.
3. Material milestone in an active project.
4. New firm assignment or commitment.
5. Risk/blocker requiring action.
6. Routine activity.

Write `完成 X，已达到 Y 状态，证据 Z`, not `开会、发消息、改文档`.

## Next-action derivation

Only derive a next action from:

- An open Todo or non-terminal Task.
- An explicit promise or due date.
- A meeting action item assigned to the person.
- A failed/blocked item with an explicit recovery action.
- A review or approval that is demonstrably pending.

Do not turn a general discussion topic into a personal next action.
