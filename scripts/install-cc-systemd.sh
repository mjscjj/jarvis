#!/usr/bin/env bash
set -euo pipefail

if (( $# != 1 )); then
  printf 'Usage: %s <cc-config-path>\n' "$(basename "$0")" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LABEL="com.bytedance.jarvis.cc-connect"
CC_CONFIG_PATH="$1"

[[ "$(uname -s)" == "Linux" ]] || { printf 'install-cc-systemd: Linux is required\n' >&2; exit 1; }
[[ -x "${REPO_ROOT}/bin/cc-connect-jarvis" ]] || { printf 'patched CC Connect binary is missing\n' >&2; exit 1; }
[[ -f "$CC_CONFIG_PATH" ]] || { printf 'CC Connect config is missing: %s\n' "$CC_CONFIG_PATH" >&2; exit 1; }
chmod 0600 "$CC_CONFIG_PATH"

unit_path="$("$SCRIPT_DIR/render-systemd-unit.sh" "$LABEL" "$CC_CONFIG_PATH")"
systemctl --user daemon-reload
systemctl --user enable --now "${LABEL}.service"
systemctl --user show --property=FragmentPath --value "${LABEL}.service" | grep -Fx "$unit_path" >/dev/null
systemctl --user --no-pager status "${LABEL}.service"
