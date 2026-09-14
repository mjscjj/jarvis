# Shared connection selection for Bash 3.2 and zsh callers. No IO on source.
# Sets JARVIS_CONNECTION_API_BASE and JARVIS_CONNECTION_TIMEZONE in the caller.
jarvis_connection() {
  local root="$1" config_path="${2:-}" api_base="${3:-}" address="${4:-}" need_timezone="${5:-false}"
  local timezone="${JARVIS_TIMEZONE:-}" connection
  local -a config_command
  config_path="${config_path:-${JARVIS_CONFIG_PATH:-${JARVIS_CONFIG:-${root}/conf/config.yaml}}}"
  [[ "$config_path" == /* ]] || config_path="${root}/${config_path}"
  api_base="${api_base:-${JARVIS_API_BASE:-}}"
  if [[ -n "$address" || -z "$api_base" || ( "$need_timezone" == true && -z "$timezone" ) ]]; then
    if [[ "${JARVIS_DESKTOP:-}" == 1 ]]; then
      if [[ -z "${JARVIS_RESOURCE_ROOT:-}" || ! -x "${JARVIS_RESOURCE_ROOT}/bin/jarvis-config" ]]; then
        printf 'Jarvis connection: bundled jarvis-config is missing; check JARVIS_RESOURCE_ROOT\n' >&2
        return 1
      fi
      config_command=("${JARVIS_RESOURCE_ROOT}/bin/jarvis-config")
    else
      config_command=(go run ./cmd/jarvis-config)
    fi
    connection="$(cd "$root" && "${config_command[@]}" show-connection --config "$config_path" --addr "$address")" || {
      printf 'Jarvis connection: show-connection failed for %s\n' "$config_path" >&2; return 1;
    }
    printf '%s' "$connection" | jq -e 'type == "object" and (.api_base | type == "string" and length > 0) and (.timezone | type == "string" and length > 0)' >/dev/null || {
      printf 'Jarvis connection: invalid show-connection response\n' >&2; return 1;
    }
    if [[ -n "$address" || -z "$api_base" ]]; then
      api_base="$(printf '%s' "$connection" | jq -r '.api_base')" || return 1
    fi
    if [[ -z "$timezone" ]]; then
      timezone="$(printf '%s' "$connection" | jq -r '.timezone')" || return 1
    fi
  fi
  case "$api_base" in
    http://?*|https://?*) ;;
    *) printf 'Jarvis connection: API address must be an http:// or https:// URL\n' >&2; return 1 ;;
  esac
  case "$api_base" in
    *[[:space:]]*|*\?*|*\#*) printf 'Jarvis connection: invalid API base URL\n' >&2; return 1 ;;
  esac
  JARVIS_CONNECTION_API_BASE="${api_base%/}"
  JARVIS_CONNECTION_TIMEZONE="$timezone"
}
