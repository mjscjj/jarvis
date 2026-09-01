#!/bin/zsh
# Stop every local process that belongs to this Jarvis installation.
set -u

script_dir=${0:A:h}
if [[ $(uname -s) == Linux ]]; then
  exec "$script_dir/stop-jarvis-linux.sh" "$@"
fi
repo_dir=${script_dir:h}
delay_seconds=0
server_pid=""
log_path="$repo_dir/var/log/jarvis-shutdown.log"

usage() {
  cat >&2 <<'EOF'
Usage: ./scripts/stop-jarvis.sh [--delay-seconds N] [--server-pid PID]

Stops Jarvis Server, Qdrant, CC Connect, and the optional Vite web service.
EOF
}

while (( $# > 0 )); do
  case "$1" in
    --delay-seconds)
      (( $# >= 2 )) || { usage; exit 2; }
      delay_seconds="$2"
      shift 2
      ;;
    --server-pid)
      (( $# >= 2 )) || { usage; exit 2; }
      server_pid="$2"
      shift 2
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
done

[[ "$delay_seconds" == <-> ]] || { print -u2 "delay seconds must be a non-negative integer"; exit 2; }
if [[ -n "$server_pid" ]]; then
  [[ "$server_pid" == <-> ]] && (( server_pid > 1 )) || { print -u2 "server pid must be greater than 1"; exit 2; }
fi

mkdir -p "${log_path:h}"
exec >>"$log_path" 2>&1
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) stopping Jarvis services"
sleep "$delay_seconds"

uid=$(id -u)
for label in com.cc-connect.service com.bytedance.jarvis.web com.bytedance.jarvis.qdrant com.bytedance.jarvis.server; do
  if /bin/launchctl print "gui/$uid/$label" >/dev/null 2>&1; then
    /bin/launchctl bootout "gui/$uid/$label" || true
  fi
done

for session in cc-connect jarvis-web jarvis-qdrant jarvis-server; do
  /usr/bin/screen -S "$session" -X quit >/dev/null 2>&1 || true
done

typeset -a pids
if [[ -n "$server_pid" ]]; then
  pids+=("$server_pid")
fi
for port in 18800 18801 6333 6334 9810 9820; do
  while IFS= read -r pid; do
    [[ -n "$pid" ]] && pids+=("$pid")
  done < <(/usr/sbin/lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
done

typeset -A seen
for pid in "${pids[@]}"; do
  [[ "$pid" == <-> && "$pid" -gt 1 && -z "${seen[$pid]-}" && "$pid" != "$$" ]] || continue
  seen[$pid]=1
  kill -TERM "$pid" 2>/dev/null || true
done

for _ in {1..20}; do
  alive=false
  for pid in "${(k)seen}"; do
    if kill -0 "$pid" 2>/dev/null; then
      alive=true
      break
    fi
  done
  [[ "$alive" == false ]] && break
  sleep 0.25
done

for pid in "${(k)seen}"; do
  kill -KILL "$pid" 2>/dev/null || true
done

echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) Jarvis services stopped"
