#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_root=${script_dir:h:h}

[[ "$(uname -s)" == "Darwin" ]] || { printf 'build-dmg: macOS is required\n' >&2; exit 1; }
[[ "$(uname -m)" == "arm64" ]] || { printf 'build-dmg: Apple Silicon is required\n' >&2; exit 1; }

export PATH="$HOME/.cargo/bin:$PATH"

command -v cargo >/dev/null 2>&1 || { printf 'build-dmg: cargo is required\n' >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { printf 'build-dmg: npm is required\n' >&2; exit 1; }

npm --prefix "$repo_root/desktop" ci
npm --prefix "$repo_root/desktop" run dmg
