#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
version=1.18.2
archive_sha256=859f487e316ae1bda3b5d7c1e129a0a7344424d992503c188979ca6ac1b47253
download_url="https://github.com/qdrant/qdrant/releases/download/v$version/qdrant-aarch64-apple-darwin.tar.gz"
label=com.bytedance.jarvis.qdrant
service_target="gui/$UID/$label"
temporary_dir=$(mktemp -d /private/tmp/jarvis-qdrant.XXXXXX)

cleanup() {
  rm -rf "$temporary_dir"
}
trap cleanup EXIT

curl -fL "$download_url" -o "$temporary_dir/qdrant.tar.gz"
actual_sha256=$(shasum -a 256 "$temporary_dir/qdrant.tar.gz" | awk '{print $1}')
if [[ "$actual_sha256" != "$archive_sha256" ]]; then
  print -u2 "qdrant archive sha256 mismatch: got=$actual_sha256 want=$archive_sha256"
  exit 1
fi
tar -xzf "$temporary_dir/qdrant.tar.gz" -C "$temporary_dir"

mkdir -p "$repo_dir/bin" "$repo_dir/var/log" "$repo_dir/var/qdrant/storage" "$repo_dir/var/qdrant/snapshots"
install -m 0755 "$temporary_dir/qdrant" "$repo_dir/bin/qdrant"

# launchd 只在登录时扫描 ~/Library/LaunchAgents，放一份到那里才能开机/重新登录后自动拉起。
agent_plist=$("$script_dir/render-launchd-plist.sh" "$label")

if launchctl print "$service_target" >/dev/null 2>&1; then
  launchctl bootout "$service_target"
fi
launchctl bootstrap "gui/$UID" "$agent_plist"

for attempt in {1..30}; do
  if curl -fsS http://127.0.0.1:6333/healthz >/dev/null; then
    launchctl print "$service_target"
    exit 0
  fi
  sleep 1
done

print -u2 "qdrant did not become healthy within 30 seconds"
exit 1
