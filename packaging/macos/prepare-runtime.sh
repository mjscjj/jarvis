#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_root=${script_dir:h:h}
output_dir=${1:-"$repo_root/build/macos-runtime"}
staging_dir="${output_dir}.next"
qdrant_bin=${JARVIS_QDRANT_BIN:-"$repo_root/bin/qdrant"}
cc_connect_bin=${JARVIS_CC_CONNECT_BIN:-"$repo_root/bin/cc-connect-jarvis"}

fail() {
  printf 'prepare-runtime: %s\n' "$*" >&2
  exit 1
}

[[ "$(uname -s)" == "Darwin" ]] || fail "macOS is required"
[[ "$(uname -m)" == "arm64" ]] || fail "only Apple Silicon is currently supported"
command -v go >/dev/null 2>&1 || fail "go is required"
command -v npm >/dev/null 2>&1 || fail "npm is required"
[[ -x "$qdrant_bin" ]] || fail "missing Qdrant binary; run ./scripts/jarvis-install install-qdrant"
[[ -x "$cc_connect_bin" ]] || fail "missing CC Connect binary; run ./scripts/jarvis-install install-cc-connect"

rm -rf "$staging_dir"
mkdir -p "$staging_dir/bin"

npm --prefix "$repo_root/web" ci
npm --prefix "$repo_root/web" run build

(
  cd "$repo_root"
  CGO_ENABLED=1 go build -trimpath -o "$staging_dir/bin/jarvis-server" ./cmd/jarvis-server
  CGO_ENABLED=0 go build -trimpath -o "$staging_dir/bin/jarvis-app-service" ./cmd/jarvis-app-service
)

install -m 0755 "$qdrant_bin" "$staging_dir/bin/qdrant"
install -m 0755 "$cc_connect_bin" "$staging_dir/bin/cc-connect-jarvis"
ditto "$repo_root/conf" "$staging_dir/conf"
rm -f "$staging_dir/conf/config.runtime.yaml"
ditto "$repo_root/.agents" "$staging_dir/.agents"
ditto "$repo_root/scripts" "$staging_dir/scripts"
mkdir -p "$staging_dir/web"
ditto "$repo_root/web/dist" "$staging_dir/web/dist"

identity=${JARVIS_APP_SIGN_IDENTITY:--}
for binary in \
  "$staging_dir/bin/qdrant" \
  "$staging_dir/bin/cc-connect-jarvis" \
  "$staging_dir/bin/jarvis-server" \
  "$staging_dir/bin/jarvis-app-service"; do
  codesign --force --timestamp=none --sign "$identity" "$binary"
  codesign --verify --strict "$binary"
done

rm -rf "$output_dir"
mv "$staging_dir" "$output_dir"
printf 'runtime=%s\n' "$output_dir"
du -sh "$output_dir"
