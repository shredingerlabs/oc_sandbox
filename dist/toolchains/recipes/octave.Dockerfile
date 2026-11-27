# octave — GNU Octave via apt (spec toolchain-sets §3); the matlab bundle's
# numeric core alongside the python-science unit. apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends octave \
    && rm -rf /var/lib/apt/lists/*
