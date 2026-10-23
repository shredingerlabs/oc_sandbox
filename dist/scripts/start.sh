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
#     .cbm_cache/           <- CBM-Graph-Datenbank (persistent)
#
# Flags:
#   --use_proxy        Startet Squid-Egress-Allowlist-Proxy und nutzt ihn
#   --search_mode      Web-Suche: websearch (Exa/Parallel, Standard), none
#                      oder searxng (oc-searxng-Pod-Member + SearXNG-MCP;
#                      nicht mit --use_proxy kombinierbar, ADR 0026)
#   --offline          Kein Netzwerk (nur lokale Modelle)
#   --hil_mode         USB-Passthrough für Oszi (scope0) und MCU (ttyUSB*, ttyACM*, ttyAMA*)
#   --cbm_ui           Aktiviert CBM Graph-UI auf Port 9749
#   --start_opencode   Startet OpenCode direkt nach Container-Start
#   --start_web        Startet OpenCode Web (Port 4096) nach Container-Start
#   --edition          Container-Edition (z.B. über build-container.sh gebaut)
#   --detach            Container im Hintergrund starten
#   --container-id      Persisted project container identity
#
# Beispiele:
#   scripts/start.sh ~/projects/kunde-x                            # Default (volle Netzanbindung)
#   scripts/start.sh ~/projects/kunde-x --use_proxy                 # Mit Egress-Allowlist
#   scripts/start.sh ~/projects/kunde-x --offline                   # Ohne Netzwerk
#   scripts/start.sh ~/projects/kunde-x --search_mode searxng       # SearXNG-Websuche
#   scripts/start.sh ~/projects/kunde-x --hil_mode                  # HIL-Tests
#   scripts/start.sh ~/projects/kunde-x --use_proxy --hil_mode      # Kombiniert
#   scripts/start.sh ~/projects/kunde-x --cbm_ui                    # Mit CBM Graph-UI
#   scripts/start.sh ~/projects/kunde-x --start_opencode            # Mit direktem OpenCode-Start
#   scripts/start.sh ~/projects/kunde-x --start_web --detach        # Als Webserver
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Flags parsen -------------------------------------------------------------
PROJECT_ROOT="${1:-}"
if [[ "$PROJECT_ROOT" == "--help" || "$PROJECT_ROOT" == "-h" ]]; then
  echo "Nutzung: $0 <projekt-root> --edition <edition> [Flags]"
  echo "  --edition          Container-Edition auswählen (erforderlich)"
  echo "  --use_proxy        Squid-Egress-Allowlist-Proxy starten und nutzen"
  echo "  --search_mode <m>  Web-Suche: websearch (Standard), none, searxng"
  echo "  --offline          Kein Netzwerk (--network=none)"
  echo "  --hil_mode         USB-Passthrough für Oszi und MCU-Geräte"
  echo "  --cbm_ui           CBM Graph-UI auf Port 9749 aktivieren"
  echo "  --start_opencode   OpenCode direkt nach Container-Start starten"
  echo "  --start_web        OpenCode Web (Port 4096) starten und veröffentlichen"
  echo "  --detach           Container im Hintergrund starten"
  echo "  --container-id     Persisted project container identity"
  exit 0
fi
if [[ -z "$PROJECT_ROOT" ]]; then
  echo "Nutzung: $0 <projekt-root> --edition <edition> [--use_proxy] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
  echo "" >&2
  echo "  <projekt-root>        Pfad zum Projekt-Root (siehe init-project.sh)" >&2
  echo "  --edition <edition>   Container-Edition auswählen (erforderlich)" >&2
  echo "  --use_proxy           Squid-Egress-Allowlist-Proxy starten und nutzen" >&2
  echo "  --search_mode <m>     Web-Suche: websearch (Standard), none, searxng" >&2
  echo "  --offline             Kein Netzwerk (--network=none)" >&2
  echo "  --hil_mode            USB-Passthrough für Oszi + MCU-Geräte" >&2
  echo "  --cbm_ui              CBM Graph-UI auf Port 9749 aktivieren" >&2
  echo "  --start_opencode      OpenCode direkt nach Container-Start starten" >&2
  echo "  --start_web           OpenCode Web (Port 4096) starten und veröffentlichen" >&2
  exit 1
