#!/usr/bin/env bash
#
# Tests für init-project.sh (dist/)
# Prüft externes Verhalten: Projekt-Root-Struktur, Git-Heal für OpenCode-Web,
# Template-Kopien und Fehler bei nicht-leerem Ziel.
#
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_SCRIPT="$PROJECT_ROOT/dist/scripts/init-project.sh"

test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

# git mocken: nur 'clone' ablehnen (offline-Szenario), Rest an echtes git
fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/git" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == "clone" ]]; then
  printf 'Mock: git clone blockiert (offline)\n' >&2
  exit 1
fi
exec /usr/bin/git "$@"
EOF
chmod +x "$fake_bin/git"
# Podman-Mock (lifecycle tests #124): Argumente protokollieren, alles
# gelingt; ss minimal antworten (start.sh Portprobe).
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
exit 0
EOF
chmod +x "$fake_bin/podman"
cat > "$fake_bin/ss" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin/ss"
export PODMAN_LOG="$test_home/podman.log"
export PATH="$fake_bin:$PATH"

# --- Frischer Projekt-Root -----------------------------------------------------
root1="$test_home/kunde-a"

if ! bash "$INIT_SCRIPT" "$root1" > "$test_home/init-output" 2>&1; then
  printf 'init-project.sh failed on fresh root\n' >&2
  cat "$test_home/init-output" >&2
  exit 1
fi

# Git-Heal: OpenCode-Web braucht ein Git-Repo in project/ (sonst worktree "/"
# und Weboberflaeche ohne neue Sessions)
[[ -e "$root1/project/.git" ]]
[[ "$(git -C "$root1/project" rev-parse --is-inside-work-tree 2>/dev/null)" == "true" ]]

# Struktur
[[ -d "$root1/.opencode_config" ]]
[[ -f "$root1/.opencode_config/opencode.json" ]]
[[ -f "$root1/.opencode_config/AGENTS.md" ]]
[[ -d "$root1/.opencode_config/skills" ]]
[[ -d "$root1/.opencode_data" ]]
[[ -d "$root1/.cbm_cache" ]]
[[ -f "$root1/.git_local/gitconfig" ]]
# Credential store bridge: credentials file is written by the wizard only
# when a VCS token is captured — no token, no file (issue #38).
[[ ! -e "$root1/.git_local/credentials" ]]
# glab-cli/config.yml is created by the wizard (configure-project.sh
# vcs-credentials) on demand — no seed, no false "already exists" prompt.
[[ ! -e "$root1/.git_local/glab-cli" ]]
[[ -f "$root1/.ssh_local/config" ]]
[[ -f "$root1/.bash_local/bash_profile" ]]
# templates/scripts seeding was removed (5f032ac) — no project/scripts anymore
[[ ! -e "$root1/project/scripts" ]]

# Rechte
[[ "$(stat -c '%a' "$root1/.git_local")" == "700" ]]
[[ "$(stat -c '%a' "$root1/.git_local/gitconfig")" == "600" ]]
[[ "$(stat -c '%a' "$root1/.ssh_local")" == "700" ]]
[[ "$(stat -c '%a' "$root1/.ssh_local/config")" == "600" ]]
[[ "$(stat -c '%a' "$root1/.bash_local/bash_profile")" == "644" ]]

# .gitignore-Empfehlung
grep -q '^\.git_local/$' "$root1/.gitignore"

# Offline-Clone bricht init nicht ab
grep -q 'Projekt-Root angelegt' "$test_home/init-output"

printf 'init-project fresh root tests passed\n'

# --- Leeres, bereits existierendes Verzeichnis -----------------------------------
root2="$test_home/kunde-b"
mkdir -p "$root2"

if ! bash "$INIT_SCRIPT" "$root2" > /dev/null 2>&1; then
  printf 'init-project.sh failed on empty existing directory\n' >&2
  exit 1
fi
[[ -e "$root2/project/.git" ]]

printf 'init-project empty-dir tests passed\n'

# --- Nicht-leeres Verzeichnis -----------------------------------------------------
root3="$test_home/kunde-c"
mkdir -p "$root3"
touch "$root3/belegt"

if bash "$INIT_SCRIPT" "$root3" > /dev/null 2>&1; then
  printf 'init-project.sh should fail on non-empty existing directory\n' >&2
  exit 1
fi
[[ ! -e "$root3/project" ]]

printf 'init-project non-empty-dir tests passed\n'

# --- cloned-Quelle (--repo_url): project/ bleibt leer, kein git init ---------------
root4="$test_home/kunde-d"

