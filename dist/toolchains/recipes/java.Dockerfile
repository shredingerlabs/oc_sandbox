# java — OpenJDK 21 + Maven via apt (spec toolchain-sets §3).
# apt is exempt from pin enforcement — its pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        openjdk-21-jdk maven \
    && rm -rf /var/lib/apt/lists/*
