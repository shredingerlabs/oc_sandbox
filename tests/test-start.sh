#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

mkdir -p "$test_home/workspace" "$test_home/.opencode_config" "$test_home/.opencode_data" \
  "$test_home/.ssh_local" "$test_home/.git_local" "$test_home/.cbm_cache" "$test_home/.bash_local"

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
case "$1 $2" in
  "image exists")
    case " ${PODMAN_MISSING_IMAGES:-} " in
      *" $3 "*) exit 1 ;;
    esac
    exit 0
    ;;
esac
exit 0
EOF
chmod +x "$fake_bin/podman"
cat > "$fake_bin/ss" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *":9749"*) printf 'LISTEN 0 128 127.0.0.1:9749 0.0.0.0:*\n' ;;
  *":4096"*) printf 'LISTEN 0 128 127.0.0.1:4096 0.0.0.0:*\n' ;;
esac
EOF
chmod +x "$fake_bin/ss"

export PATH="$fake_bin:/usr/bin:/bin"
export PODMAN_LOG="$test_home/podman.log"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --cbm_ui --detach 2>/dev/null; then
  printf 'start script failed with mocked podman\n' >&2
  exit 1
fi

# pod per workspace (ADR 0029): Recreate = rm -f (best effort) + create,
# Publishings haengen am Pod, workspace container tritt via --pod bei;
# ADR 0028s --add-host/host.oc-sandbox.internal-Mechanik ist entfallen.
grep -q -- 'pod rm -f opencode-sandbox-pod-' "$PODMAN_LOG"
pod_create_line="$(grep -m1 -- 'pod create --name opencode-sandbox-pod-' "$PODMAN_LOG")"
grep -q -- '--network pasta:--ipv4-only' <<<"$pod_create_line"
grep -q -- '-p 127.0.0.1:9750:9749' <<<"$pod_create_line"
! grep -q -- '--add-host' "$PODMAN_LOG"
! grep -q -- 'host.oc-sandbox.internal' "$PODMAN_LOG"
run_line="$(grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG")"
[[ "$run_line" == *' --pod opencode-sandbox-pod-'* ]]
[[ "$run_line" != *'--network'* ]]
[[ "$run_line" != *' -p 127.0.0.1'* ]]

# Pod-Member erben den usern-Namespace des Pods (podman: "userns and pod
# cannot be set together"): pod create setzt --userns=keep-id, der
# workspace container traegt kein eigenes --userns, wenn er via --pod beitritt.
grep -q -- '--userns=keep-id' <<<"$pod_create_line"
if grep -q -- '--userns' <<<"$run_line"; then
  printf 'project run joining a pod must not carry --userns (podman 4.9 conflict)\n' >&2
  exit 1
fi

# Git-Heal: OpenCode-Web braucht ein Git-Repo in project/ (sonst worktree "/"
# und Weboberflaeche ohne neue Sessions)
[[ -e "$test_home/workspace/.git" ]]
[[ "$(git -C "$test_home/workspace" rev-parse --is-inside-work-tree 2>/dev/null)" == "true" ]]

printf 'start networking and CBM port tests passed\n'

: > "$PODMAN_LOG"
web_output="$test_home/web-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --start_web --detach > "$web_output"; then
  printf 'start script failed with mocked podman (--start_web)\n' >&2
  exit 1
fi

grep -q -- '-p 127.0.0.1:4097:4096' "$PODMAN_LOG"
grep -q -- 'opencode service start' "$PODMAN_LOG"
grep -q -- 'exec sleep infinity' "$PODMAN_LOG"
! grep -q -- 'opencode service set' "$PODMAN_LOG"
! grep -q -- 'opencode web' "$PODMAN_LOG"
grep -q -- '-e START_WEB=false' "$PODMAN_LOG"
grep -q 'OpenCode web: http://127.0.0.1:4097' "$web_output"
# Boot-unabhaengiges Pinnen: hostname/port stehen in der gemounteten service.json
sq="$test_home/.opencode_config/service.json"
jq -e '.hostname == "0.0.0.0" and .port == 4096' "$sq" >/dev/null || { printf 'web start did not pin service.json\n' >&2; exit 1; }

# Existierende service.json (z.B. Passwort vom ersten Boot) bleibt erhalten,
# nur hostname/port werden ergaenzt.
printf '%s' '{"password":"keepme"}' > "$sq"
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --start_web --detach > "$web_output"; then
  printf 'start script failed with mocked podman (--start_web, pins merged)\n' >&2
  exit 1
fi
jq -e '.hostname == "0.0.0.0" and .port == 4096 and .password == "keepme"' "$sq" >/dev/null || { printf 'service.json merge lost fields\n' >&2; exit 1; }

: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --start_web > "$web_output"; then
  printf 'start script failed with mocked podman (--start_web foreground)\n' >&2
  exit 1
fi

grep -q -- '-it' "$PODMAN_LOG"
! grep -q -- '-c sleep infinity' "$PODMAN_LOG"
grep -q -- '-e START_WEB=true' "$PODMAN_LOG"
grep -q 'OpenCode web: http://127.0.0.1:4097' "$web_output"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" --help | grep -q -- '--start_web'; then
  printf 'help text does not mention --start_web\n' >&2
  exit 1
fi
printf 'start web option tests passed\n'

# --- Always-on OpenCode web access (#149, spec §10) ------------------------------
# Jeder detached Start publishes container port 4096 (scan-up, unchanged
# unveränderte Range), startet den v2-Hintergrundservice + Keep-alive und
# pinnt service.json — unabhaengig von --start_web. --offline ist vom
# Publishing ausgenommen (--network=none hat nichts durchzureichen).
rm -f "$test_home/.opencode_config/service.json"
: > "$PODMAN_LOG"
always_web_output="$test_home/always-web-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$always_web_output" 2>&1; then
  printf 'start script failed with mocked podman (plain detached, always-on web)\n' >&2
  exit 1
fi
pod_create_line="$(grep -m1 -- 'pod create --name opencode-sandbox-pod-' "$PODMAN_LOG")"
if ! grep -q -- '-p 127.0.0.1:4097:4096' <<<"$pod_create_line"; then
  printf 'plain detached start did not publish 4096 with scan-up: %s\n' "$pod_create_line" >&2
  exit 1
fi
run_line="$(grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG")"
if [[ "$run_line" != *'-c cd /home/dev/workspace && HOME=/home/dev/workspace opencode service start && exec sleep infinity'* ]]; then
  printf 'plain detached start lost the v2 background service command: %s\n' "$run_line" >&2
  exit 1
fi
# Service-Registrierung fuer exec-Clients: der Daemon laeuft mit
# HOME=/home/dev/workspace, aber `podman exec`-Clients (Skills-Stage, pair,
# Konsolen-TUI) laufen mit HOME=/home/dev — ohne XDG_STATE_HOME-Pin landet die
# Registrierung im Workspace-State und Clients booten einen Zweit-Service,
# der an Port 4096 kollidiert ("Managed service port ... already in use").
if ! grep -q -- '-e XDG_STATE_HOME=/home/dev/.local/state' <<<"$run_line"; then
  printf 'start did not pin XDG_STATE_HOME into the container env: %s\n' "$run_line" >&2
  exit 1
