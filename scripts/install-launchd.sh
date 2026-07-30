#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
label=com.bytedance.jarvis.server
plist_path="$repo_dir/deploy/$label.plist"
agents_dir="$HOME/Library/LaunchAgents"
agent_link="$agents_dir/$label.plist"
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

# launchd 只在登录时扫描 ~/Library/LaunchAgents，软链过去才能开机/重新登录后自动拉起。
mkdir -p "$agents_dir"
ln -sfn "$plist_path" "$agent_link"

if launchctl print "$service_target" >/dev/null 2>&1; then
  launchctl bootout "$service_target"
fi

launchctl bootstrap "gui/$UID" "$agent_link"
launchctl print "$service_target"
