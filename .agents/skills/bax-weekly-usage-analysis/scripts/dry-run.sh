#!/usr/bin/env bash
set -euo pipefail

export TZ=Asia/Shanghai

usage() {
  echo "usage: dry-run.sh [target-week-monday YYYY-MM-DD]" >&2
  exit 2
}

[[ $# -le 1 ]] || usage

date_cmd=gnu
if date -j -f "%Y-%m-%d" "2026-01-05" "+%Y-%m-%d" >/dev/null 2>&1; then
  date_cmd=bsd
fi

validate_date() {
  local value=$1
  if [[ "$date_cmd" == "bsd" ]]; then
    date -j -f "%Y-%m-%d" "$value" "+%Y-%m-%d"
  else
    date -d "$value" "+%Y-%m-%d"
  fi
}

weekday() {
  local value=$1
  if [[ "$date_cmd" == "bsd" ]]; then
    date -j -f "%Y-%m-%d" "$value" "+%u"
  else
    date -d "$value" "+%u"
  fi
}

offset_date() {
  local value=$1
  local days=$2
  if [[ "$date_cmd" == "bsd" ]]; then
    date -j -v"${days}"d -f "%Y-%m-%d" "$value" "+%Y-%m-%d"
  else
    date -d "$value ${days} days" "+%Y-%m-%d"
  fi
}

latest_complete_monday() {
  if [[ "$date_cmd" == "bsd" ]]; then
    local today
    today=$(date "+%Y-%m-%d")
    local dow
    dow=$(date "+%u")
    local days_back=$((dow + 6))
    date -j -v-"${days_back}"d -f "%Y-%m-%d" "$today" "+%Y-%m-%d"
  else
    local today
    today=$(date "+%Y-%m-%d")
    local dow
    dow=$(date "+%u")
    local days_back=$((dow + 6))
    date -d "$today -${days_back} days" "+%Y-%m-%d"
  fi
}

if [[ $# -eq 1 ]]; then
  target_monday=$1
else
  target_monday=$(latest_complete_monday)
fi

[[ "$target_monday" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || usage
validated=$(validate_date "$target_monday") || {
  echo "invalid calendar date: $target_monday" >&2
  exit 2
}
[[ "$validated" == "$target_monday" ]] || {
  echo "date normalization mismatch: $target_monday -> $validated" >&2
  exit 2
}
[[ "$(weekday "$target_monday")" == "1" ]] || {
  echo "target week must start on Monday: $target_monday" >&2
  exit 2
}

previous_monday=$(offset_date "$target_monday" -7)
target_end=$(offset_date "$target_monday" 7)

cat <<PLAN
BAX weekly usage analysis dry run

Scope:
- timezone: Asia/Shanghai
- previous week: [$previous_monday, $target_monday)
- target week:   [$target_monday, $target_end)

Primary dataset:
- bytedcli site: i18n-tt
- Aeolus region: sg
- dataset: 3574811
- tenant_id_type: bax-am

Read-only commands to run during a real analysis:

bytedcli --site i18n-tt --json aeolus dataset-fields -r sg 3574811

bytedcli --site i18n-tt --json aeolus query -r sg 3574811 '<SQL: check latest partition and max business date>'

bytedcli --site i18n-tt --json aeolus query -r sg 3574811 '<SQL: compare WAU, DAU, sessions, messages for $previous_monday..$target_end>'

bytedcli --site i18n-tt --json aeolus query -r sg 3574811 '<SQL: cohort retained/lost/new users for $previous_monday..$target_end>'

bytedcli --site i18n-tt --json aeolus query -r sg 3574811 '<SQL: department/region and agent/skill/scene contribution for $previous_monday..$target_end>'

No Aeolus query, Feishu write, document creation, or scheduled task creation was executed.
PLAN
