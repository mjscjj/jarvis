#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION="1.18.2"
LABEL="com.bytedance.jarvis.qdrant"

platform="$(uname -s)"
arch="$(uname -m)"
case "${platform}/${arch}" in
  Darwin/arm64)
    archive_name="qdrant-aarch64-apple-darwin.tar.gz"
    archive_sha256="859f487e316ae1bda3b5d7c1e129a0a7344424d992503c188979ca6ac1b47253"
    service_manager="launchd"
    ;;
  Linux/x86_64|Linux/amd64)
    archive_name="qdrant-x86_64-unknown-linux-gnu.tar.gz"
    archive_sha256="cd619c61d8d32dd176af88cf498714ecb765b7df9021d691862478d6ac35392c"
    service_manager="systemd"
    ;;
  *)
    printf 'install-qdrant: unsupported platform: %s/%s\n' "$platform" "$arch" >&2
    exit 1
    ;;
esac

download_url="https://github.com/qdrant/qdrant/releases/download/v${VERSION}/${archive_name}"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/jarvis-qdrant.XXXXXX")"

cleanup() {
  rm -rf "$temporary_dir"
}
trap cleanup EXIT

curl -fL "$download_url" -o "$temporary_dir/qdrant.tar.gz"
if command -v shasum >/dev/null 2>&1; then
  actual_sha256="$(shasum -a 256 "$temporary_dir/qdrant.tar.gz" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_sha256="$(sha256sum "$temporary_dir/qdrant.tar.gz" | awk '{print $1}')"
else
  printf 'install-qdrant: neither shasum nor sha256sum is available\n' >&2
  exit 1
fi
if [[ "$actual_sha256" != "$archive_sha256" ]]; then
  printf 'qdrant archive sha256 mismatch: got=%s want=%s\n' "$actual_sha256" "$archive_sha256" >&2
  exit 1
fi
tar -xzf "$temporary_dir/qdrant.tar.gz" -C "$temporary_dir"

mkdir -p "$REPO_ROOT/bin" "$REPO_ROOT/var/log" "$REPO_ROOT/var/qdrant/storage" "$REPO_ROOT/var/qdrant/snapshots"
install -m 0755 "$temporary_dir/qdrant" "$REPO_ROOT/bin/qdrant"

case "$service_manager" in
  launchd)
    service_target="gui/${UID}/${LABEL}"
    agent_plist="$("$SCRIPT_DIR/render-launchd-plist.sh" "$LABEL")"
    if launchctl print "$service_target" >/dev/null 2>&1; then
      launchctl bootout "$service_target"
    fi
    launchctl bootstrap "gui/${UID}" "$agent_plist"
    ;;
  systemd)
    unit_path="$("$SCRIPT_DIR/render-systemd-unit.sh" "$LABEL")"
    systemctl --user daemon-reload
    systemctl --user enable --now "${LABEL}.service"
    systemctl --user show --property=FragmentPath --value "${LABEL}.service" | grep -Fx "$unit_path" >/dev/null
    ;;
esac

for _ in {1..30}; do
  if curl -fsS http://127.0.0.1:6333/healthz >/dev/null; then
    if [[ "$service_manager" == "launchd" ]]; then
      launchctl print "gui/${UID}/${LABEL}"
    else
      systemctl --user --no-pager status "${LABEL}.service"
    fi
    exit 0
  fi
  sleep 1
done

printf 'qdrant did not become healthy within 30 seconds\n' >&2
exit 1
