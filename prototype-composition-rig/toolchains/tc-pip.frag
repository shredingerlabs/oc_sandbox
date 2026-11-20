# PROTOTYPE throwaway fragment: tc-pip — apt+pip mix toolchain (stands in for "go")
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends python3 python3-pip && \
    rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/root/.cache/pip,sharing=locked \
    pip3 install --break-system-packages requests==2.31.0
