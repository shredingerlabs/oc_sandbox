#!/bin/bash
# PROTOTYPE throwaway: generate one delta set Dockerfile (wayfinder #186).
# Usage: scripts/gen-set.sh <set-name> <tc...>   (members in canonical order)
# BASE_IMG env overrides FROM, same as gen-component.sh.
# Assembly = COPY each member's dump output (payload bytes only) + ONE RUN that
# extracts in canonical order, merges PM state, and removes staging. Fragment
# ENV lines are re-emitted (archives carry files only).
set -euo pipefail
RIG_DIR="$(cd "$(dirname "$0")/.." && pwd)"
set_name="$1"; shift
BASE_IMG="${BASE_IMG:-localhost/rig-base-real:latest}"
out="$RIG_DIR/build/sets/Dockerfile.$set_name"
mkdir -p "$(dirname "$out")"
{
  echo "# PROTOTYPE throwaway: delta set '$set_name' = $* (#186) — never merged"
  echo "FROM $BASE_IMG"
  echo "USER root"
  for tc in "$@"; do
    echo "COPY --from=localhost/rig-comp-$tc:latest /rig-out/ /rig/$tc/"
  done
  echo "COPY scripts/assemble.sh /rig/assemble.sh"
  echo "# re-emitted fragment ENVs"
  for tc in "$@"; do
    grep -h '^ENV' "$RIG_DIR/toolchains/$tc.frag" || true
  done
  echo "RUN /rig/assemble.sh $*"
} > "$out"
echo "generated $out"
