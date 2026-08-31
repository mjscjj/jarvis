#!/bin/zsh
# Rebuild and restart only the independent Chat sidecar.
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
config_path="$repo_dir/conf/config.yaml"
if (( $# > 0 )); then
  if (( $# != 2 )) || [[ $1 != --config ]]; then
    print -u2 'Usage: rebuild-chat-server.sh [--config PATH]'; exit 2
  fi
  config_path=$2
fi
instance=$("$script_dir/jarvis-instance" "$config_path")
label=$(jq -er .chat_launchd_label <<<"$instance")
api_base=$(jq -er .chat_api_base <<<"$instance")
service_target="gui/$UID/$label"
next_bin=$repo_dir/bin/jarvis-chat-server.next

mkdir -p "$repo_dir/bin" "$repo_dir/var/log"
cd "$repo_dir"
trap 'rm -f "$next_bin"' EXIT
"$script_dir/check-build-toolchain.sh"
go build -o "$next_bin" ./cmd/jarvis-chat-server
"$script_dir/sign-jarvis-server.sh" "$next_bin"
"$script_dir/verify-server-signature.sh" "$next_bin"

if ! launchctl print "$service_target" >/dev/null 2>&1; then
  echo "chat service $label is not loaded; run ./scripts/install-chat-launchd.sh --config $config_path" >&2
  exit 1
fi
mv "$next_bin" "$repo_dir/bin/jarvis-chat-server"
launchctl kickstart -k "$service_target"
for attempt in {1..10}; do
  if curl --fail --silent --show-error --max-time 2 -o /dev/null "$api_base/healthz"; then
    echo "chat health HTTP 200"
    exit 0
  fi
  sleep 1
done
echo "chat did not become reachable within 10 seconds; check var/log/jarvis-chat.error.log" >&2
exit 1
