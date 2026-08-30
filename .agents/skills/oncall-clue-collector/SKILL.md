---
name: "oncall-clue-collector"
description: "Collects current-user Oncall group evidence into Jarvis clues. Invoke only for an enabled Oncall Plugin collection Task."
---

# Oncall Clue Collector

Collect raw evidence from matching Feishu Oncall groups without triaging it in
this Task.

1. Confirm the current Task explicitly requests Oncall Plugin collection.
2. Read `search_terms` from the plugin configuration embedded in the Task
   instruction. Use `["oncall", "值班"]` only when that key is absent.
3. For every search term, search group names and descriptions visible to the
   current user:

```bash
lark-cli im +chat-search \
  --as user \
  --query "$SEARCH_TERM" \
  --disable-search-by-user \
  --chat-modes "group,topic" \
  --search-types "private,public_joined" \
  --sort update_time \
  --page-size 100 \
  --page-all \
  --page-limit 20 \
  --format json
```

4. Merge results by `chat_id`. Keep only active group/topic chats whose name or
   description contains at least one configured term, case-insensitively. Do
   not add hardcoded business, service, severity, or team names.
5. For each candidate group, read up to eight earliest messages and require
   evidence that `ByteOncall` created, invited users to, or sent the initial
   system/card content in that group:

```bash
lark-cli im +chat-messages-list \
  --as user \
  --chat-id "$CHAT_ID" \
  --order asc \
  --page-size 8 \
  --no-reactions \
  --format json
```

6. For every qualifying group, read all messages from the last 30 days. Follow
   all pages; an incomplete page sequence is a collection failure, not partial
   success:

```bash
lark-cli im +chat-messages-list \
  --as user \
  --chat-id "$CHAT_ID" \
  --order asc \
  --start "$THIRTY_DAYS_AGO_ISO" \
  --page-size 50 \
  --page-all \
  --page-limit 20 \
  --no-reactions \
  --format json
```

7. Submit one clue for each group version. Preserve the raw group metadata,
   matching terms, initiation evidence, and complete returned message data:

```bash
printf '%s' "$RAW_GROUP_AND_MESSAGES_JSON" |
  jarvis-tools append-clue \
    --source oncall \
    --external-id "$STABLE_VERSION_ID" \
    --title "$GROUP_NAME" \
    --occurred-at "$LATEST_MESSAGE_AT" \
    --content -
```

8. Derive `STABLE_VERSION_ID` from `chat_id` and the latest message identity or
   update time; hash when necessary to stay within the clue ID limit. Repeated
   collection of an unchanged group must remain idempotent.
9. Do not decide severity, ownership, escalation, or whether intervention is
   needed. M3 and M5 own those judgments.
10. Do not send messages or alter groups. Preserve complete authorization and
    provider errors in the Task result, then report queried, matched, inserted,
    duplicate, and failed counts.
