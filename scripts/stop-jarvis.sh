#!/usr/bin/env bash
# Stop only the Jarvis instance selected by its configuration.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_PATH="${JARVIS_CONFIG:-${REPO_ROOT}/conf/config.yaml}"
DELAY_SECONDS=0
SERVER_PID=""

usage() {
  cat >&2 <<'EOF'
Usage: ./scripts/stop-jarvis.sh [--config PATH] [--delay-seconds N] [--server-pid PID]

Stops only the main and optional development Web services for
the selected configuration. Shared Qdrant and CC Connect services are not stopped.
EOF
}

while (( $# > 0 )); do
  case "$1" in
    --config) (( $# >= 2 )) || { usage; exit 2; }; CONFIG_PATH="$2"; shift 2 ;;
    --delay-seconds) (( $# >= 2 )) || { usage; exit 2; }; DELAY_SECONDS="$2"; shift 2 ;;
    --server-pid) (( $# >= 2 )) || { usage; exit 2; }; SERVER_PID="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

[[ "$DELAY_SECONDS" =~ ^[0-9]+$ ]] || { printf 'delay seconds must be a non-negative integer\n' >&2; exit 2; }
if [[ -n "$SERVER_PID" ]]; then
  [[ "$SERVER_PID" =~ ^[0-9]+$ && "$SERVER_PID" -gt 1 ]] || { printf 'server pid must be greater than 1\n' >&2; exit 2; }
fi

INSTANCE="$("${SCRIPT_DIR}/jarvis-instance" "$CONFIG_PATH")"
MAIN_LABEL="$(jq -er '.launchd_label' <<<"$INSTANCE")"
sleep "$DELAY_SECONDS"

case "$(uname -s)" in
  Darwin)
    UID_VALUE="$(id -u)"
    for label in "$MAIN_LABEL" "${MAIN_LABEL}.web"; do
      launchctl bootout "gui/${UID_VALUE}/${label}" >/dev/null 2>&1 || true
    done
    ;;
  Linux)
    for unit in "${MAIN_LABEL}.service" "${MAIN_LABEL}.web.service"; do
      systemctl --user stop "$unit" >/dev/null 2>&1 || true
    done
    ;;
  *)
    printf 'unsupported operating system: %s\n' "$(uname -s)" >&2
    exit 1
    ;;
esac

# The API call originates inside the main service. If the service manager did
# not own that process, terminate precisely the PID that accepted the request.
if [[ -n "$SERVER_PID" && "$SERVER_PID" != "$$" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
  kill -TERM "$SERVER_PID" 2>/dev/null || true
fi

printf 'stopped Jarvis instance %s\n' "$MAIN_LABEL"
