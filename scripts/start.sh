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
#   --hil_mode    USB-Passthrough für Oszi (scope0) und MCU (ttyUSB*, ttyACM*, ttyAMA*)
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

if [[ ! -f "${GIT_DIR}/glab-cli/config.yml" ]]; then
  echo "Warnung: ${GIT_DIR}/glab-cli/config.yml fehlt - lege eine an (z.B. aus" >&2
  echo "  templates/git_local/glab-cli/config.yml kopieren), sonst fehlt die" >&2
  echo "  glab-Konfiguration im Container." >&2
fi

if [[ ! -f "${GIT_DIR}/gh-cli/config.yml" ]]; then
  echo "Warnung: ${GIT_DIR}/gh-cli/config.yml fehlt - lege eine an (z.B. aus" >&2
  echo "  templates/git_local/gh-cli/config.yml kopieren), sonst fehlt die" >&2
  echo "  gh (GitHub CLI)-Konfiguration im Container." >&2
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

if [[ -f "${GIT_DIR}/glab-cli/config.yml" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/glab-cli/config.yml")"
  if [[ "$perms" != "600" ]]; then
    echo "Warnung: ${GIT_DIR}/glab-cli/config.yml hat Rechte ${perms}, setze auf 600." >&2
    chmod 600 "${GIT_DIR}/glab-cli/config.yml"
  fi
fi

if [[ -f "${GIT_DIR}/gh-cli/config.yml" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/gh-cli/config.yml")"
  if [[ "$perms" != "600" ]]; then
    echo "Warnung: ${GIT_DIR}/gh-cli/config.yml hat Rechte ${perms}, setze auf 600." >&2
    chmod 600 "${GIT_DIR}/gh-cli/config.yml"
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
#
# Wichtig: Geräte werden NICHT mehr als einzelne --device-Snapshots gebunden.
# Grund: --device bindet beim Container-Start eine feste major:minor-
# Kombination. Enumeriert das USB-Gerät später neu (neue Bus/Device-Nummer,
# z.B. weil das PicoScope beim (Wieder-)Verbinden Firmware nachlädt, wegen
# USB-Autosuspend, oder weil ein MCU-Board resettet wird), zeigt der
# Snapshot im Container ins Leere (Rechte erscheinen als "c---------"),
# obwohl das Gerät auf dem Host unter neuem Pfad weiterhin funktioniert.
DEVICE_ARGS=()
if $HIL_MODE; then

  # --- PicoScope (libusb-basiert: libps2000/libps2000a lesen selbst das
  #     Verzeichnis /dev/bus/usb/<Bus>/ per readdir und öffnen dort den
  #     aktuell passenden Geräteknoten. Ein Symlink an anderer Stelle
  #     (z.B. /dev/hil/scope0) hilft dem NICHT - libusb kennt nur den
  #     Standardpfad /dev/bus/usb/*.
  #     Wir ermitteln daher zur Laufzeit die Bus-Nummer (stabil, solange
  #     das Gerät am selben physischen Port hängt - siehe udev/99-hil.rules
  #     für den Autosuspend-Fix gegen sporadisches Re-Enumerieren) und
  #     mounten NUR dieses eine Bus-Verzeichnis live, nicht ganz
  #     /dev/bus/usb. Das gibt Zugriff auf alles, was aktuell an diesem
  #     Bus/Hub hängt (mehr geht mit libusb nicht granularer), aber nicht
  #     auf andere USB-Busse des Hosts.
  if [[ -e /dev/scope0 ]]; then
    SCOPE_REAL=$(readlink -f /dev/scope0)          # z.B. /dev/bus/usb/007/055
    SCOPE_BUS_DIR="$(dirname "$SCOPE_REAL")"        # z.B. /dev/bus/usb/007
    if [[ -d "$SCOPE_BUS_DIR" ]]; then
      DEVICE_ARGS+=(-v "${SCOPE_BUS_DIR}:${SCOPE_BUS_DIR}")
      # Kein --device-cgroup-rule: im rootless-Modus (User-Namespace) gibt
      # es keinen nutzbaren devices-Cgroup-Controller, Podman lehnt das
      # Flag dort ab ("not supported in rootless mode"). Das ist unkritisch,
      # weil rootless-Container ohnehin nur über normale Unix-Rechte (DAC)
      # auf Devices zugreifen - die Cgroup-Ebene entfällt komplett. Die
      # Freigabe hier passiert allein durch den Bind-Mount + die per
      # --userns=keep-id/--group-add keep-groups übernommene
      # Gruppenmitgliedschaft (dialout, siehe udev/99-hil.rules).
    fi
  else
    echo "Hinweis: /dev/scope0 fehlt. Das ist eine Folgeerscheinung, kein Blocker:" >&2
    echo "         - udev-Regel (udev/99-hil.rules) ist auf dem Host nicht installiert, ODER" >&2
    echo "         - Oszi ist nicht angeschlossen (Bus-Nummer kann erst danach ermittelt" >&2
    echo "           werden - Container ggf. neu starten, sobald das Gerät steckt)." >&2
  fi

  # --- Serielle HIL-Geräte (ttyUSB/ttyACM): udev legt bei jedem ADD-Event
  #     (auch nach Reset/Replug) einen echten Geräteknoten unter aktuellem
  #     Kernel-Namen sowie einen stabilen Vendor/Produkt/Serial-Symlink in
  #     /dev/hil/ an (siehe udev/99-hil.rules). Das Verzeichnis wird live
  #     gemountet, sodass jede Änderung sofort im Container sichtbar ist -
  #     kein Container-Neustart nach einem Reset nötig.
  if [[ -d /dev/hil ]]; then
    DEVICE_ARGS+=(-v /dev/hil:/dev/hil)
  else
    echo "Hinweis: /dev/hil fehlt. Das ist eine Folgeerscheinung, kein Blocker:" >&2
    echo "         - udev-Regel (udev/99-hil.rules) ist auf dem Host nicht installiert, ODER" >&2
    echo "         - noch kein passendes Gerät wurde je erkannt (Verzeichnis wird von udev" >&2
    echo "           erst beim ersten ADD-Event automatisch angelegt)." >&2
  fi

  # onboard UART (kein USB, re-enumeriert nicht) weiterhin klassisch durchreichen
  for dev in /dev/ttyAMA*; do
    [[ -e "$dev" ]] && DEVICE_ARGS+=(--device="$dev")
  done
fi

# --- Container starten ---------------------------------------------------------
CONTAINER_NAME="opencode-sandbox-$(basename "$PROJECT_ROOT")"

exec podman run --rm -it \
  --replace \
  --name "$CONTAINER_NAME" \
  --userns=keep-id \
  --group-add keep-groups \
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
  -v "${SSH_DIR}:/home/dev/.ssh:Z,ro" \
  -v "${GIT_DIR}:/home/dev/.git_local:Z,ro" \
  opencode-sandbox
