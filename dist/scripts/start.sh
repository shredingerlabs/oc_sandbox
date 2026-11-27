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
#     .opencode_data/       <- OpenCode data incl. auth/credentials and plugin cache
#     .ssh_local/           <- SSH keys + config for this workspace
#     .git_local/           <- git identity/settings + optional credentials
#     .cbm_cache/           <- CBM graph database (persistent)
#
# Flags:
#   --use_proxy        starts the Squid egress-allowlist proxy and uses it
#   --use_stt          starts the oc-stt pod member (local dictation,
#                      whisper.cpp; never under --offline)
#   --search_mode      web search: websearch (Exa/Parallel, default), none
#                      or searxng (oc-searxng pod member + SearXNG MCP;
#                      not combinable with --use_proxy, ADR 0026)
#   --offline          no network (local models only)
#   --hil_mode         USB passthrough for oscilloscope (scope0) and MCU (ttyUSB*, ttyACM*, ttyAMA*)
#   --cbm_ui           enables the CBM graph UI on port 9749
#   --start_opencode   starts OpenCode directly after container start
#   --start_web        starts OpenCode web (port 4096) after container start
#   --toolchains       comma-separated toolchain ids (order-free, validated
#                      against live discovery; "" = the base-only set) — the
#                      container runs the derived opencode-sandbox-set:<hash12>
#   --edition          alias for --toolchains: expands via the manifest bundle;
#                      both flags together are refused
#   --detach            start the container in the background
#   --container-id      persisted workspace container identity
#
# Beispiele:
#   scripts/start.sh ~/workspaces/customer-x                            # default (full network)
#   scripts/start.sh ~/workspaces/customer-x --use_proxy                  # with the egress allowlist
#   scripts/start.sh ~/workspaces/customer-x --offline                    # no network
#   scripts/start.sh ~/workspaces/customer-x --search_mode searxng        # SearXNG web search
#   scripts/start.sh ~/workspaces/customer-x --hil_mode                   # HIL tests
#   scripts/start.sh ~/workspaces/customer-x --use_proxy --hil_mode       # combined
#   scripts/start.sh ~/workspaces/customer-x --cbm_ui                     # with the CBM graph UI
#   scripts/start.sh ~/workspaces/customer-x --start_opencode             # with a direct OpenCode start
#   scripts/start.sh ~/workspaces/customer-x --start_web --detach         # as a web server
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# The toolchains tree (base recipe, recipes/, manifest.json) feeds the set-tag
# derivation (lib-identity.sh, golden-vector-pinned to the Go side) and the
# live-discovery validation.
TOOLCHAINS_ROOT="${TOOLCHAINS_ROOT:-$REPO_ROOT/toolchains}"

