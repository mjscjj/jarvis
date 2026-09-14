# task commands: help, accepted flags and handlers.

task_help() {
  case "$1" in
    list-todos) cat <<'EOF'
usage: jarvis-tools list-todos [--date YYYY-MM-DD] [--status S] [--limit N]
                              [--project-id N] [--group-id N] [--page N] [--query TEXT]
                              [--source-message-id MESSAGE_ID]

List compact action-clue summaries newest-evidence first. --date narrows by
last_evidence_at to one natural day in JARVIS_TIMEZONE. --limit defaults to 20.
Use get-todo for description, context, source evidence, resolution and the
frozen snapshot.

--group-id is the sharper scope: every clue comes from a chat, while
--project-id only matches once that chat is bound to a project.

example:
  jarvis-tools list-todos --date 2026-08-02 --status extracted
  jarvis-tools list-todos --group-id 7 --status observing
EOF
      ;;
    get-todo) cat <<'EOF'
usage: jarvis-tools get-task|get-todo --id ID [--context evidence|conversation|background|SECTION|full] [--offset N --length N]
                                    [--message-id MESSAGE_ID]
Default: original evidence and scene; --context evidence reads the same source-independent view.
Use --context to read a frozen section, or --message-id for one frozen message.
These options are mutually exclusive. get-todo also accepts --revision N to
reject a read if the clue has been re-extracted since you read its overview.
EOF
      ;;
    list-delegations) cat <<'EOF'
usage: jarvis-tools list-delegations [--state open|closed|all] [--query TEXT] [--page N] [--limit N]
IDs are M3 Todo IDs; materialized/Task done does not mean delivered.
EOF
      ;;
    get-delegation) cat <<'EOF'
usage: jarvis-tools get-delegation --id TODO_ID
Returns source_payload unchanged, current content, closed_at and version.
EOF
      ;;
    update-delegation) payload_id_help "update-delegation" "Payload: expected_version, content (any valid JSON), actor, optional closed (boolean). Never changes source or Task state. Version 0 means not checked yet." ;;
    list-delegation-tasks) cat <<'EOF'
usage: jarvis-tools list-delegation-tasks --id TODO_ID [--page N] [--limit N]
All states, newest first. Includes the initial materialized check and later Tasks with source.delegation_id or annotation.delegation_id.
EOF
      ;;
    list-tasks) cat <<'EOF'
usage: jarvis-tools list-tasks [--date YYYY-MM-DD] [--status S] [--limit N]
                              [--project-id N] [--group-id N] [--page N] [--query TEXT]
                              [--source-message-id MESSAGE_ID] [--action-type TYPE]


List compact task summaries. --date narrows by COALESCE(last_progress_at,
created_at) to one natural day in JARVIS_TIMEZONE. --limit defaults to 20.
Without --status, all task statuses are queried. Use get-task for details and
recent history.

--group-id matches through the source clue, so manual, scheduled and proactive
tasks belong to no group and are excluded by it.

example:
  jarvis-tools list-tasks --date 2026-08-02 --status done,executing
  jarvis-tools list-tasks --project-id 44 --status done
EOF
      ;;
    get-task) cat <<'EOF'
usage: jarvis-tools get-task|get-todo --id ID [--context evidence|conversation|background|SECTION|full] [--offset N --length N]
                                    [--message-id MESSAGE_ID]
Default: original evidence and scene; --context evidence reads the same source-independent view.
Use --context to read a frozen section, or --message-id for one frozen message.
These options are mutually exclusive. get-todo also accepts --revision N to
reject a read if the clue has been re-extracted since you read its overview.
EOF
      ;;
    list-task-runs) cat <<'EOF'
usage: jarvis-tools list-task-runs --id TASK_ID [--page N] [--limit N]
Read paginated run summaries (default page 1, 20 per page), including failures.
EOF
      ;;
    get-task-run) cat <<'EOF'
usage: jarvis-tools get-task-run --id RUN_ID [--include-prompt]
Read complete output, effects and failure details for one execution attempt.
EOF
      ;;
    create-task) cat <<'EOF'
