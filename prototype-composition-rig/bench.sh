#!/bin/bash
# PROTOTYPE throwaway: build the full measurement matrix for ticket #179.
# Timings are wall-clock seconds; results land in results.log / results.tsv.
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$RIG_DIR"
P=./rig-podman

CANON="tc-apt tc-curl tc-pip"
SET1="tc-curl tc-pip"          # canonical order restricted to members
SET2="tc-apt tc-curl tc-pip"
SET2_INCREMENTAL="tc-curl tc-pip tc-apt"   # bonus: chain extends set1's order

RESULTS=results.tsv
LOG=results.log
: > "$RESULTS"
: > "$LOG"

timed() { # timed <phase-id> <cmd...>
  local phase="$1"; shift
  echo "===== [$phase] $(date -u +%T) $*" >> "$LOG"
  local t0 t1 rc
  t0=$(date +%s.%N)
  "$@" >> "$LOG" 2>&1
  rc=$?
  t1=$(date +%s.%N)
  local secs
  secs=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}')
  echo -e "$phase\t$secs\t$rc" | tee -a "$RESULTS"
}

wipe_caches() { # clear buildah cache-mount store so "cold" means cold
  rm -rf /tmp/opencode/vartmp/*
  echo "caches wiped: $(date -u +%T)" >> "$LOG"
}

echo "== building base (untimed context)"
wipe_caches
timed base "$P" build -t localhost/rig-base:latest --network=host base/

echo "== M1a/M2a: strategy (a) prefix-chain, cold then warm, full canonical set2"
./gen-prefix.sh "$SET2" $SET2
timed M1a-cold-set2 "$P" build -t localhost/rig-prefix-set2:latest --network=host -f "sets/prefix/Dockerfile.$SET2" .
timed M2a-warm-set2 "$P" build -t localhost/rig-prefix-set2:latest --network=host -f "sets/prefix/Dockerfile.$SET2" .

echo "== M1b/M2b: strategy (b) component-merge, cold then warm"
for tc in $CANON; do
  {
    echo "# PROTOTYPE throwaway component $tc"
    echo "FROM localhost/rig-base:latest"
    cat "toolchains/$tc.frag"
  } > "toolchains/Dockerfile.$tc"
done
./gen-merged.sh "$SET2" $CANON
wipe_caches
for tc in $CANON; do
  timed "M1b-cold-comp-$tc" "$P" build -t "localhost/rig-comp-$tc:latest" --network=host -f "toolchains/Dockerfile.$tc" .
done
timed M1b-cold-merge-set2 "$P" build -t localhost/rig-merged-set2:latest --network=host -f "sets/merged/Dockerfile.$SET2" .
for tc in $CANON; do
  timed "M2b-warm-comp-$tc" "$P" build -t "localhost/rig-comp-$tc:latest" --network=host -f "toolchains/Dockerfile.$tc" .
done
timed M2b-warm-merge-set2 "$P" build -t localhost/rig-merged-set2:latest --network=host -f "sets/merged/Dockerfile.$SET2" .

echo "== M3 headline: add-one-toolchain-to-existing-set (set1 -> set2)"
# strategy (a): set1 built cold, then set2 in global canonical order
wipe_caches
./gen-prefix.sh "$SET1" $SET1
timed M3a-set1-cold "$P" build -t localhost/rig-prefix-set1:latest --network=host -f "sets/prefix/Dockerfile.$SET1" .
./gen-prefix.sh "$SET2" $SET2
timed M3a-add-one-set2 "$P" build -t localhost/rig-prefix-set2:latest --network=host -f "sets/prefix/Dockerfile.$SET2" .

# strategy (a) bonus: set2 chain extends set1's order instead of global canonical
wipe_caches
./gen-prefix.sh "$SET1" $SET1
timed M3i-set1-cold "$P" build -t localhost/rig-prefix-set1:latest --network=host -f "sets/prefix/Dockerfile.$SET1" .
./gen-prefix.sh "$SET2-inc" $SET2_INCREMENTAL
timed M3i-add-one-set2 "$P" build -t localhost/rig-prefix-set2-inc:latest --network=host -f "sets/prefix/Dockerfile.$SET2-inc" .

# strategy (b): set1's components built cold; add = only new component + merge
wipe_caches
./gen-merged.sh "$SET1" $SET1
for tc in $SET1; do
  timed "M3b-set1-comp-$tc" "$P" build -t "localhost/rig-comp-$tc:latest" --network=host -f "toolchains/Dockerfile.$tc" .
done
timed M3b-set1-merge "$P" build -t localhost/rig-merged-set1:latest --network=host -f "sets/merged/Dockerfile.$SET1" .
timed M3b-add-one-comp-tc-apt "$P" build -t localhost/rig-comp-tc-apt:latest --network=host -f toolchains/Dockerfile.tc-apt .
./gen-merged.sh "$SET2" $CANON
timed M3b-add-one-merge-set2 "$P" build -t localhost/rig-merged-set2:latest --network=host -f "sets/merged/Dockerfile.$SET2" .

echo "== M4 sizes"
{
  echo "--- image sizes (M4) ---"
  $P image list --format '{{.Repository}}:{{.Tag}} {{.Size}}'
  echo "--- system df -v (shared/unique) ---"
  $P system df -v
} >> "$LOG" 2>&1

echo
echo "== results.tsv =="
column -t -s $'\t' "$RESULTS" 2>/dev/null || cat "$RESULTS"
echo "full log: $RIG_DIR/$LOG"
