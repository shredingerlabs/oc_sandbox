#!/usr/bin/env bash
#
# Starts the OpenCode sandbox (podman rootless, hardened).
#
# One container for every use case: coding, HIL tests, offline, proxy.
# Modes are chosen per flags, not per separate image.
#
# Expects ONE workspace root with the following structure:
#
#   <WORKSPACE_ROOT>/
#     workspace/          <- repo, mounted at /home/dev/workspace
#     .opencode_config/    <- OpenCode config, persistent per workspace
#     .opencode_data/       <- OpenCode data incl. auth/credentials
#     .ssh_local/           <- SSH keys + config for this workspace
#     .git_local/           <- git identity/settings + optional credentials
#     .cbm_cache/           <- CBM graph database (persistent)
#
# Flags:
#   --use_proxy        starts the Squid egress-allowlist proxy and uses it
#   --search_mode      web search: websearch (Exa/Parallel, default), none
#                      or searxng (oc-searxng pod member + SearXNG MCP;
#                      not combinable with --use_proxy, ADR 0026)
#   --offline          no network (local models only)
#   --hil_mode         USB passthrough for oscilloscope (scope0) and MCU (ttyUSB*, ttyACM*, ttyAMA*)
#   --cbm_ui           enables the CBM graph UI on port 9749
#   --start_opencode   starts OpenCode directly after container start
#   --start_web        starts OpenCode web (port 4096) after container start
#   --edition          container edition (e.g. built via build-container.sh)
#   --detach            start the container in the background
#   --container-id      persisted workspace container identity
#
# Beispiele:
#   scripts/start.sh ~/workspaces/customer-x                            # Default (volle Netzanbindung)
#   scripts/start.sh ~/workspaces/customer-x --use_proxy                 # Mit Egress-Allowlist
#   scripts/start.sh ~/workspaces/customer-x --offline                   # Ohne Netzwerk
#   scripts/start.sh ~/workspaces/customer-x --search_mode searxng       # SearXNG-Websuche
#   scripts/start.sh ~/workspaces/customer-x --hil_mode                  # HIL-Tests
#   scripts/start.sh ~/workspaces/customer-x --use_proxy --hil_mode      # Kombiniert
#   scripts/start.sh ~/workspaces/customer-x --cbm_ui                    # Mit CBM Graph-UI
#   scripts/start.sh ~/workspaces/customer-x --start_opencode            # Mit direktem OpenCode-Start
#   scripts/start.sh ~/workspaces/customer-x --start_web --detach        # Als Webserver
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Flags parsen -------------------------------------------------------------
WORKSPACE_ROOT="${1:-}"
if [[ "$WORKSPACE_ROOT" == "--help" || "$WORKSPACE_ROOT" == "-h" ]]; then
  echo "Usage: $0 <workspace-root> --edition <edition> [Flags]"
  echo "  --edition          select the container edition (required)"
  echo "  --use_proxy        start and use the Squid egress-allowlist proxy"
  echo "  --search_mode <m>  web search: websearch (default), none, searxng"
  echo "  --offline          no network (--network=none)"
  echo "  --hil_mode         USB passthrough for oscilloscope and MCU devices"
  echo "  --cbm_ui           enable the CBM graph UI on port 9749"
  echo "  --start_opencode   start OpenCode directly after container start"
  echo "  --start_web        start and publish OpenCode web (port 4096)"
  echo "  --detach           start the container in the background"
  echo "  --container-id     persisted workspace container identity"
  exit 0