fi
shift

USE_PROXY=false
SEARCH_MODE="websearch"
OFFLINE=false
HIL_MODE=false
CBM_UI=false
START_OPENCODE=false
START_WEB=false
DETACH=false
CONTAINER_ID=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --edition)
      [[ $# -ge 2 ]] || { echo "--edition benötigt einen Wert" >&2; exit 1; }
      EDITION="$2"
      shift 2
      ;;
    --use_proxy)        USE_PROXY=true; shift ;;
    --search_mode)
      [[ $# -ge 2 ]] || { echo "--search_mode benötigt einen Wert" >&2; exit 1; }
      SEARCH_MODE="$2"
      shift 2
      ;;
    --offline)          OFFLINE=true;   shift ;;
    --hil_mode)         HIL_MODE=true;  shift ;;
    --cbm_ui)           CBM_UI=true;    shift ;;
    --start_opencode)   START_OPENCODE=true; shift ;;
    --start_web)        START_WEB=true; shift ;;
    --detach)           DETACH=true; shift ;;
    --container-id)
      [[ $# -ge 2 ]] || { echo "--container-id benötigt einen Wert" >&2; exit 1; }
      CONTAINER_ID="$2"
      shift 2
      ;;
    *)
      echo "Unbekanntes Flag: $1" >&2
      echo "Nutzung: $0 <projekt-root> --edition <edition> [--use_proxy] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
      exit 1
      ;;
  esac
done

# --- Edition validieren ---------------------------------------------------------
#
# Editionen sind Dockerfile-Stages mit Namen opencode-sandbox-<name>
# (siehe ADR 0019). Die Liste wird live aus dem shipped Dockerfile und
# dem custom edition Dockerfile ermittelt - keine hardcodierte Whitelist.
[[ -n "${EDITION:-}" ]] || {
  echo "Fehler: --edition <edition> ist erforderlich." >&2
  echo "Nutzung: $0 <projekt-root> --edition <edition> [--use_proxy] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
  exit 1
}

# --- Suchmodus validieren --------------------------------------------------------
case "$SEARCH_MODE" in
  websearch|none|searxng) ;;
  *)
    echo "Fehler: Unbekannter --search_mode: $SEARCH_MODE" >&2
    echo "Erlaubt: websearch (Standard), none, searxng" >&2
    exit 1
    ;;
esac
SEARCH_WEBSEARCH=false
SEARCH_SEARXNG=false
[[ "$SEARCH_MODE" != "websearch" ]] || SEARCH_WEBSEARCH=true
[[ "$SEARCH_MODE" != "searxng" ]] || SEARCH_SEARXNG=true
# Kombinationsausschluesse (ADR 0026) VOR jedem Containerzugriff: der
# Proxy/oc-searxng darf nicht erst gestartet werden, um dann abzubrechen.
if $SEARCH_SEARXNG && $OFFLINE; then
  echo "Fehler: --search_mode searxng erfordert Netzwerk (--offline inkompatibel)" >&2
  exit 1
fi
if $SEARCH_SEARXNG && $USE_PROXY; then
  echo "Fehler: --search_mode searxng ist mit --use_proxy inkompatibel (ADR 0026:" >&2
  echo "  das SearXNG-MCP liest beliebige URLs; Suche und Egress-Allowlist schließen" >&2
  echo "  sich aus). Ohne --use_proxy starten oder --search_mode websearch verwenden." >&2
  exit 1
fi

if [[ ! "$EDITION" =~ ^[a-z0-9_-]+$ ]]; then
  echo "Fehler: Ungültiger Editions-Name: $EDITION" >&2
  echo "Erlaubt sind nur Kleinbuchstaben [a-z0-9_-]." >&2
  exit 1
fi

