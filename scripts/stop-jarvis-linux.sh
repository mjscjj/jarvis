#!/usr/bin/env bash
set -u

delay_seconds=0
while (( $# > 0 )); do
  case "$1" in
    --delay-seconds) (( $# >= 2 )) || exit 2; delay_seconds="$2"; shift 2 ;;
    --server-pid) (( $# >= 2 )) || exit 2; shift 2 ;;
    --help|-h) printf 'Usage: ./scripts/stop-jarvis.sh [--delay-seconds N] [--server-pid PID]\n'; exit 0 ;;
    *) exit 2 ;;
  esac
done
[[ "$delay_seconds" =~ ^[0-9]+$ ]] || { printf 'delay seconds must be a non-negative integer\n' >&2; exit 2; }
sleep "$delay_seconds"
for unit in \
  com.bytedance.jarvis.cc-connect.service \
  com.bytedance.jarvis.server.service \
  com.bytedance.jarvis.qdrant.service; do
  systemctl --user stop "$unit" >/dev/null 2>&1 || true
done
printf 'Jarvis systemd services stopped\n'