if ! bash "$INIT_SCRIPT" "$root4" --repo_url https://example.com/repo.git > /dev/null 2>&1; then
  printf 'init-project.sh failed with --repo_url\n' >&2
  exit 1
fi
[[ ! -e "$root4/project/.git" ]]
[[ ! -e "$root4/project/scripts" ]]
[[ -f "$root4/.opencode_config/opencode.json" ]]
[[ -f "$root4/.git_local/gitconfig" ]]

printf 'init-project cloned-repo tests passed\n'

# --- Chat-Agent-Vorlage (--project_kind chat) ---------------------------------
root5="$test_home/kunde-e"

if ! bash "$INIT_SCRIPT" "$root5" --project_kind chat > /dev/null 2>&1; then
  printf 'init-project.sh failed with --project_kind chat\n' >&2
  exit 1
fi
grep -q 'Chat assistant' "$root5/.opencode_config/AGENTS.md"
# Standard-Root (root1) trägt die normale Vorlage ohne den Chat-Marker
! grep -q 'Chat assistant' "$root1/.opencode_config/AGENTS.md"

# Unbekannter Vorlagenwert wird abgewiesen
if bash "$INIT_SCRIPT" "$test_home/kunde-f" --project_kind nope > /dev/null 2>&1; then
  printf 'init-project.sh should reject an unknown --project_kind\n' >&2
  exit 1
fi

printf 'init-project chat-template tests passed\n'

# --- PKM (issue #122): Seeding für --project_kind pkm ---------------------------
# Wiki-Schema-Marker aus dist/templates/pkm/AGENTS.md
pkm_tpl="$PROJECT_ROOT/dist/templates/pkm"
[[ -f "$pkm_tpl/AGENTS.md" ]]
root_pkm="$test_home/pkm-a"

if ! bash "$INIT_SCRIPT" "$root_pkm" --project_kind pkm > "$test_home/pkm-output" 2>&1; then
  printf 'init-project.sh failed with --project_kind pkm\n' >&2
  cat "$test_home/pkm-output" >&2
  exit 1
fi

# AGENTS.md ist immer das Wiki-Schema
grep -q 'Personal Knowledge Wiki' "$root_pkm/.opencode_config/AGENTS.md"
! grep -q 'Chat assistant' "$root_pkm/.opencode_config/AGENTS.md"

# agents/commands/skills aus den PKM-Templates sind kopiert
for d in agents commands skills; do
  [[ -d "$root_pkm/.opencode_config/$d" ]]
done
for a in wiki researcher linter; do
  [[ -f "$root_pkm/.opencode_config/agents/${a}.md" ]]
done
for c in ingest query lint synthesize review; do
  [[ -f "$root_pkm/.opencode_config/commands/${c}.md" ]]
done
for s in wiki-ingest wiki-lint wiki-review wiki-synthesize; do
  [[ -f "$root_pkm/.opencode_config/skills/${s}/SKILL.md" ]]
done

# opencode.json: default_agent + permissions gemerged, Rest unversehrt
[[ "$(jq -r '.default_agent' "$root_pkm/.opencode_config/opencode.json")" == "wiki" ]]
[[ "$(jq -r '.permissions | length' "$root_pkm/.opencode_config/opencode.json")" -ge 7 ]]
jq -e '.permissions | any(.action == "edit" and .resource == "wiki/*" and .effect == "allow")' \
  "$root_pkm/.opencode_config/opencode.json" > /dev/null
# Basis-Seed bleibt erhalten
jq -e '.mcp.servers["codebase-memory-mcp"], .websearch' "$root_pkm/.opencode_config/opencode.json" > /dev/null

# .gitignore: PKM-Einträge angehängt, ohne Duplikate
for entry in '.obsidian/workspace*.json' '.obsidian/cache' '.trash/' '.DS_Store'; do
  grep -qxF "$entry" "$root_pkm/.gitignore"
done
[[ "$(grep -c '^\.opencode_config/$' "$root_pkm/.gitignore")" == 1 ]]

# Vault-Skelett: notes/, raw/, wiki/
for d in notes raw wiki; do
  [[ -d "$root_pkm/project/$d" ]]
done

printf 'init-project pkm seeding tests passed\n'

# --- PKM cloned flow: Seed überlebt den In-Container-Clone ----------------------
# Der Clone (im Container, später) schreibt project/AGENTS.md — der Seed in
# .opencode_config/ bleibt das Wiki-Schema (creation-time seeding, kein Re-Run).
root_pkm_clone="$test_home/pkm-b"