fi
# XDG_CACHE_HOME: ohne Pin leitet der Daemon (HOME=/home/dev/workspace) den
# Cache-Default ($HOME/.cache) in den Workspace-Inhalt (workspace/) —
# .cache/opencode (jeder detached Boot) landet in workspace/. Der Pin haelt
# opencode unter /home/dev/.cache/opencode; dieser Pfad ist aus
# .opencode_data/cache/opencode gemountet, damit der npm-Install des
# models-discovery-Plugins die Container-Neuanlage ueberlebt (sonst 503
# auf /api/* beim ersten Client).
if ! grep -q -- '-e XDG_CACHE_HOME=/home/dev/.cache' <<<"$run_line"; then
  printf 'start did not pin XDG_CACHE_HOME into the container env: %s\n' "$run_line" >&2
  exit 1
fi
# CBM_CACHE_DIR: CBM loest den Cache-Default aus $HOME/.cache ab und ignoriert
# XDG_CACHE_HOME — der XDG-Pin oben leitet es also NICHT um. Der Service laeuft
# mit HOME=/home/dev/workspace (CONTAINER_COMMAND), also startet die Service-
# Session (opencode.json MCP-Spawn) den CBM-Daemon mit Root
# workspace/.cache/codebase-memory-mcp (DB im Git-Working-Tree!), waehrend die
# CBM-Stage (podman exec, HOME=/home/dev) den .cbm_cache-Mount anfordert — der
# Daemon weist den Client als cache-root-Konflikt ab ("active account daemon
# uses a different cache directory"). Der Pin richtet Service-Session, Daemon
# und alle exec-Clients auf denselben persistenten .cbm_cache-Mount.
if ! grep -q -- '-e CBM_CACHE_DIR=/home/dev/.cache/codebase-memory-mcp' <<<"$run_line"; then
  printf 'start did not pin CBM_CACHE_DIR into the container env: %s\n' "$run_line" >&2
  exit 1
fi
grep -q -- '-e START_WEB=false' "$PODMAN_LOG"
grep -q 'OpenCode web: http://127.0.0.1:4097' "$always_web_output"
grep -q 'Sign-in (one-time pair link): podman exec -it opencode-sandbox-' "$always_web_output"
sq="$test_home/.opencode_config/service.json"
jq -e '.hostname == "0.0.0.0" and .port == 4096' "$sq" >/dev/null || { printf 'plain detached start did not pin service.json\n' >&2; exit 1; }
# Port-Autoritaet: der gescannte Host-Port wird nirgends persistiert —
# live `podman port` (discovery.WebPort) bleibt die einzige Wahrheit.
if grep -rq '4097' "$test_home/.opencode_config" "$test_home/.opencode_data"; then
  printf 'scanned host port 4097 was persisted in workspace config\n' >&2
  exit 1
fi
printf 'start always-on web detached tests passed\n'

# --- Service readiness gate ------------------------------------------------------
# Der v2-Service antwortet waehrend des Plugin-Installs/Ladens (models-
# discovery, npm) mit 503 auf /api/provider|model|agent. Ohne Gate kehrt ein
# detached Start zurueck, bevor der erste TUI/Web-Client Modelle sieht — der
# Client rendert eine leere Liste und erst das erneute Oeffnen funktioniert.
# Zwei Zusagen: der npm-Plugin-Cache liegt auf dem persistenten Data-Mount,
# und der Start pollt den authentifizierten /api/model-Endpunkt (Passwort aus
# der gemounteten service.json), begrenzt auf OC_SANDBOX_SERVICE_READY_TRIES.
ws_run_line="$(grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG")"
if ! grep -q -- "-v ${test_home}/.opencode_data/cache/opencode:/home/dev/.cache/opencode:Z" <<<"$ws_run_line"; then
  printf 'detached start did not persist the opencode plugin cache: %s\n' "$ws_run_line" >&2
  exit 1
fi
[[ -d "$test_home/.opencode_data/cache/opencode" ]]
if ! grep -q -- '^exec opencode-sandbox-' "$PODMAN_LOG"; then
  printf 'detached start did not gate on service readiness\n' >&2
  exit 1
fi
grep -q -- '/home/dev/.config/opencode/service.json' "$PODMAN_LOG"
grep -q -- '/api/model' "$PODMAN_LOG"

# Retry bis Erfolg (2x 503/exec-Fehler, dann bereit) — Start bleibt exit 0.
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
case "$*" in
  *"/api/model"*)
    n=$(cat "${EXEC_COUNT_FILE}" 2>/dev/null || echo 0)
    n=$((n + 1))
    printf '%s' "$n" > "${EXEC_COUNT_FILE}"
    [[ "$n" -gt "${EXEC_FAIL_TIMES:-0}" ]] && exit 0
    exit 1
    ;;
esac
exit 0
EOF
chmod +x "$fake_bin/podman"
: > "$PODMAN_LOG"
rm -f "$test_home/readiness-count"
retry_output="$test_home/readiness-retry-output"
if ! EXEC_COUNT_FILE="$test_home/readiness-count" EXEC_FAIL_TIMES=2 \
  OC_SANDBOX_SERVICE_READY_TRIES=5 \
  bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$retry_output" 2>&1; then
  printf 'start script failed while retrying service readiness\n' >&2
  cat "$retry_output" >&2
  exit 1
fi
if [[ "$(cat "$test_home/readiness-count")" != "3" ]]; then
  printf 'readiness gate did not retry until success (calls: %s)\n' "$(cat "$test_home/readiness-count")" >&2
  exit 1
fi
if grep -q 'was not ready' "$retry_output"; then
  printf 'readiness gate warned despite reaching a ready service\n' >&2
  exit 1
fi

# Begrenzter Timeout: nie bereit -> Warnung statt Fehlschlag, Start bleibt
# exit 0 (der Container ist hoch; ein spaeteres Oeffnen funktioniert).
: > "$PODMAN_LOG"
rm -f "$test_home/readiness-count"
timeout_output="$test_home/readiness-timeout-output"
if ! EXEC_COUNT_FILE="$test_home/readiness-count" EXEC_FAIL_TIMES=999 \
  OC_SANDBOX_SERVICE_READY_TRIES=2 \
  bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$timeout_output" 2>&1; then
  printf 'start script failed on readiness timeout (must warn only)\n' >&2
  cat "$timeout_output" >&2
  exit 1
fi
if [[ "$(cat "$test_home/readiness-count")" != "2" ]]; then
  printf 'readiness gate did not honor OC_SANDBOX_SERVICE_READY_TRIES=2 (calls: %s)\n' "$(cat "$test_home/readiness-count")" >&2
  exit 1
