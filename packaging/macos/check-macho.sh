#!/bin/zsh
set -euo pipefail

script_dir=${0:A:h}
source "$script_dir/runtime-manifest.sh"

binary=${1:?usage: check-macho.sh /path/to/binary}
label=${2:-${binary:t}}

fail() {
  printf 'check-macho: %s: %s\n' "$label" "$*" >&2
  exit 1
}

version_lte() {
  awk -v left="$1" -v right="$2" '
    BEGIN {
      left_count = split(left, left_parts, ".")
      right_count = split(right, right_parts, ".")
      count = left_count > right_count ? left_count : right_count
      for (part = 1; part <= count; part++) {
        left_value = left_parts[part] + 0
        right_value = right_parts[part] + 0
        if (left_value < right_value) exit 0
        if (left_value > right_value) exit 1
      }
      exit 0
    }
  '
}

[[ -f "$binary" && -x "$binary" ]] || fail "not an executable regular file: $binary"

archs=$(lipo -archs "$binary" 2>/dev/null) || fail "not a Mach-O executable"
[[ "$archs" == "arm64" ]] || fail "expected thin arm64 Mach-O, got: $archs"

minos=$(vtool -show-build "$binary" 2>/dev/null | awk '$1 == "minos" { print $2; exit }')
[[ -n "$minos" ]] || fail "missing LC_BUILD_VERSION minos"
version_lte "$minos" "$JARVIS_MACOS_MIN_VERSION" ||
  fail "minimum macOS $minos exceeds supported baseline $JARVIS_MACOS_MIN_VERSION"

while IFS= read -r dependency; do
  case "$dependency" in
    /usr/lib/*|/System/Library/*) ;;
    *) fail "non-system dynamic dependency: $dependency" ;;
  esac
done < <(otool -L "$binary" | awk 'NR > 1 { print $1 }')

printf 'check-macho: %s: arm64 minos=%s dependencies=system-only\n' "$label" "$minos"
