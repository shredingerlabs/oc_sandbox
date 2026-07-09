#!/usr/bin/env bash
#
# Startet die OpenCode-Sandbox (Podman rootless, gehärtet).
#
# Ein Container für alle Use Cases: Coding, HIL-Tests, Offline, Proxy.
# Modi werden per Flags gewählt, nicht per separatem Image.
#
# Erwartet EINEN Projekt-Root mit folgender Struktur:
#
#   <PROJECT_ROOT>/
#     project/            <- Repo, wird nach /home/dev/project gemountet
#     .opencode_config/    <- OpenCode-Config, projektspezifisch persistent
#     .opencode_data/       <- OpenCode-Daten inkl. Auth/Credentials
#     .ssh_local/           <- SSH-Keys + Config für dieses Projekt
#     .git_local/           <- Git-Identität/-Settings + optionale Credentials
#
# Flags:
#   --use_proxy   Startet Squid-Egress-Allowlist-Proxy und nutzt ihn
#   --offline     Kein Netzwerk (nur lokale Modelle)
#   --hil_mode    USB-Passthrough für Oszi (oszi0) und MCU (ttyUSB*, ttyAMA*, ttyAMC*)
#
# Beispiele:
#   scripts/start.sh ~/projects/kunde-x                        # Default (volle Netzanbindung)
#   scripts/start.sh ~/projects/kunde-x --use_proxy             # Mit Egress-Allowlist
#   scripts/start.sh ~/projects/kunde-x --offline               # Ohne Netzwerk
#   scripts/start.sh ~/projects/kunde-x --hil_mode              # HIL-Tests
#   scripts/start.sh ~/projects/kunde-x --use_proxy --hil_mode  # Kombiniert
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Flags parsen -------------------------------------------------------------
PROJECT_ROOT="${1:-}"
if [[ -z "$PROJECT_ROOT" ]]; then
  echo "Nutzung: $0 <projekt-root> [--use_proxy] [--offline] [--hil_mode]" >&2
  echo "" >&2
  echo "  <projekt-root>  Pfad zum Projekt-Root (siehe init-project.sh)" >&2
  echo "  --use_proxy     Squid-Egress-Allowlist-Proxy starten und nutzen" >&2
  echo "  --offline       Kein Netzwerk (--network=none)" >&2
  echo "  --hil_mode      USB-Passthrough für Oszi + MCU-Geräte" >&2
  exit 1
fi
shift

USE_PROXY=false
OFFLINE=false
HIL_MODE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --use_proxy) USE_PROXY=true; shift ;;
    --offline)   OFFLINE=true;   shift ;;
    --hil_mode)  HIL_MODE=true;  shift ;;
    *)
      echo "Unbekanntes Flag: $1" >&2
      echo "Nutzung: $0 <projekt-root> [--use_proxy] [--offline] [--hil_mode]" >&2
      exit 1
      ;;
  esac
done

# --- Projekt-Root validieren ---------------------------------------------------
PROJECT_ROOT="$(realpath "$PROJECT_ROOT")"

PROJECT_DIR="${PROJECT_ROOT}/project"
CONFIG_DIR="${PROJECT_ROOT}/.opencode_config"
DATA_DIR="${PROJECT_ROOT}/.opencode_data"
SSH_DIR="${PROJECT_ROOT}/.ssh_local"
GIT_DIR="${PROJECT_ROOT}/.git_local"

mkdir -p "$PROJECT_DIR" "$CONFIG_DIR" "$DATA_DIR" "$SSH_DIR" "$GIT_DIR"
chmod 700 "$SSH_DIR" "$GIT_DIR"

if [[ -z "$(find "$SSH_DIR" -maxdepth 1 -type f 2>/dev/null)" ]]; then
  echo "Warnung: ${SSH_DIR} enthält keine Dateien (keine Keys/Config)." >&2
  echo "  Git-Push/Pull über SSH wird ohne Keys fehlschlagen." >&2
  echo "  Vorlage: templates/ssh_local/config, Keys erzeugen mit:" >&2
  echo "    ssh-keygen -t ed25519 -f ${SSH_DIR}/id_ed25519_github -N \"\"" >&2
fi

