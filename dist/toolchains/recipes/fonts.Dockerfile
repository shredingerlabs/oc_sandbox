# fonts — Liberation + Noto core fonts via apt (spec toolchain-sets §3);
# shared by the writing and latex bundles. apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        fonts-liberation fonts-noto-core \
    && rm -rf /var/lib/apt/lists/*
