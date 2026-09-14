#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_root=${script_dir:h:h}
source "$script_dir/runtime-manifest.sh"
source "$repo_root/integrations/cc-connect/manifest.sh"

runtime_root=${1:?usage: validate-runtime.sh /path/to/runtime [/path/to/Jarvis.app]}
app_path=${2:-}

fail() {
  printf 'validate-runtime: %s\n' "$*" >&2
  exit 1
}

for name in \
  qdrant \
  cc-connect-jarvis \
  lark-cli \
  traex \
  node \
  jq \
  jarvis-config \
  jarvis-server \
  jarvis-app-service; do
  "$script_dir/check-macho.sh" "$runtime_root/bin/$name" "$name"
done

for name in bytedcli codex; do
  [[ -f "$runtime_root/bin/$name" && -x "$runtime_root/bin/$name" ]] ||
    fail "missing executable launcher: bin/$name"
done

[[ -f "$runtime_root/scripts/jarvis-lark-auth" ]] || fail "missing scripts/jarvis-lark-auth"
bash -n "$runtime_root/scripts/jarvis-lark-auth" || fail "invalid scripts/jarvis-lark-auth"

bash -n "$runtime_root/scripts/lib/connection.sh" || fail "invalid or missing connection helper"
[[ -f "$runtime_root/scripts/json-api-data.mjs" ]] || fail "missing exact JSON helper"
for module in common commands world evidence task schedule memory skill notify; do
  module_path="$runtime_root/scripts/lib/jarvis-tools/$module.sh"
  [[ -f "$module_path" ]] || fail "missing tool module: $module"
  bash -n "$module_path" || fail "invalid tool module: $module"
done
bash -n "$runtime_root/scripts/jarvis-tools" || fail "invalid scripts/jarvis-tools"

temporary_home=$(mktemp -d "${TMPDIR:-/tmp}/jarvis-runtime-validation.XXXXXX")
trap 'rm -rf "$temporary_home"' EXIT
minimal_path="$runtime_root/bin:/usr/bin:/bin"
env -i HOME="$temporary_home" PATH="$minimal_path" \
  bash "$runtime_root/scripts/jarvis-tools" help all >/dev/null || fail "tool discovery smoke test failed"
env -i HOME="$temporary_home" PATH="$minimal_path" \
  "$runtime_root/bin/node" --check "$runtime_root/scripts/json-api-data.mjs" || fail "invalid exact JSON helper"

run_version() {
  local name=$1
  shift
  local output
  output=$(env -i HOME="$temporary_home" PATH="$minimal_path" "$runtime_root/bin/$name" "$@" 2>&1) ||
    fail "$name smoke test failed: $output"
  [[ -n "$output" ]] || fail "$name smoke test returned empty output"
  printf 'validate-runtime: %s: %s\n' "$name" "${output%%$'\n'*}"
  REPLY=$output
}

run_version qdrant --version
[[ "$REPLY" == *"qdrant "* ]] || fail "qdrant version output is invalid"
run_version cc-connect-jarvis --version
[[ "$REPLY" == *"cc-connect ${CC_CONNECT_VERSION}"* && "$REPLY" == *"${CC_CONNECT_PATCH_COMMIT}"* ]] ||
  fail "cc-connect version does not match repository manifest"
run_version lark-cli --version
[[ "$REPLY" == "lark-cli version $JARVIS_LARK_CLI_VERSION" ]] ||
  fail "lark-cli version is ${REPLY:q}, want $JARVIS_LARK_CLI_VERSION"
[[ -s "$runtime_root/licenses/lark-cli-LICENSE" ]] || fail "missing Lark CLI license"
env -i HOME="$temporary_home" PATH="$minimal_path" JARVIS_JQ_BIN="$runtime_root/bin/jq" \
  bash "$script_dir/check-lark-skills.sh" "$runtime_root/bin/lark-cli" || fail "bundled Lark Skills validation failed"
run_version traex --version
[[ "$REPLY" == *"traecli "* ]] || fail "traex version output is invalid"
run_version node --version
[[ "$REPLY" == v<->* ]] || fail "node version output is invalid"
run_version jq --version
[[ "$REPLY" == "jq-${JARVIS_JQ_VERSION}" ]] ||
  fail "jq version is ${REPLY:q}, want jq-${JARVIS_JQ_VERSION}"
run_version bytedcli --version
[[ "$REPLY" == <->.<->.<-> ]] || fail "bytedcli version output is invalid"
run_version codex --version
[[ "$REPLY" == *"traecli "* ]] || fail "codex launcher does not resolve to Trae CLI"

if [[ -n "$app_path" ]]; then
  info_plist="$app_path/Contents/Info.plist"
  [[ -f "$info_plist" ]] || fail "missing app Info.plist: $info_plist"
  declared_minimum=$(plutil -extract LSMinimumSystemVersion raw -o - "$info_plist") ||
    fail "cannot read LSMinimumSystemVersion"
  [[ "$declared_minimum" == "$JARVIS_MACOS_MIN_VERSION" ]] ||
    fail "Info.plist minimum is $declared_minimum, want $JARVIS_MACOS_MIN_VERSION"
  printf 'validate-runtime: app minimum macOS=%s\n' "$declared_minimum"
fi

printf 'validate-runtime: runtime artifact is self-contained for macOS %s+\n' "$JARVIS_MACOS_MIN_VERSION"
