#!/usr/bin/env bash
# Compatibility entrypoint; jarvis-deploy owns cross-platform deployment.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "${SCRIPT_DIR}/jarvis-deploy" --skip-pull "$@"
