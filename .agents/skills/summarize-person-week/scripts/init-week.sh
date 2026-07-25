#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: init-week.sh YYYY-MM-DD [workspace-root]   (date must be the Monday of the week)" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 2 ]] || usage

monday_value=$1
[[ "$monday_value" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || {
  echo "invalid date: $monday_value (expected YYYY-MM-DD)" >&2
  exit 2
}

# Validate calendar date and compute weekday, ISO week, and Sunday, using the
# macOS (BSD) date path first and falling back to the GNU (Linux) date path.
if date -j -f "%Y-%m-%d" "$monday_value" "+%Y-%m-%d" >/dev/null 2>&1; then
  validated_monday=$(date -j -f "%Y-%m-%d" "$monday_value" "+%Y-%m-%d")
  weekday=$(date -j -f "%Y-%m-%d" "$monday_value" "+%u")
  iso_week=$(date -j -f "%Y-%m-%d" "$monday_value" "+%G-W%V")
  sunday_value=$(date -j -v+6d -f "%Y-%m-%d" "$monday_value" "+%Y-%m-%d")
elif date -d "$monday_value" "+%Y-%m-%d" >/dev/null 2>&1; then
  validated_monday=$(date -d "$monday_value" "+%Y-%m-%d")
  weekday=$(date -d "$monday_value" "+%u")
  iso_week=$(date -d "$monday_value" "+%G-W%V")
  sunday_value=$(date -d "$monday_value +6 days" "+%Y-%m-%d")
else
  echo "invalid calendar date: $monday_value" >&2
  exit 2
fi

[[ "$validated_monday" == "$monday_value" ]] || {
  echo "date normalization mismatch: $monday_value -> $validated_monday" >&2
  exit 2
}

[[ "$weekday" == "1" ]] || {
  echo "date is not a Monday: $monday_value (weekday=$weekday, expected 1)" >&2
  echo "pass the Monday that starts the target week" >&2
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
week_dir="$workspace_root/data/personal-weekly/$monday_value"
mkdir -p "$week_dir"

create_once() {
  local path=$1
  local content=$2
  if [[ ! -e "$path" ]]; then
    printf '%s\n' "$content" >"$path"
  fi
}

create_once "$week_dir/00-context.md" "# Weekly context — ${iso_week} (${monday_value}–${sunday_value})

## Scope
- Principal:
- Identity aliases:
- Timezone:
- ISO week: $iso_week
- Monday: $monday_value
- Sunday: $sunday_value
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

## Shared leads

## Open material questions

## Current synthesis"

create_once "$week_dir/10-evidence-jarvis.md" "# Jarvis evidence — ${iso_week} (${monday_value}–${sunday_value})

## Scope

## Coverage

## Evidence

## Follow-up leads

## Gaps"

create_once "$week_dir/20-evidence-feishu.md" "# Feishu evidence — ${iso_week} (${monday_value}–${sunday_value})

## Scope

## Coverage

## Evidence

## Follow-up leads

## Gaps"

create_once "$week_dir/30-evidence-engineering.md" "# Engineering evidence — ${iso_week} (${monday_value}–${sunday_value})

## Scope

## Coverage

## Evidence

## Follow-up leads

## Gaps"

create_once "$week_dir/40-work-items.md" "# Reconciled work items — ${iso_week} (${monday_value}–${sunday_value})"
create_once "$week_dir/60-insights.md" "# Candidate insights — ${iso_week} (${monday_value}–${sunday_value})"
create_once "$week_dir/90-report-draft.md" "# 我的本周全景 · ${iso_week}（${monday_value}–${sunday_value}）

## 本周概要

## 本周主线

## 我：推进、决策与状态

## 项目：我负责范围内的进展

## 人：协作、老板待办与相互承诺

## 事：关键事件、决策与风险

## 物：交付与资产变化

## 关联洞察与本周发现

## 下一步工作台

## 数据覆盖与完整底账"

create_once "$week_dir/99-report.md" "# 我的本周全景 · ${iso_week}（${monday_value}–${sunday_value}）

## 本周概要

## 本周主线

## 我：推进、决策与状态

## 项目：我负责范围内的进展

## 人：协作、老板待办与相互承诺

## 事：关键事件、决策与风险

## 物：交付与资产变化

## 关联洞察与本周发现

## 下一步工作台

## 数据覆盖与完整底账"

echo "$week_dir"
