# arm-embedded — bare-metal ARM cross-toolchain + multiarch debugger via apt
# (spec toolchain-sets §3). HIL hardware unit. apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc-arm-none-eabi gdb-multiarch \
    && rm -rf /var/lib/apt/lists/*
