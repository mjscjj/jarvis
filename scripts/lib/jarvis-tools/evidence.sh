# evidence commands: help, accepted flags and handlers.

evidence_help() {
  case "$1" in
    query-messages) cat <<'EOF'
usage: jarvis-tools query-messages [--chat-id CHAT_ID] [--sender-open-id OPEN_ID]
                                   [--keyword TEXT] [--date YYYY-MM-DD]
                                   [--message-ids ID,ID,...]
                                   [--limit N]
Query locally captured messages, newest first. At least one narrowing filter is
recommended. Content is returned because it is the evidence being queried;
--date uses JARVIS_TIMEZONE; --limit defaults to 20 and must not exceed 100.
--message-ids exactly matches original message IDs and returns only id/message_id.
Use --limit 100 for batches; the number of requested IDs must not exceed limit.
EOF
      ;;
    get-message) cat <<'EOF'
usage: jarvis-tools get-message --id ID
Load one locally captured message by its database ID, including original content.
EOF
      ;;
    get-todo-event) cat <<'EOF'
usage: jarvis-tools get-todo-event --id ID
Load one Todo lifecycle event addressed by a Fact source pointer.
EOF
      ;;
    get-task-event) cat <<'EOF'
usage: jarvis-tools get-task-event --id ID
Load one Task lifecycle event addressed by a Fact source pointer.
EOF
      ;;
    query-captured-resources) cat <<'EOF'
usage: jarvis-tools query-captured-resources [--chat-id CHAT_ID]
                                             [--message-id MESSAGE_ID]
                                             [--resource-type TYPE]
                                             [--keyword TEXT] [--limit N]

List captured attachment/document summaries without local_path or extracted_text.
Use get-captured-resource after selecting one item.
EOF
      ;;
    get-captured-resource) cat <<'EOF'
usage: jarvis-tools get-captured-resource --id ID
Load one captured resource in full, including local_path and extracted_text.
EOF
      ;;
    append-clue) cat <<'EOF'
usage: jarvis-tools append-clue --source SOURCE --external-id ID --title TEXT
                               [--content TEXT|-] [--occurred-at RFC3339]

Deliver one observed fact into the evidence stream. Preserve the complete raw
content, including original error text; do not truncate the evidence payload.

  --source       lowercase snake_case producer id, e.g. feishu_meeting. Picks
                 the clue channel; a new source creates its channel on first use.
  --external-id  producer-side id used for idempotency. Redelivering the same
                 (source, external-id) returns inserted=false and changes nothing.
  --title        one line stating the fact, e.g. "会议结束：公会基建Agent 日会".
  --content      the raw detail. Use --content - or omit it to read stdin.
  --occurred-at  when the fact happened (defaults to now).

example:
  jarvis-tools append-clue --source feishu_meeting --external-id 7667030332496007223 \
    --title '会议结束：公会基建Agent 日会' --occurred-at 2026-07-27T14:01:00+08:00 --content - <<'TXT'
  会议 ID：7667030332496007223
  时间：2026-07-27 11:45 ~ 14:01
  参会人：...
  TXT
EOF
      ;;
    append-fact) cat <<'EOF'
usage: jarvis-tools append-fact --subject-type TYPE --subject-id ID
                              [--description TEXT|-] [--occurred-at RFC3339]
                              --source KIND [--source-id ID]

Record one evidence index entry. description is a short retrieval anchor, not
the current conclusion; current knowledge belongs on the entity page.

  --subject-type  what the fact is about: project, group, person or task are the
                  types read back today. If the fact genuinely belongs to
                  something else, write that instead of forcing a fit — the type
                  is not a fixed list.
  --subject-id    the numeric id of that subject.
  --description   at most 200 characters identifying the relevant event and
                  people. Use --description - or omit it to read stdin.
  --occurred-at   when it happened (defaults to now). Pass it explicitly when
                  recording something you learned after the event.
  --source        message, todo_event, task_event, execution_run, resource, or
                  system.
  --source-id     required for raw material sources; forbidden for system.

example:
  jarvis-tools append-fact --subject-type project --subject-id 3 \
    --source message --source-id 91 --description "讨论确认 Fact 只保留证据索引"
EOF
      ;;
    append-facts-batch) cat <<'EOF'
usage: jarvis-tools append-facts-batch --payload JSON|-

Record one batch of facts in one request.

  --payload  JSON array of fact objects. Each fact must include
             subject_type, subject_id, description and source.
             Optional: occurred_at. source_id is required for message,
             todo_event, task_event, execution_run and resource, and forbidden
             for system. description is limited to 200 characters.
             Unknown fields are rejected before the request is sent.

  --payload - reads from stdin. Omit it to pass --payload text directly.

example:
  cat <<'TXT' | jarvis-tools append-facts-batch --payload -
  [
    {"subject_type":"project","subject_id":3,"description":"消息确认本周验收窗口已冻结","source":"message","source_id":91},
    {"subject_type":"task","subject_id":18,"description":"执行结果显示开发完成、待验收","source":"execution_run","source_id":52}
  ]
  TXT
EOF
      ;;
    list-facts) cat <<'EOF'
