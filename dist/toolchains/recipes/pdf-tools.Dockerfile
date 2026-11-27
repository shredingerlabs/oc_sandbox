# pdf-tools — ghostscript + poppler-utils for PDF processing and conversion
# via apt (spec toolchain-sets §3). apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        ghostscript poppler-utils \
    && rm -rf /var/lib/apt/lists/*
