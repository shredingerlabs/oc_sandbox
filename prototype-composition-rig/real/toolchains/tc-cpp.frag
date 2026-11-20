# PROTOTYPE throwaway fragment: tc-cpp — apt-only toolchain, the #181 catalog's
# `cpp` additions on top of the base (which already carries build-essential).
# Installs weighty, real-world packages so the payload is realistically large.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends clang clangd lldb gdb libgtest-dev catch2 && \
    rm -rf /var/lib/apt/lists/*
