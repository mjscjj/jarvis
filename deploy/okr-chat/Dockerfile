FROM node:22-bookworm-slim
ARG CODEX_VERSION=0.154.0
RUN apt-get update && apt-get install -y --no-install-recommends bash socat curl jq git python3 ripgrep ca-certificates && rm -rf /var/lib/apt/lists/*
RUN npm install --global @openai/codex@${CODEX_VERSION}
COPY entrypoint.sh /usr/local/bin/okr-chat-entrypoint
RUN chmod 755 /usr/local/bin/okr-chat-entrypoint
ENV HOME=/native CODEX_HOME=/native/codex \
    JARVIS_API_BASE=http://127.0.0.1:18080 \
    HTTPS_PROXY=http://127.0.0.1:18080 HTTP_PROXY=http://127.0.0.1:18080 \
    https_proxy=http://127.0.0.1:18080 http_proxy=http://127.0.0.1:18080 \
    NO_PROXY=127.0.0.1,localhost no_proxy=127.0.0.1,localhost \
    PATH=/opt/jarvis/scripts:$PATH
ENTRYPOINT ["okr-chat-entrypoint"]