if [[ ! -f "${GIT_DIR}/gitconfig" ]]; then
  echo "Warnung: ${GIT_DIR}/gitconfig fehlt - lege eine an (z.B. aus" >&2
  echo "  templates/git_local/gitconfig kopieren), sonst fehlen user.name/" >&2
  echo "  user.email im Container." >&2
fi

while IFS= read -r -d '' key; do
  perms="$(stat -c '%a' "$key")"
  if [[ "$perms" != "600" && "$perms" != "400" ]]; then
    echo "Warnung: ${key} hat Rechte ${perms}, SSH erwartet 600. Fix: chmod 600 ${key}" >&2
  fi
done < <(find "$SSH_DIR" -maxdepth 1 -type f -name 'id_*' ! -name '*.pub' -print0 2>/dev/null)

if [[ -f "${GIT_DIR}/credentials" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/credentials")"
  if [[ "$perms" != "600" && "$perms" != "400" ]]; then
    echo "Warnung: ${GIT_DIR}/credentials hat Rechte ${perms}, sollte 600 sein." >&2
  fi
fi

# --- Proxy ----------------------------------------------------------------------
if $USE_PROXY; then
  if ! podman image exists oc-proxy 2>/dev/null; then
    echo "==> Baue oc-proxy Image ..."
    podman build -t oc-proxy -f "${REPO_ROOT}/proxy/Dockerfile" "${REPO_ROOT}/proxy/"
  fi
  if ! podman container exists oc-proxy 2>/dev/null || ! podman inspect -f '{{.State.Running}}' oc-proxy 2>/dev/null | grep -q true; then
    echo "==> Starte oc-proxy Container ..."
    podman run -d --replace --name oc-proxy --network=podman \
      -p 127.0.0.1:3128:3128 oc-proxy
  fi
fi

# --- Netzwerk-Optionen ---------------------------------------------------------
NETWORK_ARGS=(--network=slirp4netns)
PROXY_ENV=()
if $USE_PROXY; then
  PROXY_ENV=(
    -e HTTPS_PROXY="http://host.containers.internal:3128"
    -e HTTP_PROXY="http://host.containers.internal:3128"
    -e NO_PROXY="localhost,127.0.0.1"
  )
fi
if $OFFLINE; then
  echo "Hinweis: Sandbox läuft ohne Netzwerk (--offline). OpenCode-Login/API" >&2
  echo "         ist damit nicht nutzbar, nur lokale Modelle." >&2
  NETWORK_ARGS=(--network=none)
  PROXY_ENV=()
fi

# --- HIL-Geräte ----------------------------------------------------------------
DEVICE_ARGS=()
if $HIL_MODE; then
  if [[ -e /dev/oszi0 ]]; then
    DEVICE_ARGS+=(--device=/dev/oszi0)
  else
    echo "Warnung: /dev/oszi0 nicht gefunden. Ist das Oszi angeschlossen und die" >&2
    echo "         udev-Regel (udev/99-oszi.rules) installiert?" >&2
  fi
  for dev in /dev/ttyUSB* /dev/ttyAMA* /dev/ttyAMC*; do
    [[ -e "$dev" ]] && DEVICE_ARGS+=(--device="$dev")
  done
  DEVICE_ARGS+=(--group-add keep-groups)
fi

# --- Container starten ---------------------------------------------------------
CONTAINER_NAME="opencode-sandbox-$(basename "$PROJECT_ROOT")"

exec podman run --rm -it \
  --name "$CONTAINER_NAME" \
  --userns=keep-id \
  --cap-drop=ALL \
  --security-opt no-new-privileges \
  "${NETWORK_ARGS[@]}" \
  "${PROXY_ENV[@]}" \
  "${DEVICE_ARGS[@]}" \
  -e TERM="${TERM:-xterm-256color}" \
  -e XDG_CONFIG_HOME="/home/dev/.config" \
  -e XDG_DATA_HOME="/home/dev/.local/share" \
  -e GIT_CONFIG_GLOBAL="/home/dev/.git_local/gitconfig" \
  -v "${PROJECT_DIR}:/home/dev/project:Z" \
  -v "${CONFIG_DIR}:/home/dev/.config/opencode:Z" \
  -v "${DATA_DIR}:/home/dev/.local/share/opencode:Z" \
  -v "${SSH_DIR}:/home/dev/.ssh:Z" \
  -v "${GIT_DIR}:/home/dev/.git_local:Z" \
  opencode-sandbox
