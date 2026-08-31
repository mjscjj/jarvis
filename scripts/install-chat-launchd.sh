#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
config_path="$repo_dir/conf/config.yaml"
if (( $# > 0 )); then
  if (( $# != 2 )) || [[ $1 != --config ]]; then
    print -u2 'Usage: install-chat-launchd.sh [--config PATH]'; exit 2
  fi
  config_path=$2
fi
instance=$("$script_dir/jarvis-instance" "$config_path")
label=$(jq -er .chat_launchd_label <<<"$instance")
config_path=$(jq -er .config_path <<<"$instance")
service_target="gui/$UID/$label"
next_bin=$repo_dir/bin/jarvis-chat-server.next

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
trap 'rm -f "$next_bin"' EXIT
"$script_dir/check-build-toolchain.sh"
go build -o "$next_bin" ./cmd/jarvis-chat-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"
mv "$next_bin" "$repo_dir/bin/jarvis-chat-server"

agent_plist=$("$script_dir/render-launchd-plist.sh" com.bytedance.jarvis.chat "$config_path")
if launchctl print "$service_target" >/dev/null 2>&1; then
  launchctl bootout "$service_target"
fi
launchctl bootstrap "gui/$UID" "$agent_plist"
launchctl print "$service_target"
