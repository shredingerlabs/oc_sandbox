# pandoc — the universal document converter via apt (spec toolchain-sets §3);
# shared by the writing and latex bundles. apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends pandoc \
    && rm -rf /var/lib/apt/lists/*