usage: jarvis-tools create-task [--payload JSON|-]

Create one ordinary Task and hand it to the strong M5 executor. The payload uses
the existing POST /api/tasks contract and requires title, action_type, target,
background, source_payload and source_type. source_type must be manual for an
explicit foreground request or proactive for a background initiative; project_id
is optional. The Task is pending only until the common submitter wakes M5.
EOF
      ;;
    supplement-task) cat <<'EOF'
usage: jarvis-tools supplement-task --id ID [--payload JSON|-]

Append trusted user context to an existing Task without starting a new run. The
payload requires expected_version and note. Read the current version with
get-task immediately before writing.
EOF
      ;;
    resume-task) cat <<'EOF'
usage: jarvis-tools resume-task --id ID [--payload JSON|-]

Answer the pending question on a needs_human Task and resume its existing M5
session. The payload requires expected_version and response. Read the current
version with get-task immediately before writing.
EOF
      ;;
    start-task) cat <<'EOF'
usage: jarvis-tools start-task --id ID

Start one existing pending Task with the strong M5 executor. This command is
available to every trusted Jarvis Agent. It returns after M5 claims the Task;
use get-task later to inspect and verify the result.
EOF
      ;;
    update-task) cat <<'EOF'
usage: jarvis-tools update-task --id ID [--payload JSON|-] [--actor NAME]

Update the mutable current surface of one non-terminal Task. This command is
available to every trusted Jarvis Agent. The payload requires expected_version,
reason, and at least one of title, target, summary or instruction. source_payload
remains frozen. instruction is appended for M5 rather than rewriting source
evidence. A needs_human Task only accepts summary updates so an Agent cannot
silently change the goal behind a question the principal is already looking at.
Actor defaults to JARVIS_AGENT_STAGE; outside an Agent process, pass --actor.

example:
  jarvis-tools update-task --id 42 --payload '{"expected_version":2,"summary":"权限申请仍有效，等待 owner 审批","instruction":"恢复后先重新核验权限，再继续转换文档","reason":"跨日后等待条件仍有效，补充最新状态而不是关闭"}'
EOF
      ;;
    close-task) cat <<'EOF'
usage: jarvis-tools close-task --id ID [--payload JSON|-] [--actor NAME]

Close one existing non-terminal Task. This command is available to every
trusted Jarvis Agent. The payload must contain expected_version and a non-empty
result object. Put the close result in result.summary and optional supporting
data in result.evidence.
Actor defaults to JARVIS_AGENT_STAGE; outside an Agent process, pass --actor.

example:
  jarvis-tools close-task --id 42 --payload '{"expected_version":2,"result":{"summary":"关闭结果","evidence":"支持材料"}}'
EOF
      ;;
    set-todo-status) cat <<'EOF'
usage: jarvis-tools set-todo-status --id TODO_ID --status observing|extracted
                                    --reason TEXT|- [--actor NAME]

Move one clue between the two states that mean nobody is acting on it.

  observing  Nothing to do right now, but keep the clue in view. No Task is
             created; later evidence can bring it back for execution.
  extracted  Hand the clue back for direct Task materialization and execution.

Only observing and extracted are accepted target statuses; every other target
status is rejected by the server.
Pass "-" to --reason to read the reason from stdin.
Actor defaults to JARVIS_AGENT_STAGE. Outside a Jarvis Agent process, pass
--actor explicitly so the event provenance stays truthful.

  jarvis-tools set-todo-status --id 412 --status observing \
    --reason "对方已经自己处理完了，这里不需要我动手，先留着看"
EOF
      ;;
    *) return 1 ;;
  esac
}

