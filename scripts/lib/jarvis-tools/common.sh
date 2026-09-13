# Shared input, flags and HTTP transport. Sourced by the fixed CLI entrypoint.
fail() { printf 'jarvis-tools error: %s\n' "$*" >&2; exit 1; }

payload_id_help() {
  local command_name="$1" description="$2"
  printf 'usage: jarvis-tools %s --id ID [--payload JSON|-]\n%s The payload must be a JSON object; use - or omit it to read stdin.\n' \
    "$command_name" "$description"
}

delete_id_help() {
  local command_name="$1" description="$2"
  printf 'usage: jarvis-tools %s --id ID\n%s\n' "$command_name" "$description"
}

read_note_arg() {
  local text
  if [[ "$NOTE" == "-" ]]; then
    text="$(cat)"
  else
    text="$NOTE"
  fi
  # strip trailing newlines
  printf '%s' "${text%$'\n'}"
}

read_payload_object() {
  local command_name="$1" payload
  if [[ "$PAYLOAD" == "-" ]]; then
    payload="$(cat)"
  else
    payload="$PAYLOAD"
  fi
  printf '%s' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1 ||
    fail "${command_name} payload must be a JSON object"
  printf '%s' "$payload"
}

json_data() { node "${SCRIPT_DIR}/json-api-data.mjs" "$@"; }
emit_api_data() {
  local body
  body="$(api_get "$1")" || return
  printf '%s' "$body" | json_data
}

resolve_timezone() {
  printf '%s' "$JARVIS_CONNECTION_TIMEZONE"
}

# One transport for every verb, including page CAS and notice delivery. No retries.
# Failed responses keep the original body (including partial delivery receipts).
api_request() {
  local method="$1" path="$2" payload="${3:-}" timeout="${4:-30}"
  local url="${API_BASE}${path}" response body http_code code
  local -a options=(-sS -w $'\n%{http_code}' --max-time "$timeout" -X "$method")
  if [[ "$method" == GET ]]; then
    response="$(curl "${options[@]}" "$url")" || fail "curl failed for ${url}: ${response}"
  else
    response="$(printf '%s' "$payload" | curl "${options[@]}" -H 'Content-Type: application/json' --data-binary @- "$url")" ||
      fail "curl failed for ${url}: ${response}"
  fi
  http_code="${response##*$'\n'}"
  body="${response%$'\n'*}"
  if [[ "$http_code" == 204 && -z "$body" ]]; then
    printf '%s' '{"code":0,"data":null}'
    return
  fi
  if [[ "$http_code" == 404 ]]; then
    fail "not found (HTTP 404) from ${url}: ${body}"
  fi
  [[ "$http_code" =~ ^2[0-9][0-9]$ ]] || fail "HTTP ${http_code} from ${url}: ${body}"
  # Only project the machine code. The original body goes unchanged to json_data.
  code="$(printf '%s' "$body" | jq -er 'if type == "object" and (.code | type == "number") then .code else error("missing numeric code") end' 2>/dev/null)" ||
    fail "invalid API response from ${url}: ${body}"
  [[ "$code" != 40420 ]] || fail "not found (API code=40420) from ${url}: ${body}"
  [[ "$code" == 0 ]] || fail "API code=${code} from ${url}: ${body}"
  printf '%s' "$body"
}
api_get() { api_request GET "$1"; }
api_write() { api_request "$@"; }

COMMAND_COUNT=0
COMMAND_NAMES=()
COMMAND_GROUPS=()
COMMAND_SUMMARIES=()
COMMAND_HANDLERS=()
register_command() {
  local i="$COMMAND_COUNT"
  COMMAND_COUNT=$((COMMAND_COUNT + 1))
  COMMAND_NAMES[$i]="$1"
  COMMAND_GROUPS[$i]="$2"
  COMMAND_SUMMARIES[$i]="$3"
  COMMAND_HANDLERS[$i]="$4"
}

print_usage() {
  cat <<'EOF'
usage: jarvis-tools [--api-base URL] <command> [flags]

groups:
  world      Entities, current context and long-term fact pages
  evidence   Original messages, captured materials, clues and fact indexes
  task       Todos, delegations, Tasks and execution history
  schedule   Time triggers and Task continuation
  memory     Shared behavioral memory
  skill      Locally managed Skills
  notify     Principal notifications and delivery receipts

how-to:
  jarvis-tools help <group>       Discover commands in one group
  jarvis-tools help all           List every command
  jarvis-tools <command> --help   Read inputs, examples and outputs offline
  Known flat command names can be invoked directly; groups are for discovery.
  --api-base URL > JARVIS_API_BASE > effective workspace config.
  --config PATH > JARVIS_CONFIG_PATH > tool workspace conf/config.yaml.
  --date uses JARVIS_TIMEZONE or configured timezone. Help requires no server, curl, jq or Node.
  Data: JSON on stdout. Errors: original response on stderr and nonzero exit.
EOF
}