discover_editions() {
  for f in "${REPO_ROOT}/Dockerfile" "${REPO_ROOT}/Dockerfile.custom"; do
    [[ -f "$f" ]] || continue
    while IFS= read -r name; do
      [[ "$name" =~ ^[a-z0-9_-]+$ ]] || continue
      if [[ -z "${DISCOVERED_FILE[$name]:-}" ]]; then
        DISCOVERED_FILE[$name]="$f"
        printf '%s\n' "$name"
      elif [[ "${DISCOVERED_FILE[$name]}" != "$f" ]]; then
        echo "Warnung: Edition 'opencode-sandbox-$name' kommt in Dockerfile und Dockerfile.custom vor — Edition wird übersprungen." >&2
      fi
    done < <(sed -nE '/^[[:space:]]*#/d; s/^[[:space:]]*FROM[[:space:]]+[^[:space:]]+[[:space:]]+AS[[:space:]]+opencode-sandbox-([^[:space:]]+)[[:space:]]*(#.*)?$/\1/p' "$f" || true)
  done
}

declare -A DISCOVERED_FILE=()
AVAILABLE_EDITIONS=()
while IFS= read -r ed; do
  [[ -n "$ed" ]] && AVAILABLE_EDITIONS+=("$ed")
done < <(discover_editions)

edition_known=false
for ed in "${AVAILABLE_EDITIONS[@]:-}"; do
  if [[ "$ed" == "$EDITION" ]]; then
    edition_known=true
    break
  fi
done
if ! $edition_known; then
  echo "Fehler: Unbekannte Edition: $EDITION" >&2
  echo "Verfügbare Editionen: ${AVAILABLE_EDITIONS[*]:-}" >&2
  exit 1
fi

# --- Projekt-Root validieren ---------------------------------------------------
PROJECT_ROOT="$(realpath "$PROJECT_ROOT")"

PROJECT_DIR="${PROJECT_ROOT}/project"
CONFIG_DIR="${PROJECT_ROOT}/.opencode_config"
DATA_DIR="${PROJECT_ROOT}/.opencode_data"
SSH_DIR="${PROJECT_ROOT}/.ssh_local"
GIT_DIR="${PROJECT_ROOT}/.git_local"
CBM_DIR="${PROJECT_ROOT}/.cbm_cache"
BASH_DIR="${PROJECT_ROOT}/.bash_local"

mkdir -p "$PROJECT_DIR" "$CONFIG_DIR" "$DATA_DIR" "$SSH_DIR" "$GIT_DIR" "$GIT_DIR/gh-cli" "$GIT_DIR/glab-cli" "$CBM_DIR"
chmod 700 "$SSH_DIR" "$GIT_DIR"

# OpenCode-Web braucht ein Git-Repo als Worktree: ohne .git in project/
# faellt opencode auf das "global"-Projekt mit worktree "/" zurueck
# (Weboberflaeche: "+" disabled, kein Projekt anlegbar).
# Bestehende Projekte werden beim Start nachgepflegt (Heal).
# Ausnahme: Projekte, deren Quelle ein geklontes Repo ist (project_source
# in sandbox_config.json). Dort ist ein leeres project/.git oft noch der
# Zustand vor dem Clone - ein git init wuerde das Repo-Setup zerschieschen.
# Fehlende Datei/Field -> Heal laeuft (Rueckwaertskompatibilitaet).
PROJECT_SOURCE=""
if [[ -f "${CONFIG_DIR}/sandbox_config.json" ]]; then
  PROJECT_SOURCE="$(jq -r '.project_source // empty' "${CONFIG_DIR}/sandbox_config.json" 2>/dev/null || true)"
fi
if [[ ! -e "$PROJECT_DIR/.git" && "$PROJECT_SOURCE" != "cloned" ]]; then
  if git -C "$PROJECT_DIR" init -q 2>/dev/null; then
    echo "Hinweis: ${PROJECT_DIR} war kein Git-Repo; 'git init' nachgeholt." >&2
    echo "  (Noetig fuer OpenCode-Web, sonst worktree \"/\" und keine neuen Sessions.)" >&2
  else
    echo "Warnung: 'git init' in ${PROJECT_DIR} fehlgeschlagen - OpenCode-Web" >&2
    echo "  wird dann worktree \"/\" verwenden (keine Sessions anlegbar)." >&2
  fi
