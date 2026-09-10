#!/usr/bin/env bash
# Check the actual binary being bundled, not the build machine's global Skills.
set -euo pipefail

binary="${1:?usage: check-lark-skills.sh /path/to/lark-cli}"
jq_bin="${JARVIS_JQ_BIN:-jq}"
export LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1
export LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1

fail() {
  printf 'check-lark-skills: %s; use a lark-cli binary with embedded Skills\n' "$*" >&2
  exit 1
}

read_skill() {
  local content
  content="$("$binary" skills read "$1")" || fail "cannot read $1"
  [[ "$content" == *[![:space:]]* ]] || fail "empty content: $1"
}

for skill in lark-shared lark-contact lark-drive lark-doc lark-im; do
  read_skill "$skill"
  listing="$("$binary" skills list "$skill/references")" || fail "cannot list $skill references"
  paths="$("$jq_bin" -er 'select(.ok == true) | .entries | select(length > 0) | .[] | select(.is_dir == false) | .path' <<<"$listing")" || fail "missing $skill references"
  while IFS= read -r reference; do
    read_skill "$reference"
  done <<<"$paths"
done

printf 'check-lark-skills: embedded Skills and references are readable\n'