fi
grep -q 'Warning: OpenCode service was not ready after 2s' "$timeout_output"
grep -q 'OpenCode web: http://127.0.0.1:4097' "$timeout_output"

cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
case "$1 $2" in
  "image exists")
    case " ${PODMAN_MISSING_IMAGES:-} " in
      *" $3 "*) exit 1 ;;
    esac
    exit 0
    ;;
esac
exit 0
EOF
chmod +x "$fake_bin/podman"
printf 'start service readiness gate tests passed\n'

# Foreground ohne --start_web: Vertrag unveraendert (ADR 0024) — kein
# Web-Publishing, kein Service-Kommando, kein Web-Echo.
: > "$PODMAN_LOG"
fg_plain_output="$test_home/fg-plain-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev > "$fg_plain_output" 2>&1; then
  printf 'start script failed with mocked podman (plain foreground)\n' >&2
  exit 1
fi
if grep -q -- '-p 127.0.0.1:' "$PODMAN_LOG"; then
  printf 'plain foreground start grew a web publish (foreground contract must stay untouched)\n' >&2
  exit 1
fi
if grep -q -- 'opencode service start' "$PODMAN_LOG"; then
  printf 'plain foreground start grew a container service command\n' >&2
  exit 1
fi
if grep -q 'OpenCode web:' "$fg_plain_output"; then
  printf 'plain foreground start echoed a web URL\n' >&2
  exit 1
fi
if grep -q -- '/api/model' "$PODMAN_LOG"; then
  printf 'foreground start gated on a background service it does not boot\n' >&2
  exit 1
fi
printf 'start always-on web foreground-exempt tests passed\n'

# --offline detached: Service-Kommando bleibt (jeder detached Modus), aber
# kein Publishing (strukturell nichts auf --network=none) und kein Web-Echo.
: > "$PODMAN_LOG"
offline_web_output="$test_home/offline-web-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --offline --detach > "$offline_web_output" 2>&1; then
  printf 'start script failed with mocked podman (offline detached, always-on web)\n' >&2
  cat "$offline_web_output" >&2
  exit 1
fi
offline_web_run_line="$(grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG")"
if [[ "$offline_web_run_line" != *'-c cd /home/dev/workspace && HOME=/home/dev/workspace opencode service start && exec sleep infinity'* ]]; then
  printf 'offline detached start lost the v2 background service command: %s\n' "$offline_web_run_line" >&2
  exit 1
fi
if grep -q -- '-p 127.0.0.1:' "$PODMAN_LOG"; then
  printf 'offline detached start published a port (--offline must stay exempt)\n' >&2
  exit 1
fi
if grep -q 'OpenCode web:' "$offline_web_output"; then
  printf 'offline detached start echoed a web URL\n' >&2
  exit 1
fi
printf 'start always-on web offline-exempt tests passed\n'

# Bestehendes Repo in project/ bleibt unangetastet (kein Re-Init, kein Hinweis)
rm -rf "$test_home/workspace"
mkdir -p "$test_home/workspace"
git -C "$test_home/workspace" init -q
git -C "$test_home/workspace" -c user.name=T -c user.email=t@example.com commit --allow-empty -q -m seed
existing_head="$(git -C "$test_home/workspace" rev-parse HEAD)"

: > "$PODMAN_LOG"
heal_output="$test_home/heal-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$heal_output" 2>&1; then
  printf 'start script failed with mocked podman (existing repo)\n' >&2
  exit 1
fi

[[ "$(git -C "$test_home/workspace" rev-parse HEAD)" == "$existing_head" ]]
! grep -q 'nachgeholt' "$heal_output"
printf 'start git heal tests passed\n'

# Cloned-Quelle (workspace_source == "cloned") -> kein git init in project/
rm -rf "$test_home/workspace"
mkdir -p "$test_home/workspace"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1
cloned_project="$test_home/cloned-root"
mkdir -p "$cloned_project/workspace" "$cloned_project/.opencode_config"
printf '%s' '{"workspace_source":"cloned"}' > "$cloned_project/.opencode_config/sandbox_config.json"

: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$cloned_project" --edition swdev --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (cloned source)\n' >&2
  exit 1
fi
[[ ! -e "$cloned_project/workspace/.git" ]]
printf 'start cloned-source heal guard tests passed\n'

# --- opencode.json v2-Migration (ADR 0025) ---------------------------------------
# Legacy v1-Formate (permission-Map, flaches mcp, plugin/provider, auth.json)
# werden vor dem Containerlauf auf das v2-Schema migriert - nachgebildet mit
# REAL configure-workspace.sh, jq lokal vorhanden.
cat > "$test_home/.opencode_config/opencode.json" <<'EOF'
{"permission":{"external_directory":{"/tmp/**":"allow"}},"mcp":{"codebase-memory-mcp":{"type":"local","command":["cbm"],"enabled":true}},"plugin":["opencode-models-discovery@1.5.3"],"provider":{"legacy":{"npm":"@ai-sdk/openai-compatible","name":"Legacy","options":{"baseURL":"https://l.example.com/v1"}}},"custom":{"keep":true}}
EOF
printf '{"legacy":{"type":"api","key":"legacy-tok"}}' > "$test_home/.opencode_data/auth.json"

: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (v2 config migration run)\n' >&2
  exit 1
fi
cfg="$test_home/.opencode_config/opencode.json"
jq -e '.mcp.servers["codebase-memory-mcp"].command == ["cbm"]' "$cfg" > /dev/null
jq -e '.permissions == [{action:"external_directory",resource:"/tmp/*",effect:"allow"}]' "$cfg" > /dev/null
jq -e '.plugins == ["opencode-models-discovery@1.5.3"]' "$cfg" > /dev/null
jq -e '.providers.legacy.package == "@opencode/ai/providers/openai-compatible"' "$cfg" > /dev/null
jq -e '.providers.legacy.settings.baseURL == "https://l.example.com/v1"' "$cfg" > /dev/null
jq -e '.providers.legacy.settings.apiKey == "{file:/home/dev/.local/share/opencode/credentials/legacy}"' "$cfg" > /dev/null
jq -e '.custom.keep' "$cfg" > /dev/null
[[ "$(cat "$test_home/.opencode_data/credentials/legacy")" == "legacy-tok" ]]
[[ ! -e "$test_home/.opencode_data/auth.json" ]]
[[ -e "$test_home/.opencode_data/auth.json.v1-migrated" ]]
[[ "$(stat -c '%a' "$test_home/.opencode_data/credentials/legacy")" == "600" ]]
# Idempotent: second start does not rewrite.
touch -d "2026-01-01" "$cfg"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1
[[ "$(stat -c '%y' "$cfg")" == "2026-01-01 00:00:00.000000000 +0000" ]]
printf 'start v2 config migration tests passed\n'

