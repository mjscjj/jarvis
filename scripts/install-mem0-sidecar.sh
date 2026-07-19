#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
sidecar_dir="$repo_dir/sidecar/mem0"
config_path="$repo_dir/conf/config.yaml"
label=com.bytedance.jarvis.mem0
plist_path="$repo_dir/deploy/$label.plist"
service_target="gui/$UID/$label"

command -v uv >/dev/null
curl -fsS http://127.0.0.1:6333/healthz >/dev/null
mkdir -p "$repo_dir/var/log" "$repo_dir/var/mem0"

cd "$sidecar_dir"
uv sync --frozen
JARVIS_CONFIG="$config_path" uv run --frozen python -c 'from jarvis_mem0.settings import Settings; import os; Settings.from_file(os.environ["JARVIS_CONFIG"])'
plutil -lint "$plist_path"

if launchctl print "$service_target" >/dev/null 2>&1; then
  launchctl bootout "$service_target"
fi
launchctl bootstrap "gui/$UID" "$plist_path"

for attempt in {1..30}; do
  if curl -fsS http://127.0.0.1:18900/health >/dev/null; then
    launchctl print "$service_target"
    exit 0
  fi
  sleep 1
done

print -u2 "mem0 sidecar did not become healthy within 30 seconds"
exit 1
