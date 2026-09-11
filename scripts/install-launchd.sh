#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
config_path="$repo_dir/conf/config.yaml"
if (( $# > 0 )); then
  if (( $# != 2 )) || [[ $1 != --config ]]; then
    print -u2 'Usage: install-launchd.sh [--config PATH]'; exit 2
  fi
  config_path=$2
fi
instance=$("$script_dir/jarvis-instance" "$config_path")
label=$(jq -er .launchd_label <<<"$instance")
config_path=$(jq -er .config_path <<<"$instance")
service_target="gui/$UID/$label"
next_bin=$repo_dir/bin/jarvis-server.next

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
trap 'rm -f "$next_bin"' EXIT
"$script_dir/check-build-toolchain.sh"
npm --prefix "$repo_dir/web" ci
npm --prefix "$repo_dir/web" run build
go build -o "$next_bin" ./cmd/jarvis-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"
mv "$next_bin" "$repo_dir/bin/jarvis-server"

# launchd 只在登录时扫描 ~/Library/LaunchAgents，放一份到那里才能开机/重新登录后自动拉起。
agent_plist=$("$script_dir/render-launchd-plist.sh" com.bytedance.jarvis.server "$config_path")

if launchctl print "$service_target" >/dev/null 2>&1; then
  launchctl bootout "$service_target"
fi

launchctl bootstrap "gui/$UID" "$agent_plist"
launchctl print "$service_target"