task_flags() {
  case "$1" in
    list-todos) printf '%s' '--date --status --limit --project-id --group-id --page --query --source-message-id' ;;
    get-todo) printf '%s' '--id --offset --length --context --message-id --revision' ;;
    list-delegations) printf '%s' '--state --query --page --limit' ;;
    get-delegation) printf '%s' --id ;;
    update-delegation) printf '%s' '--id --payload' ;;
    list-delegation-tasks) printf '%s' '--id --page --limit' ;;
    list-tasks) printf '%s' '--date --status --limit --project-id --group-id --page --query --source-message-id --action-type' ;;
    get-task) printf '%s' '--id --offset --length --context --message-id' ;;
    list-task-runs) printf '%s' '--id --page --limit' ;;
    get-task-run) printf '%s' '--id --include-prompt' ;;
    create-task) printf '%s' --payload ;;
    supplement-task) printf '%s' '--id --payload' ;;
    resume-task) printf '%s' '--id --payload' ;;
    start-task) printf '%s' --id ;;
    update-task) printf '%s' '--id --payload --actor' ;;
    close-task) printf '%s' '--id --payload --actor' ;;
    set-todo-status) printf '%s' '--id --status --reason --actor' ;;
  esac
}

context_read_query() {
 [[ -z "$CONTEXT_SECTION" || -z "$MESSAGE_ID" ]] || fail "--context and --message-id are mutually exclusive"
 [[ -z "$REVISION" || "$REVISION" =~ ^[1-9][0-9]*$ ]] || fail "--revision must be positive"
 jq -rn --arg context "$CONTEXT_SECTION" --arg message_id "$MESSAGE_ID" --arg offset "$READ_OFFSET" --arg length "$READ_LENGTH" --arg revision "$REVISION" '"?context="+($context|@uri)+"&message_id="+($message_id|@uri)+"&revision="+($revision|@uri)+"&offset="+($offset|@uri)+"&length="+($length|@uri)'
}

cmd_list_todos() {
  require_limit list-todos "$LIMIT" 100
  if [[ -n "$PROJECT_ID" && ! "$PROJECT_ID" =~ ^[1-9][0-9]*$ ]]; then
    fail "list-todos --project-id must be a positive integer"
  fi
  if [[ -n "$GROUP_ID" && ! "$GROUP_ID" =~ ^[1-9][0-9]*$ ]]; then
    fail "list-todos --group-id must be a positive integer"
  fi
  [[ "$PAGE" =~ ^[1-9][0-9]*$ ]] || fail "--page must be positive"
  local query="?page=${PAGE}&page_size=${LIMIT}"
  query="${query}&query=$(jq -rn --arg s "$QUERY" '$s|@uri')&source_message_id=$(jq -rn --arg s "$SOURCE_MESSAGE_ID" '$s|@uri')"
  if [[ -n "$STATUS" ]]; then
    query="${query}&status=$(jq -rn --arg s "$STATUS" '$s|@uri')"
  fi
  if [[ -n "$PROJECT_ID" ]]; then
    query="${query}&project_id=${PROJECT_ID}"
  fi
  if [[ -n "$GROUP_ID" ]]; then
    query="${query}&group_id=${GROUP_ID}"
  fi
  if [[ -n "$DATE" ]]; then
    local bounds from until
    bounds="$(local_day_bounds "$DATE" list-todos)" || return 1
    read -r from until <<<"$bounds"
    query="${query}&from=$(jq -rn --arg s "$from" '$s|@uri')&until=$(jq -rn --arg s "$until" '$s|@uri')"
  fi
  local body
  body="$(api_get "/api/todos${query}")"
  printf '%s' "$body" | json_data --project-items items 'total,page,page_size' 'id,title,source_message_ids,source_quote,action_type,target,assigner_open_id,is_leader_assigned,due_at,status,revision,version,first_seen_at,last_evidence_at,group,project'
}

cmd_get_todo() {
 [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-todo requires positive --id"
 local query
 query="$(context_read_query)"
 emit_api_data "/api/todos/${ID}${query}"
}

cmd_list_delegations() {
  require_limit list-delegations "$LIMIT" 100
  [[ "$PAGE" =~ ^[1-9][0-9]*$ ]] || fail "--page must be positive"
  local query
  query="$(jq -rn --arg state "${STATUS:-open}" --arg query "$QUERY" --arg page "$PAGE" --arg limit "$LIMIT" '"?state="+($state|@uri)+"&query="+($query|@uri)+"&page="+$page+"&page_size="+$limit')"
  emit_api_data "/api/delegations${query}"
}

cmd_get_delegation() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-delegation requires positive --id"
  emit_api_data "/api/delegations/${ID}"
}

