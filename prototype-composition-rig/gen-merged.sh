#!/bin/bash
# PROTOTYPE throwaway: generate a component-merge set Dockerfile. Each toolchain
# is a previously built component image, merged in canonical order with
# COPY --from=<image> (files-only — the mechanism the research validated).
set -euo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
set_name="$1"; shift
out="$RIG_DIR/sets/merged/Dockerfile.$set_name"
mkdir -p "$(dirname "$out")"
{
  echo "# PROTOTYPE throwaway: component-merge set '$set_name' = $*"
  echo "FROM localhost/rig-base:latest"
  for tc in "$@"; do
    echo "COPY --from=localhost/rig-comp-$tc:latest / /"
  done
} > "$out"
echo "generated $out"
