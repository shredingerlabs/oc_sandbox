# libreoffice — the full LibreOffice suite via apt (spec toolchain-sets §3).
# apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends libreoffice \
    && rm -rf /var/lib/apt/lists/*
