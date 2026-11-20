#!/bin/bash
# PROTOTYPE throwaway — the injected dump step (wayfinder #186, rig real/).
# Runs INSIDE a component build as root, after the recipe fragment; the
# generated Dockerfile (scripts/gen-component.sh) injects it. Emits /rig-out/:
#
#   payload.tar.gz  — dpkg -L of newly installed packages ∪ pip RECORD paths
#                     ∪ fragment-declared extras (/rig/extras.txt)
#   pm-status       — the component's full dpkg status file (set-side stanza union)
#   pm-info.tar.gz  — /var/lib/dpkg/info (incl. <pkg>.list, so dpkg -L works in sets)
#
# "Newly installed" = current dpkg status minus the pre-fragment snapshot the
# generated Dockerfile took (/rig-snap/).
set -euo pipefail
tc="${1:-unknown}"
out=/rig-out
rm -rf "$out"
mkdir -p "$out"

python3 - <<'PY'
import csv, glob, os, subprocess

def installed_stanzas(path):
    """Package names whose Status is install ok installed."""
    pkgs, pkg, ok = set(), None, False
    with open(path) as f:
        for line in f:
            line = line.rstrip('\n')
            if not line:
                if pkg is not None and ok:
                    pkgs.add(pkg)
                pkg, ok = None, False
                continue
            if line.startswith('Package: '):
                pkg = line[len('Package: '):]
            elif line.startswith('Status: '):
                ok = line == 'Status: install ok installed'
    if pkg is not None and ok:
        pkgs.add(pkg)
    return pkgs

# --- dpkg: newly installed packages = current status minus the snapshot
cur = installed_stanzas('/var/lib/dpkg/status')
base = installed_stanzas('/rig-snap/dpkg-status')
new_pkgs = sorted(cur - base)

dpkg_paths = []
for p in new_pkgs:
    r = subprocess.run(['dpkg', '-L', p], capture_output=True, text=True)
    dpkg_paths.extend(l for l in r.stdout.splitlines() if l)

# --- pip: RECORD files not present in the pre-fragment snapshot
def find_records():
    found = []
    for pat in ('/usr/lib/python3*/*-packages/**/*.dist-info/RECORD',
                '/usr/local/lib/python3*/*-packages/**/*.dist-info/RECORD'):
        found.extend(glob.glob(pat, recursive=True))
    return sorted(set(found))

snap_records = set()
snap_file = '/rig-snap/pip-records.txt'
if os.path.exists(snap_file):
    snap_records = set(open(snap_file).read().splitlines())

pip_paths = []
for rec in find_records():
    if rec in snap_records:
        continue
    site_root = os.path.dirname(os.path.dirname(rec))  # .dist-info -> site dir
    with open(rec, newline='') as f:
        for row in csv.reader(f):
            if row and row[0]:
                pip_paths.append(os.path.normpath(os.path.join(site_root, row[0])))

# --- fragment-declared extras (one absolute path per line; may be absent)
extras = []
if os.path.exists('/rig/extras.txt'):
    extras = [l.strip() for l in open('/rig/extras.txt') if l.strip()]

# write the payload list (only paths that exist), sorted, deduped
paths = sorted(set(dpkg_paths + pip_paths + extras))
existing = [p for p in paths if os.path.lexists(p)]
missing = [p for p in paths if not os.path.lexists(p)]
if missing:
    print('dump: skipping %d nonexistent declared paths' % len(missing))
if not existing:
    existing = ['/rig/dump.sh']  # keep the tar well-formed
with open('/rig/payload-list.txt', 'w') as f:
    for p in existing:
        f.write(p + '\n')
print('dump: %d dpkg-new pkgs, %d payload paths' % (len(new_pkgs), len(existing)))
PY

tar -C / --verbatim-files-from -czf "$out/payload.tar.gz" -T /rig/payload-list.txt

cp /var/lib/dpkg/status "$out/pm-status"
tar -C /var/lib/dpkg -czf "$out/pm-info.tar.gz" info
