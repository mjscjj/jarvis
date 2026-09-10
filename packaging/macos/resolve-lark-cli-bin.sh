#!/bin/zsh
set -euo pipefail

entry=${1:?usage: resolve-lark-cli-bin.sh /path/to/lark-cli}

fail() {
  printf 'resolve-lark-cli-bin: %s\n' "$*" >&2
  exit 1
}

[[ -x "$entry" ]] || fail "lark-cli entry is not executable: $entry"

resolved=${entry:A}
candidate=$resolved
if ! lipo -archs "$candidate" 2>/dev/null | tr ' ' '\n' | grep -Fx arm64 >/dev/null; then
  # The official npm package exposes scripts/run.js on PATH. That launcher
  # locates its native binary relative to its package root and is not portable
  # when copied by itself.
  candidate="${resolved:h:h}/bin/lark-cli"
fi

[[ -x "$candidate" ]] || fail "native lark-cli binary not found for entry: $entry"
lipo -archs "$candidate" 2>/dev/null | tr ' ' '\n' | grep -Fx arm64 >/dev/null ||
  fail "native lark-cli binary is not arm64: $candidate"

printf '%s\n' "$candidate"
