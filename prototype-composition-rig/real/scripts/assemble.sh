#!/bin/bash
# PROTOTYPE throwaway — the one RUN of set assembly (wayfinder #186, rig real/).
# Generated Dockerfiles (scripts/gen-set.sh) end with this. Each /rig/<tc>/
# holds that member's dump output (payload archive + PM state), COPY'd from its
# component image. Payloads extract in canonical order; dpkg status merges as
# canonical stanza-union (last canonical writer wins per Package name); PM info
# entries overlay in the same order; ldconfig refreshes; staging is removed —
# all inside this single layer.
set -euo pipefail

for tc in "$@"; do
  tar xzf "/rig/$tc/payload.tar.gz" -C /
  tar xzf "/rig/$tc/pm-info.tar.gz" -C /var/lib/dpkg
done

python3 - "$@" <<'PY'
import os, sys

# base status first, then member dumps in canonical order; a later stanza with
# the same Package name replaces the earlier one
files = ['/var/lib/dpkg/status'] + ['/rig/%s/pm-status' % tc for tc in sys.argv[1:]]
stanzas = {}
for path in files:
    pkg, buf = None, []

    def flush(pkg, buf):
        if pkg is not None and buf:
            stanzas[pkg] = '\n'.join(buf) + '\n'

    with open(path) as f:
        for line in f:
            line = line.rstrip('\n')
            if not line:
                flush(pkg, buf)
                pkg, buf = None, []
            else:
                buf.append(line)
                if line.startswith('Package: '):
                    pkg = line[len('Package: '):]
    flush(pkg, buf)

with open('/var/lib/dpkg/status.merge', 'w') as f:
    for s in stanzas.values():
        f.write(s + '\n')
os.replace('/var/lib/dpkg/status.merge', '/var/lib/dpkg/status')
print('assemble: %d stanzas merged' % len(stanzas))
PY

ldconfig
rm -rf /rig