# --- search_mode (ADR 0026) ------------------------------------------------------
# websearch (Default): Exa/Parallel-Env gesetzt, opencode.json-Seed unangetastet.
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (websearch default)\n' >&2
  exit 1
fi
grep -q -- '-e OPENCODE_ENABLE_EXA=1' "$PODMAN_LOG"
grep -q -- '-e OPENCODE_ENABLE_PARALLEL=1' "$PODMAN_LOG"
! grep -q -- 'SEARXNG_URL' "$PODMAN_LOG"
! grep -q -- 'oc-searxng' "$PODMAN_LOG"

# searxng: Exa/Parallel fallen weg, SEARXNG_URL wird gesetzt, der gemeinsame
# oc-searxng-Container wird gestartet, opencode.json bekommt das SearXNG-MCP.
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --search_mode searxng --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (search_mode searxng)\n' >&2
  exit 1
fi
! grep -q -- 'OPENCODE_ENABLE_EXA' "$PODMAN_LOG"
# In-Namespace-Konstante im Pod (ADR 0029), kein Gateway-Eintrag mehr.
grep -q -- 'SEARXNG_URL=http://127.0.0.1:8080' "$PODMAN_LOG"
# Nackte Basis-URL: mcp-searxng haengt /search selbst an (Suffix wuerde ueber
# /search/search auf 404 laufen).
! grep -q -- 'SEARXNG_URL=http://127.0.0.1:8080/search' "$PODMAN_LOG"
grep -q -- 'oc-searxng' "$PODMAN_LOG"
! grep -q -- 'host.oc-sandbox.internal' "$PODMAN_LOG"
! grep -q -- ':8080$' "$PODMAN_LOG"
jq -e '.websearch | not' "$cfg" > /dev/null
jq -e '.mcp.servers["searxng-mcp"].command == ["mcp-searxng"]' "$cfg" > /dev/null
# Idempotent: zweiter Start mit gleichem Modus schreibt opencode.json nicht neu.
touch -d "2026-01-02" "$cfg"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --search_mode searxng --detach > /dev/null 2>&1
[[ "$(stat -c '%y' "$cfg")" == "2026-01-02 00:00:00.000000000 +0000" ]]

# Zurück auf websearch: SearXNG-MCP wird entfernt, websearch-Seed reinkarniert.
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (back to websearch)\n' >&2
  exit 1
fi
jq -e '.mcp.servers["searxng-mcp"] | not' "$cfg" > /dev/null
jq -e '.websearch.provider == "random"' "$cfg" > /dev/null

# none: weder Exa/Parallel-Env noch Seed noch MCP.
printf '%s' '{"websearch":{"provider":"exa"}}' > "$cfg"
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --search_mode none --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (search_mode none)\n' >&2
  exit 1
fi
! grep -q -- 'OPENCODE_ENABLE_EXA' "$PODMAN_LOG"
jq -e '.websearch | not' "$cfg" > /dev/null
jq -e '.mcp.servers["searxng-mcp"] | not' "$cfg" > /dev/null
printf 'start search_mode tests passed\n'

# --- start.sh re-key: --toolchains / --edition alias (toolchain sets #200, spec §8)
# Fixture tree: a full scripts + toolchains copy so the flag validation runs
# against a real discovery without touching the repo.
script_fixture="$test_home/dist"
mkdir -p "$script_fixture/scripts"
for f in start.sh configure-workspace.sh lib-identity.sh toolchains.sh; do
  cp "$PROJECT_ROOT/dist/scripts/$f" "$script_fixture/scripts/"
done
cp -r "$PROJECT_ROOT/dist/toolchains" "$script_fixture/toolchains"

run_start() {
  bash "$script_fixture/scripts/start.sh" "$@" > "$test_home/edition-output" 2>&1
}

# Expected set tags, derived with the builder's own identity lib (the
# golden-vector fixture pins it to the Go side; here it is the independent
# expectation).
# shellcheck source=../dist/scripts/lib-identity.sh
source "$PROJECT_ROOT/dist/scripts/lib-identity.sh"
ftc="$script_fixture/toolchains"
ftag() { printf '%s\n' "$ftc/recipes/$1.Dockerfile"; }
swdev_tag="$(tc_set_tag "$ftc/base.Dockerfile" cpp "$(ftag cpp)" go "$(ftag go)" java "$(ftag java)" rust "$(ftag rust)")"
baseonly_tag="$(tc_set_tag "$ftc/base.Dockerfile")"
cpg_tag="$(tc_set_tag "$ftc/base.Dockerfile" cpp "$(ftag cpp)" go "$(ftag go)")"

run_line_of() {
  grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG"
}

# 1. neither --toolchains nor --edition -> refuse (one of the two is required)
if run_start "$cloned_project" --detach; then
  printf 'start.sh accepted neither --toolchains nor --edition\n' >&2
  exit 1
fi
grep -q -- '--toolchains' "$test_home/edition-output"
grep -q -- '--edition' "$test_home/edition-output"
printf 'start missing toolchains/edition tests passed\n'

# 2. --toolchains + --edition together -> refuse (explicit conflict beats
# silent precedence)
if run_start "$cloned_project" --toolchains go --edition swdev --detach; then
  printf 'start.sh accepted --toolchains and --edition together\n' >&2
  exit 1
fi
grep -q 'mutually exclusive' "$test_home/edition-output"
printf 'start toolchains/edition conflict test passed\n'

# 3. unknown toolchain id -> refuse naming it (live discovery)
: > "$PODMAN_LOG"
if run_start "$cloned_project" --toolchains nosuch-toolchain --detach; then
  printf 'start.sh accepted an unknown toolchain id\n' >&2
  exit 1
fi
grep -q "unknown toolchain id 'nosuch-toolchain'" "$test_home/edition-output"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
printf 'start unknown toolchain id test passed\n'

# 4. --toolchains "" = the base-only set (how chat/pkm start)
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --toolchains "" --detach; then
  printf 'start.sh rejected the base-only --toolchains ""\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
[[ "$(run_line_of)" == *" $baseonly_tag"* ]]
printf 'start base-only set test passed\n'

# 5. the container runs the derived opencode-sandbox-set:<hash12>; ids are
# order-free and comma-separated; the refusal for a missing set image names
# build-images.sh --set <ids> (sorted) — start.sh never builds
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --toolchains rust,go,java,cpp --detach; then
  printf 'start.sh rejected a valid --toolchains list\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
[[ "$(run_line_of)" == *" $swdev_tag"* ]]
if grep -q 'build-images' "$PODMAN_LOG"; then
  printf 'start.sh grew a build call\n' >&2
  exit 1
fi
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --toolchains cpp,go --detach; then
  printf 'start.sh rejected a two-member --toolchains list\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
[[ "$(run_line_of)" == *" $cpg_tag"* ]]
: > "$PODMAN_LOG"
: > "$PODMAN_LOG"
export PODMAN_MISSING_IMAGES="$cpg_tag"
if run_start "$cloned_project" --toolchains go,cpp --detach; then
  printf 'start.sh started with a missing set image\n' >&2
  exit 1