cmd_update_delegation() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "update-delegation requires positive --id"
  local payload body
  payload="$(read_payload_object update-delegation)"
  body="$(api_write PATCH "/api/delegations/${ID}" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_list_delegation_tasks() {
  require_limit list-delegation-tasks "$LIMIT" 100
  [[ "$ID" =~ ^[1-9][0-9]*$ && "$PAGE" =~ ^[1-9][0-9]*$ ]] || fail "positive --id and --page required"
  emit_api_data "/api/delegations/${ID}/tasks?page=${PAGE}&page_size=${LIMIT}"
}

cmd_list_tasks() {
  require_limit list-tasks "$LIMIT" 100
  if [[ -n "$PROJECT_ID" && ! "$PROJECT_ID" =~ ^[1-9][0-9]*$ ]]; then
    fail "list-tasks --project-id must be a positive integer"
  fi
  if [[ -n "$GROUP_ID" && ! "$GROUP_ID" =~ ^[1-9][0-9]*$ ]]; then
    fail "list-tasks --group-id must be a positive integer"
  fi
  [[ "$PAGE" =~ ^[1-9][0-9]*$ ]] || fail "--page must be positive"
  local query="?page=${PAGE}&page_size=${LIMIT}"
  query="${query}&query=$(jq -rn --arg s "$QUERY" '$s|@uri')&source_message_id=$(jq -rn --arg s "$SOURCE_MESSAGE_ID" '$s|@uri')"
  if [[ -n "$ACTION_TYPE" ]]; then
    query="${query}&action_type=$(jq -rn --arg s "$ACTION_TYPE" '$s|@uri')"
  fi
  if [[ -z "$STATUS" ]]; then
    STATUS="pending,executing,waiting,needs_human,done,failed,observing"
  fi
  query="${query}&status=$(jq -rn --arg s "$STATUS" '$s|@uri')"
  if [[ -n "$PROJECT_ID" ]]; then
    query="${query}&project_id=${PROJECT_ID}"
  fi
  if [[ -n "$GROUP_ID" ]]; then
    query="${query}&group_id=${GROUP_ID}"
  fi
  if [[ -n "$DATE" ]]; then
    local bounds from until
    bounds="$(local_day_bounds "$DATE" list-tasks)" || return 1
    read -r from until <<<"$bounds"
    query="${query}&from=$(jq -rn --arg s "$from" '$s|@uri')&until=$(jq -rn --arg s "$until" '$s|@uri')"
  fi
  local body
  body="$(api_get "/api/tasks${query}")"
  printf '%s' "$body" | json_data --project-items items 'total,page,page_size' 'id,todo_id,title,source_message_ids,action_type,target,status,summary,project_id,source_type,source_id,created_at,last_progress_at,version,updated_at,resolution'
}