# --- Flags parsen -------------------------------------------------------------
WORKSPACE_ROOT="${1:-}"
if [[ "$WORKSPACE_ROOT" == "--help" || "$WORKSPACE_ROOT" == "-h" ]]; then
  echo "Usage: $0 <workspace-root> --toolchains <ids> | --edition <name> [Flags]"
  echo "  --toolchains <ids>  comma-separated toolchain ids (order-free; \"\" ="
  echo "                      base-only set); the container runs the derived"
  echo "                      opencode-sandbox-set:<hash12> image"
  echo "  --edition <name>    alias: expands via the manifest bundle; both flags"
  echo "                      together are refused"
  echo "  --use_proxy        start and use the Squid egress-allowlist proxy"
  echo "  --use_stt          start the oc-stt pod member (local dictation)"
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
  echo "Usage: $0 <workspace-root> --toolchains <ids> | --edition <name> [--use_proxy] [--use_stt] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
  echo "" >&2
  echo "  <workspace-root>        path to the workspace root (see init-workspace.sh)" >&2
  echo "  --toolchains <ids>    comma-separated toolchain ids (order-free; \"\" = base-only set)" >&2
  echo "  --edition <name>      alias: expands via the manifest bundle" >&2
  echo "  --use_proxy           start and use the Squid egress-allowlist proxy" >&2
  echo "  --use_stt             start the oc-stt pod member (local dictation)" >&2
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
USE_STT=false
SEARCH_MODE="websearch"
OFFLINE=false
HIL_MODE=false
CBM_UI=false
START_OPENCODE=false
START_WEB=false
DETACH=false
CONTAINER_ID=""
TOOLCHAINS_FLAG=""
EDITION_FLAG=""
TOOLCHAINS_GIVEN=0
EDITION_GIVEN=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --toolchains)
      [[ $# -ge 2 ]] || { echo "--toolchains requires a value" >&2; exit 1; }
      TOOLCHAINS_FLAG="$2"
      TOOLCHAINS_GIVEN=1
      shift 2
      ;;
    --edition)
      [[ $# -ge 2 ]] || { echo "--edition requires a value" >&2; exit 1; }
      EDITION_FLAG="$2"
      EDITION_GIVEN=1
      shift 2
      ;;
    --use_proxy)        USE_PROXY=true; shift ;;
    --use_stt)          USE_STT=true; shift ;;
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
      echo "Usage: $0 <workspace-root> --toolchains <ids> | --edition <name> [--use_proxy] [--use_stt] [--search_mode <none|websearch|searxng>] [--offline] [--hil_mode] [--cbm_ui] [--start_opencode] [--start_web] [--detach]" >&2
      exit 1
      ;;
  esac
done

# --- Toolchain-set derivation (toolchain sets #200, spec §8) --------------------
#
# The container runs the derived opencode-sandbox-set:<hash12> image (the hash
# is re-derived from the member recipes + base recipe — lib-identity.sh,
# golden-vector-pinned to the Go side; the set array is semantically unordered,
# the canonical order is derived here). --edition is an alias expanding via the
# manifest bundle at arg parsing; both flags together are refused (explicit
# conflict beats silent precedence); one of the two is required. start.sh still
# never builds — a missing set image refuses naming the build command.
if ((TOOLCHAINS_GIVEN)) && ((EDITION_GIVEN)); then
  echo "Error: --toolchains and --edition are mutually exclusive — pass one of the two." >&2
  exit 1
fi
if ((! TOOLCHAINS_GIVEN)) && ((! EDITION_GIVEN)); then
  echo "Error: one of --toolchains <ids> or --edition <name> is required." >&2
  echo "  --toolchains <ids>  comma-separated toolchain ids (\"\" = base-only set)" >&2
  echo "  --edition <name>    alias: expands via the manifest bundle" >&2
  exit 1
fi

[[ -f "$TOOLCHAINS_ROOT/base.Dockerfile" ]] || {
  echo "Error: toolchains tree not found at $TOOLCHAINS_ROOT (base.Dockerfile missing) — is the installation complete?" >&2
  exit 1
}
# shellcheck source=lib-identity.sh
source "$SCRIPT_DIR/lib-identity.sh"

# resolved_ids — validates and resolves each id against live discovery,
# keeping the canonical (sorted) member list in TOOLCHAIN_IDS and the recipe
# paths in RECIPE_OF.
TOOLCHAIN_IDS=()
declare -A RECIPE_OF=()
resolve_ids() {
  local id out
  for id in "$@"; do
    [[ -n "$id" ]] || continue
    [[ -n "${RECIPE_OF[$id]:-}" ]] && continue
    if ! out="$(bash "$SCRIPT_DIR/toolchains.sh" resolve "$TOOLCHAINS_ROOT" "$id" 2>&1)"; then
      echo "Error: refusing to start — $out" >&2
      exit 1
    fi
    RECIPE_OF[$id]="$out"
  done
  if ((${#RECIPE_OF[@]} > 0)); then
    mapfile -t TOOLCHAIN_IDS < <(printf '%s\n' "${!RECIPE_OF[@]}" | LC_ALL=C sort -u)
  fi
}

if ((TOOLCHAINS_GIVEN)); then
  # comma-separated, order-free, "" = base-only
  IFS=',' read -r -a _requested_ids <<< "$TOOLCHAINS_FLAG"
  resolve_ids "${_requested_ids[@]}"
else
  # --edition is an alias expanding via the manifest bundle (live discovery)
  if [[ ! "$EDITION_FLAG" =~ ^[a-z0-9-]+$ ]]; then
    echo "Error: invalid edition name: $EDITION_FLAG" >&2
    echo "Only lowercase [a-z0-9-] is allowed." >&2
    exit 1
  fi
  bundle_record="$(bash "$SCRIPT_DIR/toolchains.sh" discover "$TOOLCHAINS_ROOT" |
    awk -F'\t' -v e="$EDITION_FLAG" '$1 == "bundle" && $2 == e')"
  if [[ -z "$bundle_record" ]]; then
    echo "Error: unknown edition: $EDITION_FLAG" >&2
    echo "Available editions: $(bash "$SCRIPT_DIR/toolchains.sh" discover "$TOOLCHAINS_ROOT" |
      awk -F'\t' '$1 == "bundle" { printf "%s ", $2 }')" >&2
    exit 1
  fi
  bundle_status="$(awk -F'\t' '{print $3}' <<< "$bundle_record")"
  if [[ "$bundle_status" != "valid" ]]; then
    echo "Error: edition '$EDITION_FLAG' is invalid — $(awk -F'\t' '{print $4}' <<< "$bundle_record")" >&2
    exit 1
  fi
  # shellcheck disable=SC2086 — the bundle's member list is a word list
  resolve_ids $(awk -F'\t' '{print $4}' <<< "$bundle_record")
fi

# the derived set tag; the stored set stays plain ids — the hash is always
# re-derived (never stored)
set_tag_args=("$TOOLCHAINS_ROOT/base.Dockerfile")
for _id in "${TOOLCHAIN_IDS[@]}"; do
  set_tag_args+=("$_id" "${RECIPE_OF[$_id]}")
done
IMAGE="$(tc_set_tag "${set_tag_args[@]}")"
if ! podman image exists "$IMAGE" 2>/dev/null; then
  _ids_csv="$(printf '%s\n' "${TOOLCHAIN_IDS[@]:-}" | paste -sd, -)"
  echo "Error: the set image $IMAGE is missing — build it first:" >&2
  if [[ -n "$_ids_csv" ]]; then
    echo "  build-images.sh --set $_ids_csv" >&2
  else
    echo '  build-images.sh --set ""' >&2
  fi
  exit 1
fi
printf '==> Toolchain set: %s\n' "${TOOLCHAIN_IDS[*]:-"(base only)"}" >&2

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
# Dictation + offline is NOT an error (unlike searxng/cbm_ui above): an
# offline start of a dictation-enabled workspace just must not start the
# STT member — silent skip (no member, no publish, no STT_URL; the pod
# block below is guarded by ! $OFFLINE anyway).
if $USE_STT && $OFFLINE; then
  echo "Note: --offline excludes the STT member (dictation stays off for this run)" >&2
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

mkdir -p "$WORKSPACE_DIR" "$CONFIG_DIR" "$DATA_DIR" "$DATA_DIR/cache/opencode" "$SSH_DIR" "$GIT_DIR" "$GIT_DIR/gh-cli" "$GIT_DIR/glab-cli" "$CBM_DIR"
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

# --- Dictation seeding (every start, idempotent) --------------------------------
# Plain bash here (no jq needed): runs on EVERY start under --use_stt so it
# covers existing workspaces and post-update starts alike — init-workspace.sh
# is creation-only and cannot (it hard-fails on non-empty roots).
if $USE_STT; then
  # Language pin (#168, ADR 0036 D3): read the pinned language ONCE per start,
  # BEFORE the write-if-absent seeding below (a just-seeded file mounts
  # nothing new — its content is the member's own auto default anyway).
  # jq-guarded like the v2 migration above; a missing jq or an absent/
  # invalid file falls back to "auto" (the member's own default).
  STT_LANG="auto"
  STT_MOUNT_ARGS=()
  if [[ -f "${CONFIG_DIR}/stt.json" ]]; then
    if command -v jq >/dev/null 2>&1; then
      STT_LANG="$(jq -r '.language // "auto"' "${CONFIG_DIR}/stt.json" 2>/dev/null || true)"
    fi
    STT_LANG="${STT_LANG//[[:space:]]/}"
    if [[ -z "$STT_LANG" ]]; then
      STT_LANG="auto"
    fi
    # ADR 0034 allowlist pattern: the settings file is the persistent
    # record traveling with the workspace; mounted read-only into the
    # member only when the file exists.
    STT_MOUNT_ARGS=(-v "${CONFIG_DIR}/stt.json:/etc/stt/stt.json:ro")
  fi
  # Workspace STT settings (spec story 26): the language pin lives in
  # stt.json, hand-editable. Write only if absent — a user's pinned
  # language must survive every start (the STT mount reads it later).
  if [[ ! -f "${CONFIG_DIR}/stt.json" ]]; then
    printf '{"language": "auto"}\n' > "${CONFIG_DIR}/stt.json"
  fi
  # Dictation plugin refresh: overwrite OUR OWN seeded files on every
  # start (the update story — image/plugin updates land on the next
  # start). Exactly this dir, nothing else in plugins/ is touched.
  if [[ -d "${REPO_ROOT}/templates/opencode/plugins/dictation" ]]; then
    rm -rf "${CONFIG_DIR}/plugins/dictation"
    mkdir -p "${CONFIG_DIR}/plugins"
    cp -r "${REPO_ROOT}/templates/opencode/plugins/dictation" "${CONFIG_DIR}/plugins/dictation"
  fi
  # Keybind note (ADR 0036, D1): dictation binds ctrl+alt+s — verified
  # free against the shipped stock keybind defaults and legacy-decodable
  # (the earlier ctrl+t and ctrl+shift+s chords were retired: the latter
  # collapses to the swallowed 0x13 byte on terminals that do not report
  # modifiers), so no cli.json is seeded and no stock keybind is touched.
  # (A cli.json seeded by an older build keeps its variant.cycle entry,
  # which only moves that command to the command palette.)
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
  # Per-workspace allowlist (ADR 0034): when the workspace carries its
  # own copy (.opencode_config/allowlist.txt, seeded from the shared
  # proxy/allowlist.txt by the WebUI proxy screen), it wins over the
  # image's baked-in copy — mounted read-only into the member.
  PROXY_MOUNT_ARGS=()
  if [[ -f "${CONFIG_DIR}/allowlist.txt" ]]; then
    echo "==> Using the workspace allowlist: ${CONFIG_DIR}/allowlist.txt"
    PROXY_MOUNT_ARGS=(-v "${CONFIG_DIR}/allowlist.txt:/etc/squid/allowlist.txt:Z")
  fi
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
# Always-on web access (spec §10): EVERY detached start publishes container
# port 4096 (auto-scan upward, unchanged range) and runs the v2 background
# service — independent of --start_web, which the detached path ignores.
# --offline is exempt: --network=none has structurally nothing to publish
# through. The foreground path keeps the --start_web-only contract
# (ADR 0024, Ctrl+C stops and removes).
WEB_PORT_ARGS=()
WEB_HOST_PORT=""
PUBLISH_WEB=false
if $DETACH && ! $OFFLINE; then
  PUBLISH_WEB=true
fi
if $START_WEB && ! $DETACH; then
  PUBLISH_WEB=true
fi
if $PUBLISH_WEB; then
  WEB_HOST_PORT=$(select_free_host_port 4096 "the OpenCode web UI")
  if [[ "$WEB_HOST_PORT" != "4096" ]]; then
    echo "Note: host port 4096 is in use; OpenCode web is published at http://127.0.0.1:${WEB_HOST_PORT}." >&2
  fi
  WEB_PORT_ARGS=(-p "127.0.0.1:${WEB_HOST_PORT}:4096")
fi
# The foreground login shell only starts the webserver itself on
# --start_web; the detached container command starts the service
# unconditionally (below).
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

# --- STT member (dictation) -----------------------------------------------------
# Per ADR 0029/0036 the whisper.cpp server runs as a per-pod member
# (detached, oc-stt image, no publishings on the member, unique per-pod
# name; started during pod creation below). In-pod port constant 8090 —
# whisper.cpp's 8080 default collides with searxng's in-pod 8080, and
# both members may coexist in one pod. The one loopback publish lives on
# POD CREATE (web 4096 / CBM 9749 precedent, ADR 0029), never on the
# member. The host scan starts at 8091, not 8090: the WebUI itself
# listens on 8090 (single instance per host), so 8091 upward is the
# sidecars' own range. --offline excludes the member entirely (silent
# skip above).
STT_PORT_ARGS=()
STT_ENV=()
if $USE_STT && ! $OFFLINE; then
  STT_HOST_PORT=$(select_free_host_port 8091 "the STT member")
  if [[ "$STT_HOST_PORT" != "8091" ]]; then
    echo "Note: host port 8091 is in use; the STT member is published at http://127.0.0.1:${STT_HOST_PORT}." >&2
  fi
  STT_PORT_ARGS=(-p "127.0.0.1:${STT_HOST_PORT}:8090")
  # Bare pod-internal loopback constant on the workspace container —
  # never the scanned host port (ADR 0029: scans are never persisted;
  # `podman port` stays the truth).
  STT_ENV=(-e "STT_URL=http://127.0.0.1:8090")
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

# --- Dictation audio: PulseAudio socket cascade (ADR 0036) -----------------------
# parecord runs INSIDE the workspace container, so the mount + env go on
# the workspace run vector below — never on the STT member. Detection
# cascade, first existing socket wins:
#   1) ${XDG_RUNTIME_DIR}/pulse/<default socket> ("native" first, then the
#      first entry of the pulse dir — deterministic: the standard name
#      wins, globs only cover non-standard default socket names),
#   2) WSLg path — a start.sh VARIABLE so tests can stub it,
#   3) WSLg runtime-dir fallback: ${XDG_RUNTIME_DIR}/../wslg/PulseServer.
# No socket → empty arrays: no mount, no env — the STT member still runs
# (the dictation sidecar works), only TUI recording degrades.
STT_WSLG_PULSE="${STT_WSLG_PULSE:-/mnt/wslg/PulseServer}"
STT_DEVICE_ARGS=()
STT_AUDIO_ENV=()
STT_PULSE_SOCKET=""
if $USE_STT && ! $OFFLINE; then
  if [[ -n "${XDG_RUNTIME_DIR:-}" ]]; then
    shopt -s nullglob
    for stt_pulse_cand in "${XDG_RUNTIME_DIR}/pulse/native" "${XDG_RUNTIME_DIR}/pulse/"*; do
      if [[ -e "$stt_pulse_cand" ]]; then
        STT_PULSE_SOCKET="$stt_pulse_cand"
        break
      fi
    done
    shopt -u nullglob
  fi
  if [[ -z "$STT_PULSE_SOCKET" && -e "$STT_WSLG_PULSE" ]]; then
    STT_PULSE_SOCKET="$STT_WSLG_PULSE"
  fi
  if [[ -z "$STT_PULSE_SOCKET" && -n "${XDG_RUNTIME_DIR:-}" \
       && -e "${XDG_RUNTIME_DIR}/../wslg/PulseServer" ]]; then
    STT_PULSE_SOCKET="${XDG_RUNTIME_DIR}/../wslg/PulseServer"
  fi
  if [[ -n "$STT_PULSE_SOCKET" ]]; then
    # Bind the first existing socket at the SAME path in the container
    # (PULSE_SERVER=unix:<path> needs the socket at the path the client
    # was told). SELinux note: a socket mount must not be relabeled —
    # SELinux distros want `label=disable` appended here; the repo's
    # other mounts use :Z, which would break pulse on those hosts
    # (Ubuntu/AppArmor unaffected; real-SELinux behavior = manual
    # matrix). Deliberately plain, no :Z, no :ro.
    STT_DEVICE_ARGS=(-v "${STT_PULSE_SOCKET}:${STT_PULSE_SOCKET}")
    STT_AUDIO_ENV=(-e "PULSE_SERVER=unix:${STT_PULSE_SOCKET}")
  else
    echo "Note: no PulseAudio socket found — dictation recording is" >&2
    echo "  unavailable (side effect, not a blocker: the STT member still" >&2
    echo "  runs; the dictation sidecar path works)." >&2
  fi
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
create_workspace_pod() {
  local network="$1"

  podman pod rm -f "$POD_NAME" >/dev/null 2>&1 || true
  podman pod create --name "$POD_NAME" \
    --userns=keep-id \
    --network "$network" \
    "${CBM_PORT_ARGS[@]}" \
    "${WEB_PORT_ARGS[@]}" \
    "${STT_PORT_ARGS[@]}"
}

# start_workspace_pod starts the pod's infra container — the step that
# actually sets up the rootless network. `podman pod create` only records
# it, so a host-network limitation surfaces on the first member start;
# doing it here lets the fallback below retry before any member exists.
start_workspace_pod() {
  local err
  if err="$(podman pod start "$POD_NAME" 2>&1)"; then
    return 0
  fi
  POD_START_ERR="$err"
  return 1
}

if ! $OFFLINE; then
  echo "==> Creating pod ${POD_NAME} (recreate) ..."
  POD_START_ERR=""
  create_workspace_pod "pasta:--ipv4-only"
  if ! start_workspace_pod; then
    # pasta aborts with "External interface not usable" when the host has
    # no interface it can derive an address/gateway from: a VPN or other
    # tunnel holds the default route (no MAC), the laptop is offline, or
    # there is no IPv4 route. --ipv4-only also hides a usable IPv6 route,
    # so retry dual-stack pasta before giving up on it.
    if [[ "$POD_START_ERR" == *"External interface not usable"* ]]; then
      echo "Warning: pasta cannot use a host network interface (VPN/tunnel default route," >&2
      echo "         offline host, or no IPv4 route) — retrying with dual-stack pasta." >&2
      create_workspace_pod "pasta"
      if ! start_workspace_pod; then
        # slirp4netns runs its own NAT and does not depend on a usable
        # host interface (the documented pasta fallback on Podman < 6;
        # Podman 6 removed it, in which case the error below is the hint).
        echo "Note: retrying the sandbox network with slirp4netns." >&2
        if ! create_workspace_pod "slirp4netns" || ! start_workspace_pod; then
          printf '%s\n' "$POD_START_ERR" >&2
          echo "Error: podman could not set up the sandbox network (pasta and slirp4netns failed)." >&2
          echo "       Reconnect the host network (a VPN/tunnel can hide the usable interface)," >&2
          echo "       install slirp4netns for the fallback (Podman < 6), or use offline mode." >&2
          echo "       Podman 6 removed slirp4netns — there pasta needs a usable host interface." >&2
          podman pod rm -f "$POD_NAME" >/dev/null 2>&1 || true
          exit 1
        fi
      fi
    else
      printf '%s\n' "$POD_START_ERR" >&2
      podman pod rm -f "$POD_NAME" >/dev/null 2>&1 || true
      exit 1
    fi
  fi
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
      "${PROXY_MOUNT_ARGS[@]}" \
      oc-proxy
  fi
  if $USE_STT; then
    # Lazy image build (searxng precedent): builds once on the host,
    # whisper.cpp + model are baked into the image (no runtime download).
    if ! podman image exists oc-stt 2>/dev/null; then
      echo "==> Building the oc-stt image ..."
      podman build --network host -t oc-stt -f "${REPO_ROOT}/stt/Dockerfile" "${REPO_ROOT}/stt/"
    fi
    STT_NAME="oc-stt-${SANDBOX_ID}"
    echo "==> Starting the oc-stt pod member (${STT_NAME}) ..."
    # Language pin (next-start apply, #168): STT_LANG was read once per
    # start above; the file is re-read only when the member is recreated
    # with the pod — no live-apply of any kind. The member applies the
    # language server-side (entrypoint --language); clients stay dumb
    # (no client-side language parameter anywhere).
    podman run -d \
      --pod "$POD_NAME" \
      --name "$STT_NAME" \
      -e "STT_LANG=${STT_LANG}" \
      "${STT_MOUNT_ARGS[@]}" \
      oc-stt
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
# The pin is unconditional (always-on web access, spec §10): the service
# config is provisioned on every start, detached or foreground — the
# published 4096 port must hit a service listening on container port 4096
# regardless of the configured start option.
SERVICE_FILE="${CONFIG_DIR}/service.json"
if [[ ! -f "$SERVICE_FILE" ]]; then
  printf '{"hostname": "0.0.0.0", "port": 4096}\n' > "$SERVICE_FILE"
elif ! jq -e '.hostname == "0.0.0.0" and .port == 4096' "$SERVICE_FILE" >/dev/null 2>&1; then
  tmp_service_json="$(mktemp)"
  jq '."hostname" = "0.0.0.0" | ."port" = 4096' "$SERVICE_FILE" > "$tmp_service_json" \
    && mv "$tmp_service_json" "$SERVICE_FILE"
fi

RUN_MODE=(-it)
CONTAINER_COMMAND=()
if $DETACH; then
  RUN_MODE=(-d)
  # v2: shared background service pinned to container port 4096, then
  # keep-alive (ADR 0024) — in EVERY detached mode (always-on web access,
  # spec §10). --start_web is a foreground-only contract and is ignored
  # here; the published host port comes from the 4096 scan. Hostname/Port
  # already sit in service.json (above) — the start itself is a single
  # idempotent ensure.
  # idempotent ensure. The daemon resets its cwd to $HOME (upstream v2
  # behaviour), landing in /home/dev -> not a git repo -> workspace root
  # falls back to / (anomalyco/opencode#36858) -> the web UI shows the
  # project picker. Override HOME so the daemon boots in the workspace.
  CONTAINER_COMMAND=(-c 'cd /home/dev/workspace && HOME=/home/dev/workspace opencode service start && exec sleep infinity')
fi

if $START_WEB && ! $DETACH; then
  echo "OpenCode web: http://127.0.0.1:${WEB_HOST_PORT} (Ctrl+C stops the server)"
fi

# Service registration must land where exec clients find it: the detached
# boot runs the daemon with HOME=/home/dev/workspace (CONTAINER_COMMAND
# below), but every `podman exec` client (skills stage, pair, console TUI)
# runs with HOME=/home/dev. Without this pin the daemon's registration is
# written to workspace/.local/state/opencode/ while clients look in
# /home/dev/.local/state/opencode/ — they see no running service, boot a
# second one and abort on the pinned port 4096 ("Managed service port 4096
# ... is already in use").
# Plugin cache persistence: the models-discovery plugin is installed from
# npm into $XDG_CACHE_HOME/opencode/npm on load. On a cold (ephemeral)
# cache that install takes tens of seconds while /api/provider, /api/model
# and /api/agent answer 503 — the first TUI/web client then renders no
# models until it is reopened. Mount the cache from the persistent data
# dir so only the very first boot pays the install cost.
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
  "${STT_DEVICE_ARGS[@]}" \
  -e TERM="${TERM:-xterm-256color}" \
  -e XDG_CONFIG_HOME="/home/dev/.config" \
  -e XDG_DATA_HOME="/home/dev/.local/share" \
  -e XDG_STATE_HOME="/home/dev/.local/state" \
  -e XDG_CACHE_HOME="/home/dev/.cache" \
  -e CBM_CACHE_DIR="/home/dev/.cache/codebase-memory-mcp" \
  -e GIT_CONFIG_GLOBAL="/home/dev/.git_local/gitconfig" \
  -e GH_CONFIG_DIR="/home/dev/.git_local/gh-cli" \
  -e GLAB_CONFIG_DIR="/home/dev/.git_local/glab-cli" \
  -e START_OPENCODE="${START_OPENCODE}" \
  -e START_WEB="${START_WEB_ENV}" \
  "${WEBSEARCH_ENV[@]}" \
  "${SEARXNG_ENV[@]}" \
  "${STT_ENV[@]}" \
  "${STT_AUDIO_ENV[@]}" \
  -v "${WORKSPACE_DIR}:/home/dev/workspace:Z" \
  -v "${CONFIG_DIR}:/home/dev/.config/opencode:Z" \
  -v "${DATA_DIR}:/home/dev/.local/share/opencode:Z" \
  -v "${DATA_DIR}/cache/opencode:/home/dev/.cache/opencode:Z" \
  -v "${SSH_DIR}:/home/dev/.ssh:Z,ro" \
  -v "${GIT_DIR}:/home/dev/.git_local:Z" \
  -v "${CBM_DIR}:/home/dev/.cache/codebase-memory-mcp:Z" \
  -v "${BASH_DIR}/bash_profile:/home/dev/.bash_profile:Z" \
  "$IMAGE" "${CONTAINER_COMMAND[@]}"

# Detached starts must not report success before the v2 service has loaded
# config/plugins. During plugin load the API answers 503, so a client that
# opens immediately (TUI/web pair flow) caches an empty model list. Poll
# the authenticated model endpoint — the service password lives in the
# mounted service.json. Bounded: a slow/broken service only warns (the
# container is up; reopening the client later still works).
if $DETACH; then
  ready_tries="${OC_SANDBOX_SERVICE_READY_TRIES:-90}"
  ready=false
  for ((try = 1; try <= ready_tries; try++)); do
    if podman exec "$CONTAINER_NAME" bash -c '
      pw="$(jq -r ".password // empty" /home/dev/.config/opencode/service.json 2>/dev/null)"
      [ -n "$pw" ] && curl -fsS -u "opencode:$pw" http://127.0.0.1:4096/api/model >/dev/null 2>&1
    ' >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  if ! $ready; then
    printf 'Warning: OpenCode service was not ready after %ss; the first client may show no models until the plugin cache is warm.\n' "$ready_tries" >&2
  fi
fi

if $DETACH && ! $OFFLINE; then
  # Always-on web echo (spec §10): every detached start announces the live
  # published URL + the pair hint — the scanned host port is never
  # persisted, so this echo is informational; live `podman port
  # <ctr> 4096/tcp` (discovery.WebPort) stays the truth.
  echo "OpenCode web: http://127.0.0.1:${WEB_HOST_PORT}"
  echo "Sign-in (one-time pair link): podman exec -it $CONTAINER_NAME opencode pair"
fi
