#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
label=com.bytedance.jarvis.server
plist_path="$repo_dir/deploy/$label.plist"
service_target="gui/$UID/$label"
next_bin=$repo_dir/bin/jarvis-server.next

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
trap 'rm -f "$next_bin"' EXIT
npm --prefix "$repo_dir/web" ci --registry=https://registry.npmjs.org
npm --prefix "$repo_dir/web" run build
go build -o "$next_bin" ./cmd/jarvis-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"
mv "$next_bin" "$repo_dir/bin/jarvis-server"
plutil -lint "$plist_path"

if launchctl print "$service_target" >/dev/null 2>&1; then
  launchctl bootout "$service_target"
fi

launchctl bootstrap "gui/$UID" "$plist_path"
launchctl print "$service_target"
