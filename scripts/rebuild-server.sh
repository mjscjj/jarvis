#!/bin/zsh
# Standard backend rebuild: go build → stable codesign → restart launchd service.
# Prefer this over bare `go build` so TCC Full Disk Access stays attached.
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
label=com.bytedance.jarvis.server
service_target="gui/$UID/$label"
bin=$repo_dir/bin/jarvis-server
next_bin=$repo_dir/bin/jarvis-server.next
tasks_api="http://127.0.0.1:18800/api/tasks?status=executing&page=1&page_size=1"
force_interrupt_running_tasks=false

usage() {
  cat >&2 <<'EOF'
Usage: ./scripts/rebuild-server.sh [--force-interrupt-running-tasks]

The normal rebuild refuses to restart Jarvis while Tasks are executing because
launchctl kickstart terminates their Codex child processes. Use the force flag
only when intentionally interrupting those Tasks.
EOF
}

case $# in
  0) ;;
  1)
    case $1 in
      --force-interrupt-running-tasks)
        force_interrupt_running_tasks=true
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        usage
        exit 2
        ;;
    esac
    ;;
  *)
    usage
    exit 2
    ;;
esac

running_task_count() {
  local response
  if ! response=$(curl --fail --silent --show-error --max-time 5 "$tasks_api"); then
    echo "cannot query executing Tasks at $tasks_api; refusing to restart Jarvis" >&2
    return 1
  fi
  if ! jq -er 'if .code == 0 and (.data.total | type == "number") then .data.total else error("unexpected task-list response") end' <<<"$response"; then
    echo "cannot parse executing Task count; refusing to restart Jarvis" >&2
    return 1
  fi
}

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
trap 'rm -f "$next_bin"' EXIT

echo "building $next_bin"
"$script_dir/check-build-toolchain.sh"
go build -o "$next_bin" ./cmd/jarvis-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"

if launchctl print "$service_target" >/dev/null 2>&1; then
  running_tasks=$(running_task_count)
  if (( running_tasks > 0 )); then
    if [[ $force_interrupt_running_tasks != true ]]; then
      echo "refusing to restart $service_target: $running_tasks Task(s) are executing" >&2
      echo "wait for them to finish, or rerun with --force-interrupt-running-tasks to intentionally stop them" >&2
      exit 1
    fi
    echo "forcing restart: intentionally interrupting $running_tasks executing Task(s)" >&2
  fi
  mv "$next_bin" "$bin"
  echo "restarting $service_target"
  launchctl kickstart -k "$service_target"
else
  mv "$next_bin" "$bin"
  echo "launchd service not loaded; run ./scripts/install-launchd.sh if needed"
fi

for attempt in {1..10}; do
  if curl --fail --silent --show-error --max-time 2 -o /dev/null http://127.0.0.1:18800/healthz; then
    echo "backend health HTTP 200"
    exit 0
  fi
  sleep 1
done
echo "backend did not become reachable within 10 seconds; check var/log/jarvis-server.error.log" >&2
exit 1
