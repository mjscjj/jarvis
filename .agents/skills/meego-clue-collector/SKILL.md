---
name: "meego-clue-collector"
description: "Collects current-user unfinished Meego work items into Jarvis clues. Invoke only for an enabled Meego Plugin collection Task."
---

# Meego Clue Collector

This Skill mechanically collects evidence. It does not decide which work item
matters or what the principal should do.

1. Confirm the current Task explicitly requests Meego Plugin collection.
2. Resolve the current Meego identity with:

```bash
bytedcli --json meego user me
```

3. List every visible Meego project with `bytedcli --json meego project search`.
4. For each project, query both `story` and `issue` work-item types for items
   where the current user is owner or current status operator and the item is
   unfinished. Use the display name returned by `meego user me`, and issue:

```bash
bytedcli --json meego workitem list --project-key "$PROJECT_KEY" --mql \
  'SELECT work_item_id, name, description, work_item_status, priority, owner, current_status_operator, updated_at FROM `'"$PROJECT_SLUG"'`.`story` WHERE (owner = "'"$DISPLAY_NAME"'" OR current_status_operator = "'"$DISPLAY_NAME"'") AND finish_status = false'

bytedcli --json meego workitem list --project-key "$PROJECT_KEY" --mql \
  'SELECT work_item_id, name, description, work_item_status, priority, owner, current_status_operator, updated_at FROM `'"$PROJECT_SLUG"'`.`issue` WHERE (owner = "'"$DISPLAY_NAME"'" OR current_status_operator = "'"$DISPLAY_NAME"'") AND archiving_status = false'
```

5. Detail rows are inside the inner MCP JSON under `data["<groupId>"]`, not the
   outer group-count list. Follow `session_id` and `group_infos` pagination with
   `--session-id` and `--group-pagination-list` until each reported count is
   complete. A missing work-item type or inaccessible
   project is evidence about that project, not permission to discard successful
   results from other projects.
6. Submit each returned work-item version through:

```bash
printf '%s' "$RAW_WORK_ITEM_JSON" |
  jarvis-tools append-clue \
    --source meego \
    --external-id "$STABLE_VERSION_ID" \
    --title "$WORK_ITEM_TITLE" \
    --occurred-at "$UPDATED_AT" \
    --content -
```

7. Derive `STABLE_VERSION_ID` from project, work-item type, work-item ID, and
   provider update time; hash when necessary to respect the clue ID length.
   Redelivery of the same version must be idempotent.
8. Do not infer priority, create follow-up tasks, modify work items, or send
   messages during collection.
9. Preserve complete provider errors in the Task result and finish with queried,
   inserted, duplicate, skipped-project, and failed counts.