cmd_get_task() {
 [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-task requires positive --id"
 local query
 query="$(context_read_query)"
 emit_api_data "/api/tasks/${ID}${query}"
}

cmd_list_task_runs() {
 [[ "$ID" =~ ^[1-9][0-9]*$ && "$PAGE" =~ ^[1-9][0-9]*$ ]] || fail "list-task-runs requires positive --id and --page"
 require_limit list-task-runs "$LIMIT" 100
 api_get "/api/tasks/${ID}/runs?page=${PAGE}&page_size=${LIMIT}" | json_data --project-items items 'total,page,page_size' 'id,task_id,status,summary,started_at,finished_at'
}

cmd_get_task_run() {
 [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-task-run requires positive --id"
 emit_api_data "/api/task-runs/${ID}?include_prompt=${INCLUDE_PROMPT}"
}

cmd_create_task() {
  local payload body
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1 || fail "create-task payload must be a JSON object"
  printf '%s' "$payload" | jq -e '.source_type == "manual" or .source_type == "proactive"' >/dev/null 2>&1 || \
    fail "create-task payload source_type must be manual or proactive"
  payload="$(printf '%s' "$payload" | jq -c --arg actor "$(task_actor)" '. + {actor:$actor}')"
  body="$(api_write POST /api/tasks "$payload")"
  printf '%s' "$body" | json_data
}

cmd_supplement_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "supplement-task requires positive --id"
  local payload body
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e 'type == "object" and (.expected_version | type == "number") and (.note | type == "string" and length > 0)' >/dev/null 2>&1 || \
    fail "supplement-task payload requires numeric expected_version and non-empty note"
  body="$(api_write POST "/api/tasks/${ID}/supplement" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_resume_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "resume-task requires positive --id"
  local payload body
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e 'type == "object" and (.expected_version | type == "number") and (.response | type == "string" and length > 0)' >/dev/null 2>&1 || \
    fail "resume-task payload requires numeric expected_version and non-empty response"
  body="$(api_write POST "/api/tasks/${ID}/resume" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_start_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "start-task requires positive --id"
  local body
  body="$(api_write POST "/api/tasks/${ID}/execute" '{}')"
  printf '%s' "$body" | json_data
}

cmd_update_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "update-task requires positive --id"
  local payload body actor
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e '
    type == "object" and
    (.expected_version | type == "number" and . >= 0 and floor == .) and
    (.reason | type == "string" and length > 0) and
    (has("title") or has("target") or has("summary") or has("instruction")) and
    ((has("title") | not) or (.title | type == "string" and length > 0)) and
    ((has("target") | not) or (.target | type == "string")) and
    ((has("summary") | not) or (.summary | type == "string" and length > 0)) and
    ((has("instruction") | not) or (.instruction | type == "string" and length > 0))
  ' >/dev/null 2>&1 || fail "update-task payload requires expected_version, reason and at least one mutable Task field"
  actor="${ACTOR:-${JARVIS_AGENT_STAGE:-}}"
  [[ -n "$actor" ]] || fail "update-task requires JARVIS_AGENT_STAGE or explicit --actor"
  payload="$(printf '%s' "$payload" | json_data --set actor_type "$(jq -cn --arg actor "$actor" '$actor')")"
  body="$(api_write PATCH "/api/tasks/${ID}" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_close_task() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "close-task requires positive --id"
  local payload body actor
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e '
    type == "object" and
    (.expected_version | type == "number" and . >= 0 and floor == .) and
    (.result | type == "object" and (.summary | type == "string" and length > 0))
  ' >/dev/null 2>&1 || fail "close-task payload requires non-negative expected_version and non-empty result.summary"
  actor="${ACTOR:-${JARVIS_AGENT_STAGE:-}}"
  [[ -n "$actor" ]] || fail "close-task requires JARVIS_AGENT_STAGE or explicit --actor"
  payload="$(printf '%s' "$payload" | json_data --set actor_type "$(jq -cn --arg actor "$actor" '$actor')")"
  body="$(api_write POST "/api/tasks/${ID}/close" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_set_todo_status() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "set-todo-status requires positive --id"
  case "$STATUS" in
    observing|extracted) ;;
    *) fail "set-todo-status --status must be observing or extracted" ;;
  esac
  local reason
  if [[ "$REASON" == "-" ]]; then
    reason="$(cat)"
  else
    reason="$REASON"
  fi
  [[ -n "${reason//[[:space:]]/}" ]] || fail "set-todo-status reason is empty"
  local actor="$ACTOR"
  if [[ -z "$actor" ]]; then
    actor="${JARVIS_AGENT_STAGE:-}"
  fi
  [[ -n "${actor//[[:space:]]/}" ]] || fail "set-todo-status requires --actor outside a Jarvis Agent process"
  local payload body
  payload="$(jq -cn --arg status "$STATUS" --arg actor "$actor" --arg reason "$reason" \
    '{status:$status,actor:$actor,reason:$reason}')"
  body="$(api_write PATCH "/api/todos/${ID}/status" "$payload")"
  printf '%s' "$body" | json_data
}

task_actor() {
  case "${JARVIS_AGENT_STAGE:-}" in
    execute) printf 'm5' ;;
    '') printf 'user' ;;
    *) printf '%s' "$JARVIS_AGENT_STAGE" ;;
  esac
}
