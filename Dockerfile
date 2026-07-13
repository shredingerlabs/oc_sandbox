FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

# --- System packages ---------------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl wget git build-essential pkg-config gnupg \
        python3 python3-pip python3-venv python3-dev \
        golang-go \
        nodejs npm \
        gcc-arm-none-eabi gdb-multiarch \
        cmake ninja-build \
        jq unzip openssh-client usbutils \
        libusb-1.0-0 \
        simavr \
        gh \
    && rm -rf /var/lib/apt/lists/*

# --- GitLab CLI (glab) ---------------------------------------------------------
RUN GLAB_VER=$(curl -s https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases?per_page=1 \
        | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['tag_name'][1:])") \
    && curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VER}/downloads/glab_${GLAB_VER}_linux_amd64.tar.gz" \
        | tar xz --strip-components=1 -C /usr/local/bin bin/glab

# --- PicoTech vendor SDK for Picoscope 2204A (PS2000A API) ---------------------
RUN curl -fsSL https://labs.picotech.com/debian/dists/picoscope/Release.gpg.key \
        | gpg --dearmor -o /usr/share/keyrings/picotech.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/picotech.gpg] https://labs.picotech.com/debian/ picoscope main" \
        > /etc/apt/sources.list.d/picoscope.list \
    && apt-get update \
    && apt-get download libps2000a libpicoipp \
    && mkdir -p /etc/udev/rules.d \
    && ln -sf /bin/true /usr/local/bin/udevadm \
    && dpkg -i --force-depends libps2000a*.deb libpicoipp*.deb \
    && rm -f /usr/local/bin/udevadm \
    && rm -rf /var/lib/apt/lists/* libps2000a*.deb libpicoipp*.deb
# postinst von libps2000a versucht udevadm control --reload, was im Container
# fehlschlägt. Die shared libraries (.so) sind trotzdem korrekt installiert.

# --- Arduino CLI binary (extract as root, install cores as dev) ---------------
RUN curl -fsSL https://downloads.arduino.cc/arduino-cli/arduino-cli_latest_Linux_64bit.tar.gz \
        | tar xz -C /usr/local/bin arduino-cli

# --- OpenCode ------------------------------------------------------------------
RUN curl -fsSL https://github.com/anomalyco/opencode/releases/latest/download/opencode-linux-x64.tar.gz \
        | tar xz -C /usr/local/bin opencode \
    && chmod +x /usr/local/bin/opencode

# --- Python packages -----------------------------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir \
        mpremote esptool platformio \
        pyvisa pyvisa-py pyusb \
        picoscope \
        pytest pytest-cov

# --- Node / TypeScript / Jest / Playwright -------------------------------------
ENV GLAB_CONFIG_DIR=/home/dev/.git_local/glab-cli \
    PLAYWRIGHT_BROWSERS_PATH=/usr/local/share/playwright-browsers
RUN npm install -g typescript jest playwright \
    && npx playwright install chromium firefox \
    && npx playwright install-deps

# --- Non-root user -------------------------------------------------------------
RUN userdel -r ubuntu \
    && useradd -m -s /bin/bash -u 1000 dev \
    && usermod -aG dialout dev \
    && mkdir -p /home/dev/.ssh /home/dev/project /home/dev/.git_local/glab-cli \
             /home/dev/.config/opencode /home/dev/.local/share/opencode \
    && chown -R dev:dev /home/dev

USER dev
WORKDIR /home/dev/project

RUN git config --global --add safe.directory /home/dev/project

# Run core install as dev so data lands in /home/dev/.arduino15/
RUN arduino-cli core update-index \
    && arduino-cli core install arduino:avr \
    && arduino-cli core install arduino:esp32

ENTRYPOINT ["/bin/bash", "-l"]