usage: jarvis-tools list-facts --subject-type TYPE --subject-id ID
                             [--date YYYY-MM-DD] [--limit N]

Read a subject's evidence indexes, newest first. description is only a retrieval
anchor; follow source_kind/source_id to the raw material before relying on it.
--date narrows to one natural day in JARVIS_TIMEZONE; --limit defaults to 20
and must not exceed 100.

example:
  jarvis-tools list-facts --subject-type group --subject-id 12 --date 2026-08-02
EOF
      ;;
    *) return 1 ;;
  esac
}

evidence_flags() {
  case "$1" in
    query-messages) printf '%s' '--chat-id --sender-open-id --keyword --date --limit --message-ids' ;;
    get-message) printf '%s' --id ;;
    get-todo-event) printf '%s' --id ;;
    get-task-event) printf '%s' --id ;;
    query-captured-resources) printf '%s' '--chat-id --message-id --resource-type --keyword --limit' ;;
    get-captured-resource) printf '%s' --id ;;
    append-clue) printf '%s' '--source --external-id --title --content --occurred-at' ;;
    append-fact) printf '%s' '--subject-type --subject-id --description --occurred-at --source --source-id' ;;
    append-facts-batch) printf '%s' --payload ;;
    list-facts) printf '%s' '--subject-type --subject-id --date --limit' ;;
  esac
}

cmd_query_messages() {
  require_limit query-messages "$LIMIT" 100
  local query="limit=${LIMIT}" value
  if [[ -n "$MESSAGE_IDS" ]]; then
    value="$(jq -rn --arg s "$MESSAGE_IDS" '$s|@uri')"
    query="${query}&message_ids=${value}"
  fi
  if [[ -n "$CHAT_ID" ]]; then
    value="$(jq -rn --arg s "$CHAT_ID" '$s|@uri')"
    query="${query}&chat_id=${value}"
  fi
  if [[ -n "$SENDER_OPEN_ID" ]]; then
    value="$(jq -rn --arg s "$SENDER_OPEN_ID" '$s|@uri')"
    query="${query}&sender_open_id=${value}"
  fi
  if [[ -n "$KEYWORD" ]]; then
    value="$(jq -rn --arg s "$KEYWORD" '$s|@uri')"
    query="${query}&keyword=${value}"
  fi
  if [[ -n "$DATE" ]]; then
    local bounds from until
    bounds="$(local_day_bounds "$DATE" query-messages)" || return 1
    read -r from until <<<"$bounds"
    query="${query}&from=$(jq -rn --arg s "$from" '$s|@uri')&until=$(jq -rn --arg s "$until" '$s|@uri')"
  fi
  if [[ -n "$MESSAGE_IDS" ]]; then
    api_get "/api/messages?${query}" | jq -c '{items:[.data.items[] | {id,message_id}]}'
  else
    api_get "/api/messages?${query}" | json_data
  fi
}

cmd_get_message() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-message requires positive --id"
  api_get "/api/messages/${ID}" | json_data
}

cmd_get_todo_event() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-todo-event requires positive --id"
  api_get "/api/todo-events/${ID}" | json_data
}

cmd_get_task_event() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-task-event requires positive --id"
  api_get "/api/task-events/${ID}" | json_data
}

cmd_query_captured_resources() {
  require_limit query-captured-resources "$LIMIT" 100
  local query="limit=${LIMIT}" value
  if [[ -n "$CHAT_ID" ]]; then
    value="$(jq -rn --arg s "$CHAT_ID" '$s|@uri')"
    query="${query}&chat_id=${value}"
  fi
  if [[ -n "$MESSAGE_ID" ]]; then
    value="$(jq -rn --arg s "$MESSAGE_ID" '$s|@uri')"
    query="${query}&message_id=${value}"
  fi
  if [[ -n "$RESOURCE_TYPE" ]]; then
    value="$(jq -rn --arg s "$RESOURCE_TYPE" '$s|@uri')"
    query="${query}&resource_type=${value}"
  fi
  if [[ -n "$KEYWORD" ]]; then
    value="$(jq -rn --arg s "$KEYWORD" '$s|@uri')"
    query="${query}&keyword=${value}"
  fi
  api_get "/api/captured-resources?${query}" | json_data
}

cmd_get_captured_resource() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-captured-resource requires positive --id"
  api_get "/api/captured-resources/${ID}" | json_data
}

cmd_append_clue() {
  [[ -n "$SOURCE" ]] || fail "append-clue requires --source"
  [[ -n "$EXTERNAL_ID" ]] || fail "append-clue requires --external-id"
  [[ -n "$TITLE" ]] || fail "append-clue requires --title"
  local content
  if [[ "$CONTENT" == "-" ]]; then
    content="$(cat)"
  else
    content="$CONTENT"
  fi
  content="${content%$'\n'}"
  local occurred_at="$OCCURRED_AT"
  if [[ -z "$occurred_at" ]]; then
    occurred_at="$(date +%Y-%m-%dT%H:%M:%S%z | sed -E 's/([0-9]{2})([0-9]{2})$/\1:\2/')"
  fi
  local payload body
  payload="$(printf '%s' "$content" | jq -Rsc --arg source "$SOURCE" --arg external_id "$EXTERNAL_ID" \
    --arg title "$TITLE" --arg occurred_at "$occurred_at" \
    '{source:$source,external_id:$external_id,title:$title,content:.,occurred_at:$occurred_at}')"
  body="$(api_write POST /api/clues "$payload")"
  printf '%s' "$body" | json_data
}

