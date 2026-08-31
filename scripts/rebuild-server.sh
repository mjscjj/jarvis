#!/bin/zsh
# Standard backend rebuild: go build → stable codesign → restart launchd service.
# Prefer this over bare `go build` so TCC Full Disk Access stays attached.
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
config_path="$repo_dir/conf/config.yaml"
bin=$repo_dir/bin/jarvis-server
next_bin=$repo_dir/bin/jarvis-server.next
force_interrupt_running_tasks=false
build_only=false

usage() {
  cat >&2 <<'EOF'
Usage: ./scripts/rebuild-server.sh [--config PATH] [--force-interrupt-running-tasks | --build-only]

The normal rebuild refuses to restart Jarvis while Tasks are executing because
launchctl kickstart terminates their Codex child processes. Use the force flag
only when intentionally interrupting those Tasks.
Use --build-only to build and verify the signed binary without restarting any service.
EOF
}

while (( $# > 0 )); do
    case $1 in
      --config)
        (( $# >= 2 )) || { usage; exit 2; }
        config_path=$2; shift
        ;;
      --force-interrupt-running-tasks)
        force_interrupt_running_tasks=true
        ;;
      --build-only)
        build_only=true
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
    shift
done

running_task_count() {
  local response running_pid running_address running_api
  # The config may already contain a new port. Inspect this exact launchd
  # process for the old listening address before checking its active Tasks.
  running_pid=$(launchctl print "$service_target" | awk '/^[[:space:]]*pid = / {print $3; exit}')
  [[ -n $running_pid ]] || { echo "no running PID for $service_target" >&2; return 1; }
  running_address=$(lsof -nP -a -p "$running_pid" -iTCP -sTCP:LISTEN -Fn | awk '/^n/ {print substr($0,2)}' | sort -u)
  [[ -n $running_address && $running_address != *$'\n'* ]] || { echo "expected one HTTP listener for $service_target" >&2; return 1; }
  case "$running_address" in
    \*:*) running_address="127.0.0.1:${running_address##*:}" ;;
  esac
  running_api="http://$running_address/api/tasks?status=executing&page=1&page_size=1"
  if ! response=$(curl --fail --silent --show-error --max-time 5 "$running_api"); then
    echo "cannot query executing Tasks at $running_api; refusing to restart Jarvis" >&2
    return 1
  fi
  if ! jq -er 'if .code == 0 and (.data.total | type == "number") then .data.total else error("unexpected task-list response") end' <<<"$response"; then
    echo "cannot parse executing Task count; refusing to restart Jarvis" >&2
    return 1
  fi
}

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
instance=$("$script_dir/jarvis-instance" "$config_path")
label=$(jq -er .launchd_label <<<"$instance")
api_base=$(jq -er .api_base <<<"$instance")
service_target="gui/$UID/$label"
trap 'rm -f "$next_bin"' EXIT

echo "building $next_bin"
"$script_dir/check-build-toolchain.sh"
go build -o "$next_bin" ./cmd/jarvis-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"

if [[ $build_only == true ]]; then
  mv "$next_bin" "$bin"
  echo "signed backend built at $bin; no service restarted"
  exit 0
fi

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
  echo "instance service $label is not loaded; run ./scripts/install-launchd.sh --config $config_path" >&2
  exit 1
fi

for attempt in {1..10}; do
  if curl --fail --silent --show-error --max-time 2 -o /dev/null "$api_base/healthz"; then
    echo "backend health HTTP 200"
    exit 0
  fi
  sleep 1
done
echo "backend did not become reachable within 10 seconds; check var/log/jarvis-server.error.log" >&2
exit 1