fi
unset PODMAN_MISSING_IMAGES
grep -q -- "build-images.sh --set cpp,go" "$test_home/edition-output"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 1 ]] # only the image-exists probe
printf 'start set-tag derivation and refusal test passed\n'

# 6. --edition is an alias expanding via the manifest bundle at arg parsing —
# it derives exactly the same set image as the sorted member list
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --edition swdev --detach; then
  printf 'start.sh rejected the --edition alias\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
[[ "$(run_line_of)" == *" $swdev_tag"* ]]
printf 'start edition alias expansion test passed\n'

# 7. an unknown edition -> refuse listing the discovered bundles; an invalid
# name is refused the same way
if run_start "$cloned_project" --edition doesnotexist --detach; then
  printf 'start.sh accepted an unknown edition\n' >&2
  exit 1
fi
for known in web embedded swdev matlab ros2 writing latex chat pkm; do
  grep -q "$known" "$test_home/edition-output" || {
    printf 'edition list does not mention %s\n' "$known" >&2
    exit 1
  }
done
if run_start "$cloned_project" --edition "BadName" --detach; then
  printf 'start.sh accepted an invalid edition name\n' >&2
  exit 1
fi
printf 'start unknown edition tests passed\n'

# 8. --edition chat/--edition pkm expand to the base-only set (the empty bundle)
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --edition chat --detach; then
  printf 'start.sh rejected the chat edition (base-only)\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
[[ "$(run_line_of)" == *" $baseonly_tag"* ]]
printf 'start chat/pkm base-only alias test passed\n'

# --- 6: SearXNG-Recreate-Pfad (ADR 0029) --------------------------------------
# Regressionsblock gegen die alte hostseitige Topologie: ein (evtl.
# vorlaufender) hostseitiger oc-searxng mit echtem `podman port`-Ausgabe-
# format bleibt unangetastet - kein `podman port oc-searxng`, kein
# hostseitiger oc-searxng-Run mehr. Der Wiederverwendungs-Pfad ist zum
# Pod-Check zusammengefallen: deterministisches Recreate (rm -f + create +
# Member-Run) bei jedem Start, SearXNG-URL ist die Konstante 127.0.0.1.
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
case "$*" in
  *"-f {{.State.Running}} oc-searxng"*) printf 'true\n' ;;
  *"port oc-searxng 8080/tcp"*) printf '127.0.0.1:8080->8080/tcp\n' ;;
  *"image exists oc-searxng"*) exit 0 ;;
  *"container exists oc-searxng"*) exit 0 ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$fake_bin/podman"
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --search_mode searxng --detach > /dev/null 2>&1; then
  printf 'start.sh failed with pre-running oc-searxng (recreate mock)\n' >&2
  exit 1
fi
grep -q -- 'pod rm -f opencode-sandbox-pod-' "$PODMAN_LOG"
grep -q -- 'pod create --name opencode-sandbox-pod-' "$PODMAN_LOG"
if ! grep -q -- 'run -d.*--name oc-searxng' "$PODMAN_LOG"; then
  printf 'recreate path skipped the oc-searxng pod member\n' >&2
  exit 1
fi
if ! grep -q -- 'SEARXNG_URL=http://127.0.0.1:8080' "$PODMAN_LOG"; then
  printf 'recreate path lost the SEARXNG_URL\n' >&2
  exit 1
fi
if grep -q -- 'SEARXNG_URL=http://127.0.0.1:8080/search' "$PODMAN_LOG"; then
  printf 'recreate path wrote a /search-suffixed SEARXNG_URL (/search/search -> 404)\n' >&2
  exit 1
fi
if grep -q -- 'port oc-searxng' "$PODMAN_LOG"; then
  printf 'recreate path still parses host-side oc-searxng ports\n' >&2
  exit 1
fi
if grep -q -- 'run -d --replace --name oc-searxng' "$PODMAN_LOG"; then
  printf 'host-side shared oc-searxng run is back\n' >&2
  exit 1
fi
# --- Proxy-Pod-Member (#127, ADR 0029) ------------------------------------------
# use_proxy startet einen per-Pod Squid-Member (detached, oc-proxy-Image,
# keine Publishings, pro-Pod-eindeutiger Name) und der workspace container
# zeigt auf die In-Namespace-Konstante 127.0.0.1:3128. Der hostseitige
# geteilte oc-proxy-Container ist entfallen.
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_proxy --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (use_proxy)\n' >&2
  exit 1
fi
proxy_run_line="$(grep -m1 -- '--name oc-proxy-' "$PODMAN_LOG" || true)"
if [[ -z "$proxy_run_line" ]]; then
  printf 'no per-pod proxy member run\n' >&2
  exit 1
fi
if [[ "$proxy_run_line" != *'run -d --pod opencode-sandbox-pod-'*' --name oc-proxy-'* ]]; then
  printf 'proxy member run missing --pod or per-pod name: %s\n' "$proxy_run_line" >&2
  exit 1
fi
if [[ "$proxy_run_line" == *' -p '* ]]; then
  printf 'proxy member run carries a publish: %s\n' "$proxy_run_line" >&2
  exit 1
fi
grep -q -- '-e HTTP_PROXY=http://127.0.0.1:3128' "$PODMAN_LOG"
grep -q -- '-e HTTPS_PROXY=http://127.0.0.1:3128' "$PODMAN_LOG"
# NO_PROXY muss 0.0.0.0 decken: opencode v2 baut seine Managed-Service-URLs
# aus dem gepinnten service.json-Hostname (0.0.0.0, ADR 0024). Deckt NO_PROXY
# den Host nicht, wandern die Health-Probes durch den Squid-Allowlist-Proxy,
# der sie verweigert - der Client haelt den Dienst fuer fehlend und startet
# einen zweiten serve --service, der mit dem ersten auf 4096 kollidiert
# ("Managed service port 4096 ... is already in use", Spawn-Sturm).
grep -q -- '-e NO_PROXY=localhost,127.0.0.1,0.0.0.0' "$PODMAN_LOG"
# Kein hostseitig geteilter oc-proxy-Run und kein Host-Loopback-Publishing.
if grep -q -- 'run -d --replace --name oc-proxy' "$PODMAN_LOG"; then
  printf 'host-side shared oc-proxy run is back\n' >&2
  exit 1
fi
if grep -q -- ':3128:3128' "$PODMAN_LOG"; then
  printf 'host-side 3128 publish is back\n' >&2
  exit 1
fi
printf 'start proxy pod member tests passed\n'

# --- Dictation engine (#164/#166): --use_stt --------------------------------------
# use_stt startet den oc-stt Pod-Member (whisper.cpp, In-Pod-Konstante 8090,
# != searxng 8080): Loopback-Publish haengt am POD CREATE (web/CBM-Prezedenz,
# ADR 0029), nie am Member; der workspace container zeigt auf die bare
# In-Pod-Konstante STT_URL=http://127.0.0.1:8090.
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" --help | grep -q -- '--use_stt'; then
  printf 'help text does not mention --use_stt\n' >&2
  exit 1
