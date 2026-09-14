# notify commands: help, accepted flags and handlers.

notify_help() {
  case "$1" in
    notice-principal) cat <<'EOF'
usage: jarvis-tools notice-principal [--payload JSON|- | --payload-file PATH]

Send one Bot card to the configured principal. No recipient or card JSON needed.
Required: content (Markdown), idempotency_key (stable, <=50 bytes).
Optional: type (defaults to Notice), links [{label,url}], details (folded
Markdown), extra (free text or open JSON, displayed in a supplemental panel),
task_id (defaults to JARVIS_TASK_ID; validates/audits the task and adds its details link).
The task link uses the runtime listen address; packaged local links require the
computer running Jarvis. Use links for business materials, not hand-built task URLs.

type is free text, not an enum. There is no separate title field.

Reuse the same idempotency_key on retry. An error can contain a partial delivery
receipt: inspect message_id/send_response, prior run effects and
var/log/principal-notices.jsonl before retry.
The Feishu deduplication window is limited; this is not exactly-once delivery.
Success returns verified, message_id, chat_id, url and effect. M5 records the
returned effect verbatim in its run effects. Sending does not change Task state.

example:
  jarvis-tools notice-principal --payload-file notice.json
  jarvis-tools notice-principal --payload - < notice.json
EOF
      ;;
    *) return 1 ;;
  esac
}

notify_flags() {
  case "$1" in
    notice-principal) printf '%s' '--payload --payload-file' ;;
  esac
}

cmd_notice_principal() {
  [[ -z "$PAYLOAD_FILE" || "$PAYLOAD" == "-" ]] || fail "choose --payload or --payload-file"
  if [[ -n "$PAYLOAD_FILE" ]]; then
    [[ -f "$PAYLOAD_FILE" ]] || fail "notice payload file not found: $PAYLOAD_FILE"
    PAYLOAD="$(cat -- "$PAYLOAD_FILE")"
  fi
  local payload body
  payload="$(read_payload_object notice-principal)"
  if [[ -n "${JARVIS_TASK_ID:-}" ]] && ! jq -e 'has("task_id")' <<<"$payload" >/dev/null; then
    [[ "$JARVIS_TASK_ID" =~ ^[1-9][0-9]*$ ]] || fail "JARVIS_TASK_ID must be positive"
    payload="$(printf '%s' "$payload" | json_data --set task_id "$JARVIS_TASK_ID")"
  fi
  body="$(api_write POST /api/notices/principal "$payload" 300)"
  printf '%s' "$body" | json_data
}
