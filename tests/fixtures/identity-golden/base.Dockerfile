FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git jq python3
RUN useradd -m -s /bin/bash dev
USER dev
WORKDIR /home/dev/workspace
