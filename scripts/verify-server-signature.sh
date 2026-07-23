#!/bin/zsh
# Refuse to run jarvis-server unless it carries the stable TCC identity.
set -euo pipefail

script_dir=${0:A:h}
repo_dir=${script_dir:h}
server_bin=${1:-$repo_dir/bin/jarvis-server}
expected_identifier=${JARVIS_CODESIGN_IDENTIFIER:-com.bytedance.jarvis.server}
expected_authority=${JARVIS_CODESIGN_IDENTITY:-Jarvis Local}

if [[ ! -x $server_bin ]]; then
  echo "jarvis-server is missing or not executable: $server_bin" >&2
  exit 1
fi

codesign --verify --strict "$server_bin"
signature_info=$(codesign -dv --verbose=4 "$server_bin" 2>&1)
identifier=$(printf '%s\n' "$signature_info" | sed -n 's/^Identifier=//p')
authority=$(printf '%s\n' "$signature_info" | sed -n 's/^Authority=//p' | head -1)

if [[ $identifier != $expected_identifier ]]; then
  echo "invalid jarvis-server identifier: got \"$identifier\", want \"$expected_identifier\"" >&2
  echo "rebuild with: $repo_dir/scripts/rebuild-server.sh" >&2
  exit 1
fi
if [[ $authority != $expected_authority ]]; then
  echo "invalid jarvis-server signing authority: got \"${authority:-adhoc}\", want \"$expected_authority\"" >&2
  echo "rebuild with: $repo_dir/scripts/rebuild-server.sh" >&2
  exit 1
fi

echo "jarvis-server signature verified: $expected_identifier / $expected_authority"
