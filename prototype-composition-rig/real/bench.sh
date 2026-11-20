#!/bin/bash
# PROTOTYPE throwaway: delta-assembly measurement matrix for wayfinder #186
# (real-sized base). Wall-clock seconds; results land in results.log/results.tsv.
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$RIG_DIR"
P="$RIG_DIR/../rig-podman"

# Base selection: BASE_IMG wins; otherwise reuse the runner's real base image
# when present (max fidelity), else build the rig's own real-sized base.
if [ -z "${BASE_IMG:-}" ]; then
  if "$P" image exists localhost/opencode-sandbox-base:latest 2>/dev/null; then
    BASE_IMG="localhost/opencode-sandbox-base:latest"
    echo "== using the runner's real base image: $BASE_IMG"
  else
    BASE_IMG="localhost/rig-base-real:latest"
    echo "== rig base image not found; will build $BASE_IMG"
  fi
fi
export BASE_IMG

CANON="tc-cpp tc-go tc-playwright tc-pysci"
SET1="tc-go tc-playwright tc-pysci"         # canonical order restricted to members
SET2="tc-cpp tc-go tc-playwright tc-pysci"  # adds tc-cpp at the FRONT of canonical order

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

echo "== M0: base"
if [ "$BASE_IMG" = "localhost/rig-base-real:latest" ]; then
  wipe_caches
  timed M0-base-real "$P" build -t "$BASE_IMG" --network=host base/
fi
timed M0-base-inspect "$P" image inspect "$BASE_IMG" --format '{{.Size}}' || true

echo "== generate component/set Dockerfiles"
for tc in $CANON; do ./scripts/gen-component.sh "$tc"; done
./scripts/gen-set.sh "${SET1// /-}" $SET1
./scripts/gen-set.sh "${SET2// /-}" $SET2

echo "== M3 (runs first so cold numbers stay uncontaminated): set1 cold, then add-one"
wipe_caches
for tc in $SET1; do
  timed "M3-comp-$tc-cold" "$P" build -t "localhost/rig-comp-$tc:latest" --network=host -f "build/toolchains/Dockerfile.$tc" .
done
timed M3-set1-assembly-cold "$P" build -t localhost/rig-set1:latest --network=host -f "build/sets/Dockerfile.$SET1" .
echo "== headline add-one: set1 -> set2 (tc-cpp never built before)"
timed M3-add-one-comp-cold "$P" build -t localhost/rig-comp-tc-cpp:latest --network=host -f build/toolchains/Dockerfile.tc-cpp .
timed M3-add-one-assembly "$P" build -t localhost/rig-set2:latest --network=host -f "build/sets/Dockerfile.$SET2" .

echo "== M2 warm: same assembly + one component rebuild, nothing wiped"
timed M2-assembly-warm "$P" build -t localhost/rig-set2:latest --network=host -f "build/sets/Dockerfile.$SET2" .
timed M2-comp-tc-go-warm "$P" build -t localhost/rig-comp-tc-go:latest --network=host -f build/toolchains/Dockerfile.tc-go .

echo "== M3d: incremental delta add-one (parent set1 image + tc-cpp payload only)"
./scripts/gen-set-delta.sh set2-delta localhost/rig-set1:latest tc-cpp
timed M3d-add-one-delta-assembly "$P" build -t localhost/rig-set2-delta:latest --network=host -f build/sets/Dockerfile.set2-delta .
timed M3d-add-one-delta-warm "$P" build -t localhost/rig-set2-delta:latest --network=host -f build/sets/Dockerfile.set2-delta .

echo "== sizes summary"
{
  echo "--- image sizes"
  "$P" image list --format '{{.Repository}}:{{.Tag}} {{.Size}}' | grep -E 'rig-|opencode-sandbox-base' | sort
  echo "--- payload archives"
  for tc in $CANON; do
    "$P" run --rm "localhost/rig-comp-$tc:latest" bash -c 'stat -c "%n %s" /rig-out/*' || true
  done
} | tee sizes-summary.txt

echo "== payload archive sizes (inside component images)"
for tc in $CANON; do
  echo "--- $tc"
  "$P" run --rm "localhost/rig-comp-$tc:latest" bash -c 'stat -c "%n %s" /rig-out/*' || true
done >> "$LOG" 2>&1

echo "== M4 sizes"
{
  echo "--- image sizes (M4)"
  "$P" image list --format '{{.Repository}}:{{.Tag}} {{.Size}}'
  echo "--- system df -v"
  "$P" system df -v
} >> "$LOG" 2>&1

echo
echo "== results.tsv =="
column -t -s $'\t' "$RESULTS" 2>/dev/null || cat "$RESULTS"
echo "full log: $RIG_DIR/$LOG"
