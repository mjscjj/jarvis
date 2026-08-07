#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"
REPO_ROOT="${1:-$DEFAULT_REPO_ROOT}"

missing=()
for command_name in bash curl git go jq lark-cli; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    missing+=("$command_name")
  fi
done
for relative_path in conf/config.yaml conf/skills.yaml scripts/jarvis-tools .agents/skills/install-jarvis/SKILL.md .agents/skills/install-jarvis/scripts/jarvis-install .agents/skills/initialize-jarvis/SKILL.md .agents/skills/initialize-jarvis/scripts/jarvis-init; do
  if [[ ! -e "${REPO_ROOT}/${relative_path}" ]]; then
    missing+=("${relative_path}")
  fi
done

runtime_config="${REPO_ROOT}/conf/config.runtime.yaml"
runtime_exists=false
[[ -f "$runtime_config" ]] && runtime_exists=true
runtime_ignored=false
if git -C "$REPO_ROOT" check-ignore -q conf/config.runtime.yaml; then
  runtime_ignored=true
fi
dirty_count="$(git -C "$REPO_ROOT" status --porcelain | wc -l | tr -d ' ')"
missing_json="$(printf '%s\n' "${missing[@]-}" | jq -Rsc 'split("\n") | map(select(length > 0))')"
ready=true
if (( ${#missing[@]} > 0 )) || [[ "$runtime_ignored" != "true" ]]; then
  ready=false
fi

jq -nc \
  --arg repo_root "$REPO_ROOT" \
  --argjson ready "$ready" \
  --argjson missing "$missing_json" \
  --argjson runtime_exists "$runtime_exists" \
  --argjson runtime_ignored "$runtime_ignored" \
  --argjson dirty_count "$dirty_count" \
  '{ready:$ready,repo_root:$repo_root,missing:$missing,runtime_config_exists:$runtime_exists,runtime_config_ignored:$runtime_ignored,dirty_file_count:$dirty_count}'

[[ "$ready" == "true" ]]
