---
name: "codebase-clue-collector"
description: "Collects current-user Codebase MR evidence into Jarvis clues. Invoke only for an enabled Codebase Plugin collection Task."
---

# Codebase Clue Collector

This Skill is a source collector, not a reviewer or decision maker.

1. Confirm the current Task explicitly requests Codebase Plugin collection.
2. Run both searches and follow every `next_page_token` until exhausted:

```bash
bytedcli --json codebase search mr --reviewer @me --status open --page-size 100
bytedcli --json codebase search mr --author @me --status open --page-size 100
```

3. Preserve each returned merge request as one raw observation. Do not classify
   urgency, infer ownership beyond the returned reviewer/author facet, fetch
   diffs, review code, or decide whether the MR deserves work.
4. Deliver each observation through the only clue entry:

```bash
printf '%s' "$RAW_MR_JSON" |
  jarvis-tools append-clue \
    --source codebase \
    --external-id "$STABLE_VERSION_ID" \
    --title "$MR_TITLE" \
    --occurred-at "$UPDATED_AT" \
    --content -
```

5. Build `STABLE_VERSION_ID` from the MR identity, facet, and provider
   `UpdatedAt`; hash that text when necessary so the ID remains at most 64
   characters after the `clue:codebase:` prefix. The same MR version must
   produce the same ID, while a later provider update must produce a new ID.
6. If the same MR appears in both searches, submit both facets only when that
   distinction exists in the raw provider response; otherwise submit it once.
7. On authorization or provider failure, preserve the complete original error
   in the Task result. Do not invent a clue or retry indefinitely.
8. Finish after reporting counts for queried, inserted, duplicate, and failed
   observations.
