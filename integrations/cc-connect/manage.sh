#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'cc-connect integration: %s\n' "$*" >&2
  exit 1
}

command -v jq >/dev/null 2>&1 || fail "jq is required but not found in PATH"
command -v curl >/dev/null 2>&1 || fail "curl is required but not found in PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CONFIG_PATH="${JARVIS_CONFIG:-${REPO_ROOT}/conf/config.yaml}"
CC_CONFIG_PATH="${HOME}/.cc-connect/config.toml"
REUSE_EXISTING_SECRET=false
REPLACE_ALLOW_FROM=false
APP_SECRET=""

usage() {
  cat <<'EOF'
usage: manage.sh <bind|validate> [flags]

Project-owned adapter for the Jarvis CC Connect integration. It is called by
scripts/jarvis-install and is not exposed to Jarvis M3/M5.

commands:
  bind      Create the jarvis-codex project when absent and bind it to the
            current default lark-cli App. Reads one App Secret from stdin/TTY,
            unless --reuse-existing-secret is explicitly selected. Sets Feishu
            allow_from to the configured principal open_id.
  validate  Validate the default lark-cli identity, CC project route,
            principal-only Feishu access, context bootstrap contract and
            localhost approval relay.

flags:
  --config PATH
  --cc-config PATH
  --reuse-existing-secret   bind only; keep the current non-placeholder secret
  --replace-allow-from      bind only; explicitly replace an existing non-principal allow_from
EOF
}