fi

: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (use_stt)\n' >&2
  exit 1
fi
pod_create_line="$(grep -m1 -- 'pod create --name opencode-sandbox-pod-' "$PODMAN_LOG")"
if ! grep -q -- '-p 127.0.0.1:8091:8090' <<<"$pod_create_line"; then
  printf 'use_stt start did not publish the STT loopback on pod create: %s\n' "$pod_create_line" >&2
  exit 1
fi
stt_run_line="$(grep -m1 -- '--name oc-stt-' "$PODMAN_LOG" || true)"
if [[ -z "$stt_run_line" ]]; then
  printf 'no oc-stt pod member run\n' >&2
  exit 1
fi
if [[ "$stt_run_line" != *'run -d --pod opencode-sandbox-pod-'*' --name oc-stt-'* ]]; then
  printf 'stt member run missing --pod or per-pod name: %s\n' "$stt_run_line" >&2
  exit 1
fi
if [[ "$stt_run_line" == *' -p '* ]]; then
  printf 'stt member run carries a publish (publish lives on pod create, ADR 0029): %s\n' "$stt_run_line" >&2
  exit 1
fi
if [[ "$stt_run_line" == *'--network'* ]]; then
  printf 'stt member run carries --network (pod members inherit the pod netns): %s\n' "$stt_run_line" >&2
  exit 1
fi
# Bare In-Pod-Konstante auf dem workspace container (nie der gescannte
# Host-Port, ADR 0029) — und ohne 8080-Kollision mit SEARXNG_URL.
grep -q -- 'STT_URL=http://127.0.0.1:8090' "$PODMAN_LOG"
! grep -q -- 'STT_URL=http://127.0.0.1:8080' "$PODMAN_LOG"
if grep -q -- 'PULSE_SERVER' "$PODMAN_LOG"; then
  printf 'use_stt start without any PulseAudio socket grew a PULSE_SERVER env\n' >&2
  exit 1
fi
# Kein STT-irgendwas ohne --use_stt.
: > "$PODMAN_LOG"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1
if grep -q -- 'oc-stt\|STT_URL\|8091:8090' "$PODMAN_LOG"; then
  printf 'plain start grew STT artifacts (opt-in flag must gate everything)\n' >&2
  exit 1
fi
printf 'start stt member tests passed\n'

# --- Dictation + searxng coexistence ----------------------------------------------
# Beide Member koexistieren in einem Pod ohne Port-Konflikt (8090 vs 8080).
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --search_mode searxng --use_stt --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (searxng + use_stt)\n' >&2
  exit 1
fi
grep -q -- '--name oc-searxng-' "$PODMAN_LOG"
grep -q -- '--name oc-stt-' "$PODMAN_LOG"
grep -q -- 'SEARXNG_URL=http://127.0.0.1:8080' "$PODMAN_LOG"
grep -q -- 'STT_URL=http://127.0.0.1:8090' "$PODMAN_LOG"
pod_create_line="$(grep -m1 -- 'pod create --name opencode-sandbox-pod-' "$PODMAN_LOG")"
if ! grep -q -- '-p 127.0.0.1:8091:8090' <<<"$pod_create_line"; then
  printf 'coexistence lost the STT publish on pod create: %s\n' "$pod_create_line" >&2
  exit 1
fi
# Der STT-Client-URL fehlt der SearXNG-Port (getrennte In-Pod-Konstanten).
! grep -q -- 'STT_URL=http://127.0.0.1:8080' "$PODMAN_LOG"
printf 'start stt searxng coexistence tests passed\n'

# --- Dictation offline: SILENT SKIP, kein Fehler ------------------------------------
# --offline + --use_stt: kein Member, kein Publish, kein STT_URL — der Start
# bleibt ohne harten Exit stehen (Notiz statt ADR-0026-Abbruch).
: > "$PODMAN_LOG"
offline_stt_output="$test_home/offline-stt-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --offline --detach > "$offline_stt_output" 2>&1; then
  printf 'start script failed for offline + use_stt (must be a silent skip, not an error)\n' >&2
  cat "$offline_stt_output" >&2
  exit 1
fi
if grep -q -- '--name oc-stt-' "$PODMAN_LOG"; then
  printf 'offline start grew an oc-stt member\n' >&2
  exit 1
fi
if grep -q -- 'STT_URL\|8091:8090' "$PODMAN_LOG"; then
  printf 'offline start published/announced STT\n' >&2
  exit 1
fi
grep -q 'offline excludes the STT member' "$offline_stt_output"
printf 'start stt offline skip tests passed\n'

# --- Dictation audio: PulseAudio-Socket-Kaskade (ADR 0036) --------------------------
# 1) XDG_RUNTIME_DIR-Pulse-Socket, 2) WSLg-Pfad (start.sh-Variable, stubbar),
# 3) WSLg-Runtime-Dir-Fallback. Erster existierender Socket gewinnt -> Bind-Mount
# am selben Pfad + PULSE_SERVER=unix:<Pfad> am workspace container; kein Socket
# -> kein Mount, kein Env, Member laeuft trotzdem.
ws_run_line() {
  grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG"
}
xdg_dir="$test_home/xdg"
mkdir -p "$xdg_dir/pulse"
printf 'socket' > "$xdg_dir/pulse/native"
: > "$PODMAN_LOG"
if ! XDG_RUNTIME_DIR="$xdg_dir" STT_WSLG_PULSE="$test_home/does-not-exist" \
  bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (use_stt, XDG pulse socket)\n' >&2
  exit 1
fi
xdg_run_line="$(ws_run_line)"
if ! grep -q -- "-v ${xdg_dir}/pulse/native:${xdg_dir}/pulse/native" <<<"$xdg_run_line"; then
  printf 'XDG pulse socket not bind-mounted at the same path: %s\n' "$xdg_run_line" >&2
  exit 1
fi
if ! grep -q -- "-e PULSE_SERVER=unix:${xdg_dir}/pulse/native" <<<"$xdg_run_line"; then
  printf 'XDG pulse socket did not set PULSE_SERVER: %s\n' "$xdg_run_line" >&2
  exit 1
fi

# WSLg-Zweig: kein XDG-Socket, aber die stubbare STT_WSLG_PULSE-Variable zeigt
# auf eine existierende Datei.
wslg_socket="$test_home/wslg/PulseServer"
mkdir -p "$test_home/wslg"
printf 'socket' > "$wslg_socket"
: > "$PODMAN_LOG"
if ! XDG_RUNTIME_DIR="$test_home/xdg-empty" STT_WSLG_PULSE="$wslg_socket" \
  bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (use_stt, WSLg socket stub)\n' >&2
  exit 1