fi
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
  echo "  glab-Authentifizierung im Container." >&2
fi

if [[ ! -f "${GIT_DIR}/gh-cli/hosts.yml" ]]; then
  echo "Warnung: ${GIT_DIR}/gh-cli/hosts.yml fehlt - lege eine an (z.B. aus" >&2
  echo "  templates/git_local/gh-cli/hosts.yml kopieren), sonst fehlt die" >&2
  echo "  gh (GitHub CLI)-Authentifizierung im Container." >&2
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

if [[ -f "${GIT_DIR}/gh-cli/hosts.yml" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/gh-cli/hosts.yml")"
  if [[ "$perms" != "600" ]]; then
    echo "Warnung: ${GIT_DIR}/gh-cli/hosts.yml hat Rechte ${perms}, setze auf 600." >&2
    chmod 600 "${GIT_DIR}/gh-cli/hosts.yml"
  fi
fi

# --- OpenCode-Konfiguration (v2-Schema, ADR 0025) ------------------------------
# Bestehende Projekte: eine v1-geformte opencode.json (provider/plugin/
# permission, flaches mcp) und auth.json-Credentials werden einmalig vor dem
# Container-Start auf das v2-Schema migriert - bevor der OpenCode-Service
# das erste Mal die Konfiguration laedt. Idempotent; ein Scheitern blockiert
# den Start nicht (die Go-TUI liest beide Formate, configure-project.sh
# migriert lazy bei jedem Provider-Subcommand nach).
if [[ -f "${CONFIG_DIR}/opencode.json" ]] && command -v jq >/dev/null 2>&1; then
  bash "${SCRIPT_DIR}/configure-project.sh" migrate-opencode-config "$PROJECT_ROOT" ||
    echo "Warnung: opencode.json v2-Migration fehlgeschlagen - wiederholt sich beim naechsten Start oder beim ersten Provider-Edit." >&2
  # Suchmodus (search_mode) auf opencode.json anwenden: SearXNG-MCP
  # +/− Built-in-Websearch-Seed. Idempotent, schreibt nur bei Aenderung;
  # ein Scheitern blockiert den Start nicht.
  bash "${SCRIPT_DIR}/configure-project.sh" search-mode "$PROJECT_ROOT" --mode "$SEARCH_MODE" ||
    echo "Warnung: search-mode auf opencode.json fehlgeschlagen - wiederholt sich beim naechsten Start." >&2
fi

# --- Proxy ----------------------------------------------------------------------
if $USE_PROXY; then
  if ! podman image exists oc-proxy 2>/dev/null; then
    echo "==> Baue oc-proxy Image ..."
    podman build -t oc-proxy -f "${REPO_ROOT}/proxy/Dockerfile" "${REPO_ROOT}/proxy/"
  fi
fi

# --- Netzwerk: Pod pro Projekt (ADR 0029) ---------------------------------------
# Ein Pod pro Projekt, Netzmodus pasta:--ipv4-only. Pod-Member teilen einen
# Netz-Namespace: Companion-Dienste werden ueber 127.0.0.1 erreicht (in-
# Namespace-Konstanten) - ohne Host-Loopback-Publishing fuer Dienste und
# ohne podmans host.containers.internal, das bei pasta auf die EIGENE
# Container-Adresse zeigt (sie schattiert die Host-Adresse). Die
# Host-Publishings der UIs (Web 4096-Scan, CBM-UI 9749-Scan) haengen am
# Pod (siehe "Pod starten"), nicht mehr am Projektcontainer. Der
# ADR-0028-Detour (default-route-Gateway-Ermittlung, --add-host
# host.oc-sandbox.internal) ist damit entfallen.
SANDBOX_ID="${CONTAINER_ID:-$(basename "$PROJECT_ROOT")}"
POD_NAME="opencode-sandbox-pod-${SANDBOX_ID}"
CONTAINER_NAME="opencode-sandbox-${SANDBOX_ID}"

NETWORK_ARGS=()
if $OFFLINE; then
  echo "Hinweis: Sandbox läuft ohne Netzwerk (--offline). OpenCode-Login/API" >&2
  echo "         ist damit nicht nutzbar, nur lokale Modelle." >&2
  # Offline laeuft der Projektcontainer ohne Pod (und ohne Companion-Member):
  # --network=none klassisch, Publishings bleiben - wie bisher - am
  # Container selbst.
  NETWORK_ARGS=(--network=none)
fi

# Proxy: In-Namespace-Konstante (ADR 0029) - der per-Pod Squid-Member
# wird beim Pod-Aufbau unten gestartet.
PROXY_ENV=()
if $USE_PROXY; then
  PROXY_ENV=(
    -e HTTPS_PROXY="http://127.0.0.1:3128"
    -e HTTP_PROXY="http://127.0.0.1:3128"
    -e NO_PROXY="localhost,127.0.0.1"
  )
fi
if $OFFLINE; then
  PROXY_ENV=()
fi

# Such-Umgebung: websearch aktiviert die Built-in-Exa/Parallel-Backends
# (Standard, Rückwärtskompatibilität: fehlender --search_mode liest als
# websearch). searxng traegt nur SEARXNG_URL ein, none nichts.
WEBSEARCH_ENV=()
if $SEARCH_WEBSEARCH; then
  WEBSEARCH_ENV=(-e OPENCODE_ENABLE_EXA=1 -e OPENCODE_ENABLE_PARALLEL=1)
fi
SEARXNG_ENV=()

port_is_in_use() {
  local port="$1"

  if command -v ss >/dev/null 2>&1; then
    ss -H -ltn "sport = :${port}" 2>/dev/null | grep -q .
    return
  fi

  if command -v timeout >/dev/null 2>&1; then
    timeout 1 bash -c ": </dev/tcp/127.0.0.1/${port}" >/dev/null 2>&1
  else
    bash -c ": </dev/tcp/127.0.0.1/${port}" >/dev/null 2>&1
  fi
}

select_free_host_port() {
  local base_port="$1"
  local label="$2"
  local port="$base_port"
  local last_port=$((base_port + 100))

  while (( port <= last_port )); do
    if ! port_is_in_use "$port"; then
      printf '%s\n' "$port"
      return 0
    fi
    port=$((port + 1))
  done

  echo "Fehler: Kein freier Host-Port für ${label} im Bereich ${base_port}-${last_port}." >&2
  return 1
}

select_cbm_host_port() {
  select_free_host_port 9749 "die CBM-UI"
}

# --- Flag-Validierung ----------------------------------------------------------
if $OFFLINE && $CBM_UI; then
  echo "Fehler: --cbm_ui erfordert Netzwerk (--offline inkompatibel)" >&2
  exit 1
fi

# --- CBM-UI-Optionen -----------------------------------------------------------
CBM_PORT_ARGS=()
CBM_ENV_UI=()
if $CBM_UI; then
  CBM_HOST_PORT=$(select_cbm_host_port)
  if [[ "$CBM_HOST_PORT" != "9749" ]]; then
    echo "Hinweis: Host-Port 9749 ist belegt; CBM-UI wird auf http://127.0.0.1:${CBM_HOST_PORT} veröffentlicht." >&2
  fi
  CBM_PORT_ARGS=(-p "127.0.0.1:${CBM_HOST_PORT}:9749")
  CBM_ENV_UI=(-e CBM_UI=true)
fi

# --- OpenCode-Web-Optionen ------------------------------------------------------
WEB_PORT_ARGS=()
if $START_WEB; then
  WEB_HOST_PORT=$(select_free_host_port 4096 "die OpenCode-Weboberfläche")
  if [[ "$WEB_HOST_PORT" != "4096" ]]; then
    echo "Hinweis: Host-Port 4096 ist belegt; OpenCode Web wird auf http://127.0.0.1:${WEB_HOST_PORT} veröffentlicht." >&2
  fi
  WEB_PORT_ARGS=(-p "127.0.0.1:${WEB_HOST_PORT}:4096")
fi
# Im Detached-Modus startet der Containerbefehl den Webserver selbst;
# START_WEB=true würde über die Login-Shell den Server doppelt starten.
START_WEB_ENV=false
if $START_WEB && ! $DETACH; then
  START_WEB_ENV=true
fi

# --- SearXNG-Member (search_mode=searxng) ---------------------------------------
# Nach ADR 0029 laeuft SearXNG als per-Pod-Member im Netz-Namespace des
# Projekt-Pods (detached, oc-searxng-Image, settings.yml-Bind, generiertes
# Secret, keine Publishing, pro-Pod-eindeutiger Name; gestartet beim
# Pod-Aufbau unten). Der hostseitig geteilte oc-searxng-Container (und der
# zugehoerige Port-Scan) ist entfallen.
if $SEARCH_SEARXNG; then
  if [[ ! -f "${REPO_ROOT}/searxng/settings.yml" ]]; then
    echo "Fehler: ${REPO_ROOT}/searxng/settings.yml fehlt (siehe Install)." >&2
    exit 1
  fi
  if ! podman image exists oc-searxng 2>/dev/null; then
    echo "==> Baue oc-searxng Image ..."
    podman build --network host -t oc-searxng -f "${REPO_ROOT}/searxng/Dockerfile" "${REPO_ROOT}/searxng/"
  fi
  SEARXNG_SECRET="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  SEARXNG_NAME="oc-searxng-${SANDBOX_ID}"
  SEARXNG_ENV=(-e "SEARXNG_URL=http://127.0.0.1:8080/search")
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

# --- Pod pro Projekt (ADR 0029) ---------------------------------------------------
# Lifecycle-Entscheidung: deterministisches Recreate bei jedem Start. Ein
# `podman pod create --replace` konnte auf podman 4.9.3 nicht verifiziert
# werden (kein Podman in dieser Umgebung fuer den Probe), also der sichere
# Pfad: forciertes `podman pod rm -f` (Best-Effort, fehlender Pod/Fehler
# wird ignoriert) gefolgt von einem frischen `podman pod create` - das
# heilt auch haengengebliebene Member, statt sie zu recyceln. Die
# Pod-Level-Publishings (Web-UI-4096-Scan, CBM-UI-9749-Scan, oben
# gesammelt) haengen am Pod; der Projektcontainer tritt mit --pod bei.
if ! $OFFLINE; then
  echo "==> Lege Pod ${POD_NAME} an (Recreate) ..."
  podman pod rm -f "$POD_NAME" >/dev/null 2>&1 || true
  podman pod create --name "$POD_NAME" \
    --network pasta:--ipv4-only \
    "${CBM_PORT_ARGS[@]}" \
    "${WEB_PORT_ARGS[@]}"
  POD_ARGS=(--pod "$POD_NAME")
  CONTAINER_PORT_ARGS=()
  if $SEARCH_SEARXNG; then
    echo "==> Starte oc-searxng Pod-Member (${SEARXNG_NAME}) ..."
    podman run -d \
      --pod "$POD_NAME" \
      --name "$SEARXNG_NAME" \
      -e "SEARXNG_SECRET=${SEARXNG_SECRET}" \
      -v "${REPO_ROOT}/searxng/settings.yml:/etc/searxng/settings.yml:Z" \
      oc-searxng
  fi
  if $USE_PROXY; then
    PROXY_NAME="oc-proxy-${SANDBOX_ID}"
    echo "==> Starte oc-proxy Pod-Member (${PROXY_NAME}) ..."
    podman run -d \
      --pod "$POD_NAME" \
      --name "$PROXY_NAME" \
      oc-proxy
  fi
else
  # Offline: kein Pod, kein Member - Publishings bleiben (wie vor der
  # Umstellung) am Projektcontainer.
  POD_ARGS=()
  CONTAINER_PORT_ARGS=("${CBM_PORT_ARGS[@]}" "${WEB_PORT_ARGS[@]}")
fi

# --- Container starten ---------------------------------------------------------
# v2-Web: Hostname/Port werden bootunabhaengig in die gemountete
# service-config vorgesehen (ADR 0024), damit der Container start nur noch
# `service start` braucht. Die alten Aufrufe `service set hostname/port`
# stoppen jeweils einen laufenden Service (ServiceConfig.set -> Service.stop)
# und koennen die Bootkette abbrechen ("Server process ... is still running"),
# sobald ein Container mit lebendem Service neu startet.
if $START_WEB && $DETACH; then
  SERVICE_FILE="${CONFIG_DIR}/service.json"
  if [[ ! -f "$SERVICE_FILE" ]]; then
    printf '{"hostname": "0.0.0.0", "port": 4096}\n' > "$SERVICE_FILE"
  elif ! jq -e '.hostname == "0.0.0.0" and .port == 4096' "$SERVICE_FILE" >/dev/null 2>&1; then
    tmp_service_json="$(mktemp)"
    jq '."hostname" = "0.0.0.0" | ."port" = 4096' "$SERVICE_FILE" > "$tmp_service_json" \
      && mv "$tmp_service_json" "$SERVICE_FILE"
  fi
fi

IMAGE="opencode-sandbox-${EDITION}"
RUN_MODE=(-it)
CONTAINER_COMMAND=()
if $DETACH; then
  RUN_MODE=(-d)
  if $START_WEB; then
    # v2: shared background service pinned to container port 4096, then
    # keep-alive (ADR 0024). Published host port comes from the 4096 scan.
    # Hostname/Port stehen schon in service.json (siehe oben) - der Start
    # selbst ist ein einziger idempotenter Ensure.
    CONTAINER_COMMAND=(-c 'opencode service start && exec sleep infinity')
  else
    CONTAINER_COMMAND=(-c 'sleep infinity')
  fi
fi

if $START_WEB && ! $DETACH; then
  echo "OpenCode Web: http://127.0.0.1:${WEB_HOST_PORT} (Strg+C beendet den Server)"
fi

podman run --rm "${RUN_MODE[@]}" \
  --name "$CONTAINER_NAME" \
  --userns=keep-id \
  --group-add keep-groups \
  --cap-drop=ALL \
  --security-opt no-new-privileges \
  "${POD_ARGS[@]}" \
  "${NETWORK_ARGS[@]}" \
  "${PROXY_ENV[@]}" \
  "${CONTAINER_PORT_ARGS[@]}" \
  "${CBM_ENV_UI[@]}" \
  "${DEVICE_ARGS[@]}" \
  -e TERM="${TERM:-xterm-256color}" \
  -e XDG_CONFIG_HOME="/home/dev/.config" \
  -e XDG_DATA_HOME="/home/dev/.local/share" \
  -e GIT_CONFIG_GLOBAL="/home/dev/.git_local/gitconfig" \
  -e START_OPENCODE="${START_OPENCODE}" \
  -e START_WEB="${START_WEB_ENV}" \
  "${WEBSEARCH_ENV[@]}" \
  "${SEARXNG_ENV[@]}" \
  -v "${PROJECT_DIR}:/home/dev/project:Z" \
  -v "${CONFIG_DIR}:/home/dev/.config/opencode:Z" \
  -v "${DATA_DIR}:/home/dev/.local/share/opencode:Z" \
  -v "${SSH_DIR}:/home/dev/.ssh:Z,ro" \
  -v "${GIT_DIR}:/home/dev/.git_local:Z" \
  -v "${CBM_DIR}:/home/dev/.cache/codebase-memory-mcp:Z" \
  -v "${BASH_DIR}/bash_profile:/home/dev/.bash_profile:Z" \
  "$IMAGE" "${CONTAINER_COMMAND[@]}"

if $START_WEB && $DETACH; then
  echo "OpenCode Web: http://127.0.0.1:${WEB_HOST_PORT}"
  echo "Anmeldung (einmaliger Pair-Link): podman exec -it $CONTAINER_NAME opencode pair"
fi