if ! bash "$INIT_SCRIPT" "$root_pkm_clone" --project_kind pkm \
  --repo_url https://example.com/repo.git > /dev/null 2>&1; then
  printf 'init-project.sh failed with pkm --repo_url\n' >&2
  exit 1
fi

# Clone-Simulation: das geklonte Repo bringt seine eigene AGENTS.md mit
printf '# fremde AGENTS.md aus dem Repo\n' > "$root_pkm_clone/project/AGENTS.md"

grep -q 'Personal Knowledge Wiki' "$root_pkm_clone/.opencode_config/AGENTS.md"
! cmp -s "$root_pkm_clone/project/AGENTS.md" "$root_pkm_clone/.opencode_config/AGENTS.md"
[[ "$(jq -r '.default_agent' "$root_pkm_clone/.opencode_config/opencode.json")" == "wiki" ]]

printf 'init-project pkm cloned-repo tests passed\n'

# --- PKM lifecycle (#124): created → started on the pkm edition → edition switch
# Full chain with mocked podman: the seeded project starts on
# opencode-sandbox-pkm, and a later edition switch (the next-start apply of
# the change-settings flow) never re-seeds or removes the PKM files.
seed_files=(
  ".opencode_config/AGENTS.md"
  ".opencode_config/opencode.json"
  ".opencode_config/agents/wiki.md"
  ".opencode_config/commands/ingest.md"
  ".opencode_config/skills/wiki-ingest/SKILL.md"
  ".gitignore"
  "project/notes"
  "project/raw"
  "project/wiki"
)
seed_state() {
  ( cd "$root_pkm" && for f in "${seed_files[@]}"; do
      if [[ -d "$f" ]]; then printf 'dir %s\n' "$f"
      else sha256sum "$f"; fi
    done )
}

# Das Go-Wizard (sandboxcfg.Create + PatchString) legt die sandbox_config.json
# an, bevor der erste Start laeuft — hier nachgebildet (jq-Spiegel des
# create-Payloads fuer ein pkm-Projekt).
jq -n '{container_edition: "pkm", container_modes: ["skills"], start_option: "opencode",
        cbm_auto_index: true, cbm_auto_watch: true, ai_provider: "none",
        project_source: "empty", repo_url: "", project_kind: "pkm",
        search_mode: "websearch", setup_complete: false, version: "1.0"}' \
  > "$root_pkm/.opencode_config/sandbox_config.json"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$root_pkm" --edition pkm --detach \
  > "$test_home/pkm-start" 2>&1; then
  printf 'start.sh failed for the pkm edition\n' >&2
  cat "$test_home/pkm-start" >&2
  exit 1
fi
grep -q -- 'opencode-sandbox-pkm' "$PODMAN_LOG"
printf 'pkm start on opencode-sandbox-pkm passed\n'

baseline="$(seed_state)"
# Und zurueck... (vorerst) — Switch: das Settings-Rewrite der Go-TUI aendert
# nur container_edition in sandbox_config.json; hier gespiegelt mit jq.
tmp_cfg="$(mktemp)"
jq '.container_edition = "swdev"' "$root_pkm/.opencode_config/sandbox_config.json" > "$tmp_cfg"
mv "$tmp_cfg" "$root_pkm/.opencode_config/sandbox_config.json"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$root_pkm" --edition swdev --detach \
  > "$test_home/pkm-switch" 2>&1; then
  printf 'start.sh failed for the switched-away pkm project\n' >&2
  cat "$test_home/pkm-switch" >&2
  exit 1
fi
[[ "$(seed_state)" == "$baseline" ]]
grep -q -- 'opencode-sandbox-swdev' "$PODMAN_LOG"

# und zurueck auf pkm — ebenfalls ohne Re-Seed (Rewrite der Go-TUI gespiegelt)
jq '.container_edition = "pkm"' "$root_pkm/.opencode_config/sandbox_config.json" > "$tmp_cfg"
mv "$tmp_cfg" "$root_pkm/.opencode_config/sandbox_config.json"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$root_pkm" --edition pkm --detach \
  > /dev/null 2>&1; then
  printf 'start.sh failed switching back to the pkm edition\n' >&2
  exit 1
fi
[[ "$(seed_state)" == "$baseline" ]]

# project_kind bleibt im Config-Dokument vermerkt (decided once at creation)
[[ "$(jq -r '.project_kind' "$root_pkm/.opencode_config/sandbox_config.json")" == "pkm" ]]

printf 'init-project pkm lifecycle tests passed\n'
