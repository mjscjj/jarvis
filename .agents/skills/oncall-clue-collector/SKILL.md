---
name: "oncall-clue-collector"
description: "Collects current-user Oncall ticket evidence into Jarvis clues. Invoke only for an enabled Oncall Plugin collection Task."
---

# Oncall Clue Collector

Collect provider evidence without triaging incidents in this Task.

1. Confirm the current Task explicitly requests Oncall Plugin collection.
2. Read the current username from `bytedcli --json auth status`.
3. Collect the last 30 days of non-closed tickets twice, once with that username
   as assignee and once as reporter:

```bash
bytedcli --json lark-oncall ticket collect \
  --assignee "$USERNAME" --status "TO跟进中,TO待处理,RD跟进中,RD待处理" \
  --range 30d --page-size 100 --max-pages 50

bytedcli --json lark-oncall ticket collect \
  --reporter "$USERNAME" --status "TO跟进中,TO待处理,RD跟进中,RD待处理" \
  --range 30d --page-size 100 --max-pages 50
```

4. Do not add a hardcoded business, service, severity, team, or ticket type.
5. Preserve the provider's raw ticket and included
   group-message data.
6. Submit one clue for each ticket version, deduplicating tickets returned by
   both queries:

```bash
printf '%s' "$RAW_TICKET_JSON" |
  jarvis-tools append-clue \
    --source oncall \
    --external-id "$STABLE_VERSION_ID" \
    --title "$TICKET_TITLE" \
    --occurred-at "$UPDATED_AT" \
    --content -
```

7. Derive `STABLE_VERSION_ID` from ticket identity and provider update time;
   hash when necessary to stay within the clue ID limit. Repeated collection of
   an unchanged ticket must remain idempotent.
8. Do not decide severity, ownership, escalation, or whether intervention is
   needed. M3 and M5 own those judgments.
9. Do not send messages or alter tickets. Preserve complete authorization and
   provider errors in the Task result, then report queried, inserted, duplicate,
   and failed counts.
