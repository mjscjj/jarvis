FROM node:22-bookworm-slim
ARG CODEX_VERSION=0.154.0
RUN apt-get update && apt-get install -y --no-install-recommends bash socat curl jq git python3-yaml ripgrep ca-certificates build-essential sqlite3 procps supervisor && rm -rf /var/lib/apt/lists/*
RUN npm install --global @openai/codex@${CODEX_VERSION}
# The mounted CLI programs contain no credentials; HOME belongs to this instance.
RUN touch /run/emily-development /usr/local/bin/lark-cli && ln -s /opt/bytedcli/dist/bytedcli.js /usr/local/bin/bytedcli
COPY entrypoint.sh /usr/local/bin/emily-dev-entrypoint
COPY supervisord.conf /etc/supervisor/supervisord.conf
RUN chmod 755 /usr/local/bin/emily-dev-entrypoint
ENV HOME=/dev-state/home CODEX_HOME=/dev-state/home/.codex \
    GOCACHE=/dev-state/go-cache GOMODCACHE=/dev-state/go-mod GOPATH=/dev-state/go \
    PATH=/dev-state/home/.local/bin:/opt/go/bin:/opt/jarvis/scripts:/usr/local/bin:/usr/bin:/bin \
    JARVIS_API_BASE=http://127.0.0.1:18812 JARVIS_CONTAINER_DEPLOY=1 \
    JARVIS_CONFIG_PATH=/opt/jarvis/conf/config.yaml
WORKDIR /opt/jarvis
ENTRYPOINT ["emily-dev-entrypoint"]
