#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

mkdir -p "$test_home/project" "$test_home/.opencode_config" "$test_home/.opencode_data" \
  "$test_home/.ssh_local" "$test_home/.git_local" "$test_home/.cbm_cache" "$test_home/.bash_local"

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
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

grep -q -- '--network=pasta:--ipv4-only' "$PODMAN_LOG"
grep -q -- '-p 127.0.0.1:9750:9749' "$PODMAN_LOG"

# Git-Heal: OpenCode-Web braucht ein Git-Repo in project/ (sonst worktree "/"
# und Weboberflaeche ohne neue Sessions)
[[ -e "$test_home/project/.git" ]]
[[ "$(git -C "$test_home/project" rev-parse --is-inside-work-tree 2>/dev/null)" == "true" ]]

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
grep -q 'OpenCode Web: http://127.0.0.1:4097' "$web_output"
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
grep -q 'OpenCode Web: http://127.0.0.1:4097' "$web_output"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" --help | grep -q -- '--start_web'; then
  printf 'help text does not mention --start_web\n' >&2
  exit 1
fi
printf 'start web option tests passed\n'

# Bestehendes Repo in project/ bleibt unangetastet (kein Re-Init, kein Hinweis)
rm -rf "$test_home/project"
mkdir -p "$test_home/project"
git -C "$test_home/project" init -q
git -C "$test_home/project" -c user.name=T -c user.email=t@example.com commit --allow-empty -q -m seed
existing_head="$(git -C "$test_home/project" rev-parse HEAD)"

: > "$PODMAN_LOG"
heal_output="$test_home/heal-output"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > "$heal_output" 2>&1; then
  printf 'start script failed with mocked podman (existing repo)\n' >&2
  exit 1
fi

[[ "$(git -C "$test_home/project" rev-parse HEAD)" == "$existing_head" ]]
! grep -q 'nachgeholt' "$heal_output"
printf 'start git heal tests passed\n'

# Cloned-Quelle (project_source == "cloned") -> kein git init in project/
rm -rf "$test_home/project"
mkdir -p "$test_home/project"
bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --edition swdev --detach > /dev/null 2>&1
cloned_project="$test_home/cloned-root"
mkdir -p "$cloned_project/project" "$cloned_project/.opencode_config"
printf '%s' '{"project_source":"cloned"}' > "$cloned_project/.opencode_config/sandbox_config.json"

: > "$PODMAN_LOG"
if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$cloned_project" --edition swdev --detach > /dev/null 2>&1; then
  printf 'start script failed with mocked podman (cloned source)\n' >&2
  exit 1
fi
[[ ! -e "$cloned_project/project/.git" ]]
printf 'start cloned-source heal guard tests passed\n'

# --- opencode.json v2-Migration (ADR 0025) ---------------------------------------
# Legacy v1-Formate (permission-Map, flaches mcp, plugin/provider, auth.json)
# werden vor dem Containerlauf auf das v2-Schema migriert - nachgebildet mit
# REAL configure-project.sh, jq lokal vorhanden.
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

# --- Edition-Validierung ---------------------------------------------------------
# Fixture-Baum: Kopie von dist/scripts + Dockerfiles, damit die
# Dockerfile.custom ohne Eingriff in das Repo gefüttert werden kann.
script_fixture="$test_home/dist"
mkdir -p "$script_fixture/scripts"
cp "$PROJECT_ROOT/dist/scripts/start.sh" "$script_fixture/scripts/"
cp "$PROJECT_ROOT/dist/scripts/configure-project.sh" "$script_fixture/scripts/"
cp "$PROJECT_ROOT/dist/Dockerfile" "$script_fixture/"
cat > "$script_fixture/Dockerfile.custom" <<'EOF'
FROM opencode-sandbox-base AS opencode-sandbox-rust
ENV RUSTUP_HOME=/usr/local/rustup
EOF

run_start() {
  bash "$script_fixture/scripts/start.sh" "$@" > "$test_home/edition-output" 2>&1
}

# 1. fehlende --edition -> Nutzungsfehler (kein Default mehr)
if run_start "$cloned_project" --detach; then
  printf 'start.sh accepted missing --edition\n' >&2
  exit 1
fi
grep -q 'edition' "$test_home/edition-output"
printf 'start missing edition tests passed\n'

# 2. unbekannte Edition -> Fehler mit Liste der entdeckten Editionen
if run_start "$cloned_project" --edition doesnotexist; then
  printf 'start.sh accepted unknown edition\n' >&2
  exit 1
fi
out="$(cat "$test_home/edition-output")"
for known in base web embedded swdev rust; do
  if ! grep -q "$known" "$test_home/edition-output"; then
    printf 'edition list does not mention %s\n' "$known" >&2
    exit 1
  fi
done
printf 'start unknown edition tests passed\n'

# 3. Edition mit ungueltigem Namen -> abgelehnt
if run_start "$cloned_project" --edition "BadName"; then
  printf 'start.sh accepted invalid edition name\n' >&2
  exit 1
fi
printf 'start invalid edition name tests passed\n'

# 4. Custom-Edition wird akzeptiert und an podman durchgereicht
: > "$PODMAN_LOG"
if ! run_start "$cloned_project" --edition rust --detach; then
  printf 'start script rejected valid custom edition\n' >&2
  cat "$test_home/edition-output" >&2
  exit 1
fi
grep -q -- 'opencode-sandbox-rust' "$PODMAN_LOG"
printf 'start custom edition tests passed\n'
