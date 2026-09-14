FROM jarvis-okr-chat:0.154.0
RUN sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources
RUN env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy sh -c 'apt-get update && apt-get install -y --no-install-recommends build-essential sqlite3 python3-yaml procps && rm -rf /var/lib/apt/lists/*'
RUN touch /run/emily-development
COPY lark-cli /usr/local/bin/lark-cli
COPY bytedcli /usr/local/bin/bytedcli
COPY entrypoint.sh /usr/local/bin/emily-dev-entrypoint
COPY agent.sh /usr/local/bin/emily-dev-agent
RUN chmod 755 /usr/local/bin/bytedcli /usr/local/bin/lark-cli /usr/local/bin/emily-dev-entrypoint /usr/local/bin/emily-dev-agent
ENV HOME=/dev-state/home CODEX_HOME=/dev-state/home/.codex \
    GOCACHE=/dev-state/go-cache GOMODCACHE=/dev-state/go-mod GOPATH=/dev-state/go \
    PATH=/opt/go/bin:/opt/bytedcli/dist:/opt/jarvis/scripts:/usr/local/bin:/usr/bin:/bin \
    JARVIS_API_BASE=http://127.0.0.1:18812 JARVIS_CONTAINER_DEPLOY=1 \
    GOFLAGS=-buildvcs=false
WORKDIR /opt/jarvis
ENTRYPOINT ["emily-dev-entrypoint"]