fi
if [[ -z "$WORKSPACE_ROOT" ]]; then
  echo "Usage: $0 <workspace-root> --edition <edition> [--use_proxy] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
  echo "" >&2
  echo "  <workspace-root>        path to the workspace root (see init-workspace.sh)" >&2
  echo "  --edition <edition>   select the container edition (required)" >&2
  echo "  --use_proxy           start and use the Squid egress-allowlist proxy" >&2
  echo "  --search_mode <m>     web search: websearch (default), none, searxng" >&2
  echo "  --offline             no network (--network=none)" >&2
  echo "  --hil_mode            USB passthrough for oscilloscope + MCU devices" >&2
  echo "  --cbm_ui              enable the CBM graph UI on port 9749" >&2
  echo "  --start_opencode      start OpenCode directly after container start" >&2
  echo "  --start_web           start and publish OpenCode web (port 4096)" >&2
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
      [[ $# -ge 2 ]] || { echo "--edition requires a value" >&2; exit 1; }
      EDITION="$2"
      shift 2
      ;;
    --use_proxy)        USE_PROXY=true; shift ;;
    --search_mode)
      [[ $# -ge 2 ]] || { echo "--search_mode requires a value" >&2; exit 1; }
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
      [[ $# -ge 2 ]] || { echo "--container-id requires a value" >&2; exit 1; }
      CONTAINER_ID="$2"
      shift 2
      ;;
    *)
      echo "Unknown flag: $1" >&2
      echo "Usage: $0 <workspace-root> --edition <edition> [--use_proxy] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
      exit 1
      ;;
  esac
done

# --- Edition validation ---------------------------------------------------------
#
# Editions are Dockerfile stages named opencode-sandbox-<name>
# (see ADR 0019). The list is discovered live from the shipped Dockerfile
# and the custom edition Dockerfile — no hardcoded whitelist.
[[ -n "${EDITION:-}" ]] || {
  echo "Error: --edition <edition> is required." >&2
  echo "Usage: $0 <workspace-root> --edition <edition> [--use_proxy] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
  exit 1
}

# --- Search mode validation --------------------------------------------------------
case "$SEARCH_MODE" in
  websearch|none|searxng) ;;
  *)
    echo "Error: unknown --search_mode: $SEARCH_MODE" >&2
    echo "Allowed: websearch (default), none, searxng" >&2
    exit 1
    ;;
esac
SEARCH_WEBSEARCH=false
SEARCH_SEARXNG=false
[[ "$SEARCH_MODE" != "websearch" ]] || SEARCH_WEBSEARCH=true
[[ "$SEARCH_MODE" != "searxng" ]] || SEARCH_SEARXNG=true
# Combination exclusions (ADR 0026) BEFORE any container access: the
# proxy/oc-searxng must not be started just to abort afterwards.
if $SEARCH_SEARXNG && $OFFLINE; then
  echo "Error: --search_mode searxng requires network (incompatible with --offline)" >&2
  exit 1
fi
if $SEARCH_SEARXNG && $USE_PROXY; then
  echo "Error: --search_mode searxng is incompatible with --use_proxy (ADR 0026:" >&2
  echo "  the SearXNG MCP fetches arbitrary URLs; search and the egress allowlist" >&2
  echo "  exclude each other). Start without --use_proxy or use --search_mode websearch." >&2
  exit 1
fi

if [[ ! "$EDITION" =~ ^[a-z0-9_-]+$ ]]; then
  echo "Error: invalid edition name: $EDITION" >&2
  echo "Only lowercase [a-z0-9_-] is allowed." >&2
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
        echo "Warning: edition 'opencode-sandbox-$name' appears in both Dockerfile and Dockerfile.custom — edition skipped." >&2
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
  echo "Error: unknown edition: $EDITION" >&2
  echo "Available editions: ${AVAILABLE_EDITIONS[*]:-}" >&2
  exit 1
fi

# --- Workspace root validation -------------------------------------------------
WORKSPACE_ROOT="$(realpath "$WORKSPACE_ROOT")"

WORKSPACE_DIR="${WORKSPACE_ROOT}/workspace"
CONFIG_DIR="${WORKSPACE_ROOT}/.opencode_config"
DATA_DIR="${WORKSPACE_ROOT}/.opencode_data"
SSH_DIR="${WORKSPACE_ROOT}/.ssh_local"
GIT_DIR="${WORKSPACE_ROOT}/.git_local"
CBM_DIR="${WORKSPACE_ROOT}/.cbm_cache"
BASH_DIR="${WORKSPACE_ROOT}/.bash_local"

mkdir -p "$WORKSPACE_DIR" "$CONFIG_DIR" "$DATA_DIR" "$SSH_DIR" "$GIT_DIR" "$GIT_DIR/gh-cli" "$GIT_DIR/glab-cli" "$CBM_DIR"
chmod 700 "$SSH_DIR" "$GIT_DIR"

# OpenCode web needs a git repo as worktree: without .git in workspace/
# opencode falls back to the "global" project with worktree "/" (web UI:
# "+" disabled, no new sessions). Existing workspaces are healed at
# start. Exception: workspaces whose source is a cloned repo
# (workspace_source in sandbox_config.json): an empty workspace/.git is
# often the pre-clone state — a git init would destroy the repo setup.
# Missing file/field -> the heal runs (backward compatibility).
# Fehlende Datei/Field -> Heal laeuft (Rueckwaertskompatibilitaet).
WORKSPACE_SOURCE=""
if [[ -f "${CONFIG_DIR}/sandbox_config.json" ]]; then
  WORKSPACE_SOURCE="$(jq -r '.workspace_source // empty' "${CONFIG_DIR}/sandbox_config.json" 2>/dev/null || true)"
fi
if [[ ! -e "$WORKSPACE_DIR/.git" && "$WORKSPACE_SOURCE" != "cloned" ]]; then
  if git -C "$WORKSPACE_DIR" init -q 2>/dev/null; then
    echo "Note: ${WORKSPACE_DIR} was not a git repo; ran \"git init\" (heal)." >&2
    echo "  (Needed for OpenCode web; otherwise worktree \"/\" and no new sessions.)" >&2
  else
    echo "Warning: 'git init' in ${WORKSPACE_DIR} failed — OpenCode web" >&2
    echo "  will use worktree \"/\" (no sessions can be created)." >&2
  fi
fi
chmod 700 "$SSH_DIR" "$GIT_DIR"

if [[ -z "$(find "$SSH_DIR" -maxdepth 1 -type f 2>/dev/null)" ]]; then
  echo "Warning: ${SSH_DIR} contains no files (no keys/config)." >&2
  echo "  Git push/pull over SSH will fail without keys." >&2
  echo "  Template: templates/ssh_local/config; create keys with:" >&2
  echo "    ssh-keygen -t ed25519 -f ${SSH_DIR}/id_ed25519_github -N \"\"" >&2
fi

if [[ ! -f "${GIT_DIR}/gitconfig" ]]; then
  echo "Warning: ${GIT_DIR}/gitconfig is missing — create one (e.g. copy" >&2
  echo "  from templates/git_local/gitconfig), otherwise user.name/" >&2
  echo "  user.email are missing in the container." >&2
fi

if [[ ! -f "${GIT_DIR}/glab-cli/config.yml" ]]; then
  echo "Warning: ${GIT_DIR}/glab-cli/config.yml is missing — create one (e.g. copy" >&2
  echo "  from templates/git_local/glab-cli/config.yml), otherwise the" >&2
  echo "  glab authentication is missing in the container." >&2
fi

if [[ ! -f "${GIT_DIR}/gh-cli/hosts.yml" ]]; then
  echo "Warning: ${GIT_DIR}/gh-cli/hosts.yml is missing — create one (e.g. copy" >&2
  echo "  from templates/git_local/gh-cli/hosts.yml), otherwise the" >&2
  echo "  gh (GitHub CLI) authentication is missing in the container." >&2
fi

while IFS= read -r -d '' key; do
  perms="$(stat -c '%a' "$key")"
  if [[ "$perms" != "600" && "$perms" != "400" ]]; then
    echo "Warnung: ${key} has mode ${perms}; SSH expects 600. Fix: chmod 600 ${key}" >&2
  fi
done < <(find "$SSH_DIR" -maxdepth 1 -type f -name 'id_*' ! -name '*.pub' -print0 2>/dev/null)

if [[ -f "${GIT_DIR}/credentials" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/credentials")"
  if [[ "$perms" != "600" && "$perms" != "400" ]]; then
    echo "Warnung: ${GIT_DIR}/credentials has mode ${perms}; it should be 600." >&2
  fi
fi

if [[ -f "${GIT_DIR}/glab-cli/config.yml" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/glab-cli/config.yml")"
  if [[ "$perms" != "600" ]]; then
    echo "Warnung: ${GIT_DIR}/glab-cli/config.yml has mode ${perms}; setting it to 600." >&2
    chmod 600 "${GIT_DIR}/glab-cli/config.yml"
  fi
fi

if [[ -f "${GIT_DIR}/gh-cli/hosts.yml" ]]; then
  perms="$(stat -c '%a' "${GIT_DIR}/gh-cli/hosts.yml")"
  if [[ "$perms" != "600" ]]; then
    echo "Warnung: ${GIT_DIR}/gh-cli/hosts.yml has mode ${perms}; setting it to 600." >&2
    chmod 600 "${GIT_DIR}/gh-cli/hosts.yml"
  fi
fi

# --- OpenCode-Konfiguration (v2-Schema, ADR 0025) ------------------------------
# Existing installations: a v1-shaped opencode.json (provider/plugin/
# permission, flat mcp) and auth.json credentials are migrated to the
# v2 schema once before container start — before the OpenCode service
# loads the config for the first time. Idempotent; a failure does not
# block the start (configure-workspace.sh re-migrates lazily on the
# next start or the first provider edit).
if [[ -f "${CONFIG_DIR}/opencode.json" ]] && command -v jq >/dev/null 2>&1; then
  bash "${SCRIPT_DIR}/configure-workspace.sh" migrate-opencode-config "$WORKSPACE_ROOT" ||
    echo "Warning: opencode.json v2 migration failed — it retries on the next start or the first provider edit." >&2
  # Apply search_mode to opencode.json: SearXNG MCP +/- the built-in
  # websearch seed. Idempotent, writes only on change; failure does not
  # block the start.
  bash "${SCRIPT_DIR}/configure-workspace.sh" search-mode "$WORKSPACE_ROOT" --mode "$SEARCH_MODE" ||
    echo "Warning: applying search-mode to opencode.json failed — it retries on the next start." >&2
fi

# --- Proxy ----------------------------------------------------------------------
# The proxy image is rebuilt on every proxy start: the copy layer for
# squid.conf/allowlist.txt stays fresh on every change — a stale oc-proxy
# image (built once, never rebuilt) would silently let allowlist changes
# slip past the running editions (bug: a bungled allowlist killed the
# Squid member quietly instead of failing loudly at build).
if $USE_PROXY; then
  echo "==> Building/updating the oc-proxy image ..."
  podman build -t oc-proxy -f "${REPO_ROOT}/proxy/Dockerfile" "${REPO_ROOT}/proxy/"
fi

# --- Network: one pod per workspace (ADR 0029) ----------------------------------
# One pod per workspace, network mode pasta:--ipv4-only. Pod members share
# one network namespace: companion services are reached via 127.0.0.1
# (in-namespace constants) — no host loopback publishing for services and
# no podman host.containers.internal, which under pasta points at the
# container's OWN address (it shadows the host address). The UI host
# publishings (web 4096 scan, CBM UI 9749 scan) live on the pod (see "pod
# create" below), no longer on the workspace container. The ADR-0028 detour
# (default-route gateway discovery, --add-host host.oc-sandbox.internal)
# is gone.
SANDBOX_ID="${CONTAINER_ID:-$(basename "$WORKSPACE_ROOT")}"
POD_NAME="opencode-sandbox-pod-${SANDBOX_ID}"
CONTAINER_NAME="opencode-sandbox-${SANDBOX_ID}"

NETWORK_ARGS=()
if $OFFLINE; then
  echo "Note: sandbox runs without network (--offline). OpenCode login/API" >&2
  echo "         are not usable; local models only." >&2
  # Offline the workspace container runs without a pod (and without
  # companion members): plain --network=none; publishings stay — as before
  # — on the container itself.
  NETWORK_ARGS=(--network=none)
fi

# Proxy in-namespace constant (ADR 0029) — the per-pod Squid member is
# started during pod creation below.
# NO_PROXY includes 0.0.0.0: opencode v2 hangs its managed-service URLs on
# the pinned service.json hostname (0.0.0.0 with the web-pinned config,
# ADR 0024). Without this entry the service handshakes/health probes of
# background-service discovery go through the Squid allowlist proxy (it
# denies them), the client considers the running service "missing",
# colliding `opencode serve --service` children instead start on 4096 and
# abort with "Managed service port 4096 on 0.0.0.0 is already in use".
# 0.0.0.0 ist nur als Zieladresse des In-Container-Service wirksam - ein
# Egress-Kanal des Agents bleibt gebunden (der Proxy ist die Regel).
PROXY_ENV=()
if $USE_PROXY; then
  PROXY_ENV=(
    -e HTTPS_PROXY="http://127.0.0.1:3128"
    -e HTTP_PROXY="http://127.0.0.1:3128"
    -e NO_PROXY="localhost,127.0.0.1,0.0.0.0"
  )
fi
if $OFFLINE; then
  PROXY_ENV=()
fi

# Search environment: websearch enables the built-in Exa/Parallel backends
# (default; backward compat: a missing --search_mode reads as
# websearch). searxng only sets SEARXNG_URL, none sets nothing.
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

  echo "Error: no free host port for ${label} in range ${base_port}-${last_port}." >&2
  return 1
}

select_cbm_host_port() {
  select_free_host_port 9749 "the CBM UI"
}

# --- Flag-Validierung ----------------------------------------------------------
if $OFFLINE && $CBM_UI; then
  echo "Error: --cbm_ui requires network (incompatible with --offline)" >&2
  exit 1
fi

# --- CBM-UI-Optionen -----------------------------------------------------------
CBM_PORT_ARGS=()
CBM_ENV_UI=()
if $CBM_UI; then
  CBM_HOST_PORT=$(select_cbm_host_port)
  if [[ "$CBM_HOST_PORT" != "9749" ]]; then
    echo "Note: host port 9749 is in use; the CBM UI is published at http://127.0.0.1:${CBM_HOST_PORT}." >&2
  fi
  CBM_PORT_ARGS=(-p "127.0.0.1:${CBM_HOST_PORT}:9749")
  CBM_ENV_UI=(-e CBM_UI=true)
fi

# --- OpenCode-Web-Optionen ------------------------------------------------------
WEB_PORT_ARGS=()
if $START_WEB; then
  WEB_HOST_PORT=$(select_free_host_port 4096 "the OpenCode web UI")
  if [[ "$WEB_HOST_PORT" != "4096" ]]; then
    echo "Note: host port 4096 is in use; OpenCode web is published at http://127.0.0.1:${WEB_HOST_PORT}." >&2
  fi
  WEB_PORT_ARGS=(-p "127.0.0.1:${WEB_HOST_PORT}:4096")
fi
# In detached mode the container command starts the webserver itself;
# START_WEB=true would start the server twice through the login shell.
START_WEB_ENV=false
if $START_WEB && ! $DETACH; then
  START_WEB_ENV=true
fi

# --- SearXNG member (search_mode=searxng) ---------------------------------------
# Per ADR 0029 SearXNG runs as a per-pod member in the network namespace
# of the workspace pod (detached, oc-searxng image, settings.yml bind,
# generated secret, no publishings, unique per-pod name; started during
# pod creation below). The host-shared oc-searxng container (and its port
# scan) is gone.
if $SEARCH_SEARXNG; then
  if [[ ! -f "${REPO_ROOT}/searxng/settings.yml" ]]; then
    echo "Error: ${REPO_ROOT}/searxng/settings.yml is missing (see install)." >&2
    exit 1
  fi
  if ! podman image exists oc-searxng 2>/dev/null; then
    echo "==> Building the oc-searxng image ..."
    podman build --network host -t oc-searxng -f "${REPO_ROOT}/searxng/Dockerfile" "${REPO_ROOT}/searxng/"
  fi
  SEARXNG_SECRET="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  SEARXNG_NAME="oc-searxng-${SANDBOX_ID}"
  # Without a /search suffix: mcp-searxng appends /search itself (otherwise /search/search -> 404).
  SEARXNG_ENV=(-e "SEARXNG_URL=http://127.0.0.1:8080")
fi

# --- HIL devices ----------------------------------------------------------------
#
# Important: devices are NO longer bound as individual --device snapshots.
# Reason: --device binds a fixed major:minor combination at container
# start. When USB re-enumerates later (new bus/device number, e.g. because
# the PicoScope re-loads firmware on (re-)connect, because of USB
# autosuspend, or because an MCU board is reset), the snapshot in the
# container points nowhere (rights show as "c---------"), although the
# device keeps working under the new path on the host.
DEVICE_ARGS=()
if $HIL_MODE; then

  # --- PicoScope (libusb-based: libps2000/libps2000a read the
  #     /dev/bus/usb/<Bus>/ directory themselves via readdir and open the
  #     matching device node there. A symlink elsewhere (e.g.
  #     /dev/hil/scope0) does NOT help — libusb only knows the standard
  #     path /dev/bus/usb/*.
  #     We therefore resolve the bus number at runtime (stable as long as
  #     the device stays on the same physical port — see
  #     udev/99-hil.rules for the autosuspend fix against sporadic
  #     re-enumeration) and mount ONLY this one bus directory live, not
  #     all of /dev/bus/usb. That gives access to everything currently
  #     attached to this bus/hub (libusb cannot be more granular), but
  #     not to the host's other USB buses.
  if [[ -e /dev/scope0 ]]; then
    SCOPE_REAL=$(readlink -f /dev/scope0)          # z.B. /dev/bus/usb/007/055
    SCOPE_BUS_DIR="$(dirname "$SCOPE_REAL")"        # z.B. /dev/bus/usb/007
    if [[ -d "$SCOPE_BUS_DIR" ]]; then
      DEVICE_ARGS+=(-v "${SCOPE_BUS_DIR}:${SCOPE_BUS_DIR}")
      # No --device-cgroup-rule: in rootless mode (user namespace) there
      # is no usable devices cgroup controller; podman rejects the flag
      # ("not supported in rootless mode"). That is harmless because
      # rootless containers anyway only access devices through ordinary
      # Unix rights (DAC) — the cgroup layer is gone entirely. Access is
      # granted solely by the bind mount + the group membership inherited
      # via --userns=keep-id/--group-add keep-groups (dialout, see
      # udev/99-hil.rules).
    fi
  else
    echo "Note: /dev/scope0 is missing. That is a side effect, not a blocker:" >&2
    echo "         - the udev rule (udev/99-hil.rules) is not installed on the host, OR" >&2
    echo "         - the oscilloscope is not connected (the bus number is resolved" >&2
    echo "           afterwards; restart the container once the device is there)." >&2
  fi

  # --- Serial HIL devices (ttyUSB/ttyACM): on every ADD event (also
  #     after reset/replug) udev creates a real device node under the
  #     current kernel name plus a stable vendor/product/serial symlink
  #     in /dev/hil/ (see udev/99-hil.rules). The directory is mounted
  #     live, so every change is visible in the container immediately —
  #     no container restart needed after a reset.
  if [[ -d /dev/hil ]]; then
    DEVICE_ARGS+=(-v /dev/hil:/dev/hil)
  else
    echo "Note: /dev/hil is missing. That is a side effect, not a blocker:" >&2
    echo "         - the udev rule (udev/99-hil.rules) is not installed on the host, OR" >&2
    echo "         - no matching device was ever detected (udev creates the" >&2
    echo "           directory automatically on the first ADD event)." >&2
  fi

  # onboard UART (not USB, does not re-enumerate) still passes through classically
  for dev in /dev/ttyAMA*; do
    [[ -e "$dev" ]] && DEVICE_ARGS+=(--device="$dev")
  done
fi

# --- One pod per workspace (ADR 0029) --------------------------------------------
# Lifecycle decision: deterministic recreate on every start.
# `podman pod create --replace` could not be verified on podman 4.9.3
# (no podman in this environment for the probe), so the safe path: force
# `podman pod rm -f` (best effort, a missing pod/error is ignored) then a
# fresh `podman pod create` — this also heals stuck members instead of
# recycling them. The pod-level publishings (web UI 4096 scan, CBM UI
# 9749 scan, collected above) live on the pod; the workspace container
# joins with --pod.
if ! $OFFLINE; then
  echo "==> Creating pod ${POD_NAME} (recreate) ..."
  podman pod rm -f "$POD_NAME" >/dev/null 2>&1 || true
  podman pod create --name "$POD_NAME" \
    --userns=keep-id \
    --network pasta:--ipv4-only \
    "${CBM_PORT_ARGS[@]}" \
    "${WEB_PORT_ARGS[@]}"
  POD_ARGS=(--pod "$POD_NAME")
  CONTAINER_PORT_ARGS=()
  USERNS_ARGS=()
  # Pod create inherits --userns=keep-id for the shared namespace: pod
  # members may not set --userns on podman 4.9 ("--userns and --pod
  # cannot be set together") so the workspace container inherits keep-id
  # from the pod.
  if $SEARCH_SEARXNG; then
    echo "==> Starting the oc-searxng pod member (${SEARXNG_NAME}) ..."
    podman run -d \
      --pod "$POD_NAME" \
      --name "$SEARXNG_NAME" \
      -e "SEARXNG_SECRET=${SEARXNG_SECRET}" \
      -v "${REPO_ROOT}/searxng/settings.yml:/etc/searxng/settings.yml:Z" \
      oc-searxng
  fi
  if $USE_PROXY; then
    PROXY_NAME="oc-proxy-${SANDBOX_ID}"
    echo "==> Starting the oc-proxy pod member (${PROXY_NAME}) ..."
    podman run -d \
      --pod "$POD_NAME" \
      --name "$PROXY_NAME" \
      oc-proxy
  fi
else
  # Offline: no pod, no members — publishings stay (as before the change)
  # on the workspace container.
  POD_ARGS=()
  CONTAINER_PORT_ARGS=("${CBM_PORT_ARGS[@]}" "${WEB_PORT_ARGS[@]}")
  USERNS_ARGS=(--userns=keep-id)
  USERNS_POD_ARGS=()
fi

# --- Container starten ---------------------------------------------------------
# v2 web: hostname/port are provisioned boot-independently into the
# mounted service config (ADR 0024), so container start only needs
# `service start`. The old `service set hostname/port` calls each stop a
# running service (ServiceConfig.set -> Service.stop) and can break the
# boot chain ("Server process ... is still running") as soon as a
# container with a live service restarts.
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
    # Hostname/Port already sit in service.json (above) — the start itself
    # is a single idempotent ensure.
    CONTAINER_COMMAND=(-c 'opencode service start && exec sleep infinity')
  else
    CONTAINER_COMMAND=(-c 'sleep infinity')
  fi
fi

if $START_WEB && ! $DETACH; then
  echo "OpenCode web: http://127.0.0.1:${WEB_HOST_PORT} (Ctrl+C stops the server)"
fi

podman run --rm "${RUN_MODE[@]}" \
  --name "$CONTAINER_NAME" \
  "${USERNS_ARGS[@]}" \
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
  -v "${WORKSPACE_DIR}:/home/dev/workspace:Z" \
  -v "${CONFIG_DIR}:/home/dev/.config/opencode:Z" \
  -v "${DATA_DIR}:/home/dev/.local/share/opencode:Z" \
  -v "${SSH_DIR}:/home/dev/.ssh:Z,ro" \
  -v "${GIT_DIR}:/home/dev/.git_local:Z" \
  -v "${CBM_DIR}:/home/dev/.cache/codebase-memory-mcp:Z" \
  -v "${BASH_DIR}/bash_profile:/home/dev/.bash_profile:Z" \
  "$IMAGE" "${CONTAINER_COMMAND[@]}"

if $START_WEB && $DETACH; then
  echo "OpenCode Web: http://127.0.0.1:${WEB_HOST_PORT}"
  echo "Sign-in (one-time pair link): podman exec -it $CONTAINER_NAME opencode pair"
fi
