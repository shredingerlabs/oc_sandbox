#!/bin/bash
# PROTOTYPE throwaway: M5 sanity checks on the final set images.
# dpkg status coherent, pip metadata intact, /usr/local not clobbered.
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$RIG_DIR"
P=./rig-podman

check() { # check <image> <name> <cmd...>
  local img="$1" name="$2"; shift 2
  local out rc=0
  out=$("$P" run --rm --network=host "$img" bash -c "$*" 2>&1) || rc=$?
  if [ "$rc" -eq 0 ]; then
    echo "PASS  $img  $name  ($out)"
  else
    echo "FAIL  $img  $name  (rc=$rc: $(echo "$out" | tail -2))"
  fi
}

for img in localhost/rig-prefix-set2:latest localhost/rig-merged-set2:latest; do
  echo "--- $img"
  check "$img" "dpkg knows gcc"        'dpkg-query -W -f="${Status} ${Version}\n" gcc 2>&1'
  check "$img" "dpkg knows python3"    'dpkg-query -W -f="${Status} ${Version}\n" python3 2>&1'
  check "$img" "dpkg --audit empty"    'out=$(dpkg --audit); [ -z "$out" ] && echo coherent'
  check "$img" "pip metadata intact"   'python3 -c "import requests; print(requests.__version__)"'
  check "$img" "base binary yq runs"   '/usr/local/bin/yq --version'
  check "$img" "curl binary hadolint"  '/usr/local/bin/hadolint --version'
  check "$img" "dpkg entry count"      'echo $(dpkg-query -W 2>/dev/null | wc -l) entries'
done