consume_flags() {
  while (( $# > 0 )); do
    case "$1" in
      --config)
        (( $# >= 2 )) || fail "--config requires a value"
        CONFIG_PATH="$2"
        shift 2
        ;;
      --cc-config)
        (( $# >= 2 )) || fail "--cc-config requires a value"
        CC_CONFIG_PATH="$2"
        shift 2
        ;;
      --reuse-existing-secret)
        REUSE_EXISTING_SECRET=true
        shift
        ;;
      --replace-allow-from)
        REPLACE_ALLOW_FROM=true
        shift
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *) fail "unknown argument: $1" ;;
    esac
  done
  if [[ "$CONFIG_PATH" != /* ]]; then
    CONFIG_PATH="${REPO_ROOT}/${CONFIG_PATH}"
  fi
}

emit() {
  printf '%s\n' "$1" | jq -c .
}

toml_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\t'/\\t}"
  printf '%s' "$value"
}

sha256_text() {
  if command -v shasum >/dev/null 2>&1; then
    LC_ALL=C LANG=C shasum -a 256 | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    LC_ALL=C LANG=C sha256sum | awk '{print $1}'
  else
    fail "shasum or sha256sum is required but not found in PATH"
  fi
}

lark_default_config() {
  command -v lark-cli >/dev/null 2>&1 || fail "lark-cli is required but not found in PATH"
  local output json error_file error_output
  error_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-cc-lark-config.XXXXXX")"
  if ! output="$(
    LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1 \
    LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1 \
      lark-cli config show 2>"$error_file"
  )"; then
    error_output="$(<"$error_file")"
    rm -f "$error_file"
    fail "lark-cli config show failed for the current default identity${error_output:+: ${error_output}}"
  fi
  rm -f "$error_file"
  json="$(printf '%s\n' "$output" | sed -n '/^{/,$p')"
  printf '%s' "$json" | jq -e 'type == "object" and ((.appId // "") | length > 0)' >/dev/null 2>&1 || \
    fail "lark-cli config show did not return appId for the current default identity"
  printf '%s' "$json"
}

configured_api_base() {
  "${REPO_ROOT}/scripts/jarvis-instance" "$CONFIG_PATH" | jq -er .api_base
}

card_callback_preflight() {
  local output json error_file error_output
  error_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-cc-card-preflight.XXXXXX")"
  if ! output="$(
    LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1 \
    LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1 \
      lark-cli event consume card.action.trigger --as bot --dry-run 2>"$error_file"
  )"; then
    error_output="$(<"$error_file")"
    rm -f "$error_file"
    fail "card.action.trigger App permission/event preflight failed: ${output}${error_output:+$'\n'${error_output}}"
  fi
  rm -f "$error_file"
  json="$(printf '%s\n' "$output" | sed -n '/^{/,$p')"
  printf '%s' "$json" | jq -e '
    .ok == true and
    .data.decision.event_key == "card.action.trigger" and
    .data.decision.identity == "bot" and
    .data.decision.status == "ready" and
    ([.data.decision.preconditions[]? | select(.name == "console_event_published" and .status == "ok")] | length == 1) and
    ([.data.decision.preconditions[]? | select(.name == "scopes_granted" and .status == "ok")] | length == 1)
  ' >/dev/null 2>&1 || fail "card.action.trigger App permission/event preflight was not ready: ${output}"
  printf '%s' "$json"
}

cc_app_credentials_probe() {
  local app_id="$1" app_secret="$2" brand="$3" endpoint response
  case "$brand" in
    lark) endpoint="https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal" ;;
    feishu|"") endpoint="https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" ;;
    *) fail "unsupported lark-cli brand for CC credential validation: ${brand}" ;;
  esac
  if ! response="$(curl -fsS --max-time 15 -X POST "$endpoint" \
    -H 'Content-Type: application/json; charset=utf-8' \
    --data "$(jq -nc --arg app_id "$app_id" --arg app_secret "$app_secret" '{app_id:$app_id,app_secret:$app_secret}')")"; then
    fail "validate CC Connect App credentials failed to reach ${endpoint}"
  fi
  printf '%s' "$response" | jq -ce '
    {
      ok: ((.code // -1) == 0 and ((.tenant_access_token // "") | length) > 0),
      error: (if ((.code // -1) == 0) then null else (.msg // "credential validation failed") end)
    }'
}

configured_identity() {
  command -v go >/dev/null 2>&1 || fail "go is required but not found in PATH"
  local output error_file error_output
  error_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-cc-identity.XXXXXX")"
  if ! output="$(
    cd "$REPO_ROOT"
    go run ./cmd/jarvis-config show-principal --config "$CONFIG_PATH" 2>"$error_file"
  )"; then
    error_output="$(<"$error_file")"
    rm -f "$error_file"
    fail "load initialized Jarvis identity failed${error_output:+: ${error_output}}"
  fi
  rm -f "$error_file"
  printf '%s' "$output"
}

jarvis_project_count() {
  [[ -f "$CC_CONFIG_PATH" ]] || { printf '0\n'; return; }
  awk '
    function flush() { if (inside && project_name == "jarvis-codex") found++ }
    /^[[:space:]]*\[\[projects\]\][[:space:]]*$/ {
      flush(); inside = 1; project_name = ""; next
    }
    inside && /^[[:space:]]*name[[:space:]]*=/ {
      value = $0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      sub(/[[:space:]]*#.*/, "", value)
      gsub(/^[[:space:]]*"|"[[:space:]]*$/, "", value)
      project_name = value
    }
    END { flush(); print found + 0 }
  ' "$CC_CONFIG_PATH"
}

cc_jarvis_project_block() {
  [[ -f "$CC_CONFIG_PATH" ]] || fail "CC Connect config not found: ${CC_CONFIG_PATH}"
  local block
  if ! block="$(awk -v target="jarvis-codex" '
    function flush() {
      if (inside && project_name == target) {
        found++
        if (found == 1) printf "%s", block
      }
    }
    /^[[:space:]]*\[\[projects\]\][[:space:]]*$/ {
      flush(); inside = 1; project_name = ""; block = $0 ORS; next
    }
    {
      if (!inside) next
      block = block $0 ORS
      if ($0 ~ /^[[:space:]]*name[[:space:]]*=/) {
        value = $0
        sub(/^[^=]*=[[:space:]]*/, "", value)
        sub(/[[:space:]]*#.*/, "", value)
        gsub(/^[[:space:]]*"|"[[:space:]]*$/, "", value)
        project_name = value
      }
    }
    END { flush(); if (found != 1) exit 7 }
  ' "$CC_CONFIG_PATH")"; then
    fail "CC Connect config must contain exactly one [[projects]] block named jarvis-codex"
  fi
  printf '%s\n' "$block"
}

toml_string_value() {
  local block="$1" key="$2" raw
  if ! raw="$(printf '%s\n' "$block" | awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      count++; value = $0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      sub(/[[:space:]]*#.*/, "", value)
      print value
    }
    END { if (count != 1) exit 8 }
  ')"; then
    fail "jarvis-codex must contain exactly one ${key} setting"
  fi
  [[ "$raw" == \"*\" && ${#raw} -ge 2 ]] || fail "jarvis-codex ${key} must be a literal quoted string"
  raw="${raw#\"}"
  raw="${raw%\"}"
  [[ -n "$raw" ]] || fail "jarvis-codex ${key} must not be empty"
  printf '%s' "$raw"
}

toml_bool_value() {
  local block="$1" key="$2" raw
  if ! raw="$(printf '%s\n' "$block" | awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      count++; value = $0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      sub(/[[:space:]]*#.*/, "", value)
      gsub(/[[:space:]]/, "", value)
      print value
    }
    END { if (count != 1) exit 8 }
  ')"; then
    fail "jarvis-codex must contain exactly one ${key} setting"
  fi
  [[ "$raw" == "true" || "$raw" == "false" ]] || fail "jarvis-codex ${key} must be true or false"
  printf '%s' "$raw"
}

toml_section_string_value() {
  local block="$1" section="$2" key="$3" raw
  if ! raw="$(printf '%s\n' "$block" | awk -v wanted_section="$section" -v key="$key" '
    /^[[:space:]]*\[/ { current = $0; gsub(/[[:space:]]/, "", current); next }
    current == wanted_section && $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      count++; value = $0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      sub(/[[:space:]]+#.*/, "", value)
      print value
    }
    END { if (count != 1) exit 8 }
  ')"; then
    fail "jarvis-codex ${section} must contain exactly one ${key} setting"
  fi
  [[ "$raw" == \"*\" && ${#raw} -ge 2 ]] || fail "jarvis-codex ${section} ${key} must be a literal quoted string"
  raw="${raw#\"}"
  raw="${raw%\"}"
  [[ -n "$raw" ]] || fail "jarvis-codex ${section} ${key} must not be empty"
  printf '%s' "$raw"
}

toml_optional_section_string_value() {
  local block="$1" section="$2" key="$3"
  printf '%s\n' "$block" | awk -v wanted_section="$section" -v key="$key" '
    /^[[:space:]]*\[/ { current = $0; gsub(/[[:space:]]/, "", current); next }
    current == wanted_section && $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      value = $0
      sub(/^[^=]*=[[:space:]]*"/, "", value)
      sub(/"[[:space:]]*(#.*)?$/, "", value)
      print value
      exit
    }
  '
}

cc_bootstrap_prompt() {
  local prompt_path="${REPO_ROOT}/conf/prompts/cc-system-prompt.md" prompt
  [[ -s "$prompt_path" ]] || fail "CC system prompt is missing or empty: ${prompt_path}"
  prompt="$(<"$prompt_path")"
  prompt="${prompt//\{\{REPO_ROOT\}\}/$REPO_ROOT}"
  [[ "$prompt" != *'{{REPO_ROOT}}'* ]] || fail "CC system prompt still contains unresolved REPO_ROOT"
  printf '%s' "$prompt"
}

append_fresh_project() {
  local identity="$1" app_id="$2" relay_secret principal_open_id prompt config_dir api_base
  api_base="$(configured_api_base)"
  relay_secret="$(jq -r '.relay_secret // ""' <<<"$identity")"
  [[ -n "$relay_secret" ]] || fail "Jarvis relay secret is empty; configure machine identity first"
  principal_open_id="$(jq -r '.principal_open_id // ""' <<<"$identity")"
  [[ -n "$principal_open_id" && "$principal_open_id" != "null" ]] || fail "Jarvis principal open_id is empty; configure machine identity first"
  config_dir="$(dirname "$CC_CONFIG_PATH")"
  mkdir -p "$config_dir"
  touch "$CC_CONFIG_PATH"
  chmod 0600 "$CC_CONFIG_PATH"
  prompt="$(cc_bootstrap_prompt)"
  printf '\n[[projects]]\nname = "jarvis-codex"\ninject_sender = true\n\n[projects.display]\nmode = "quiet"\nthinking_messages = false\ntool_messages = false\n\n[projects.agent]\ntype = "codex"\n\n[projects.agent.options]\nwork_dir = "%s"\nmode = "yolo"\ncmd = "codex"\nappend_system_prompt = "%s"\n\n[[projects.platforms]]\ntype = "feishu"\n\n[projects.platforms.options]\napp_id = "%s"\napp_secret = "replace-during-bind"\nallow_from = "%s"\nthread_isolation = true\ndocument_comments = true\njarvis_approval_url = "%s/internal/card-approval/callback"\njarvis_approval_secret = "%s"\njarvis_approval_timeout_ms = 2500\njarvis_route_claim_url = "%s/internal/message-routing/claim"\njarvis_route_claim_secret = "%s"\njarvis_route_claim_timeout_ms = 2500\njarvis_event_relay_url = "%s/internal/meeting-sweep/wake"\njarvis_event_relay_secret = "%s"\njarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"\n' \
    "$(toml_escape "$REPO_ROOT")" "$(toml_escape "$prompt")" "$(toml_escape "$app_id")" "$(toml_escape "$principal_open_id")" \
    "$(toml_escape "$api_base")" "$(toml_escape "$relay_secret")" "$(toml_escape "$api_base")" "$(toml_escape "$relay_secret")" \
    "$(toml_escape "$api_base")" "$(toml_escape "$relay_secret")" >>"$CC_CONFIG_PATH"
}

read_app_secret() {
  if [[ -t 0 ]]; then
    printf '%s' 'App Secret: ' >&2
    IFS= read -r -s APP_SECRET || fail "read App Secret from terminal failed"
    printf '\n' >&2
  else
    IFS= read -r APP_SECRET || fail "read App Secret from stdin failed"
  fi
  [[ -n "$APP_SECRET" ]] || fail "App Secret must not be empty"
  [[ "$APP_SECRET" != *$'\r'* && "$APP_SECRET" != *$'\n'* ]] || fail "App Secret must be one line"
}

write_cc_app_credentials() {
  local app_id="$1" identity="$2" config_dir temp_path prompt principal_open_id relay_secret api_base
  api_base="$(configured_api_base)"
  prompt="$(cc_bootstrap_prompt)"
  principal_open_id="$(jq -r '.principal_open_id // ""' <<<"$identity")"
  relay_secret="$(jq -r '.relay_secret // ""' <<<"$identity")"
  [[ -n "$principal_open_id" && "$principal_open_id" != "null" ]] || fail "Jarvis principal open_id is empty; configure machine identity first"
  [[ -n "$relay_secret" && "$relay_secret" != "null" ]] || fail "Jarvis relay secret is empty; configure machine identity first"
  config_dir="$(cd "$(dirname "$CC_CONFIG_PATH")" && pwd)"
  temp_path="$(mktemp "${config_dir}/.jarvis-cc-config.XXXXXX")"
  chmod 0600 "$temp_path"
  local inside=false target=false in_agent_options=false in_platform_options=false project_count=0 inject_sender_count=0 app_id_count=0 app_secret_count=0
  local mode_count=0 cmd_count=0 prompt_count=0 allow_from_count=0 event_relay_url_count=0 event_relay_secret_count=0 event_relay_types_count=0 line value escaped_secret
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" =~ ^[[:space:]]*\[\[projects\]\][[:space:]]*$ ]]; then
      if [[ "$in_agent_options" == "true" ]]; then
        [[ "$mode_count" -gt 0 ]] || { printf '%s\n' 'mode = "yolo"' >>"$temp_path"; ((mode_count += 1)); }
        [[ "$cmd_count" -gt 0 ]] || { printf '%s\n' 'cmd = "codex"' >>"$temp_path"; ((cmd_count += 1)); }
        [[ "$prompt_count" -gt 0 ]] || { printf 'append_system_prompt = "%s"\n' "$(toml_escape "$prompt")" >>"$temp_path"; ((prompt_count += 1)); }
      fi
      if [[ "$in_platform_options" == "true" ]]; then
        [[ "$event_relay_url_count" -gt 0 ]] || { printf 'jarvis_event_relay_url = "%s/internal/meeting-sweep/wake"\n' "$(toml_escape "$api_base")" >>"$temp_path"; ((event_relay_url_count += 1)); }
        [[ "$event_relay_secret_count" -gt 0 ]] || { printf 'jarvis_event_relay_secret = "%s"\n' "$(toml_escape "$relay_secret")" >>"$temp_path"; ((event_relay_secret_count += 1)); }
        [[ "$event_relay_types_count" -gt 0 ]] || { printf '%s\n' 'jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"' >>"$temp_path"; ((event_relay_types_count += 1)); }
      fi
      inside=true; target=false
      in_agent_options=false
      in_platform_options=false
    elif [[ "$inside" == "true" && "$line" =~ ^[[:space:]]*name[[:space:]]*=[[:space:]]*\"([^\"]+)\"[[:space:]]*(#.*)?$ ]]; then
      value="${BASH_REMATCH[1]}"
      if [[ "$value" == "jarvis-codex" ]]; then
        target=true
        ((project_count += 1))
        printf '%s\n' "$line" >>"$temp_path"
        printf '%s\n' 'inject_sender = true' >>"$temp_path"
        ((inject_sender_count += 1))
        continue
      else
        target=false
      fi
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*inject_sender[[:space:]]*= ]]; then
      continue
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*\[projects\.agent\.options\][[:space:]]*$ ]]; then
      in_agent_options=true
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*\[projects\.platforms\.options\][[:space:]]*$ ]]; then
      in_platform_options=true
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*\[ ]]; then
      if [[ "$in_agent_options" == "true" ]]; then
        [[ "$mode_count" -gt 0 ]] || { printf '%s\n' 'mode = "yolo"' >>"$temp_path"; ((mode_count += 1)); }
        [[ "$cmd_count" -gt 0 ]] || { printf '%s\n' 'cmd = "codex"' >>"$temp_path"; ((cmd_count += 1)); }
        [[ "$prompt_count" -gt 0 ]] || { printf 'append_system_prompt = "%s"\n' "$(toml_escape "$prompt")" >>"$temp_path"; ((prompt_count += 1)); }
        in_agent_options=false
      fi
      if [[ "$in_platform_options" == "true" ]]; then
        [[ "$event_relay_url_count" -gt 0 ]] || { printf 'jarvis_event_relay_url = "%s/internal/meeting-sweep/wake"\n' "$(toml_escape "$api_base")" >>"$temp_path"; ((event_relay_url_count += 1)); }
        [[ "$event_relay_secret_count" -gt 0 ]] || { printf 'jarvis_event_relay_secret = "%s"\n' "$(toml_escape "$relay_secret")" >>"$temp_path"; ((event_relay_secret_count += 1)); }
        [[ "$event_relay_types_count" -gt 0 ]] || { printf '%s\n' 'jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"' >>"$temp_path"; ((event_relay_types_count += 1)); }
        in_platform_options=false
      fi
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*app_id[[:space:]]*= ]]; then
      printf 'app_id = "%s"\n' "$(toml_escape "$app_id")" >>"$temp_path"
      ((app_id_count += 1)); continue
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*app_secret[[:space:]]*= ]]; then
      escaped_secret="$(toml_escape "$APP_SECRET")"
      printf 'app_secret = "%s"\n' "$escaped_secret" >>"$temp_path"
      ((app_secret_count += 1))
      if [[ "$allow_from_count" -eq 0 ]]; then
        printf 'allow_from = "%s"\n' "$(toml_escape "$principal_open_id")" >>"$temp_path"
        ((allow_from_count += 1))
      fi
      continue
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*allow_from[[:space:]]*= ]]; then
      continue
    elif [[ "$in_platform_options" == "true" && "$line" =~ ^[[:space:]]*jarvis_event_relay_url[[:space:]]*= ]]; then
      printf 'jarvis_event_relay_url = "%s/internal/meeting-sweep/wake"\n' "$(toml_escape "$api_base")" >>"$temp_path"
      ((event_relay_url_count += 1)); continue
    elif [[ "$in_platform_options" == "true" && "$line" =~ ^[[:space:]]*jarvis_event_relay_secret[[:space:]]*= ]]; then
      printf 'jarvis_event_relay_secret = "%s"\n' "$(toml_escape "$relay_secret")" >>"$temp_path"
      ((event_relay_secret_count += 1)); continue
    elif [[ "$in_platform_options" == "true" && "$line" =~ ^[[:space:]]*jarvis_event_relay_types[[:space:]]*= ]]; then
      printf '%s\n' 'jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"' >>"$temp_path"
      ((event_relay_types_count += 1)); continue
    elif [[ "$in_agent_options" == "true" && "$line" =~ ^[[:space:]]*mode[[:space:]]*= ]]; then
      printf '%s\n' 'mode = "yolo"' >>"$temp_path"
      ((mode_count += 1)); continue
    elif [[ "$in_agent_options" == "true" && "$line" =~ ^[[:space:]]*cmd[[:space:]]*= ]]; then
      printf '%s\n' 'cmd = "codex"' >>"$temp_path"
      ((cmd_count += 1)); continue
    elif [[ "$in_agent_options" == "true" && "$line" =~ ^[[:space:]]*append_system_prompt[[:space:]]*= ]]; then
      printf 'append_system_prompt = "%s"\n' "$(toml_escape "$prompt")" >>"$temp_path"
      ((prompt_count += 1)); continue
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*jarvis_approval_url[[:space:]]*= ]]; then
      printf 'jarvis_approval_url = "%s/internal/card-approval/callback"\n' "$(toml_escape "$api_base")" >>"$temp_path"
      continue
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*jarvis_route_claim_url[[:space:]]*= ]]; then
      printf 'jarvis_route_claim_url = "%s/internal/message-routing/claim"\n' "$(toml_escape "$api_base")" >>"$temp_path"
      continue
    elif [[ "$target" == "true" && "$line" =~ ^[[:space:]]*jarvis_event_relay_url[[:space:]]*= ]]; then
      printf 'jarvis_event_relay_url = "%s/internal/meeting-sweep/wake"\n' "$(toml_escape "$api_base")" >>"$temp_path"
      continue
    fi
    printf '%s\n' "$line" >>"$temp_path"
  done <"$CC_CONFIG_PATH"
  if [[ "$in_agent_options" == "true" ]]; then
    [[ "$mode_count" -gt 0 ]] || { printf '%s\n' 'mode = "yolo"' >>"$temp_path"; ((mode_count += 1)); }
    [[ "$cmd_count" -gt 0 ]] || { printf '%s\n' 'cmd = "codex"' >>"$temp_path"; ((cmd_count += 1)); }
    [[ "$prompt_count" -gt 0 ]] || { printf 'append_system_prompt = "%s"\n' "$(toml_escape "$prompt")" >>"$temp_path"; ((prompt_count += 1)); }
  fi
  if [[ "$in_platform_options" == "true" ]]; then
    [[ "$event_relay_url_count" -gt 0 ]] || { printf 'jarvis_event_relay_url = "%s/internal/meeting-sweep/wake"\n' "$(toml_escape "$api_base")" >>"$temp_path"; ((event_relay_url_count += 1)); }
    [[ "$event_relay_secret_count" -gt 0 ]] || { printf 'jarvis_event_relay_secret = "%s"\n' "$(toml_escape "$relay_secret")" >>"$temp_path"; ((event_relay_secret_count += 1)); }
    [[ "$event_relay_types_count" -gt 0 ]] || { printf '%s\n' 'jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"' >>"$temp_path"; ((event_relay_types_count += 1)); }
  fi
  if [[ "$project_count" -ne 1 || "$inject_sender_count" -ne 1 || "$app_id_count" -ne 1 || "$app_secret_count" -ne 1 || "$allow_from_count" -ne 1 || "$mode_count" -ne 1 || "$cmd_count" -ne 1 || "$prompt_count" -ne 1 || "$event_relay_url_count" -ne 1 || "$event_relay_secret_count" -ne 1 || "$event_relay_types_count" -ne 1 ]]; then
    rm -f "$temp_path"
    fail "CC Connect config must contain exactly one complete jarvis-codex project"
  fi
  mv "$temp_path" "$CC_CONFIG_PATH"
  chmod 0600 "$CC_CONFIG_PATH"
}

