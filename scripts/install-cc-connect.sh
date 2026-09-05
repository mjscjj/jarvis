#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${REPO_ROOT}/integrations/cc-connect/manifest.sh"

PATCH_PATH="${REPO_ROOT}/${CC_CONNECT_PATCH_RELATIVE_PATH}"
TARGET_BIN="${REPO_ROOT}/bin/cc-connect-jarvis"

fail() {
  printf 'install-cc-connect: %s\n' "$*" >&2
  exit 1
}

for command_name in git go install jq npm; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required command is missing: ${command_name}"
done
platform="$(uname -s)"
arch="$(uname -m)"
if [[ ! ( "$platform" == "Darwin" && "$arch" == "arm64" ) && \
      ! ( "$platform" == "Linux" && ( "$arch" == "x86_64" || "$arch" == "amd64" ) ) ]]; then
  fail "the Jarvis CC Connect build supports macOS arm64 and Linux x86_64 only (got ${platform}/${arch})"
fi
[[ -s "$PATCH_PATH" ]] || fail "CC Connect patch is missing or empty: ${PATCH_PATH}"

version_output=""
if [[ -x "$TARGET_BIN" ]]; then
  version_output="$("$TARGET_BIN" --version 2>/dev/null || true)"
  if [[ "$version_output" == *"cc-connect ${CC_CONNECT_VERSION}"* && "$version_output" == *"${CC_CONNECT_PATCH_COMMIT}"* ]]; then
    jq -nc --arg path "$TARGET_BIN" --arg version "$CC_CONNECT_VERSION" --arg base_commit "$CC_CONNECT_BASE_COMMIT" --arg patch_commit "$CC_CONNECT_PATCH_COMMIT" \
      '{ok:true,changed:false,path:$path,version:$version,base_commit:$base_commit,patch_commit:$patch_commit}'
    exit 0
  fi
fi

build_root="$(mktemp -d "${TMPDIR:-/tmp}/jarvis-cc-connect.XXXXXX")"
source_dir="${build_root}/source"
built_binary="${build_root}/cc-connect-jarvis"
target_temp=""
cleanup() {
  if [[ -n "$target_temp" && -e "$target_temp" ]]; then rm -f "$target_temp"; fi
  if [[ -n "$build_root" && "$build_root" == "${TMPDIR:-/tmp}"/jarvis-cc-connect.* && -d "$build_root" ]]; then rm -rf "$build_root"; fi
}
trap cleanup EXIT

git init -q "$source_dir"
git -C "$source_dir" remote add origin "$CC_CONNECT_REPO_URL"
git -C "$source_dir" fetch -q --depth 1 origin "$CC_CONNECT_BASE_COMMIT"
git -C "$source_dir" checkout -q --detach FETCH_HEAD
[[ "$(git -C "$source_dir" rev-parse HEAD)" == "$CC_CONNECT_BASE_COMMIT" ]] || fail "CC Connect checkout does not match pinned base commit"
git -C "$source_dir" apply --check "$PATCH_PATH"
git -C "$source_dir" apply "$PATCH_PATH"
(
  cd "$source_dir"
  (
    cd web
    npm install --no-audit --no-fund
    npm run build
  )
  go test ./platform/feishu ./agent/cursor >&2
  build_time="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  go build -tags goolm \
    -ldflags "-s -w -X main.version=${CC_CONNECT_VERSION} -X main.commit=${CC_CONNECT_PATCH_COMMIT} -X main.buildTime=${build_time}" \
    -o "$built_binary" ./cmd/cc-connect
)
version_output="$("$built_binary" --version)"
[[ "$version_output" == *"cc-connect ${CC_CONNECT_VERSION}"* && "$version_output" == *"${CC_CONNECT_PATCH_COMMIT}"* ]] || fail "built CC Connect binary does not report the pinned Jarvis version"

mkdir -p "${REPO_ROOT}/bin"
target_temp="$(mktemp "${REPO_ROOT}/bin/.cc-connect-jarvis.XXXXXX")"
install -m 0755 "$built_binary" "$target_temp"
mv "$target_temp" "$TARGET_BIN"
target_temp=""
jq -nc --arg path "$TARGET_BIN" --arg version "$CC_CONNECT_VERSION" --arg base_commit "$CC_CONNECT_BASE_COMMIT" --arg patch_commit "$CC_CONNECT_PATCH_COMMIT" \
  '{ok:true,changed:true,path:$path,version:$version,base_commit:$base_commit,patch_commit:$patch_commit}'
