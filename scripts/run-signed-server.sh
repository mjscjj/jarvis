#!/bin/zsh
# launchd entrypoint: an unsigned rebuild must never reach macOS TCC.
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
server_bin=$repo_dir/bin/jarvis-server

export PSM=${PSM:-tikcast.agency.jarvis}
export HERTZ_CONF_DIR=${HERTZ_CONF_DIR:-"$repo_dir/conf"}
export HERTZ_LOG_DIR=${HERTZ_LOG_DIR:-"$repo_dir/var/log"}

if ! "$script_dir/verify-server-signature.sh" "$server_bin"; then
  echo "refusing to start jarvis-server with an unstable code-signing identity" >&2
  # KeepAlive.SuccessfulExit=false: exit cleanly so launchd does not restart-loop.
  exit 0
fi

exec "$server_bin" "$@"
