#!/bin/zsh
# launchd entrypoint: an unsigned rebuild must never reach macOS TCC.
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
server_bin=$repo_dir/bin/jarvis-server

# Keep Jarvis-launched Go builds responsive on this 14-core workstation:
# at most two packages compile concurrently, with two Go scheduler threads each.
export GOMAXPROCS=2
export GOFLAGS=-p=2

if ! "$script_dir/verify-server-signature.sh" "$server_bin"; then
  echo "refusing to start jarvis-server with an unstable code-signing identity" >&2
  # KeepAlive.SuccessfulExit=false: exit cleanly so launchd does not restart-loop.
  exit 0
fi

exec "$server_bin" "$@"
