#!/usr/bin/env bash
#
# Legt die Verzeichnisstruktur für einen neuen Projekt-Root an:
#
#   <PROJECT_ROOT>/
#     project/            <- euer eigentliches Repo (git clone / git init hier)
#     .opencode_config/    <- OpenCode-Config (persistent, projektspezifisch)
#     .opencode_data/       <- OpenCode-Daten inkl. Auth/Credentials
#     .ssh_local/           <- SSH-Keys + Config für dieses Projekt
#     .git_local/           <- Git-Identität/-Settings + optionale Credentials
#     .cbm_cache/           <- CBM-Graph-Datenbank (persistent)
#
# Nutzung:
#   scripts/init-project.sh ~/projects/kunde-x
#
set -euo pipefail

PROJECT_ROOT="${1:-}"
if [[ -z "$PROJECT_ROOT" ]]; then
  echo "Nutzung: $0 <neuer-projekt-root>" >&2
  exit 1
fi

if [[ -e "$PROJECT_ROOT" ]]; then
  echo "Fehler: $PROJECT_ROOT existiert bereits." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_SSH_CONFIG="${SCRIPT_DIR}/../templates/ssh_local/config"
TEMPLATE_GIT_CONFIG="${SCRIPT_DIR}/../templates/git_local/gitconfig"
TEMPLATE_GIT_CREDENTIALS="${SCRIPT_DIR}/../templates/git_local/credentials"
TEMPLATE_GLAB_CONFIG="${SCRIPT_DIR}/../templates/git_local/glab-cli/config.yml"
TEMPLATE_OPENCODE_CONFIG="${SCRIPT_DIR}/../templates/opencode/opencode.json"
TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/opencode/AGENTS.md"
TEMPLATE_OPENCODE_SKILLS="${SCRIPT_DIR}/../templates/opencode/skills"
TEMPLATE_SCRIPTS="${SCRIPT_DIR}/../templates/scripts"

mkdir -p \
  "${PROJECT_ROOT}/project" \
  "${PROJECT_ROOT}/.opencode_config" \
  "${PROJECT_ROOT}/.opencode_data" \
  "${PROJECT_ROOT}/.ssh_local" \
  "${PROJECT_ROOT}/.git_local" \
  "${PROJECT_ROOT}/.cbm_cache"

chmod 700 "${PROJECT_ROOT}/.ssh_local"
chmod 700 "${PROJECT_ROOT}/.git_local"

if [[ -f "$TEMPLATE_SSH_CONFIG" ]]; then
  cp "$TEMPLATE_SSH_CONFIG" "${PROJECT_ROOT}/.ssh_local/config"
  chmod 600 "${PROJECT_ROOT}/.ssh_local/config"
fi

if [[ -f "$TEMPLATE_GIT_CONFIG" ]]; then
  cp "$TEMPLATE_GIT_CONFIG" "${PROJECT_ROOT}/.git_local/gitconfig"
  chmod 600 "${PROJECT_ROOT}/.git_local/gitconfig"
fi

if [[ -f "$TEMPLATE_GIT_CREDENTIALS" ]]; then
  cp "$TEMPLATE_GIT_CREDENTIALS" "${PROJECT_ROOT}/.git_local/credentials"
  chmod 600 "${PROJECT_ROOT}/.git_local/credentials"
fi

if [[ -f "$TEMPLATE_GLAB_CONFIG" ]]; then
  mkdir -p "${PROJECT_ROOT}/.git_local/glab-cli"
  cp "$TEMPLATE_GLAB_CONFIG" "${PROJECT_ROOT}/.git_local/glab-cli/config.yml"
  chmod 600 "${PROJECT_ROOT}/.git_local/glab-cli/config.yml"
fi

if [[ -f "$TEMPLATE_OPENCODE_CONFIG" ]]; then
  cp "$TEMPLATE_OPENCODE_CONFIG" "${PROJECT_ROOT}/.opencode_config/opencode.json"
fi

if [[ -f "$TEMPLATE_OPENCODE_AGENTS" ]]; then
  cp "$TEMPLATE_OPENCODE_AGENTS" "${PROJECT_ROOT}/.opencode_config/AGENTS.md"
