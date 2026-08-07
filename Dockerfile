# syntax=docker/dockerfile:1

ARG UBUNTU_VERSION=24.04
FROM ubuntu:${UBUNTU_VERSION} AS opencode-sandbox-base

ARG DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

ENV DEBIAN_FRONTEND=${DEBIAN_FRONTEND} \
    LANG=${LANG} \
    LC_ALL=${LC_ALL}

# --- System packages (common core) -------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl wget git build-essential pkg-config gnupg \
        python3 python3-pip python3-venv python3-dev \
        cmake ninja-build \
        jq unzip openssh-client usbutils \
        libusb-1.0-0 \
        gh \
    && rm -rf /var/lib/apt/lists/*

# --- GitLab CLI (glab) -------------------------------------------------------
RUN GLAB_VER=$(curl -s https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases?per_page=1 \
        | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['tag_name'][1:])") \
    && curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VER}/downloads/glab_${GLAB_VER}_linux_amd64.tar.gz" \
        | tar xz --strip-components=1 -C /usr/local/bin bin/glab

# --- OpenCode ----------------------------------------------------------------
RUN curl -fsSL https://github.com/anomalyco/opencode/releases/latest/download/opencode-linux-x64.tar.gz \
        | tar xz -C /usr/local/bin opencode \
    && chmod +x /usr/local/bin/opencode

# --- codebase-memory-mcp (UI variant) ----------------------------------------
RUN curl -fsSL https://github.com/DeusData/codebase-memory-mcp/releases/latest/download/codebase-memory-mcp-ui-linux-amd64.tar.gz \
        | tar xz -C /usr/local/bin codebase-memory-mcp \
    && chmod +x /usr/local/bin/codebase-memory-mcp

# --- Python packages (base) --------------------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir \
        pytest pytest-cov

# --- Non-root user -----------------------------------------------------------
RUN userdel -r ubuntu \
    && useradd -m -s /bin/bash -u 1000 dev \
    && mkdir -p /home/dev/.ssh /home/dev/project /home/dev/.git_local/glab-cli \
              /home/dev/.config/opencode /home/dev/.local/share/opencode /home/dev/.cache/opencode \
    && chown -R dev:dev /home/dev

USER dev
WORKDIR /home/dev/project

RUN git config --global --add safe.directory /home/dev/project

# =============================================================================
# EDITION: web
# =============================================================================
FROM opencode-sandbox-base AS opencode-sandbox-web

USER root

ENV GLAB_CONFIG_DIR=/home/dev/.git_local/glab-cli \
    GH_CONFIG_DIR=/home/dev/.git_local/gh-cli \
    PLAYWRIGHT_BROWSERS_PATH=/usr/local/share/playwright-browsers

# --- Node / TypeScript / Jest / Playwright -----------------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g npm@12.0.2 \
    && npm config set allow-scripts=unrs-resolver --location=user \
    && npm install -g typescript jest playwright pnpm corepack \
    && npm update -g \
    && npx playwright install-deps \
    && npx playwright install chromium firefox

USER dev

ENTRYPOINT ["/bin/bash", "-l"]

# =============================================================================
# EDITION: embedded
# =============================================================================
FROM opencode-sandbox-base AS opencode-sandbox-embedded

# --- System packages (embedded toolchains) -----------------------------------
USER root

RUN apt-get update && apt-get install -y --no-install-recommends \
        golang-go \
        gcc-arm-none-eabi gdb-multiarch \
        simavr \
    && rm -rf /var/lib/apt/lists/*

# --- PicoTech vendor SDK for Picoscope (PS2000 + PS2000A APIs) ---------------
RUN curl -fsSL https://labs.picotech.com/debian/dists/picoscope/Release.gpg.key \
        | gpg --dearmor -o /usr/share/keyrings/picotech.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/picotech.gpg] https://labs.picotech.com/debian/ picoscope main" \
        > /etc/apt/sources.list.d/picoscope.list \
    && apt-get update \
    && apt-get download libps2000 libps2000a libpicoipp \
    && mkdir -p /etc/udev/rules.d \
    && ln -sf /bin/true /usr/local/bin/udevadm \
    && dpkg -i --force-depends libps2000*.deb libps2000a*.deb libpicoipp*.deb \
    && rm -f /usr/local/bin/udevadm \
    && rm -rf /var/lib/apt/lists/* libps2000*.deb libps2000a*.deb libpicoipp*.deb

# --- Arduino CLI binary ------------------------------------------------------
RUN curl -fsSL https://downloads.arduino.cc/arduino-cli/arduino-cli_latest_Linux_64bit.tar.gz \
        | tar xz -C /usr/local/bin arduino-cli

# --- Python packages (embedded) ----------------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir \
        mpremote esptool platformio \
        pyvisa pyvisa-py pyusb \
        picoscope picosdk

USER dev

# Run core install as dev so data lands in /home/dev/.arduino15/
RUN arduino-cli core update-index \
    && arduino-cli core install arduino:avr \
    && arduino-cli core install arduino:esp32

ENTRYPOINT ["/bin/bash", "-l"]

# =============================================================================
# EDITION: full (web + embedded)
# =============================================================================
FROM opencode-sandbox-web AS opencode-sandbox-full

USER root

# --- System packages (embedded toolchains) -----------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        golang-go \
        gcc-arm-none-eabi gdb-multiarch \
        simavr \
    && rm -rf /var/lib/apt/lists/*

# --- PicoTech vendor SDK for Picoscope (PS2000 + PS2000A APIs) ---------------
RUN curl -fsSL https://labs.picotech.com/debian/dists/picoscope/Release.gpg.key \
        | gpg --dearmor -o /usr/share/keyrings/picotech.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/picotech.gpg] https://labs.picotech.com/debian/ picoscope main" \
        > /etc/apt/sources.list.d/picoscope.list \
    && apt-get update \
    && apt-get download libps2000 libps2000a libpicoipp \
    && mkdir -p /etc/udev/rules.d \
    && ln -sf /bin/true /usr/local/bin/udevadm \
    && dpkg -i --force-depends libps2000*.deb libps2000a*.deb libpicoipp*.deb \
    && rm -f /usr/local/bin/udevadm \
    && rm -rf /var/lib/apt/lists/* libps2000*.deb libps2000a*.deb libpicoipp*.deb

# --- Arduino CLI binary ------------------------------------------------------
RUN curl -fsSL https://downloads.arduino.cc/arduino-cli/arduino-cli_latest_Linux_64bit.tar.gz \
        | tar xz -C /usr/local/bin arduino-cli

# --- Python packages (embedded) ----------------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir \
        mpremote esptool platformio \
        pyvisa pyvisa-py pyusb \
        picoscope picosdk

USER dev

# Run core install as dev so data lands in /home/dev/.arduino15/
RUN arduino-cli core update-index \
    && arduino-cli core install arduino:avr \
    && arduino-cli core install arduino:esp32

ENTRYPOINT ["/bin/bash", "-l"]
