# schedule commands: help, accepted flags and handlers.

schedule_help() {
  case "$1" in
    list-scheduled-tasks) cat <<'EOF'
usage: jarvis-tools list-scheduled-tasks [--status binding|active|running|completed] [--limit N]
List compact time-trigger summaries. binding entries are owned by waiting Tasks.
Use get-scheduled-task for the instruction, context and latest result.
EOF
      ;;
    get-scheduled-task) cat <<'EOF'
usage: jarvis-tools get-scheduled-task --id ID
Get one time trigger in full.
EOF
      ;;
    create-scheduled-task) cat <<'EOF'
usage: jarvis-tools create-scheduled-task [--payload JSON|-]
Create a trigger for an independent new task. The payload must contain title,
instruction, context_snapshot, schedule_type and its matching schedule field.

once example:
  {"title":"明早检查进展","instruction":"查询项目最新进展","context_snapshot":{},"schedule_type":"once","run_at":"2026-07-25T09:00:00+08:00"}
daily example:
  {"title":"每日汇总","instruction":"汇总今日进展","context_snapshot":{},"schedule_type":"daily","daily_time":"18:00"}
weekly example (weekday: 0=Sunday, 1=Monday, ... 6=Saturday):
  {"title":"每周汇总","instruction":"汇总上周进展","context_snapshot":{},"schedule_type":"weekly","weekday":1,"daily_time":"09:00"}
daily_time uses the Jarvis server's local timezone for daily and weekly schedules.
interval example:
  {"title":"轮询状态","instruction":"检查任务是否完成","context_snapshot":{},"schedule_type":"interval","interval_minutes":60}
EOF
      ;;
    update-scheduled-task) cat <<'EOF'
usage: jarvis-tools update-scheduled-task --id ID [--payload JSON|-]
Replace one standalone trigger using the same payload contract as create.
Continuation triggers owned by waiting Tasks cannot be edited.
EOF
      ;;
    trigger-scheduled-task) cat <<'EOF'
usage: jarvis-tools trigger-scheduled-task --id ID
Run one standalone trigger immediately.
EOF
      ;;
    yield-until) cat <<'EOF'
usage: jarvis-tools yield-until --at RFC3339 [--reason TEXT|-]
Pause the current executing Task and arrange its continuation. JARVIS_TASK_ID
must be provided by the Task runner.
EOF
      ;;
    delete-scheduled-task) cat <<'EOF'
usage: jarvis-tools delete-scheduled-task --id ID
Delete a standalone trigger. A continuation owned by a waiting Task cannot be deleted.
EOF
      ;;
    *) return 1 ;;
  esac
}

schedule_flags() {
  case "$1" in
    list-scheduled-tasks) printf '%s' '--status --limit' ;;
    get-scheduled-task) printf '%s' --id ;;
    create-scheduled-task) printf '%s' --payload ;;
    update-scheduled-task) printf '%s' '--id --payload' ;;
    trigger-scheduled-task) printf '%s' --id ;;
    yield-until) printf '%s' '--at --reason' ;;
    delete-scheduled-task) printf '%s' --id ;;
  esac
}

cmd_list_scheduled_tasks() {
  require_limit list-scheduled-tasks "$LIMIT" 100
  local query="?limit=${LIMIT}"
  if [[ -n "$STATUS" ]]; then
    local enc
    enc="$(jq -rn --arg s "$STATUS" '$s|@uri')"
    query="${query}&status=${enc}"
  fi
  local body
  body="$(api_get "/api/scheduled-tasks${query}")"
  printf '%s' "$body" | json_data --project-items items '' 'id,title,action_type,dispatch_kind,subject_type,subject_id,schedule_type,daily_time,weekday,interval_minutes,run_at,next_run_at,enabled,status,last_run_status,last_task_id,last_started_at,last_finished_at,updated_at'
}

cmd_get_scheduled_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-scheduled-task requires positive --id"
  api_get "/api/scheduled-tasks/${ID}" | json_data
}

cmd_create_scheduled_task() {
  local payload body
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1 || fail "create-scheduled-task payload must be a JSON object"
  body="$(api_write POST /api/scheduled-tasks "$payload")"
  printf '%s' "$body" | json_data
}

cmd_update_scheduled_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "update-scheduled-task requires positive --id"
  local payload body
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1 || fail "update-scheduled-task payload must be a JSON object"
  body="$(api_write PUT "/api/scheduled-tasks/${ID}" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_trigger_scheduled_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "trigger-scheduled-task requires positive --id"
  local body
  body="$(api_write POST "/api/scheduled-tasks/${ID}/trigger" '{}')"
  printf '%s' "$body" | json_data
}

cmd_yield_until() {
  [[ "${JARVIS_TASK_ID:-}" =~ ^[1-9][0-9]*$ ]] || fail "yield-until requires JARVIS_TASK_ID from the Task runner"
  [[ -n "$AT" ]] || fail "yield-until requires --at"
  local reason
  if [[ "$REASON" == "-" ]]; then
    reason="$(cat)"
  else
    reason="$REASON"
  fi
  [[ -n "${reason//[[:space:]]/}" ]] || fail "yield-until reason is empty"
  local payload body
  payload="$(jq -cn --argjson task_id "$JARVIS_TASK_ID" --arg run_at "$AT" --arg reason "$reason" \
    '{task_id:$task_id,run_at:$run_at,reason:$reason}')"
  body="$(api_write POST /api/scheduled-tasks/yield "$payload")"
  printf '%s' "$body" | json_data
}

cmd_delete_scheduled_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "delete-scheduled-task requires positive --id"
  local body
  body="$(api_write DELETE "/api/scheduled-tasks/${ID}" '{}')"
  printf '%s' "$body" | json_data
}