fi

if [[ -d "$TEMPLATE_OPENCODE_SKILLS" ]]; then
  cp -r "$TEMPLATE_OPENCODE_SKILLS" "${PROJECT_ROOT}/.opencode_config/skills"
fi

if [[ -d "$TEMPLATE_SCRIPTS" ]]; then
  cp -r "$TEMPLATE_SCRIPTS" "${PROJECT_ROOT}/project/scripts"
fi

SKILLS_TARGET="${PROJECT_ROOT}/.opencode_config/skills"
mkdir -p "$SKILLS_TARGET"
TEMP_DIR="$(mktemp -d)"

echo "==> Pulling skills from mattpocock/skills..."
if git clone --depth 1 https://github.com/mattpocock/skills.git "$TEMP_DIR/mattpocock" 2>/dev/null; then
  for category in "$TEMP_DIR/mattpocock/skills"/*/; do
    cat_name="$(basename "$category")"
    [[ "$cat_name" == "deprecated" || "$cat_name" == "in-progress" ]] && continue
    for skill_dir in "$category"*/; do
      name="$(basename "$skill_dir")"
      if [[ ! -d "${SKILLS_TARGET}/${name}" ]]; then
        cp -r "$skill_dir" "${SKILLS_TARGET}/${name}"
      fi
    done
  done
else
  echo "  Warn: Konnte mattpocock/skills nicht klonen (kein Netz?)." >&2
fi

echo "==> Pulling skills from shredingerlabs/shredinger-skills..."
if git clone --depth 1 https://github.com/shredingerlabs/shredinger-skills.git "$TEMP_DIR/shredinger" 2>/dev/null; then
  for skill_dir in "$TEMP_DIR/shredinger/skills"/*/; do
    name="$(basename "$skill_dir")"
    if [[ ! -d "${SKILLS_TARGET}/${name}" ]]; then
      cp -r "$skill_dir" "${SKILLS_TARGET}/${name}"
    fi
  done
else
  echo "  Warn: Konnte shredingerlabs/shredinger-skills nicht klonen (kein Netz?)." >&2
fi

rm -rf "$TEMP_DIR"

# .gitignore-Empfehlung, falls ihr aus Versehen im PROJECT_ROOT (statt in
# project/) ein Git-Repo initialisiert. Schadet aber nicht als Sicherheitsnetz.
cat > "${PROJECT_ROOT}/.gitignore" <<'EOF'
.opencode_config/
.opencode_data/
.ssh_local/
.git_local/
.cbm_cache/
EOF

echo "Projekt-Root angelegt: ${PROJECT_ROOT}"
echo
echo "Nächste Schritte:"
echo "  1. Repo nach ${PROJECT_ROOT}/project klonen/initialisieren"
echo "  2. ${PROJECT_ROOT}/.git_local/gitconfig anpassen (user.name/user.email)"
echo "  3. Deploy-Keys erzeugen und nach ${PROJECT_ROOT}/.ssh_local legen, z.B.:"
echo "       ssh-keygen -t ed25519 -f ${PROJECT_ROOT}/.ssh_local/id_ed25519_github -N \"\""
echo "       ssh-keygen -t ed25519 -f ${PROJECT_ROOT}/.ssh_local/id_ed25519_gitlab -N \"\""
echo "       chmod 600 ${PROJECT_ROOT}/.ssh_local/id_ed25519_*"
echo "  4. ${PROJECT_ROOT}/.ssh_local/config an eure Hosts anpassen"
echo "     (Vorlage wurde bereits kopiert, siehe templates/ssh_local/config)"
echo "  5. (optional) ${PROJECT_ROOT}/.opencode_config/opencode.json anpassen"
echo "     (Vorlage wurde bereits kopiert, siehe templates/opencode/)"
echo "  6. Skills von mattpocock/skills + shredingerlabs/shredinger-skills"
echo "     wurden nach .opencode_config/skills/ geladen."
echo "  7. Sandbox starten: scripts/start.sh ${PROJECT_ROOT}"