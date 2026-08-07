#!/usr/bin/env bash
#
# Baut Sandbox-Editionen und Proxy-Image.
#
# Editionen:
#   opencode-sandbox-base     — Python + core system packages
#   opencode-sandbox-web      — base + Node/TypeScript/Playwright
#   opencode-sandbox-embedded — base + ARM toolchains/Arduino/MicroPython
#   opencode-sandbox-full     — web + embedded (default, backward compat)
#
set -euo pipefail
cd "$(dirname "$0")/.."

EDITION="${1:-full}"

case "$EDITION" in
  base)
    echo "==> Baue opencode-sandbox-base"
    podman build -t opencode-sandbox-base --target opencode-sandbox-base -f Dockerfile .
    ;;
  web)
    echo "==> Baue opencode-sandbox-web"
    podman build -t opencode-sandbox-web --target opencode-sandbox-web -f Dockerfile .
    ;;
  embedded)
    echo "==> Baue opencode-sandbox-embedded"
    podman build -t opencode-sandbox-embedded --target opencode-sandbox-embedded -f Dockerfile .
    ;;
  full)
    echo "==> Baue opencode-sandbox-base"
    podman build -t opencode-sandbox-base --target opencode-sandbox-base -f Dockerfile .
    echo "==> Baue opencode-sandbox-web"
    podman build -t opencode-sandbox-web --target opencode-sandbox-web -f Dockerfile .
    echo "==> Baue opencode-sandbox-embedded"
    podman build -t opencode-sandbox-embedded --target opencode-sandbox-embedded -f Dockerfile .
    echo "==> Baue opencode-sandbox-full (web + embedded)"
    podman build -t opencode-sandbox-full --target opencode-sandbox-full -f Dockerfile .
    ;;
  all)
    echo "==> Baue alle Editionen"
    podman build -t opencode-sandbox-base --target opencode-sandbox-base -f Dockerfile .
    podman build -t opencode-sandbox-web --target opencode-sandbox-web -f Dockerfile .
    podman build -t opencode-sandbox-embedded --target opencode-sandbox-embedded -f Dockerfile .
    podman build -t opencode-sandbox-full --target opencode-sandbox-full -f Dockerfile .
    ;;
  *)
    echo "Unbekannte Edition: $EDITION" >&2
    echo "Nutzung: $0 [base|web|embedded|full|all]" >&2
    echo "" >&2
    echo "  Standard: full (web + embedded)" >&2
    exit 1
    ;;
esac

echo "==> Baue Egress-Proxy (Squid)"
podman build -t oc-proxy -f proxy/Dockerfile proxy/

echo "==> Fertig."
echo ""
echo "Editionen:"
echo "  opencode-sandbox-base     — Python + core system packages"
echo "  opencode-sandbox-web      — base + Node/TypeScript/Playwright"
echo "  opencode-sandbox-embedded — base + ARM toolchains/Arduino/MicroPython"
echo "  opencode-sandbox-full     — web + embedded (default)"
echo ""
echo "Proxy starten mit:"
echo "    podman run -d --name oc-proxy -p 127.0.0.1:3128:3128 oc-proxy"
echo ""
echo "Sandbox starten mit:"
echo "    scripts/start.sh <projekt-root> --edition <base|web|embedded|full> [Flags]"
echo ""
echo "Beispiele:"
echo "    scripts/start.sh ~/proj --edition web"
echo "    scripts/start.sh ~/proj --edition embedded --hil_mode"
echo "    scripts/start.sh ~/proj --edition full --use_proxy"
