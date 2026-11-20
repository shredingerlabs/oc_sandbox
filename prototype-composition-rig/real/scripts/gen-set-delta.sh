#!/bin/bash
# PROTOTYPE throwaway: generate an incremental delta set Dockerfile (#186).
# Usage: scripts/gen-set-delta.sh <set-name> <parent-set-tag> <tc...>
#   <parent-set-tag> e.g. localhost/rig-set1 — the already-built parent set
#   <tc...> the members being ADDED, canonical order among themselves
# The new set builds FROM the parent set image: only the new members' payload
# archives are COPY'd and extracted. Add-one costs the new payload's extract,
# not a full re-extract of every member.
set -euo pipefail
RIG_DIR="$(cd "$(dirname "$0")/.." && pwd)"
set_name="$1"; shift
parent_tag="$1"; shift
out="$RIG_DIR/build/sets/Dockerfile.$set_name"
mkdir -p "$(dirname "$out")"
{
  echo "# PROTOTYPE throwaway: incremental delta set '$set_name' from $parent_tag, adds $* (#186) — never merged"
  echo "FROM $parent_tag"
  echo "USER root"
  for tc in "$@"; do
    echo "COPY --from=localhost/rig-comp-$tc:latest /rig-out/ /rig/$tc/"
  done
  echo "COPY scripts/assemble-delta.sh /rig/assemble-delta.sh"
  echo "# re-emitted fragment ENVs (new members only)"
  for tc in "$@"; do
    grep -h '^ENV' "$RIG_DIR/toolchains/$tc.frag" || true
  done
  echo "RUN /rig/assemble-delta.sh $*"
} > "$out"
echo "generated $out"
