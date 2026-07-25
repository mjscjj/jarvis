#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: init-day.sh YYYY-MM-DD [workspace-root]" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 2 ]] || usage

day_value=$1
[[ "$day_value" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || {
  echo "invalid date: $day_value (expected YYYY-MM-DD)" >&2
  exit 2
}

if date -j -f "%Y-%m-%d" "$day_value" "+%Y-%m-%d" >/dev/null 2>&1; then
  validated_day=$(date -j -f "%Y-%m-%d" "$day_value" "+%Y-%m-%d")
elif date -d "$day_value" "+%Y-%m-%d" >/dev/null 2>&1; then
  validated_day=$(date -d "$day_value" "+%Y-%m-%d")
else
  echo "invalid calendar date: $day_value" >&2
  exit 2
fi

[[ "$validated_day" == "$day_value" ]] || {
  echo "date normalization mismatch: $day_value -> $validated_day" >&2
  exit 2
}

if [[ $# -eq 2 ]]; then
  workspace_root=$2
else
  workspace_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "cannot resolve workspace root; pass it explicitly" >&2
    exit 2
  }
fi

workspace_root=$(cd "$workspace_root" && pwd)
day_dir="$workspace_root/data/personal-daily/$day_value"
mkdir -p "$day_dir"

create_once() {
  local path=$1
  local content=$2
  if [[ ! -e "$path" ]]; then
    printf '%s\n' "$content" >"$path"
  fi
}

create_once "$day_dir/00-context.md" "# Daily context — $day_value

## Scope
- Principal:
- Identity aliases:
- Timezone:
- Window:
- Current cutoff:
- Report kind:

## Durable context
- Active goals:
- Project bindings:
- People relationships:
- Repository and group bindings:

## Run log

## Coverage
- Jarvis: pending
- Feishu: pending
- Engineering: pending

## Current synthesis"

create_once "$day_dir/10-evidence-jarvis.md" "# Jarvis evidence — $day_value

## Scope

## Coverage

## Evidence

## Gaps"

create_once "$day_dir/20-evidence-feishu.md" "# Feishu evidence — $day_value

## Scope

## Coverage

## Evidence

## Gaps"

create_once "$day_dir/30-evidence-engineering.md" "# Engineering evidence — $day_value

## Scope

## Coverage

## Evidence

## Gaps"

echo "$day_dir"
