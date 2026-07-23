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

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
trap 'rm -f "$next_bin"' EXIT

echo "building $next_bin"
go build -o "$next_bin" ./cmd/jarvis-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"
mv "$next_bin" "$bin"

if launchctl print "$service_target" >/dev/null 2>&1; then
  echo "restarting $service_target"
  launchctl kickstart -k "$service_target"
else
  echo "launchd service not loaded; bootstrap from deploy/$label.plist if needed"
fi

sleep 1
curl -sS -o /dev/null -w "backend health HTTP %{http_code}\n" http://127.0.0.1:18800/api/debug/status \
  || echo "backend not reachable yet — check var/log/jarvis-server.error.log"
