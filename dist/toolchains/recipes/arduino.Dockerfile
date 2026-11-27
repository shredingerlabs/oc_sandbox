# arduino — arduino-cli with the avr + esp32 cores and the simavr simulator
# (spec toolchain-sets §3). HIL hardware unit. The CLI binary is a
# version-pinned direct download (no dpkg/pip record — author-declared
# extra); the cores install as dev into /home/dev/.arduino15 (auto-captured
# by the payload dump).
# extra: /usr/local/bin/arduino-cli
USER root
RUN curl -fsSL https://downloads.arduino.cc/arduino-cli/arduino-cli_0.35.3_Linux_64bit.tar.gz \
        | tar xz -C /usr/local/bin arduino-cli \
    && apt-get update && apt-get install -y --no-install-recommends simavr \
    && rm -rf /var/lib/apt/lists/*
USER dev
RUN arduino-cli core update-index \
    && arduino-cli core install arduino:avr \
    && arduino-cli core install arduino:esp32
