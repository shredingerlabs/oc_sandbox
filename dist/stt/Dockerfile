# oc-stt image: local speech-to-text (dictation) infra image (ADR 0036)
# — the whisper.cpp server with the small multilingual ggml model baked
# in (spec story 29: zero runtime downloads). Infra image outside the
# edition stage registry, like oc-proxy/oc-searxng: own Dockerfile, own
# context, fixed tag oc-stt.
#
# Request shape (VERIFIED against the pinned whisper.cpp v1.9.5 server
# README): POST /inference multipart with `file=@audio.wav` and
# `response_format=json` -> {"text": "..."}. The server's `--public`
# static mount serves /srv/stt/public/index.html at `/` (dictation
# sidecar page; ticket #170 ships the real assets).
#
# In-pod port constant: 8090 (ADR 0036, D2) — whisper.cpp's 8080
# default collides with searxng's in-pod 127.0.0.1:8080, and both pod
# members may coexist in one pod.

ARG WHISPER_VERSION=v1.9.5

# --- Build stage: compile the whisper.cpp server ------------------------------
FROM alpine:3.21 AS build

ARG WHISPER_VERSION

RUN apk add --no-cache build-base cmake git ca-certificates curl

# WHISPER_BUILD_SERVER=ON is the pinned target; tests are trimmed
# (WHISPER_BUILD_TESTS=OFF). Examples stay ON: the server lives inside
# the examples subtree, so WHISPER_BUILD_EXAMPLES=OFF would not build it
# at all (VERIFIED against v1.9.5 CMakeLists) — the extra example
# binaries stay in this stage and are discarded. Static libwhisper/ggml
# (BUILD_SHARED_LIBS=OFF) keeps the runtime stage a single binary.
RUN git clone --depth 1 --branch "${WHISPER_VERSION}" \
      https://github.com/ggml-org/whisper.cpp /src/whisper.cpp \
 && cmake -S /src/whisper.cpp -B /src/whisper.cpp/build \
      -DWHISPER_BUILD_SERVER=ON \
      -DWHISPER_BUILD_TESTS=OFF \
      -DBUILD_SHARED_LIBS=OFF \
 && cmake --build /src/whisper.cpp/build --target whisper-server -j"$(nproc)"

# Bake the small multilingual model: BUILD-time download only, sha256
# verified against the LFS oid published by the HF API
# (https://huggingface.co/api/models/ggerganov/whisper.cpp/tree/main) —
# a wrong/missing download fails the build loudly here, never at
# runtime (proxy-Dockerfile loud-validation style).
RUN mkdir -p /models \
 && curl -fL -o /models/ggml-small.bin \
      https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin \
 && echo "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b  /models/ggml-small.bin" \
      | sha256sum -c -

# --- Runtime stage: server binary + baked model, nothing else -----------------
FROM alpine:3.21

# libgomp: whisper.cpp compiles with OpenMP enabled (GGML_OPENMP defaults
# ON; the build stage's build-base gcc brings libgomp along) — the runtime
# stage must carry it too, or whisper-server dies on start with
# "Error loading shared library libgomp.so.1" (found in the first live
# member start; verification row 1 covered the build, not the run).
RUN apk add --no-cache libstdc++ libgcc libgomp

COPY --from=build /src/whisper.cpp/build/bin/whisper-server /usr/local/bin/whisper-server
COPY --from=build /models/ggml-small.bin /models/ggml-small.bin
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY public/ /srv/stt/public/

# In-pod constant: 8090 — NOT searxng's in-pod 8080, both members may
# coexist in one pod (ADR 0036, D2).
EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
