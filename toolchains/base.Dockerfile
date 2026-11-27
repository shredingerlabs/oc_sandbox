# syntax=docker/dockerfile:1
# base.Dockerfile — the fixed base recipe (spec "toolchain sets" §1/§3).
#
# Not a unit: its content hash IS the base identity and feeds every component
# and set tag (lib-identity.sh), so any edit re-addresses the whole family at
# the next build. The same pin lint as the unit recipes applies to the
# RUN/ENV/USER lines (build-images.sh --base runs it): everything is pinned
# inline, no ARG — new versions arrive only via update-flow rewrites.
#
# Base keeps (§3): ca-certificates, curl, wget, git, python3, python3-pip,
# python3-venv, jq, unzip, openssh-client, pulseaudio-utils, gh; pinned
# glab / opencode / codebase-memory-mcp; pip pytest pytest-cov; node runtime
# + npm + mcp-searxng; dev user + safe.directory; the GLAB_CONFIG_DIR /
# GH_CONFIG_DIR ENVs (moved web → base). Base purge: compilers → cpp,
# usbutils/libusb → usb, gnupg → transient recipe deps.

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    GLAB_CONFIG_DIR=/home/dev/.git_local/glab-cli \
    GH_CONFIG_DIR=/home/dev/.git_local/gh-cli

# --- System packages (base keeps) ---------------------------------------------
# pulseaudio-utils: parecord — the dictation mic capture runs INSIDE the
# workspace container against the bind-mounted host PulseAudio socket
# (ADR-0036); without it the TUI plugin's record spawn fails with
# "Executable not found in $PATH".
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl wget git \
        python3 python3-pip python3-venv \
        jq unzip openssh-client \
        gh pulseaudio-utils \
    && rm -rf /var/lib/apt/lists/*

# --- GitLab CLI (glab) — pinned ------------------------------------------------
RUN curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/v1.133.0/downloads/glab_1.133.0_linux_amd64.tar.gz" \
        | tar xz --strip-components=1 -C /usr/local/bin bin/glab

# --- OpenCode — pinned ----------------------------------------------------------
# GitHub release asset with the version embedded (the floating
# opencode.ai/update/api/latest channel died with CACHEBUST).
RUN curl -fsSL https://github.com/anomalyco/opencode/releases/download/v2.4.2/opencode-linux-x64.tar.gz \
        | tar xz -C /usr/local/bin opencode \
    && chmod +x /usr/local/bin/opencode

# --- codebase-memory-mcp (UI variant) — pinned ----------------------------------
RUN curl -fsSL https://github.com/DeusData/codebase-memory-mcp/releases/download/v0.12.1/codebase-memory-mcp-ui-linux-amd64.tar.gz \
        | tar xz -C /usr/local/bin codebase-memory-mcp \
    && chmod +x /usr/local/bin/codebase-memory-mcp

# --- Python packages (base) — pinned --------------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir \
        pytest==9.2.0 pytest-cov==7.1.0

# --- Node runtime for the SearXNG-MCP (ADR 0026) --------------------------------
# The SearXNG-MCP (mcp-searxng) is registered as a local MCP in EVERY
# search_mode=searxng project and needs node in the container, so the node
# runtime + the package live in the base (including web, which reuses node for
# its dev toolchain). Deliberately the only npm packages at base level.
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g npm@12.0.2 mcp-searxng@2.12.0

# --- Dev user + the ~/.profile.d hook (§2) --------------------------------------
# PATH/env additions from toolchain fragments ride ~/.profile.d/<id>.sh hooks;
# the dev skeleton is born with the sourcing hook. Written into /etc/skel
# BEFORE useradd so the hooks land in /home/dev at user creation (no direct
# write to the live ~/.profile — the recipe lint refuses that everywhere).
RUN userdel -r ubuntu \
    && mkdir -p /etc/skel/.profile.d \
    && printf '%s\n' \
        '# toolchain PATH/env hooks (managed — write ~/.profile.d/<id>.sh instead)' \
        'if [ -d "$HOME/.profile.d" ]; then' \
        '    for f in "$HOME"/.profile.d/*.sh; do' \
        '        [ -f "$f" ] && . "$f"' \
        '    done' \
        'fi' >> /etc/skel/.profile \
    && useradd -m -s /bin/bash -u 1000 dev \
    && mkdir -p /home/dev/.ssh /home/dev/workspace /home/dev/.git_local/glab-cli \
              /home/dev/.config/opencode /home/dev/.local/share/opencode /home/dev/.cache/opencode \
    && chown -R dev:dev /home/dev

USER dev
WORKDIR /home/dev/workspace

RUN git config --global --add safe.directory /home/dev/workspace
