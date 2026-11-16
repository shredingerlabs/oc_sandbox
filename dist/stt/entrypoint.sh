#!/bin/sh
# STT member entrypoint (ADR 0036, D2/D3): one process — the
# whisper.cpp server itself.
# --host 0.0.0.0 is REQUIRED: the server default is loopback, which the
# pod publish could not reach.
# --language is ALWAYS passed: the server default is `en`, so omitting
# the flag would silently pin English; `STT_LANG:-auto` keeps
# auto-detect (start.sh passes -e STT_LANG=<stt.json value>).
set -eu
exec whisper-server \
  -m /models/ggml-small.bin \
  --host 0.0.0.0 \
  --port 8090 \
  --public /srv/stt/public \
  --inference-path /inference \
  --language "${STT_LANG:-auto}"
