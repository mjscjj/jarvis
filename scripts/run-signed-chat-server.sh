#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
chat_bin=$repo_dir/bin/jarvis-chat-server

export GOMAXPROCS=2
export GOFLAGS=-p=2

if ! "$script_dir/verify-server-signature.sh" "$chat_bin"; then
  echo "refusing to start jarvis-chat-server with an unstable code-signing identity" >&2
  exit 0
fi

exec "$chat_bin" "$@"
