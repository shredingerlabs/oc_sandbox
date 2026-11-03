#!/usr/bin/env bash
#
# Regressionsklasse: Squid 6.x stirbt beim Container-Start — und OpenCode leidet
# durch ECONNREFUSED am Proxy — wenn die dstdomain-Allowlist One-Way-Overlay-
# Paare enthaelt, z.B. ".github.com" zusammen mit "github.com"
# (squid-FATAL: ... Bungled ... acl allowed_domains dstdomain ...).
#
# Der Test baut die exakte squid-6.12-Regel aus src/acl/DomainData.cc
# (aclDomainCompare) + src/anyp/Uri.cc (matchDomainName) als Paar-Check nach:
# match(h, d) == 0, wenn host (ohne Fuehrendes Punkte) == d oder d
# ('.'-Wildcard) ein abschliessendes Suffix von host ist. Ein solches Paar
# erzeugt mindestens die WARNING "Eintrag ignoriert", in der kritischen
# Insert-Richtung das FATAL: Bungled squid.conf = toter Squid-Member.
#
# Identische Doppel-Eintraege sind erlaubt (squid droppt sie ohne Overlay).

set -euo pipefail
cd "$(dirname "$0")/.."

ALLOWLIST="dist/proxy/allowlist.txt"

python3 - "$ALLOWLIST" <<'PYEOF'
import sys

entries = []
for raw in open(sys.argv[1], encoding="utf-8"):
    line = raw.strip()
    if not line or line.startswith("#"):
        continue
    entries.append(line)

def match(host_entry, domain_entry):
    """matchDomainName(h, d): 0 = match (Overlay), sonst != 0.

    matchDomainName entfernt alle Fuehrendes Punkte vom host-Parameter,
    der domain-Parameter (inkl. Fuehrendes Punkt) matcht jedes
    abschliessende Domain-Suffix."""
    h = host_entry.lstrip(".")
    d = domain_entry
    if h == d:
        return 0
    if d.startswith(".") and d[1:] and h.endswith(d[1:]):
        return 0
    return 1

overlap = []
for i, a in enumerate(entries):
    for j in range(i + 1, len(entries)):
        b = entries[j]
        if a == b:
            continue  # identischer Doppel-Eintrag: harmloser Drop
        if match(a, b) == 0 or match(b, a) == 0:
            overlap.append((a, b))

if not entries:
    print(f"keine Einträge erkannt in {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if overlap:
    for a, b in overlap:
        print(f"Overlay-Paare in {sys.argv[1]}: '{a}' vs '{b}'", file=sys.stderr)
    print("  squid: minimum WARNING (Eintrag ignoriert),", file=sys.stderr)
    print("  max FATAL 'Bungled ... dstdomain' → toter Squid-Member.", file=sys.stderr)
    sys.exit(1)

print(f"allowlist ok ({len(entries)} entry-sets, keine Overlay-Paare)")
PYEOF