cmd_append_fact() {
  [[ -n "$SUBJECT_TYPE" ]] || fail "append-fact requires --subject-type"
  [[ "$SUBJECT_ID" =~ ^[1-9][0-9]*$ ]] || fail "append-fact requires a positive --subject-id"
  local description
  if [[ "$DESCRIPTION" == "-" ]]; then
    description="$(cat)"
  else
    description="$DESCRIPTION"
  fi
  description="${description%$'\n'}"
  [[ -n "${description//[[:space:]]/}" ]] || fail "append-fact description is empty"
  local occurred_at="$OCCURRED_AT"
  if [[ -z "$occurred_at" ]]; then
    occurred_at="$(date +%Y-%m-%dT%H:%M:%S%z | sed -E 's/([0-9]{2})([0-9]{2})$/\1:\2/')"
  fi
  local source_kind="$SOURCE"
  local source_id="null"
  [[ -n "$source_kind" ]] || fail "append-fact requires --source"
  if [[ -n "$SOURCE_ID" ]]; then
    [[ "$SOURCE_ID" =~ ^[1-9][0-9]*$ ]] || fail "append-fact --source-id must be a positive integer"
    source_id="$SOURCE_ID"
  fi
  local payload body
  payload="$(jq -cn --arg subject_type "$SUBJECT_TYPE" --argjson subject_id "$SUBJECT_ID" \
    --arg description "$description" --arg occurred_at "$occurred_at" \
    --arg source_kind "$source_kind" --argjson source_id "$source_id" \
    '{subject_type:$subject_type,subject_id:$subject_id,description:$description,occurred_at:$occurred_at,
      source_kind:(if $source_kind=="" then null else $source_kind end),source_id:$source_id}')"
  body="$(api_write POST /api/facts "$payload")"
  printf '%s' "$body" | json_data
}

cmd_append_facts_batch() {
  local payload occurred_at normalized
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  occurred_at="$(date +%Y-%m-%dT%H:%M:%S%z | sed -E 's/([0-9]{2})([0-9]{2})$/\1:\2/')"
  normalized="$(printf '%s' "$payload" | jq -ce \
    --arg occurred_at "$occurred_at" '
      def nonblank: type == "string" and test("[^[:space:]]");
      def positive_integer: type == "number" and . > 0 and floor == .;
      if type != "array" or length == 0 then
        error("payload must be a non-empty JSON array")
      else
        map(
          if type != "object" then
            error("every fact must be a JSON object")
          elif ((keys_unsorted - ["subject_type", "subject_id", "description", "occurred_at", "source", "source_id"]) | length) != 0 then
            error("fact contains an unknown field")
          elif (.subject_type | nonblank) | not then
            error("subject_type must be a non-empty string")
          elif (.subject_id | positive_integer) | not then
            error("subject_id must be a positive integer")
          elif (.description | nonblank) | not then
            error("description must be a non-empty string")
          elif (has("occurred_at") and .occurred_at != null and (.occurred_at | type != "string")) then
            error("occurred_at must be a string or null")
          elif (.source | nonblank) | not then
            error("source must be a non-empty string")
          elif (has("source_id") and .source_id != null and ((.source_id | positive_integer) | not)) then
            error("source_id must be a positive integer or null")
          else
            {
              subject_type: .subject_type,
              subject_id: .subject_id,
              description: .description,
              occurred_at: (if ((.occurred_at // "") == "") then $occurred_at else .occurred_at end),
              source_kind: .source,
              source_id: (.source_id // null)
            }
          end
        )
      end
    ')" || fail "append-facts-batch payload is invalid"
  body="$(api_write POST /api/facts/batch "$normalized")"
  printf '%s' "$body" | json_data data items
}

cmd_list_facts() {
  [[ -n "$SUBJECT_TYPE" ]] || fail "list-facts requires --subject-type"
  [[ "$SUBJECT_ID" =~ ^[1-9][0-9]*$ ]] || fail "list-facts requires a positive --subject-id"
  require_limit list-facts "$LIMIT" 100
  local enc_type query
  enc_type="$(jq -rn --arg s "$SUBJECT_TYPE" '$s|@uri')"
  query="?subject_type=${enc_type}&subject_id=${SUBJECT_ID}&limit=${LIMIT}"
  if [[ -n "$DATE" ]]; then
    local bounds from until
    bounds="$(local_day_bounds "$DATE" list-facts)" || return 1
    read -r from until <<<"$bounds"
    query="${query}&from=$(jq -rn --arg s "$from" '$s|@uri')&until=$(jq -rn --arg s "$until" '$s|@uri')"
  fi
  local body
  body="$(api_get "/api/facts${query}")"
  printf '%s' "$body" | json_data
}
