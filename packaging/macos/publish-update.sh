#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
repo_root=${script_dir:h:h}
config_path="$repo_root/desktop/src-tauri/tauri.conf.json"
package_path="$repo_root/desktop/package.json"
cargo_path="$repo_root/desktop/src-tauri/Cargo.toml"
bundle_root="$repo_root/desktop/src-tauri/target/release/bundle"
private_key=${TAURI_SIGNING_PRIVATE_KEY_PATH:-"$HOME/.tauri/jarvis-updater.key"}
remote=${JARVIS_UPDATE_REMOTE:-"chujiejie.1@10.199.197.219"}
remote_root=${JARVIS_UPDATE_REMOTE_ROOT:-"/data00/home/chujiejie.1/jarvis-updates"}
base_url=${JARVIS_UPDATE_BASE_URL:-"https://jarvisx.bytedance.net/jarvis-updates"}
notes=${1:-"Jarvis desktop update"}

fail() {
  printf 'publish-update: %s\n' "$*" >&2
  exit 1
}

[[ "$(uname -s)" == "Darwin" ]] || fail "macOS is required"
[[ "$(uname -m)" == "arm64" ]] || fail "Apple Silicon is required"
[[ -f "$private_key" ]] || fail "missing updater private key: $private_key"
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v scp >/dev/null 2>&1 || fail "scp is required"
command -v ssh >/dev/null 2>&1 || fail "ssh is required"

version=$(jq -er '.version' "$config_path") || fail "read Tauri version"
[[ "$version" == <->.<->.<->(|-[0-9A-Za-z.-]##) ]] ||
  fail "version is not SemVer: $version"
package_version=$(jq -er '.version' "$package_path") || fail "read desktop package version"
cargo_version=$(awk -F ' *= *' '/^version *=/ { gsub(/"/, "", $2); print $2; exit }' "$cargo_path")
[[ "$package_version" == "$version" ]] ||
  fail "desktop/package.json version $package_version does not match $version"
[[ "$cargo_version" == "$version" ]] ||
  fail "Cargo.toml version $cargo_version does not match $version"

export TAURI_SIGNING_PRIVATE_KEY=$(<"$private_key")
export TAURI_SIGNING_PRIVATE_KEY_PASSWORD=${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}
"$script_dir/build-dmg.sh"

artifact="$bundle_root/macos/Jarvis.app.tar.gz"
signature_path="$artifact.sig"
dmg="$bundle_root/dmg/Jarvis_${version}_aarch64.dmg"
for path in "$artifact" "$signature_path" "$dmg"; do
  [[ -f "$path" ]] || fail "missing build artifact: $path"
done

release_name="Jarvis_${version}_aarch64.app.tar.gz"
staging=$(mktemp -d "${TMPDIR:-/tmp}/jarvis-update.XXXXXX")
trap 'rm -rf "$staging"' EXIT
cp "$artifact" "$staging/$release_name"
cp "$dmg" "$staging/$(basename "$dmg")"
jq -n \
  --arg version "$version" \
  --arg notes "$notes" \
  --arg pub_date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg url "$base_url/$release_name" \
  --rawfile signature "$signature_path" \
  '{
    version: $version,
    notes: $notes,
    pub_date: $pub_date,
    platforms: {
      "darwin-aarch64": {
        signature: $signature,
        url: $url
      }
    }
  }' >"$staging/latest.json"

remote_staging="$remote_root/.publish-${version}-$$"
ssh "$remote" "mkdir -p '$remote_staging'"
scp "$staging/$release_name" "$staging/$(basename "$dmg")" "$staging/latest.json" \
  "$remote:$remote_staging/"
ssh "$remote" "
  set -eu
  test -s '$remote_staging/$release_name'
  test -s '$remote_staging/$(basename "$dmg")'
  test -s '$remote_staging/latest.json'
  mkdir -p '$remote_root'
  mv '$remote_staging/$release_name' '$remote_root/$release_name'
  mv '$remote_staging/$(basename "$dmg")' '$remote_root/$(basename "$dmg")'
  mv '$remote_staging/latest.json' '$remote_root/latest.json'
  rmdir '$remote_staging'
"

curl -fsS "$base_url/latest.json" |
  jq -e --arg version "$version" '.version == $version' >/dev/null ||
  fail "published manifest is not reachable"
curl -fsSI "$base_url/$release_name" >/dev/null ||
  fail "published update artifact is not reachable"

printf 'publish-update: version=%s\n' "$version"
printf 'publish-update: manifest=%s/latest.json\n' "$base_url"
printf 'publish-update: dmg=%s/Jarvis_%s_aarch64.dmg\n' "$base_url" "$version"
