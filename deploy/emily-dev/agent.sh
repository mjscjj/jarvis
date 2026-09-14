#!/usr/bin/env bash
set -euo pipefail
run_id="$1"
shift
[[ "$run_id" =~ ^[a-f0-9]{24}$ ]] || exit 2
mkdir -p /dev-state/agent-runs "$CODEX_HOME"
cp /credentials/auth.json "$CODEX_HOME/auth.json"
exec setsid --wait bash -c 'echo $$ > "/dev-state/agent-runs/$1.pid"; shift; exec codex -c web_search="\"disabled\"" -c features.apps=false "$@" --ignore-user-config' bash "$run_id" "$@"
