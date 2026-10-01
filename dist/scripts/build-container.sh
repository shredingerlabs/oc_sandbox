#!/usr/bin/env bash
#
# Baut Sandbox-Editionen und Proxy-Image.
#
# Editions-Registry (ADR 0019): entdeckt alle Container-Stages der Form
#   FROM <parent> AS opencode-sandbox-<name>
# in Dockerfile und Dockerfile.custom (= <script_dir>/../dist/). Jede
# entdeckte Edition ist ein gueltiges Argument; Namen sind klein
# [a-z0-9_-]. Ohne Argument wird jede entdeckte Edition gebaut.
#
set -euo pipefail
cd "$(dirname "$0")/.."

# --- Stage-Registry ---------------------------------------------------------
declare -A STAGE_FILE=()
STAGE_ORDER=()

strip_stage() { sed -E 's/.*[[:space:]]AS[[:space:]]+//; s/[[:space:]]+$//'; }

for dockerfile in Dockerfile Dockerfile.custom; do
  [[ -f "$dockerfile" ]] || continue
  while IFS= read -r line; do
    stage=$(strip_stage <<< "$line")
    name=${stage#opencode-sandbox-}
    [[ -n "$name" && "$name" != "$stage" ]] || continue
    [[ -z "${STAGE_FILE[$stage]:-}" ]] || continue
    STAGE_FILE[$stage]="$dockerfile"
    STAGE_ORDER+=("$stage")
  done < <(grep -E '^[[:space:]]*FROM[[:space:]].*[[:space:]]AS[[:space:]]+opencode-sandbox-[a-z0-9_-]+$' "$dockerfile" || true)
done

USAGE_EDITIONS=""
for stage in "${STAGE_ORDER[@]}"; do
  USAGE_EDITIONS+="${stage#opencode-sandbox-}|"
done
USAGE_EDITIONS="${USAGE_EDITIONS%|}"

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  echo "Nutzung: $0 [$USAGE_EDITIONS|all]"
  echo "Editionen: $(echo "$USAGE_EDITIONS" | tr '|' ' ')   (Standard: all)"
  echo "Entdeckt aus den FROM ... AS opencode-sandbox-<name>-Stages von Dockerfile(.custom)."
  exit 0
fi

if [[ ${#STAGE_ORDER[@]} -eq 0 ]]; then
  echo "Fehler: keine Editionen gefunden (keine Stage FROM ... AS opencode-sandbox-<name> in Dockerfile)." >&2
  exit 1
fi

stage_of_edition() {
  if [[ "$1" == opencode-sandbox-* ]]; then
    printf '%s\n' "$1"
  else
    printf 'opencode-sandbox-%s\n' "$1"
  fi
}

parent_of_stage() {
  local stage="$1" file="$2" from_line
  from_line=$(grep -E "^[[:space:]]*FROM[[:space:]].*[[:space:]]AS[[:space:]]+${stage}[[:space:]]*$" "$file" | head -1 || true)
  sed -E 's/^[[:space:]]*FROM[[:space:]]+//; s/[[:space:]]+AS[[:space:]]+.*$//; s/[[:space:]]+$//' <<< "$from_line"
}

build_edition() {
  local stage="$1"
  local file="${STAGE_FILE[$stage]:-}"
  if [[ -z "$file" ]]; then
    echo "Fehler: unbekannte Edition: $stage" >&2
    exit 1
  fi

  local parent
  parent=$(parent_of_stage "$stage" "$file")
  if [[ "$parent" == opencode-sandbox-* ]]; then
    if ! podman image exists "$parent" >/dev/null 2>&1; then
      if [[ "$parent" == "opencode-sandbox-base" ]]; then
        echo "==> Übergeordnetes Image $parent fehlt — baue es zuerst"
        build_edition "$parent"
      else
        echo "Fehler: übergeordnetes Image '$parent' für '$stage' fehlt." >&2
        echo "Baue es zuerst: $0 ${parent#opencode-sandbox-}" >&2
        exit 1
      fi
    fi
  fi

  echo "==> Baue $stage ($file)"
  podman build --network host -t "$stage" --target "$stage" -f "$file" .
}

if [[ $# -eq 0 || "${1:-}" == "all" ]]; then
  echo "==> Baue alle Editionen: $(echo "${STAGE_ORDER[@]}" | tr ' ' ' ')"
  for stage in "${STAGE_ORDER[@]}"; do
    build_edition "$stage"
  done
elif arg_stage=$(stage_of_edition "$1") && [[ -n "${STAGE_FILE[$arg_stage]:-}" ]]; then
  build_edition "$arg_stage"
else
  echo "Unbekannte Edition: ${1:-}" >&2
  echo "Verfügbare Editionen:" >&2
  for stage in "${STAGE_ORDER[@]}"; do
    echo "  $stage" >&2
  done
  echo "Nutzung: $0 [$USAGE_EDITIONS|all]" >&2
  exit 1
fi

echo "==> Baue Egress-Proxy (Squid)"
podman build -t oc-proxy -f proxy/Dockerfile proxy/

echo "==> Fertig."
echo ""
echo "Gefundene Editionen:"
for stage in "${STAGE_ORDER[@]}"; do
  echo "  $stage"
done
echo ""
echo "Proxy starten mit:"
echo "    podman run -d --name oc-proxy -p 127.0.0.1:3128:3128 oc-proxy"
echo ""
echo "Sandbox starten mit:"
echo "    scripts/start.sh <projekt-root> --edition <edition> [Flags]"
echo ""
echo "Beispiele:"
echo "    scripts/start.sh ~/proj --edition web"
echo "    scripts/start.sh ~/proj --edition embedded --hil_mode"
