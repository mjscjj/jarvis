#!/usr/bin/env bash
set -euo pipefail
umask 077
mkdir -p "$CODEX_HOME" /opt/jarvis/var/log /run/emily-web
cp /credentials/auth.json "$CODEX_HOME/auth.json"
socat TCP-LISTEN:18080,bind=127.0.0.1,reuseaddr,fork UNIX-CONNECT:/run/emily-egress/access.sock &
rm -f /run/emily-web/web.sock
socat UNIX-LISTEN:/run/emily-web/web.sock,mode=600,fork TCP:127.0.0.1:18812 &
if [[ -x bin/jarvis-server ]]; then
  ./bin/jarvis-server -config "${JARVIS_CONFIG_PATH:-conf/config.yaml}" >>var/log/jarvis-server.log 2>>var/log/jarvis-server.error.log &
  echo "$!" > var/server.pid
fi
wait
