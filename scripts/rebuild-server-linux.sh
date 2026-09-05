#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LABEL="com.bytedance.jarvis.server"
UNIT="${LABEL}.service"
BIN="${REPO_ROOT}/bin/jarvis-server"
NEXT_BIN="${REPO_ROOT}/bin/jarvis-server.next"
TASKS_API="http://127.0.0.1:18800/api/tasks?status=executing&page=1&page_size=1"
FORCE_INTERRUPT=false

usage() {
  printf 'Usage: ./scripts/rebuild-server.sh [--force-interrupt-running-tasks]\n' >&2
}

case $# in
  0) ;;
  1)
    case "$1" in
      --force-interrupt-running-tasks) FORCE_INTERRUPT=true ;;
      --help|-h) usage; exit 0 ;;
      *) usage; exit 2 ;;
    esac
    ;;
  *) usage; exit 2 ;;
esac

wait_for_health() {
  for _ in {1..30}; do
    if curl --fail --silent --show-error --max-time 2 -o /dev/null http://127.0.0.1:18800/healthz; then
      printf 'backend health HTTP 200\n'
      return 0
    fi
    sleep 1
  done
  printf 'backend did not become reachable within 30 seconds; inspect journalctl --user -u %s\n' "$UNIT" >&2
  return 1
}

mkdir -p "${REPO_ROOT}/bin" "${REPO_ROOT}/var/log"
cd "$REPO_ROOT"
trap 'rm -f "$NEXT_BIN"' EXIT

if ! systemctl --user cat "$UNIT" >/dev/null 2>&1; then
  printf 'systemd service is not installed; rebuilding and registering the current checkout\n'
  "$SCRIPT_DIR/install-systemd.sh"
  wait_for_health
  exit 0
fi

response="$(curl --fail --silent --show-error --max-time 5 "$TASKS_API")" || {
  printf 'cannot query executing Tasks at %s; refusing to restart Jarvis\n' "$TASKS_API" >&2
  exit 1
}
running_tasks="$(jq -er 'if .code == 0 and (.data.total | type == "number") then .data.total else error("unexpected task-list response") end' <<<"$response")" || {
  printf 'cannot parse executing Task count; refusing to restart Jarvis\n' >&2
  exit 1
}
if (( running_tasks > 0 )) && [[ "$FORCE_INTERRUPT" != "true" ]]; then
  printf 'refusing to restart %s: %s Task(s) are executing\n' "$UNIT" "$running_tasks" >&2
  printf 'wait for them to finish, or rerun with --force-interrupt-running-tasks\n' >&2
  exit 1
fi
if (( running_tasks > 0 )); then
  printf 'forcing restart: intentionally interrupting %s executing Task(s)\n' "$running_tasks" >&2
fi

"$SCRIPT_DIR/check-build-toolchain.sh"
go build -o "$NEXT_BIN" ./cmd/jarvis-server
mv "$NEXT_BIN" "$BIN"
systemctl --user restart "$UNIT"
wait_for_health
