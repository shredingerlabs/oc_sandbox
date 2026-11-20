#!/bin/bash
# PROTOTYPE throwaway: generate a per-set prefix-chain Dockerfile from an ordered
# toolchain list. One layer per toolchain (Dockerfile fragments concatenated
# under a single FROM base — the settled recipe format).
set -euo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
set_name="$1"; shift
out="$RIG_DIR/sets/prefix/Dockerfile.$set_name"
mkdir -p "$(dirname "$out")"
{
  echo "# PROTOTYPE throwaway: prefix-chain set '$set_name' = $*"
  echo "FROM localhost/rig-base:latest"
  for tc in "$@"; do
    echo
    cat "$RIG_DIR/toolchains/$tc.frag"
  done
} > "$out"
echo "generated $out"
