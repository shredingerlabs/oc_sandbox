# PROTOTYPE throwaway fragment: tc-curl — curl'd pinned binary (stands in for "playwright")
RUN curl -fsSL -o /usr/local/bin/hadolint \
      https://github.com/hadolint/hadolint/releases/download/v2.12.0/hadolint-Linux-x86_64 && \
    chmod +x /usr/local/bin/hadolint
