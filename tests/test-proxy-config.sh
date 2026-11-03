#!/usr/bin/env bash
#
# Regressionsklasse: der Per-Pod-Squid-Member läuft unter rootless podman
# mit Pod-Level --userns=keep-id (ADR 0029), --cap-drop=ALL, und wird per
# `podman start` wiederverwendet (Write-Layer bleibt bestehen). Der
# Squid-Prozess darf beim Start daher NICHTS ins Image-Rootfs schreiben:
# sonst stirbt er je nach Host-Mount/Mapping sofort im squid-FATAL
# ("failed to open /var/run/squid.pid: Permission denied", live auf WSL
# belegt), und eine stale PID-Datei crash-t Reuse-Restarts.
#
# Contract:Alle Startup-Writes gehen nach /dev (stdio) bzw. ab.

set -euo pipefail
cd "$(dirname "$0")/.."

CONF="dist/proxy/squid.conf"
fail=0

require() {
  grep -q -- "$1" "$CONF" || { printf 'fehlt: %s\n' "$1" >&2; fail=1; }
}
forbid() {
  if grep -q -- "$1" "$CONF"; then
    printf 'verboten (Image-Rootfs-Write beim Start): %s\n' "$1" >&2
    fail=1
  fi
}

require 'pid_filename none'
require 'pinger_enable off'
require 'access_log stdio:/dev/stdout'
require 'cache_log stdio:/dev/stderr'
require 'cache_store_log none'
require 'cache deny all'

forbid 'pid_filename /var'
forbid 'access_log /var/'
forbid 'cache_log /var/'
forbid 'cache_store_log /var/'
forbid 'coredump_dir /var/'

if [[ "$fail" == 1 ]]; then
  exit 1
fi
printf 'proxy squid.conf rootless-contract ok\n'
