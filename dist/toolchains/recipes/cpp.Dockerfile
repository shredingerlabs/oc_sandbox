# cpp — C/C++ toolchain (spec toolchain-sets §3): build-essential, cmake,
# ninja-build, pkg-config, python3-dev, the autotools, clang/LLVM with
# clangd/lldb, gdb, and the gtest/catch2 test frameworks.
# The base no longer carries compilers — sets that build native code
# (runtime pip/npm installs needing a compiler included) add this unit.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential cmake ninja-build pkg-config python3-dev \
        autoconf automake libtool \
        clang clangd lldb gdb \
        libgtest-dev catch2 \
    && rm -rf /var/lib/apt/lists/*
