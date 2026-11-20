#!/bin/bash
# PROTOTYPE throwaway: M5 sanity on the delta-assembly set images (#186).
# PM-merge coherence at real scale: dpkg --audit clean, the full dpkg -l keeps
# every member's packages AND the base's (the gcc case naive merging lost),
# pip metadata intact, binaries run.
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$RIG_DIR"
P="$RIG_DIR/../rig-podman"

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

echo "--- localhost/rig-set1:latest (tc-go, tc-playwright, tc-pysci)"
check localhost/rig-set1:latest "dpkg --audit empty"        'out=$(dpkg --audit); [ -z "$out" ] && echo coherent'
check localhost/rig-set1:latest "dpkg knows golang-go"      'dpkg-query -W -f="${Status}\n" golang-go'
check localhost/rig-set1:latest "dpkg knows libnss3"        'dpkg-query -W -f="${Status}\n" libnss3'
check localhost/rig-set1:latest "base gcc still known"      'dpkg-query -W -f="${Status}\n" gcc'
check localhost/rig-set1:latest "pip numpy"                 'python3 -c "import numpy; print(numpy.__version__)"'
check localhost/rig-set1:latest "pip matplotlib"            'python3 -c "import matplotlib; print(matplotlib.__version__)"'
check localhost/rig-set1:latest "pip pandas"                'python3 -c "import pandas; print(pandas.__version__)"'
check localhost/rig-set1:latest "go runs"                   'go version'
check localhost/rig-set1:latest "playwright pkg runs"       'playwright --version'
check localhost/rig-set1:latest "base glab runs"            'glab --version'
check localhost/rig-set1:latest "base opencode runs"        'opencode --version'
check localhost/rig-set1:latest "dpkg entry count"          'echo $(dpkg-query -W 2>/dev/null | wc -l) entries'

echo "--- localhost/rig-set2:latest (set1 + tc-cpp)"
check localhost/rig-set2:latest "dpkg --audit empty"        'out=$(dpkg --audit); [ -z "$out" ] && echo coherent'
check localhost/rig-set2:latest "dpkg knows gdb (tc-cpp)"   'dpkg-query -W -f="${Status}\n" gdb'
check localhost/rig-set2:latest "dpkg knows golang-go"      'dpkg-query -W -f="${Status}\n" golang-go'
check localhost/rig-set2:latest "dpkg knows libnss3"        'dpkg-query -W -f="${Status}\n" libnss3'
check localhost/rig-set2:latest "base gcc still known"      'dpkg-query -W -f="${Status}\n" gcc'
check localhost/rig-set2:latest "pip numpy"                 'python3 -c "import numpy; print(numpy.__version__)"'
check localhost/rig-set2:latest "pip matplotlib"            'python3 -c "import matplotlib; print(matplotlib.__version__)"'
check localhost/rig-set2:latest "clang runs"                'clang --version | head -1'
check localhost/rig-set2:latest "go runs"                   'go version'
check localhost/rig-set2:latest "playwright pkg runs"       'playwright --version'
check localhost/rig-set2:latest "base glab runs"            'glab --version'
check localhost/rig-set2:latest "base opencode runs"        'opencode --version'
check localhost/rig-set2:latest "dpkg entry count"          'echo $(dpkg-query -W 2>/dev/null | wc -l) entries'

echo "--- localhost/rig-set2-delta:latest (incremental: set1 + tc-cpp payload only)"
check localhost/rig-set2-delta:latest "dpkg --audit empty"        'out=$(dpkg --audit); [ -z "$out" ] && echo coherent'
check localhost/rig-set2-delta:latest "dpkg knows gdb (tc-cpp)"   'dpkg-query -W -f="${Status}\n" gdb'
check localhost/rig-set2-delta:latest "dpkg knows golang-go"      'dpkg-query -W -f="${Status}\n" golang-go'
check localhost/rig-set2-delta:latest "dpkg knows libnss3"        'dpkg-query -W -f="${Status}\n" libnss3'
check localhost/rig-set2-delta:latest "base gcc still known"      'dpkg-query -W -f="${Status}\n" gcc'
check localhost/rig-set2-delta:latest "pip numpy"                 'python3 -c "import numpy; print(numpy.__version__)"'
check localhost/rig-set2-delta:latest "pip matplotlib"            'python3 -c "import matplotlib; print(matplotlib.__version__)"'
check localhost/rig-set2-delta:latest "clang runs"                'clang --version | head -1'
check localhost/rig-set2-delta:latest "go runs"                   'go version'
check localhost/rig-set2-delta:latest "playwright pkg runs"       'playwright --version'
check localhost/rig-set2-delta:latest "base glab runs"            'glab --version'
check localhost/rig-set2-delta:latest "base opencode runs"        'opencode --version'
check localhost/rig-set2-delta:latest "dpkg entry count"          'echo $(dpkg-query -W 2>/dev/null | wc -l) entries'

echo "--- equivalence: full set2 vs incremental set2-delta file lists"
if "$P" image exists localhost/rig-set2:latest && "$P" image exists localhost/rig-set2-delta:latest; then
  "$P" run --rm --network=host localhost/rig-set2:latest bash -c 'cd / && find -xdev | sort' > /tmp/set2-files.txt
  "$P" run --rm --network=host localhost/rig-set2-delta:latest bash -c 'cd / && find -xdev | sort' > /tmp/set2-delta-files.txt
  if diff -q /tmp/set2-files.txt /tmp/set2-delta-files.txt > /dev/null; then
    echo "PASS  equivalence  file lists identical ($(wc -l < /tmp/set2-files.txt) paths)"
  else
    echo "FAIL  equivalence  first diffs: $(diff /tmp/set2-files.txt /tmp/set2-delta-files.txt | head -6 | tr '\n' ' ')"
  fi
fi

echo "--- reference: base image entry count"
BASE_IMG="${BASE_IMG:-localhost/rig-base-real:latest}"
if [ "$BASE_IMG" = "localhost/rig-base-real:latest" ] && ! "$P" image exists "$BASE_IMG" 2>/dev/null \
   && "$P" image exists localhost/opencode-sandbox-base:latest 2>/dev/null; then
  BASE_IMG="localhost/opencode-sandbox-base:latest"
fi
check "$BASE_IMG" "base dpkg entry count" 'echo $(dpkg-query -W 2>/dev/null | wc -l) entries'
