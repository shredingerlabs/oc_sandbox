# PROTOTYPE throwaway base: small apt set + one pinned curl'd binary
FROM docker.io/library/ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

RUN curl -fsSL -o /usr/local/bin/yq \
      https://github.com/mikefarah/yq/releases/download/v4.44.3/yq_linux_amd64 && \
    chmod +x /usr/local/bin/yq
