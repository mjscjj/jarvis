#!/usr/bin/env bash
set -euo pipefail
umask 077
mkdir -p "$CODEX_HOME" /opt/jarvis/var/log /run/emily-web
rm -f /run/emily-web/web.sock
rm -f /opt/jarvis/var/server.pid
exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
