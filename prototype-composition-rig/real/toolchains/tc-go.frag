# PROTOTYPE throwaway fragment: tc-go — apt-only toolchain, the #181 catalog's
# `go`.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends golang-go && \
    rm -rf /var/lib/apt/lists/*
