#!/bin/bash
# PROTOTYPE throwaway: generate one delta component Dockerfile (wayfinder #186).
# Usage: scripts/gen-component.sh <tc>
# BASE_IMG env overrides FROM (default localhost/rig-base-real:latest; set
# localhost/opencode-sandbox-base:latest to reuse the runner's real base image).
# The component wraps the recipe fragment and injects:
#   1. a pre-fragment snapshot of what the base already had,
#   2. the dump step (scripts/dump.sh) after the fragment.
set -euo pipefail
RIG_DIR="$(cd "$(dirname "$0")/.." && pwd)"
tc="$1"
frag="$RIG_DIR/toolchains/$tc.frag"
BASE_IMG="${BASE_IMG:-localhost/rig-base-real:latest}"
out="$RIG_DIR/build/toolchains/Dockerfile.$tc"
mkdir -p "$(dirname "$out")"
{
  echo "# PROTOTYPE throwaway: delta component $tc (#186) — never merged"
  echo "FROM $BASE_IMG"
  echo "USER root"
  echo
  echo "# injected pre-fragment snapshot (what the base already had)"
  echo "RUN mkdir -p /rig-snap \\"
  echo "    && cp /var/lib/dpkg/status /rig-snap/dpkg-status \\"
  echo "    && find /usr/lib/python3* /usr/local/lib/python3* -name RECORD 2>/dev/null | sort > /rig-snap/pip-records.txt"
  echo
  cat "$frag"
  echo
  echo "# injected dump step — payload archive + PM-state dump"
  echo "COPY scripts/dump.sh /rig/dump.sh"
  if [ -f "$RIG_DIR/toolchains/$tc.extras" ]; then
    echo "COPY toolchains/$tc.extras /rig/extras.txt"
  fi
  echo "RUN /rig/dump.sh $tc"
} > "$out"
echo "generated $out"
