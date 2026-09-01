#!/usr/bin/env bash
# Fail before `go build` when the C toolchain SQLite needs is missing.
#
# The persistence layer uses gorm.io/driver/sqlite, which wraps
# mattn/go-sqlite3 and therefore requires CGO. Without this check the failure
# mode is actively misleading: `go build` succeeds and links a stub, and the
# binary only dies at startup with "Binary was compiled with 'CGO_ENABLED=0',
# go-sqlite3 requires cgo to work" — an error that says nothing about the
# missing compiler and shows up long after the step that caused it.
set -euo pipefail

if [[ "$(go env CGO_ENABLED)" != 1 ]]; then
  printf '%s\n' "CGO_ENABLED=$(go env CGO_ENABLED), but the SQLite driver requires CGO." >&2
  printf '%s\n' "Unset CGO_ENABLED (or set it to 1) and rebuild; a CGO_ENABLED=0 binary" >&2
  printf '%s\n' "builds fine and then fails at startup when it opens the database." >&2
  exit 1
fi

# On macOS a bare `clang` on PATH is a shim that only reports the missing
# Command Line Tools once invoked, so ask xcode-select whether they are there.
if [[ "$(uname -s)" == Darwin ]] && ! xcode-select -p >/dev/null 2>&1; then
  printf '%s\n' "Xcode Command Line Tools are not installed, so cgo has no C compiler." >&2
  printf '%s\n' "Install them with: xcode-select --install" >&2
  exit 1
fi

compiler=$(go env CC)
if [[ -z $compiler ]] || ! command -v "$compiler" >/dev/null 2>&1; then
  printf '%s\n' "C compiler ${compiler:-<unset>} from \`go env CC\` is not executable." >&2
  printf '%s\n' "Install a C toolchain (macOS: xcode-select --install) or set CC to one." >&2
  exit 1
fi
