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
    PLAYWRIGHT_BROWSERS_PATH=/usr/local/share/playwright-browsers \
    PLAYWRIGHT_DOWNLOAD_CONNECTION_TIMEOUT=0

# --- Node / TypeScript / Jest / Playwright -----------------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g npm@12.0.2 \
    && npm config set allow-scripts=unrs-resolver --location=user \
    && npm install -g typescript jest playwright pnpm corepack better-sqlite3 \
    && npm update -g \
    && npx playwright install-deps \
    && npx playwright install chromium firefox \
    || (sleep 10 && npx playwright install-deps && npx playwright install --force chromium firefox)

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
# EDITION: swdev (C/C++, Java, Rust, Go)
# =============================================================================
FROM opencode-sandbox-base AS opencode-sandbox-swdev

USER root

# --- System packages (C/C++ + Java + Go) -------------------------------------
# Base bringt bereits build-essential (gcc/g++/make), cmake, pkg-config, pytest.
RUN apt-get update && apt-get install -y --no-install-recommends \
        autoconf automake libtool \
        clang clangd lldb gdb \
        libgtest-dev catch2 \
        openjdk-21-jdk maven \
        golang-go \
    && rm -rf /var/lib/apt/lists/*

# --- Rust via rustup (als dev, damit cargo in /home/dev/.cargo landet) -------
USER dev
RUN curl -fsSL https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --profile minimal

ENTRYPOINT ["/bin/bash", "-l"]

# =============================================================================
# EDITION: matlab (GNU Octave + Python Scientific-Stack, lizenzfrei)
# =============================================================================
FROM opencode-sandbox-base AS opencode-sandbox-matlab

USER root

# --- GNU Octave --------------------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        octave \
    && rm -rf /var/lib/apt/lists/*

# --- Python packages (Scientific-Stack) --------------------------------------
RUN pip3 install --break-system-packages --no-cache-dir \
        numpy scipy matplotlib pandas openpyxl pillow

USER dev

ENTRYPOINT ["/bin/bash", "-l"]

# =============================================================================
# EDITION: ros2 (ROS 2 Jazzy)
# =============================================================================
FROM opencode-sandbox-base AS opencode-sandbox-ros2

USER root

# --- ROS 2 apt repository (packages.ros.org) ---------------------------------
RUN curl -fsSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key \
        -o /usr/share/keyrings/ros-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] http://packages.ros.org/ros2/ubuntu noble main" \
        > /etc/apt/sources.list.d/ros2.list

# --- ROS 2 packages ----------------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        ros-jazzy-ros-base \
        ros-jazzy-rviz2 \
        ros-jazzy-rmw-cyclonedds-cpp \
        python3-rosdep python3-colcon-common-extensions python3-vcstool \
    && rm -rf /var/lib/apt/lists/*

# rosdep init als Root; rosdep update läuft als dev
RUN rosdep init

USER dev

# ROS-Umgebung in Login-/Interaktiven-Shells; rosdep-Cache für dev
RUN echo 'source /opt/ros/jazzy/setup.bash' >> ~/.bashrc \
    && rosdep update

ENTRYPOINT ["/bin/bash", "-l"]

# =============================================================================
# EDITION: writing (LibreOffice + pandoc, lizenzfrei)
# =============================================================================
FROM opencode-sandbox-base AS opencode-sandbox-writing

USER root

# --- System packages (Office + Dokumentkonvertierung + Fonts) ----------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        libreoffice \
        pandoc \
        fonts-liberation fonts-noto-core \
    && rm -rf /var/lib/apt/lists/*

USER dev

ENTRYPOINT ["/bin/bash", "-l"]
