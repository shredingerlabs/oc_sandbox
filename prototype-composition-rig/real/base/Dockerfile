# PROTOTYPE throwaway real-sized base (#186) — mirror of dist/Dockerfile's
# opencode-sandbox-base stage: same apt core, node 22, glab, opencode, the
# base pip set. Dev-user/entrypoint stage omitted (rig runs as root); CBM-mcp
# dropped (flake risk, ~30 MB). No CACHEBUST — the rig has no cache-bust story.
FROM docker.io/library/ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8 LC_ALL=C.UTF-8

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl wget git build-essential pkg-config gnupg \
        python3 python3-pip python3-venv python3-dev \
        cmake ninja-build \
        jq unzip openssh-client \
        gh \
    && rm -rf /var/lib/apt/lists/*

# --- glab (floating latest, like production) ---------------------------------
RUN GLAB_VER=$(curl -s https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases?per_page=1 \
        | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['tag_name'][1:])") \
    && curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VER}/downloads/glab_${GLAB_VER}_linux_amd64.tar.gz" \
        | tar xz --strip-components=1 -C /usr/local/bin bin/glab

# --- opencode (floating latest channel, like production) ---------------------
RUN curl -fsSL https://opencode.ai/update/api/latest/cli/opencode/opencode-linux-x64.tar.gz \
        | tar xz -C /usr/local/bin opencode \
    && chmod +x /usr/local/bin/opencode

# --- node 22 + the base's one npm package ------------------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g npm@12.0.2 \
    && npm install -g mcp-searxng

# --- Python packages (base) --------------------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir pytest pytest-cov
