# memory commands: help, accepted flags and handlers.

memory_help() {
  case "$1" in
    get-shared-memory) cat <<'EOF'
usage: jarvis-tools get-shared-memory
Read the complete shared memory Markdown.
EOF
      ;;
    set-shared-memory) cat <<'EOF'
usage: jarvis-tools set-shared-memory [--content TEXT|-]
Replace shared memory. The complete content must not exceed 2000 characters.
EOF
      ;;
    append-shared-memory) cat <<'EOF'
usage: jarvis-tools append-shared-memory [--note TEXT|-]
Atomically append one non-empty entry. The result must not exceed 2000 characters.
Use --note - or omit it to read stdin.
EOF
      ;;
    *) return 1 ;;
  esac
}

memory_flags() {
  case "$1" in
    get-shared-memory) printf '%s' '' ;;
    set-shared-memory) printf '%s' --content ;;
    append-shared-memory) printf '%s' --note ;;
  esac
}

cmd_get_shared_memory() {
  local body
  body="$(api_get /api/shared-memory)"
  printf '%s' "$body" | json_data
}

cmd_set_shared_memory() {
  local content payload body
  if [[ "$CONTENT" == "-" ]]; then
    content="$(cat)"
  else
    content="$CONTENT"
  fi
  content="${content%$'\n'}"
  payload="$(jq -cn --arg content "$content" '{content:$content}')"
  body="$(api_write PUT /api/shared-memory "$payload")"
  printf '%s' "$body" | json_data
}

cmd_append_shared_memory() {
  local note
  note="$(read_note_arg)"
  if [[ -z "${note//[[:space:]]/}" ]]; then
    fail "append-shared-memory note is empty"
  fi
  local payload body
  payload="$(jq -cn --arg note "$note" '{note:$note}')"
  body="$(api_write POST /api/shared-memory/append "$payload")"
  printf '%s' "$body" | json_data
}
