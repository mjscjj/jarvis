#!/usr/bin/env bash
set -euo pipefail
umask 077
mkdir -p "$CODEX_HOME"
# Carry credentials only, never the host config, MCP servers, sessions or memory.
if [[ ! -f "$CODEX_HOME/auth.json" ]]; then
  cp /credentials/auth.json "$CODEX_HOME/auth.json"
fi
socat TCP-LISTEN:18080,bind=127.0.0.1,reuseaddr,fork UNIX-CONNECT:/run/okr-access.sock &
relay_pid=$!
trap 'kill "$relay_pid" 2>/dev/null || true' EXIT
# The empty client home has no host MCP or app configuration. Force these
# switches on every run so persistent native state cannot change the profile.
codex -c web_search='"disabled"' -c features.apps=false "$@" --ignore-user-config