validation_result() {
  command -v lark-cli >/dev/null 2>&1 || fail "lark-cli is required but not found in PATH"
  local default_config auth_output auth_status card_callback configured block app_id app_brand cc_app_id cc_app_secret credential_probe error_file error_output
  local relay_url cc_relay_secret route_claim_url cc_route_claim_secret agent_type platform_type work_dir agent_mode agent_cmd bootstrap_prompt
  local allow_from principal_open_id
  local inject_sender document_comments thread_isolation relay_hash cc_relay_hash cc_route_claim_hash
  local event_relay_url event_relay_types cc_event_relay_secret cc_event_relay_hash
  local api_base
  api_base="$(configured_api_base)"
  default_config="$(lark_default_config)" || return 1
  error_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-cc-lark-auth.XXXXXX")"
  if ! auth_output="$(LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1 LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1 lark-cli auth status --json --verify 2>"$error_file")"; then
    error_output="$(<"$error_file")"
    rm -f "$error_file"
    fail "lark-cli auth is not ready for the current default identity: ${auth_output}${error_output:+$'\n'${error_output}}"
    return 1
  fi
  rm -f "$error_file"
  auth_status="$(printf '%s\n' "$auth_output" | sed -n '/^{/,$p')"
  printf '%s' "$auth_status" | jq -e 'type == "object"' >/dev/null 2>&1 || { fail "lark-cli auth status did not return JSON"; return 1; }
  card_callback="$(card_callback_preflight)" || return 1
  configured="$(configured_identity)" || return 1
  block="$(cc_jarvis_project_block)" || return 1
  app_id="$(jq -r '.appId' <<<"$default_config")"
  app_brand="$(jq -r '.brand // "feishu"' <<<"$default_config")"
  cc_app_id="$(toml_string_value "$block" app_id)" || return 1
  cc_app_secret="$(toml_string_value "$block" app_secret)" || return 1
  allow_from="$(toml_section_string_value "$block" '[projects.platforms.options]' allow_from)" || return 1
  relay_url="$(toml_string_value "$block" jarvis_approval_url)" || return 1
  cc_relay_secret="$(toml_string_value "$block" jarvis_approval_secret)" || return 1
  route_claim_url="$(toml_string_value "$block" jarvis_route_claim_url)" || return 1
  cc_route_claim_secret="$(toml_string_value "$block" jarvis_route_claim_secret)" || return 1
  event_relay_url="$(toml_string_value "$block" jarvis_event_relay_url)" || return 1
  event_relay_types="$(toml_string_value "$block" jarvis_event_relay_types)" || return 1
  cc_event_relay_secret="$(toml_string_value "$block" jarvis_event_relay_secret)" || return 1
  inject_sender="$(toml_bool_value "$block" inject_sender)" || return 1
  document_comments="$(toml_bool_value "$block" document_comments)" || return 1
  thread_isolation="$(toml_bool_value "$block" thread_isolation)" || return 1
  agent_type="$(toml_section_string_value "$block" '[projects.agent]' type)" || return 1
  work_dir="$(toml_section_string_value "$block" '[projects.agent.options]' work_dir)" || return 1
  agent_mode="$(toml_section_string_value "$block" '[projects.agent.options]' mode)" || return 1
  agent_cmd="$(toml_section_string_value "$block" '[projects.agent.options]' cmd)" || return 1
  bootstrap_prompt="$(toml_section_string_value "$block" '[projects.agent.options]' append_system_prompt)" || return 1
  platform_type="$(toml_section_string_value "$block" '[[projects.platforms]]' type)" || return 1
  credential_probe="$(cc_app_credentials_probe "$cc_app_id" "$cc_app_secret" "$app_brand")" || return 1
  principal_open_id="$(jq -r '.principal_open_id // ""' <<<"$configured")"
  relay_hash="$(jq -r '.relay_secret_sha256 // ""' <<<"$configured")"
  cc_relay_hash="$(printf '%s' "$cc_relay_secret" | sha256_text)"
  cc_route_claim_hash="$(printf '%s' "$cc_route_claim_secret" | sha256_text)"
  cc_event_relay_hash="$(printf '%s' "$cc_event_relay_secret" | sha256_text)"
  jq -nc \
    --arg app_id "$app_id" --arg cc_app_id "$cc_app_id" \
    --arg expected_api_base "$api_base" \
    --arg relay_url "$relay_url" --arg route_claim_url "$route_claim_url" --arg agent_type "$agent_type" --arg platform_type "$platform_type" \
    --arg event_relay_url "$event_relay_url" --arg event_relay_types "$event_relay_types" \
    --arg work_dir "$work_dir" --arg agent_mode "$agent_mode" --arg agent_cmd "$agent_cmd" \
    --arg repo_root "$REPO_ROOT" --arg bootstrap_prompt "$bootstrap_prompt" \
    --arg allow_from "$allow_from" --arg principal_open_id "$principal_open_id" \
    --argjson auth "$auth_status" --argjson card_callback "$card_callback" --argjson configured "$configured" --argjson credential_probe "$credential_probe" \
    --argjson cc_app_secret_configured "$([[ -n "$cc_app_secret" && "$cc_app_secret" != "replace-during-bind" ]] && printf true || printf false)" \
    --argjson relay_secret_matches "$([[ -n "$relay_hash" && "$relay_hash" == "$cc_relay_hash" ]] && printf true || printf false)" \
    --argjson route_claim_secret_matches "$([[ -n "$relay_hash" && "$relay_hash" == "$cc_route_claim_hash" ]] && printf true || printf false)" \
    --argjson event_relay_secret_matches "$([[ -n "$relay_hash" && "$relay_hash" == "$cc_event_relay_hash" ]] && printf true || printf false)" \
    --argjson inject_sender "$inject_sender" --argjson document_comments "$document_comments" --argjson thread_isolation "$thread_isolation" '
      ($auth.identities.user // {}) as $user |
      ($auth.identities.bot // {}) as $bot |
      (($auth.verified == true) and ($user.status == "ready") and ($user.verified == true) and ($user.tokenStatus == "valid")) as $auth_ok |
      (($auth.verified == true) and ($bot.status == "ready") and ($bot.verified == true)) as $bot_ok |
      (($configured.card_approval_enabled == true) and
       ($configured.card_approval_principal_open_id == $configured.principal_open_id) and
       ($user.openId == $configured.principal_open_id)) as $identity_ok |
      (($agent_type == "codex") and ($platform_type == "feishu") and ($work_dir == $repo_root)) as $route_ok |
      (($bootstrap_prompt | contains("[cc-connect sender_id=")) and
       ($bootstrap_prompt | contains($repo_root + "/scripts/jarvis-tools get-context --chat-id")) and
       ($bootstrap_prompt | contains($repo_root + "/scripts/jarvis-tools get-shared-memory")) and
       ($bootstrap_prompt | contains("agent_identity.display_name")) and
       ($bootstrap_prompt | contains("prior_messages")) and
       ($bootstrap_prompt | contains("create-task")) and
       ($bootstrap_prompt | contains("source_type=manual")) and
       ($bootstrap_prompt | contains("delivery_required"))) as $context_contract_ok |
      (($agent_mode == "yolo") and ($agent_cmd == "codex")) as $context_runtime_ok |
      (($card_callback.ok == true) and ($card_callback.data.decision.status == "ready")) as $card_callback_ok |
      (($principal_open_id != "") and ($allow_from == $principal_open_id)) as $feishu_access_ok |
      (($event_relay_url == ($expected_api_base + "/internal/meeting-sweep/wake")) and
       ($event_relay_types | contains("vc.meeting.participant_meeting_ended_v1")) and
       $event_relay_secret_matches) as $meeting_event_relay_ok |
      {
        ready: ($auth_ok and $bot_ok and $identity_ok and ($app_id == $cc_app_id) and
          $cc_app_secret_configured and ($credential_probe.ok == true) and $relay_secret_matches and $route_claim_secret_matches and
          ($relay_url == ($expected_api_base + "/internal/card-approval/callback")) and
          ($route_claim_url == ($expected_api_base + "/internal/message-routing/claim")) and
          $route_ok and $inject_sender and $context_contract_ok and $context_runtime_ok and $document_comments and $thread_isolation and
          $feishu_access_ok and $card_callback_ok and $meeting_event_relay_ok),
        checks: {
          lark_user_authenticated: $auth_ok,
          lark_default_bot_authenticated: $bot_ok,
          jarvis_identity_uses_lark_default: $identity_ok,
          cc_connect_app_id_matches_lark_default: ($app_id == $cc_app_id),
          cc_connect_app_secret_configured: $cc_app_secret_configured,
          cc_connect_app_credentials_valid: ($credential_probe.ok == true),
          cc_connect_app_credentials_error: $credential_probe.error,
          approval_relay_secret_matches: $relay_secret_matches,
          approval_relay_url_is_local: ($relay_url == ($expected_api_base + "/internal/card-approval/callback")),
          route_claim_secret_matches: $route_claim_secret_matches,
          route_claim_url_is_local: ($route_claim_url == ($expected_api_base + "/internal/message-routing/claim")),
          meeting_event_relay_configured: $meeting_event_relay_ok,
          card_callback_app_permission_and_event_ready: $card_callback_ok,
          feishu_allow_from_is_principal_only: $feishu_access_ok,
          jarvis_project_routes_to_current_checkout: $route_ok,
          cc_connect_injects_trusted_chat_id: $inject_sender,
          agent_loads_jarvis_context_each_turn: $context_contract_ok,
          agent_can_reach_jarvis_context: $context_runtime_ok,
          document_comments_enabled: $document_comments,
          thread_isolation_enabled: $thread_isolation
        },
        identity: {app_id:$app_id,principal_open_id:($configured.principal_open_id // null),cc_connect_project:"jarvis-codex"},
        card_callback_preflight: $card_callback
      }'
}

cmd_bind() {
  local default_config configured app_id app_brand principal_open_id count block current_secret current_allow_from credential_probe result
  default_config="$(lark_default_config)" || exit 1
  configured="$(configured_identity)" || exit 1
  app_id="$(jq -r '.appId' <<<"$default_config")"
  app_brand="$(jq -r '.brand // "feishu"' <<<"$default_config")"
  principal_open_id="$(jq -r '.principal_open_id // ""' <<<"$configured")"
  count="$(jarvis_project_count)"
  [[ "$count" -le 1 ]] || fail "CC Connect config contains multiple jarvis-codex projects"
  if [[ "$count" -eq 0 ]]; then
    current_secret=""
    current_allow_from=""
  else
    block="$(cc_jarvis_project_block)" || exit 1
    current_secret="$(toml_string_value "$block" app_secret)" || exit 1
    current_allow_from="$(toml_optional_section_string_value "$block" '[projects.platforms.options]' allow_from)"
  fi
  if [[ -n "$current_allow_from" && "$current_allow_from" != "$principal_open_id" && "$REPLACE_ALLOW_FROM" != "true" ]]; then
    fail "existing allow_from is ${current_allow_from}; rerun bind-cc with --replace-allow-from only after the user approves replacing it with ${principal_open_id}"
  fi
  if [[ "$REUSE_EXISTING_SECRET" == "true" ]]; then
    [[ -n "$current_secret" && "$current_secret" != "replace-during-bind" ]] || \
      fail "--reuse-existing-secret requires an existing non-placeholder secret"
    APP_SECRET="$current_secret"
  else
    read_app_secret
  fi
  credential_probe="$(cc_app_credentials_probe "$app_id" "$APP_SECRET" "$app_brand")" || exit 1
  jq -e '.ok == true' >/dev/null <<<"$credential_probe" || \
    fail "the supplied App Secret is not valid for lark-cli App ${app_id}: $(jq -r '.error // "credential validation failed"' <<<"$credential_probe")"
  if [[ "$count" -eq 0 ]]; then
    append_fresh_project "$configured" "$app_id"
  fi
  write_cc_app_credentials "$app_id" "$configured"
  APP_SECRET=""
  result="$(validation_result)" || exit 1
  emit "$result"
  jq -e '.ready == true' >/dev/null <<<"$result"
}

(( $# > 0 )) || { usage >&2; exit 2; }
command_name="$1"
shift
consume_flags "$@"

case "$command_name" in
  bind) cmd_bind ;;
  validate)
    [[ "$REUSE_EXISTING_SECRET" == "false" ]] || fail "validate does not accept --reuse-existing-secret"
    [[ "$REPLACE_ALLOW_FROM" == "false" ]] || fail "validate does not accept --replace-allow-from"
    result="$(validation_result)" || exit 1
    emit "$result"
    jq -e '.ready == true' >/dev/null <<<"$result"
    ;;
  help|--help|-h) usage ;;
  *) fail "unknown command: ${command_name}" ;;
esac
