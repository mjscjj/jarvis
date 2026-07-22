#!/bin/zsh
# Sign bin/jarvis-server with the stable local identity + fixed identifier so
# macOS TCC grants (Full Disk Access / Photos / …) survive rebuilds.
set -euo pipefail

identity=${JARVIS_CODESIGN_IDENTITY:-Jarvis Local}
identifier=${JARVIS_CODESIGN_IDENTIFIER:-com.bytedance.jarvis.server}
script_dir=${0:A:h}
repo_dir=${script_dir:h}
bin=${1:-$repo_dir/bin/jarvis-server}

if [[ ! -x $bin ]]; then
  echo "binary not found or not executable: $bin" >&2
  exit 1
fi

"$script_dir/ensure-codesign-identity.sh"

echo "signing $bin"
echo "  identity   = $identity"
echo "  identifier = $identifier"
codesign --force --sign "$identity" --identifier "$identifier" "$bin"
codesign --verify --verbose=2 "$bin"
codesign -dv --verbose=4 "$bin" 2>&1 | grep -E '^(Identifier|Signature|Authority|TeamIdentifier)=' || true
echo "signed ok: $bin"