print_group_help() {
  local group="$1" i
  case "$group" in world|evidence|task|schedule|memory|skill|notify|all) ;; *) fail "unknown help group: ${group}" ;; esac
  printf '%s commands:\n' "$group"
  for ((i=0; i<${#COMMAND_NAMES[@]}; i++)); do
    if [[ "$group" == all || "${COMMAND_GROUPS[$i]}" == "$group" ]]; then
      printf '  %-24s %s\n' "${COMMAND_NAMES[$i]}" "${COMMAND_SUMMARIES[$i]}"
    fi
  done
  printf '\nUse jarvis-tools <command> --help. Global: --api-base URL, --config PATH; defaults to workspace config.\n'
}

require_command_flag() {
  local cmd="$1" flag="$2" allowed
  allowed="--api-base --config $("${COMMAND_GROUP}_flags" "$cmd")"
  case " ${allowed} " in
    *" ${flag} "*) ;;
    *) fail "${cmd} does not accept ${flag}" ;;
  esac
}

main() {
  initialize_flags
  # Global address may precede the flat command or follow it among command flags.
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --config)
        [[ $# -ge 2 && -n "$2" ]] || fail "--config requires a value"
        CONFIG_PATH_FLAG="$2"; shift 2 ;;
      --config=*)
        CONFIG_PATH_FLAG="${1#*=}"
        [[ -n "$CONFIG_PATH_FLAG" ]] || fail "--config requires a value"
        shift ;;
      --api-base)
        [[ $# -ge 2 && -n "$2" ]] || fail "--api-base requires a value"
        API_BASE_FLAG="$2"; shift 2 ;;
      --api-base=*)
        API_BASE_FLAG="${1#*=}"
        [[ -n "$API_BASE_FLAG" ]] || fail "--api-base requires a value"
        shift ;;
      *) break ;;
    esac
  done
  [[ $# -gt 0 ]] || { print_usage >&2; return 1; }
  SUBCOMMAND="$1"; shift
  case "$SUBCOMMAND" in
    --help|-h) [[ $# -eq 0 ]] || fail "unexpected help arguments"; print_usage; return ;;
    help) [[ $# -eq 1 ]] || fail "usage: jarvis-tools help <group>|all"; print_group_help "$1"; return ;;
  esac
  local i handler="" COMMAND_GROUP=""
  for ((i=0; i<${#COMMAND_NAMES[@]}; i++)); do
    if [[ "${COMMAND_NAMES[$i]}" == "$SUBCOMMAND" ]]; then
      COMMAND_GROUP="${COMMAND_GROUPS[$i]}"
      handler="${COMMAND_HANDLERS[$i]}"
      break
    fi
  done
  [[ -n "$handler" ]] || fail "unknown subcommand \"${SUBCOMMAND}\"; run jarvis-tools --help"
  if [[ $# -eq 1 && ( "$1" == --help || "$1" == -h ) ]]; then
    "${COMMAND_GROUP}_help" "$SUBCOMMAND"
    return
  fi
  consume_flags "$@"
  local dependency
  for dependency in curl jq node; do
    command -v "$dependency" >/dev/null 2>&1 || fail "${dependency} is required but not found in PATH"
  done
  local need_timezone=false
  [[ -z "$DATE" ]] || need_timezone=true
  jarvis_connection "$(cd "${SCRIPT_DIR}/.." && pwd)" "$CONFIG_PATH_FLAG" "$API_BASE_FLAG" "" "$need_timezone"
  API_BASE="$JARVIS_CONNECTION_API_BASE"
  "$handler"
}

initialize_flags() {
  ID=""
  CODE=""
  CHAT_ID=""
  OPEN_ID=""
  ROLE=""
  PROJECT_ID=""
  GROUP_ID=""
  PERSON_OPEN_ID=""
  PRINCIPAL_ONLY="false"
  KEYWORD=""
  NOTE="-"
  NAME=""
  STATUS=""
  LIMIT="20"
  PAYLOAD="-"
  PAYLOAD_FILE=""
  AT=""
  REASON="-"
  SOURCE=""
  SOURCE_ID=""
  ACTOR=""
  EXTERNAL_ID=""
  TITLE=""
  CONTENT="-"
  OCCURRED_AT=""
  SUBJECT_TYPE=""
  SUBJECT_ID=""
  DESCRIPTION="-"
  DATE=""
  TYPE=""
  ALL="false"
  STALE_DAYS=""
  OVER_LIMIT="false"
  IF_UNCHANGED_SINCE=""
  INCLUDE_PROMPT="false"
  MESSAGE_ID=""
  MESSAGE_IDS=""
  RESOURCE_TYPE=""
  SENDER_OPEN_ID=""
  PAGE="1"
  QUERY=""
  SOURCE_MESSAGE_ID=""
  ACTION_TYPE=""
  CONTEXT_SECTION=""
  REVISION=""
  API_BASE_FLAG=""
  CONFIG_PATH_FLAG=""
}

require_limit() {
  local cmd="$1" value="$2" max="$3"
  [[ "$value" =~ ^[1-9][0-9]*$ ]] || fail "${cmd} limit must be a positive integer"
  (( value <= max )) || fail "${cmd} limit must not exceed ${max}"
}

consume_flags() {
  local flag value variable
  while [[ $# -gt 0 ]]; do
    flag="${1%%=*}"
    require_command_flag "$SUBCOMMAND" "$flag"
    case "$flag" in
      --principal-only) [[ "$1" == "$flag" ]] || fail "${flag} does not take a value"; PRINCIPAL_ONLY="true"; shift; continue ;;
      --include-prompt) [[ "$1" == "$flag" ]] || fail "${flag} does not take a value"; INCLUDE_PROMPT="true"; shift; continue ;;
      --all) [[ "$1" == "$flag" ]] || fail "${flag} does not take a value"; ALL="true"; shift; continue ;;
      --over-limit) [[ "$1" == "$flag" ]] || fail "${flag} does not take a value"; OVER_LIMIT="true"; shift; continue ;;
    esac
    if [[ "$1" == *=* ]]; then
      value="${1#*=}"
      shift
    else
      [[ $# -ge 2 ]] || fail "${flag} requires a value"
      value="$2"
      shift 2
    fi
    case "$flag" in
      --api-base) variable=API_BASE_FLAG ;;
      --config) variable=CONFIG_PATH_FLAG ;;
      --id) variable=ID ;;
      --code) variable=CODE ;;
      --chat-id) variable=CHAT_ID ;;
      --open-id) variable=OPEN_ID ;;
      --role) variable=ROLE ;;
      --project-id) variable=PROJECT_ID ;;
      --group-id) variable=GROUP_ID ;;
      --person-open-id) variable=PERSON_OPEN_ID ;;
      --page) variable=PAGE ;;
      --query) variable=QUERY ;;
      --source-message-id) variable=SOURCE_MESSAGE_ID ;;
      --action-type) variable=ACTION_TYPE ;;
      --context) variable=CONTEXT_SECTION ;;
      --revision) variable=REVISION ;;
      --keyword) variable=KEYWORD ;;
      --note) variable=NOTE ;;
      --name) variable=NAME ;;
      --state) variable=STATUS ;;
      --status) variable=STATUS ;;
      --limit) variable=LIMIT ;;
      --payload) variable=PAYLOAD ;;
      --payload-file) variable=PAYLOAD_FILE ;;
      --at) variable=AT ;;
      --reason) variable=REASON ;;
      --source) variable=SOURCE ;;
      --source-id) variable=SOURCE_ID ;;
      --actor) variable=ACTOR ;;
      --external-id) variable=EXTERNAL_ID ;;
      --title) variable=TITLE ;;
      --content) variable=CONTENT ;;
      --subject-type) variable=SUBJECT_TYPE ;;
      --subject-id) variable=SUBJECT_ID ;;
      --description) variable=DESCRIPTION ;;
      --date) variable=DATE ;;
      --occurred-at) variable=OCCURRED_AT ;;
      --message-ids) variable=MESSAGE_IDS ;;
      --message-id) variable=MESSAGE_ID ;;
      --resource-type) variable=RESOURCE_TYPE ;;
      --sender-open-id) variable=SENDER_OPEN_ID ;;
      --type) variable=TYPE ;;
      --stale-days) variable=STALE_DAYS ;;
      --if-unchanged-since) variable=IF_UNCHANGED_SINCE ;;
      *) fail "unknown flag: ${flag}" ;;
    esac
    case "$flag" in
      --api-base|--config|--message-ids) [[ -n "$value" ]] || fail "${flag} requires a nonempty value" ;;
    esac
    printf -v "$variable" '%s' "$value"
  done
}

