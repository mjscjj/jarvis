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

JARVIS_API_BASE=""
JARVIS_TIMEZONE=""
if [[ "$LABEL" == "com.bytedance.jarvis.cc-connect" ]]; then
  # Bind generated these values from effective Jarvis config. Reuse that same
  # Agent environment for the daemon; no config parsing or Go at Agent runtime.
  connection_value() {
    awk -v key="$1" '
      /^[[:space:]]*\[\[projects\]\][[:space:]]*$/ { target = 0; in_env = 0 }
      /^[[:space:]]*name[[:space:]]*=[[:space:]]*"jarvis-codex"[[:space:]]*(#.*)?$/ { target = 1 }
      /^[[:space:]]*\[/ { in_env = target && ($0 ~ /^[[:space:]]*\[projects\.agent\.options\.env\][[:space:]]*$/) }
      in_env && $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
        value = $0; sub(/^[^=]*=[[:space:]]*"/, "", value); sub(/"[[:space:]]*(#.*)?$/, "", value)
        print value; count++
      }
      END { if (count != 1) exit 1 }
    ' "$CC_CONFIG_PATH"
  }
  JARVIS_API_BASE="$(connection_value JARVIS_API_BASE)" || { printf 'CC Agent JARVIS_API_BASE is missing; bind CC first\n' >&2; exit 1; }
  JARVIS_TIMEZONE="$(connection_value JARVIS_TIMEZONE)" || { printf 'CC Agent JARVIS_TIMEZONE is missing; bind CC first\n' >&2; exit 1; }
  [[ -n "$JARVIS_API_BASE" && -n "$JARVIS_TIMEZONE" ]] || { printf 'CC Agent connection environment must not be empty\n' >&2; exit 1; }
fi

# Escape replacements before passing connection values to sed.
sed_value() { printf '%s' "$1" | sed 's/[\\&|]/\\&/g'; }

mkdir -p "$UNITS_DIR"
temporary="$(mktemp "${UNITS_DIR}/.${LABEL}.XXXXXX")"
trap 'rm -f "$temporary"' EXIT
sed -e "s|__JARVIS_ROOT__|${REPO_ROOT}|g" \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__CC_CONFIG__|${CC_CONFIG_PATH}|g" \
    -e "s|__JARVIS_API_BASE__|$(sed_value "${JARVIS_API_BASE//%/%%}")|g" \
    -e "s|__JARVIS_TIMEZONE__|$(sed_value "$JARVIS_TIMEZONE")|g" \
    "$TEMPLATE" >"$temporary"
chmod 0600 "$temporary"
mv "$temporary" "$TARGET"
trap - EXIT
printf '%s\n' "$TARGET"
