#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
source "$script_dir/runtime-manifest.sh"
runtime_root=${1:?usage: prepare-lark-cli.sh /path/to/runtime}

temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/jarvis-lark-cli.XXXXXX")
trap 'rm -rf "$temporary_dir"' EXIT
archive="$temporary_dir/lark-cli.tar.gz"
curl -fL "$JARVIS_LARK_CLI_URL" -o "$archive"
actual_sha256=$(shasum -a 256 "$archive" | awk '{ print $1 }')
if [[ "$actual_sha256" != "$JARVIS_LARK_CLI_SHA256" ]]; then
  printf 'prepare-lark-cli: sha256 mismatch: got=%s want=%s\n' "$actual_sha256" "$JARVIS_LARK_CLI_SHA256" >&2
  exit 1
fi

# Check the release archive before extracting or executing its contents.
tar -xzf "$archive" -C "$temporary_dir" lark-cli LICENSE
mkdir -p "$runtime_root/bin" "$runtime_root/licenses"
install -m 0755 "$temporary_dir/lark-cli" "$runtime_root/bin/lark-cli"
install -m 0644 "$temporary_dir/LICENSE" "$runtime_root/licenses/lark-cli-LICENSE"
printf 'prepare-lark-cli: installed official v%s darwin-arm64\n' "$JARVIS_LARK_CLI_VERSION"