local_day_bounds() {
  local date="$1" cmd="$2" timezone
  [[ "$date" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || fail "${cmd} --date must be YYYY-MM-DD"
  timezone="$(resolve_timezone)" || return 1
  local from until
  local -a date_command=(env "TZ=$timezone" date)
  if date --version >/dev/null 2>&1; then
    from="$("${date_command[@]}" -d "$date" +%Y-%m-%dT%H:%M:%S%z 2>/dev/null)" ||
      fail "${cmd} could not parse --date $date"
    until="$("${date_command[@]}" -d "$date +1 day" +%Y-%m-%dT%H:%M:%S%z 2>/dev/null)" ||
      fail "${cmd} could not advance --date $date"
  else
    from="$("${date_command[@]}" -j -f '%Y-%m-%d %H:%M:%S' "$date 00:00:00" +%Y-%m-%dT%H:%M:%S%z 2>/dev/null)" ||
      fail "${cmd} could not parse --date $date"
    until="$("${date_command[@]}" -j -v+1d -f '%Y-%m-%d %H:%M:%S' "$date 00:00:00" +%Y-%m-%dT%H:%M:%S%z 2>/dev/null)" ||
      fail "${cmd} could not advance --date $date"
  fi
  [[ "${from:0:10}" == "$date" ]] || fail "${cmd} could not parse --date $date"
  from="$(printf '%s' "$from" | sed -E 's/([0-9]{2})([0-9]{2})$/\1:\2/')"
  until="$(printf '%s' "$until" | sed -E 's/([0-9]{2})([0-9]{2})$/\1:\2/')"
  printf '%s %s' "$from" "$until"
}
