#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LABEL="com.bytedance.jarvis.server"
NEXT_BIN="${REPO_ROOT}/bin/jarvis-server.next"

[[ "$(uname -s)" == "Linux" ]] || { printf 'install-systemd: Linux is required\n' >&2; exit 1; }
command -v systemctl >/dev/null 2>&1 || { printf 'install-systemd: systemctl is required\n' >&2; exit 1; }

mkdir -p "${REPO_ROOT}/bin" "${REPO_ROOT}/var/log"
cd "$REPO_ROOT"
trap 'rm -f "$NEXT_BIN"' EXIT
"$SCRIPT_DIR/check-build-toolchain.sh"
npm --prefix "$REPO_ROOT/web" ci
npm --prefix "$REPO_ROOT/web" run build
go build -o "$NEXT_BIN" ./cmd/jarvis-server
mv "$NEXT_BIN" "$REPO_ROOT/bin/jarvis-server"

unit_path="$("$SCRIPT_DIR/render-systemd-unit.sh" "$LABEL")"
systemctl --user daemon-reload
systemctl --user enable --now "${LABEL}.service"
systemctl --user show --property=FragmentPath --value "${LABEL}.service" | grep -Fx "$unit_path" >/dev/null
systemctl --user --no-pager status "${LABEL}.service"