fi
wslg_run_line="$(ws_run_line)"
if ! grep -q -- "-v $wslg_socket:$wslg_socket" <<<"$wslg_run_line"; then
  printf 'WSLg pulse socket not bind-mounted: %s\n' "$wslg_run_line" >&2
  exit 1
fi
if ! grep -q -- "-e PULSE_SERVER=unix:$wslg_socket" <<<"$wslg_run_line"; then
  printf 'WSLg pulse socket did not set PULSE_SERVER: %s\n' "$wslg_run_line" >&2
  exit 1
fi

# Kein Socket: kein Mount, kein Env — der Member laeuft trotzdem (Sidecar-Pfad).
: > "$PODMAN_LOG"
if ! XDG_RUNTIME_DIR="$test_home/xdg-empty" STT_WSLG_PULSE="$test_home/does-not-exist" \
  bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (use_stt, no socket)\n' >&2
  exit 1
fi
nosocket_run_line="$(ws_run_line)"
if grep -q -- 'PULSE_SERVER\|pulse/native' <<<"$nosocket_run_line"; then
  printf 'no-socket start grew a pulse mount/env: %s\n' "$nosocket_run_line" >&2
  exit 1
fi
if ! grep -q -- '--name oc-stt-' "$PODMAN_LOG"; then
  printf 'no-socket start skipped the STT member (member must still run)\n' >&2
  exit 1
fi
printf 'start stt socket cascade tests passed\n'

# --- Dictation seeding: stt.json + Plugin-Dir, idempotent bei jedem Start -----------
stt_json="$test_home/.opencode_config/stt.json"
dictation_plugin_dir="$test_home/.opencode_config/plugins/dictation"
rm -f "$stt_json"
rm -rf "$test_home/.opencode_config/plugins"
: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (use_stt seeding)\n' >&2
  exit 1
fi
[[ "$(cat "$stt_json")" == '{"language": "auto"}' ]]
[[ -f "$dictation_plugin_dir/index.ts" ]]
[[ -f "$dictation_plugin_dir/tui.tsx" ]]
# Write-if-absent: Hand-Edits (Language-Pin, #168) ueberleben jeden Start.
touch -d "2026-01-02" "$stt_json"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1
[[ "$(stat -c '%y' "$stt_json")" == "2026-01-02 00:00:00.000000000 +0000" ]]
# Plugin-Refresh: unsere eigenen Dateien werden bei jedem Start überschrieben
# (Update-Story), fremde Dateien in plugins/ bleiben unangetastet.
printf 'stale' > "$dictation_plugin_dir/tui.tsx"
printf 'keepme' > "$test_home/.opencode_config/plugins/foreign.txt"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1
[[ "$(cat "$dictation_plugin_dir/tui.tsx")" != "stale" ]]
[[ "$(cat "$test_home/.opencode_config/plugins/foreign.txt")" == "keepme" ]]
# Kein Seeding ohne --use_stt.
rm -f "$stt_json"
rm -rf "$test_home/.opencode_config/plugins"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1
if [[ -e "$stt_json" || -e "$test_home/.opencode_config/plugins" ]]; then
  printf 'plain start seeded STT files (opt-in flag must gate seeding)\n' >&2
  exit 1
fi
printf 'start stt seeding tests passed\n'

# --- Dictation keybind: no cli.json seeding (D1 chord switch) -----------------------
# dictation binds ctrl+alt+s (verified free against the stock defaults and
# legacy-decodable, ADR 0036 D1 second amendment),
# so start.sh writes NO cli.json — with and without --use_stt.
cli_json="$test_home/.opencode_config/cli.json"
rm -rf "$test_home/.opencode_config"
: > "$PODMAN_LOG"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1
if [[ -e "$cli_json" ]]; then
  printf 'stt start seeded a cli.json (chord is free — no seeding expected)\n' >&2
  exit 1
fi
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1
if [[ -e "$cli_json" ]]; then
  printf 'plain start seeded cli.json (opt-in flag must gate seeding)\n' >&2
  exit 1
fi
printf 'start stt cli.json tests passed\n'

# --- Dictation language pin: stt.json-Mount + STT_LANG-Env (#168) -------------------
# Der Member-Run-Vektor traegt BEIDES: den ADR-0034-Mount (persistenter
# Datensatz, reist mit dem Workspace) und -e STT_LANG (der Member wendet die
# Sprache serverseitig an — Clients bleiben dumm, kein Client-seitiger
# Sprachparameter). Re-Read nur bei der Member-Neuanlage mit dem Pod
# (next-start apply, kein Live-Apply). Fehlende Datei -> kein Mount,
# Member-Default auto.
config_dir="$test_home/.opencode_config"
stt_json="$test_home/.opencode_config/stt.json"
stt_member_line() {
  grep -m1 -- '--name oc-stt-' "$PODMAN_LOG"
}
run_stt_start() {
  : > "$PODMAN_LOG"
  if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --use_stt --detach > /dev/null 2>&1; then
    printf 'start script failed with mocked podman (use_stt language pin)\n' >&2
    exit 1
  fi
}

# Gepinnte Sprache: Mount UND -e STT_LANG=de am Member-Run-Vektor.
printf '{"language": "de"}\n' > "$stt_json"
run_stt_start
stt_pin_line="$(stt_member_line)"
if [[ "$stt_pin_line" != *" -v ${config_dir}/stt.json:/etc/stt/stt.json:ro"* ]]; then
  printf 'pinned stt.json not bind-mounted into the member (ADR 0034 pattern): %s\n' "$stt_pin_line" >&2
  exit 1
fi
if [[ "$stt_pin_line" != *' -e STT_LANG=de'* ]]; then
  printf 'pinned language de missing from the member run vector: %s\n' "$stt_pin_line" >&2
  exit 1
fi

# Geseedeter Default: STT_LANG=auto (Datei existiert -> Mount bleibt).
printf '{"language": "auto"}\n' > "$stt_json"
run_stt_start
if [[ "$(stt_member_line)" != *' -e STT_LANG=auto'* ]]; then
  printf 'seeded default did not yield STT_LANG=auto on the member run: %s\n' "$(stt_member_line)" >&2
  exit 1
fi

# Fehlende Datei: KEIN stt.json-Mount, STT_LANG faellt auf auto zurueck,
# Member laeuft trotzdem.
rm -f "$stt_json"
run_stt_start
stt_absent_line="$(stt_member_line)"
if grep -q -- 'stt.json:/etc/stt/stt.json' <<<"$stt_absent_line"; then
  printf 'absent stt.json was mounted: %s\n' "$stt_absent_line" >&2
  exit 1
fi
if [[ "$stt_absent_line" != *' -e STT_LANG=auto'* ]]; then
  printf 'absent stt.json did not default STT_LANG to auto: %s\n' "$stt_absent_line" >&2
  exit 1
fi

