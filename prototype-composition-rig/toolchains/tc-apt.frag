# PROTOTYPE throwaway fragment: tc-apt — apt-only toolchain (stands in for "cpp")
# cache-mount the apt archives so a rebuild of this toolchain elsewhere skips downloads
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends gcc make file && \
    rm -rf /var/lib/apt/lists/*
