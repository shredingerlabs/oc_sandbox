# go — the Go toolchain via apt (spec toolchain-sets §3); shared by the
# swdev and embedded bundles. apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends golang-go \
    && rm -rf /var/lib/apt/lists/*