# Next-start apply + Write-if-absent: Hand-Edit zwischen zwei Starts -> der
# zweite Start traegt den neuen Wert am Member, aber die Datei selbst wird
# vom Start NICHT ueberschrieben (Hand-Edit ueberlebt, #166-Vertrag).
printf '{"language": "fr"}\n' > "$stt_json"
run_stt_start
if [[ "$(stt_member_line)" != *' -e STT_LANG=fr'* ]]; then
  printf 'hand-edited language fr did not apply on the next start: %s\n' "$(stt_member_line)" >&2
  exit 1
fi
if ! jq -e '.language == "fr"' "$stt_json" >/dev/null; then
  printf 'start overwrote the hand-edited stt.json (write-if-absent contract): %s\n' "$(cat "$stt_json")" >&2
  exit 1
fi

printf 'start stt language pin tests passed\n'


# --- 7: Offline-Userns (#125-Regression) ---------------------------------------
# Offline gibt es keinen Pod: der workspace container laeuft solo und behaelt
# --userns=keep-id (+ keep-groups). Der Pod-Pfad (oben) traegt es dagegen
# nie selbst - der Pod uebernimmt es.
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --toolchains go --offline --detach; then
  printf 'start script failed with mocked podman (--offline)\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
! grep -q -- 'pod create' "$PODMAN_LOG"
offline_run_line="$(grep -m1 -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG")"
if [[ "$offline_run_line" != *'--userns=keep-id'* ]]; then
  printf 'offline run lost --userns=keep-id: %s\n' "$offline_run_line" >&2
  exit 1
fi
if grep -q -- '--pod' <<<"$offline_run_line"; then
  printf 'offline run must not join a pod: %s\n' "$offline_run_line" >&2
  exit 1
fi
printf 'start offline userns tests passed\n'

printf 'start searxng recreate tests passed\n'

# --- 8: Host-network fallback (pasta "External interface not usable") -----------
# The pod infra start is the step that sets up the rootless network. On a
# host where pasta cannot derive an interface/gateway (VPN or other tun
# default route, offline laptop, no IPv4 route) the start retries dual-stack
# pasta and then slirp4netns instead of dying with the raw libpod error.
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
if [[ "$1 $2" == "pod start" ]]; then
  if [[ "${FAKE_POD_START_FAIL:-}" == "port" ]]; then
    echo 'time="..." level=error msg="Starting some container dependencies"' >&2
    echo 'time="..." level=error msg="pasta failed with exit code 1: Failed to bind port 4102"' >&2
    exit 125
  fi
  last_create="$(grep 'pod create' "${PODMAN_LOG}" | tail -n1)"
  if [[ "$last_create" == *"--network pasta:--ipv4-only"* ]]; then
    : # always fails (IPv4 route unusable)
  elif [[ "${FAKE_PASTA_IPV6:-}" == "1" && "$last_create" == *"--network pasta"* ]]; then
    printf 'pod-started\n'
    exit 0
  elif [[ "$last_create" == *"--network slirp4netns"* ]]; then
    printf 'pod-started\n'
    exit 0
  fi
  echo 'time="..." level=error msg="Starting some container dependencies"' >&2
  echo 'time="..." level=error msg="\"pasta failed with exit code 1:\\nExternal interface not usable\\n\""' >&2
  exit 125
fi
exit 0
EOF
chmod +x "$fake_bin/podman"
: > "$PODMAN_LOG"
fallback_output="$test_home/network-fallback-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --search_mode searxng --detach > "$fallback_output" 2>&1; then
  printf 'start script died instead of falling back from pasta\n' >&2
  cat "$fallback_output" >&2
  exit 1
fi
if ! grep -q -- '--network pasta:--ipv4-only' "$PODMAN_LOG"; then
  printf 'network fallback lost the primary pasta:--ipv4-only pod\n' >&2
  exit 1
fi
if ! grep -qE 'pod create .*(--network pasta)( |$)' "$PODMAN_LOG"; then
  printf 'network fallback did not retry dual-stack pasta\n' >&2
  exit 1
fi
if ! grep -q -- '--network slirp4netns' "$PODMAN_LOG"; then
  printf 'network fallback did not retry slirp4netns\n' >&2
  exit 1
fi
if [[ "$(grep -c -- 'pod create' "$PODMAN_LOG")" != "3" ]]; then
  printf 'network fallback pod create count = %s, want 3\n' "$(grep -c -- 'pod create' "$PODMAN_LOG")" >&2
  exit 1
fi
if ! grep -q -- 'run -d.*--pod opencode-sandbox-pod-.*--name oc-searxng-' "$PODMAN_LOG"; then
  printf 'network fallback skipped the oc-searxng member\n' >&2
  exit 1
fi
if ! grep -q -- '--rm -d --name opencode-sandbox-' "$PODMAN_LOG"; then
  printf 'network fallback skipped the workspace container\n' >&2
  exit 1
fi
grep -q -- 'retrying with dual-stack pasta' "$fallback_output" || {
  printf 'network fallback did not announce the dual-stack retry\n' >&2
  exit 1
}
grep -q -- 'retrying the sandbox network with slirp4netns' "$fallback_output" || {
  printf 'network fallback did not announce slirp4netns\n' >&2
  exit 1
}

# A host with a usable IPv6 route but no IPv4 route is rescued by the
# dual-stack retry alone (no slirp downgrade).
: > "$PODMAN_LOG"
ipv6_output="$test_home/network-ipv6-output"
if ! FAKE_PASTA_IPV6=1 bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$ipv6_output" 2>&1; then
  printf 'start script did not use the dual-stack pasta retry\n' >&2
  cat "$ipv6_output" >&2
  exit 1
fi
if [[ "$(grep -c -- 'pod create' "$PODMAN_LOG")" != "2" ]]; then
  printf 'dual-stack retry pod create count = %s, want 2\n' "$(grep -c -- 'pod create' "$PODMAN_LOG")" >&2
  exit 1
fi
if grep -q -- '--network slirp4netns' "$PODMAN_LOG"; then
  printf 'dual-stack success still fell through to slirp4netns\n' >&2
  exit 1
fi
grep -q -- 'retrying with dual-stack pasta' "$ipv6_output" || {
  printf 'dual-stack retry was not announced\n' >&2
  exit 1
}

# A pod-start failure that is not the pasta interface error (e.g. a port
# bind race) is surfaced, not silently retried on another network stack.
: > "$PODMAN_LOG"
port_fail_output="$test_home/network-port-fail-output"
if FAKE_POD_START_FAIL=port bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$port_fail_output" 2>&1; then
  printf 'start script swallowed a non-interface pod-start failure\n' >&2
  exit 1
fi
if grep -q -- 'slirp4netns' "$PODMAN_LOG"; then
  printf 'non-interface pod-start failure triggered the network fallback\n' >&2
  exit 1
fi
if ! grep -q -- 'Failed to bind port 4102' "$port_fail_output"; then
  printf 'non-interface pod-start failure error was not surfaced\n' >&2
  exit 1
fi
printf 'start host-network fallback tests passed\n'
