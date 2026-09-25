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
[[ -f "$root1/.git_local/glab-cli/config.yml" ]]
[[ -f "$root1/.ssh_local/config" ]]
[[ -f "$root1/.bash_local/bash_profile" ]]
[[ -d "$root1/project/scripts" ]]

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
