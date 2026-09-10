#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_root=${script_dir:h:h}
source "$script_dir/runtime-manifest.sh"
export MACOSX_DEPLOYMENT_TARGET="$JARVIS_MACOS_MIN_VERSION"
export CGO_CFLAGS="-mmacosx-version-min=$JARVIS_MACOS_MIN_VERSION"
export CGO_LDFLAGS="-mmacosx-version-min=$JARVIS_MACOS_MIN_VERSION"

output_dir=${1:-"$repo_root/build/macos-runtime"}
staging_dir="${output_dir}.next"
qdrant_bin=${JARVIS_QDRANT_BIN:-"$repo_root/bin/qdrant"}
cc_connect_bin=${JARVIS_CC_CONNECT_BIN:-"$repo_root/bin/cc-connect-jarvis"}
lark_cli_entry=${JARVIS_LARK_CLI_BIN:-"$(command -v lark-cli 2>/dev/null || true)"}
traex_bin=${JARVIS_TRAEX_BIN:-"$(command -v traex 2>/dev/null || true)"}
node_bin=${JARVIS_NODE_BIN:-"$(command -v node 2>/dev/null || true)"}
bytedcli_version=${JARVIS_BYTEDCLI_VERSION:-0.147.0}

fail() {
  printf 'prepare-runtime: %s\n' "$*" >&2
  exit 1
}

[[ "$(uname -s)" == "Darwin" ]] || fail "macOS is required"
[[ "$(uname -m)" == "arm64" ]] || fail "only Apple Silicon is currently supported"
command -v go >/dev/null 2>&1 || fail "go is required"
command -v npm >/dev/null 2>&1 || fail "npm is required"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v shasum >/dev/null 2>&1 || fail "shasum is required"
if [[ "${GOSUMDB:-}" == "off" || -z "${GOSUMDB:-}" ]]; then
  export GOSUMDB=sum.golang.org
fi
[[ -x "$qdrant_bin" ]] || fail "missing Qdrant binary; run ./scripts/jarvis-install install-qdrant"
[[ -x "$cc_connect_bin" ]] || fail "missing CC Connect binary; run ./scripts/jarvis-install install-cc-connect"
[[ -x "$lark_cli_entry" ]] || fail "missing lark-cli binary; set JARVIS_LARK_CLI_BIN"
lark_cli_bin="$("$script_dir/resolve-lark-cli-bin.sh" "$lark_cli_entry")"
if [[ ! -x "$traex_bin" && -x "$HOME/.local/bin/traex" ]]; then
  traex_bin="$HOME/.local/bin/traex"
fi
[[ -x "$traex_bin" ]] || fail "missing Trae CLI binary; set JARVIS_TRAEX_BIN"
[[ -x "$node_bin" ]] || fail "missing Node binary; set JARVIS_NODE_BIN"

rm -rf "$staging_dir"
mkdir -p "$staging_dir/bin"

npm --prefix "$repo_root/web" ci
npm --prefix "$repo_root/web" run build

(
  cd "$repo_root"
  CGO_ENABLED=1 go build -trimpath -o "$staging_dir/bin/jarvis-server" ./cmd/jarvis-server
  CGO_ENABLED=0 go build -trimpath -o "$staging_dir/bin/jarvis-app-service" ./cmd/jarvis-app-service
  CGO_ENABLED=0 go build -trimpath -o "$staging_dir/bin/jarvis-config" ./cmd/jarvis-config
)

install -m 0755 "$qdrant_bin" "$staging_dir/bin/qdrant"
install -m 0755 "$cc_connect_bin" "$staging_dir/bin/cc-connect-jarvis"
install -m 0755 "$lark_cli_bin" "$staging_dir/bin/lark-cli"
install -m 0755 "$traex_bin" "$staging_dir/bin/traex"
install -m 0755 "$node_bin" "$staging_dir/bin/node"
curl -fL "$JARVIS_JQ_URL" -o "$staging_dir/bin/jq"
jq_sha256=$(shasum -a 256 "$staging_dir/bin/jq" | awk '{ print $1 }')
[[ "$jq_sha256" == "$JARVIS_JQ_SHA256" ]] ||
  fail "jq sha256 mismatch: got=$jq_sha256 want=$JARVIS_JQ_SHA256"
chmod 0755 "$staging_dir/bin/jq"
JARVIS_JQ_BIN="$staging_dir/bin/jq" bash "$script_dir/check-lark-skills.sh" "$staging_dir/bin/lark-cli"
mkdir -p "$staging_dir/lib/bytedcli"
NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY:-http://bnpm.byted.org} \
  npm install --prefix "$staging_dir/lib/bytedcli" \
  --omit=dev --no-audit --no-fund "@bytedance-dev/bytedcli@${bytedcli_version}"
cat >"$staging_dir/bin/bytedcli" <<'EOF'
#!/bin/sh
root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
exec "$root/bin/node" "$root/lib/bytedcli/node_modules/@bytedance-dev/bytedcli/dist/bytedcli.js" "$@"
EOF
chmod 0755 "$staging_dir/bin/bytedcli"
cat >"$staging_dir/bin/codex" <<'EOF'
#!/bin/sh
exec "$(CDPATH= cd -- "$(dirname "$0")" && pwd)/traex" "$@"
EOF
chmod 0755 "$staging_dir/bin/codex"
ditto "$repo_root/conf" "$staging_dir/conf"
rm -f "$staging_dir/conf/config.runtime.yaml"
ditto "$repo_root/.agents" "$staging_dir/.agents"
ditto "$repo_root/scripts" "$staging_dir/scripts"
install -m 0644 "$repo_root/AGENTS.md" "$staging_dir/AGENTS.md"
install -m 0644 "$repo_root/goal.md" "$staging_dir/goal.md"
install -m 0644 "$repo_root/README.md" "$staging_dir/README.md"
mkdir -p "$staging_dir/web"
ditto "$repo_root/web/dist" "$staging_dir/web/dist"

"$script_dir/validate-runtime.sh" "$staging_dir"

identity=${JARVIS_APP_SIGN_IDENTITY:--}
for binary in \
  "$staging_dir/bin/qdrant" \
  "$staging_dir/bin/cc-connect-jarvis" \
  "$staging_dir/bin/lark-cli" \
  "$staging_dir/bin/traex" \
  "$staging_dir/bin/node" \
  "$staging_dir/bin/jq" \
  "$staging_dir/bin/jarvis-config" \
  "$staging_dir/bin/jarvis-server" \
  "$staging_dir/bin/jarvis-app-service"; do
  codesign --force --timestamp=none --sign "$identity" "$binary"
  codesign --verify --strict "$binary"
done

rm -rf "$output_dir"
mv "$staging_dir" "$output_dir"
printf 'runtime=%s\n' "$output_dir"
du -sh "$output_dir"
