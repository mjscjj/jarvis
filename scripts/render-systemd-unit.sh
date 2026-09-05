#!/usr/bin/env bash
set -euo pipefail

if (( $# < 1 || $# > 2 )); then
  printf 'Usage: %s <systemd-label> [cc-config-path]\n' "$(basename "$0")" >&2
  exit 2
fi

LABEL="$1"
CC_CONFIG_PATH="${2:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TEMPLATE="${REPO_ROOT}/deploy/${LABEL}.service.template"
UNITS_DIR="${HOME}/.config/systemd/user"
TARGET="${UNITS_DIR}/${LABEL}.service"

[[ -f "$TEMPLATE" ]] || { printf 'systemd template not found: %s\n' "$TEMPLATE" >&2; exit 1; }
[[ "$REPO_ROOT" != *$'\n'* && "$REPO_ROOT" != *' '* ]] || { printf 'repository path must not contain whitespace for systemd rendering: %s\n' "$REPO_ROOT" >&2; exit 1; }
[[ "$HOME" != *$'\n'* && "$HOME" != *' '* ]] || { printf 'HOME must not contain whitespace for systemd rendering: %s\n' "$HOME" >&2; exit 1; }
if grep -q '__CC_CONFIG__' "$TEMPLATE"; then
  [[ -n "$CC_CONFIG_PATH" && -f "$CC_CONFIG_PATH" ]] || { printf 'CC config is required and must exist: %s\n' "$CC_CONFIG_PATH" >&2; exit 1; }
  [[ "$CC_CONFIG_PATH" != *$'\n'* && "$CC_CONFIG_PATH" != *' '* ]] || { printf 'CC config path must not contain whitespace: %s\n' "$CC_CONFIG_PATH" >&2; exit 1; }
fi

mkdir -p "$UNITS_DIR"
temporary="$(mktemp "${UNITS_DIR}/.${LABEL}.XXXXXX")"
trap 'rm -f "$temporary"' EXIT
sed -e "s|__JARVIS_ROOT__|${REPO_ROOT}|g" \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__CC_CONFIG__|${CC_CONFIG_PATH}|g" \
    "$TEMPLATE" >"$temporary"
chmod 0600 "$temporary"
mv "$temporary" "$TARGET"
trap - EXIT
printf '%s\n' "$TARGET"
