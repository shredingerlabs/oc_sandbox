#!/usr/bin/env bash
#
# Baut Sandbox- und Proxy-Image in einem Rutsch.
#
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Baue opencode-sandbox (alle Use Cases)"
podman build -t opencode-sandbox -f Dockerfile .

echo "==> Baue Egress-Proxy (Squid)"
podman build -t oc-proxy -f proxy/Dockerfile proxy/

echo "==> Fertig. Proxy starten mit:"
echo "    podman run -d --name oc-proxy -p 127.0.0.1:3128:3128 oc-proxy"
echo "Sandbox starten mit:"
echo "    scripts/start.sh <projekt-root> [--use_proxy] [--offline] [--hil_mode] [--cbm_ui]"
