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
#   scripts/init-project.sh ~/projects/kunde-x [--repo_url <url>] [--project_kind chat]
#
set -euo pipefail

PROJECT_ROOT="${1:-}"
shift 2>/dev/null || true

REPO_URL=""
PROJECT_KIND="code"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo_url)
      [[ $# -ge 2 ]] || { echo "--repo_url benötigt einen Wert" >&2; exit 1; }
      REPO_URL="$2"
      shift 2
      ;;
    --project_kind)
      [[ $# -ge 2 ]] || { echo "--project_kind benötigt einen Wert" >&2; exit 1; }
      PROJECT_KIND="$2"
      shift 2
      ;;
    *)
      echo "Unbekanntes Argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$PROJECT_ROOT" ]]; then
  echo "Nutzung: $0 <neuer-projekt-root> [--repo_url <url>] [--project_kind chat]" >&2
  exit 1
fi

case "$PROJECT_KIND" in
  code|chat|pkm) ;;
  *) echo "Unbekanntes --project_kind: $PROJECT_KIND (code|chat|pkm)" >&2; exit 1 ;;
esac

# Mit --repo_url wird das eigentliche Repo von aussen geklont: project/
# bleibt leer (kein Template-Seed, kein git init - die Quelle ist die
# geklonte Quelle, nicht ein leeres Repo).
PROJECT_SOURCE="empty"
if [[ -n "$REPO_URL" ]]; then
  PROJECT_SOURCE="cloned"
fi

if [[ -e "$PROJECT_ROOT" ]]; then
  if [[ ! -d "$PROJECT_ROOT" ]] || [[ -n "$(find "$PROJECT_ROOT" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
    echo "Fehler: $PROJECT_ROOT existiert bereits und ist nicht leer." >&2
    exit 1
  fi
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_SSH_CONFIG="${SCRIPT_DIR}/../templates/ssh_local/config"
TEMPLATE_GIT_CONFIG="${SCRIPT_DIR}/../templates/git_local/gitconfig"
TEMPLATE_OPENCODE_CONFIG="${SCRIPT_DIR}/../templates/opencode/opencode-basic.json"
TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/opencode/AGENTS.md"
if [[ "$PROJECT_KIND" == "chat" ]]; then
  TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/opencode/AGENTS_chat.md"
elif [[ "$PROJECT_KIND" == "pkm" ]]; then
  # pkm seedet IMMER das Wiki-Schema — auch bei geklonter Quelle (die in
  # project/ gelandete Repo-AGENTS.md wird nicht überschrieben, das koennen
  # wir hier stabil nicht; .opencode_config/ überlebt den In-Container-Clone).
  TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/pkm/AGENTS.md"
fi
TEMPLATE_OPENCODE_SKILLS="${SCRIPT_DIR}/../templates/opencode/skills"
TEMPLATE_BASH_PROFILE="${SCRIPT_DIR}/../templates/bash/bash_profile"

mkdir -p \
  "${PROJECT_ROOT}/project" \
  "${PROJECT_ROOT}/.opencode_config" \
  "${PROJECT_ROOT}/.opencode_data" \
  "${PROJECT_ROOT}/.ssh_local" \
  "${PROJECT_ROOT}/.git_local" \
  "${PROJECT_ROOT}/.cbm_cache"

# OpenCode-Web braucht ein Git-Repo als Worktree: ohne .git in project/
# faellt opencode auf das "global"-Projekt mit worktree "/" zurueck
# (Weboberflaeche: "+" disabled, kein Projekt anlegbar).
if [[ "$PROJECT_SOURCE" != "cloned" && ! -e "${PROJECT_ROOT}/project/.git" ]]; then
  git -C "${PROJECT_ROOT}/project" init -q
fi

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

if [[ -f "$TEMPLATE_OPENCODE_CONFIG" ]]; then
  cp "$TEMPLATE_OPENCODE_CONFIG" "${PROJECT_ROOT}/.opencode_config/opencode.json"
fi

if [[ -f "$TEMPLATE_OPENCODE_AGENTS" ]]; then
  cp "$TEMPLATE_OPENCODE_AGENTS" "${PROJECT_ROOT}/.opencode_config/AGENTS.md"
fi

if [[ -d "$TEMPLATE_OPENCODE_SKILLS" ]]; then
  cp -r "$TEMPLATE_OPENCODE_SKILLS" "${PROJECT_ROOT}/.opencode_config/skills"
fi

if [[ -f "$TEMPLATE_BASH_PROFILE" ]]; then
  mkdir -p "${PROJECT_ROOT}/.bash_local"
  cp "$TEMPLATE_BASH_PROFILE" "${PROJECT_ROOT}/.bash_local/bash_profile"
  chmod 644 "${PROJECT_ROOT}/.bash_local/bash_profile"
fi

# PKM-Seeding (creation-time only, issue #122): Template-Ordner, opencode.json
# Key-Merge, .gitignore-Append und Vault-Skelett — vor der üblichen
# Provider/Search-Seed-Kette (configure-project.sh läuft nach diesem Script).
PKM_TEMPLATE="${SCRIPT_DIR}/../templates/pkm"
if [[ "$PROJECT_KIND" == "pkm" ]]; then
  if [[ ! -d "$PKM_TEMPLATE" ]]; then
    echo "pkm-Template fehlt: $PKM_TEMPLATE" >&2
    exit 1
  fi

  for dir in agents commands skills; do
    [[ -d "$PKM_TEMPLATE/$dir" ]] || continue
    # In den vorhandenen Ziel-Ordner mergen (spätere Skill-Pulls fügen hinzu)
    mkdir -p "${PROJECT_ROOT}/.opencode_config/$dir"
    cp -r "$PKM_TEMPLATE/$dir/." "${PROJECT_ROOT}/.opencode_config/$dir/"
  done

  # default_agent + permissions aus dem pkm-Template (Plain-JSON, jq-tauglich)
  # in die seeded opencode.json mergen, ohne den Rest zu beschädigen.
  OC_JSON="${PROJECT_ROOT}/.opencode_config/opencode.json"
  if [[ -f "$PKM_TEMPLATE/opencode.jsonc" && -f "$OC_JSON" ]]; then
    if ! command -v jq >/dev/null 2>&1; then
      echo "jq nicht gefunden — opencode.json-Merge nicht möglich" >&2
      exit 1
    fi
    MERGE_TMP="$(mktemp)"
    if jq -s '.[0] * (.[1] | {default_agent, permissions})' "$OC_JSON" "$PKM_TEMPLATE/opencode.jsonc" > "$MERGE_TMP"; then
      mv "$MERGE_TMP" "$OC_JSON"
    else
      rm -f "$MERGE_TMP"
      echo "Fehler beim Mergen der pkm-Keys in $OC_JSON" >&2
      exit 1
    fi
  fi

  # Vault-Skelett im Projekt-Repo
  mkdir -p "${PROJECT_ROOT}/project/notes" "${PROJECT_ROOT}/project/raw" "${PROJECT_ROOT}/project/wiki"
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
.bash_local/
EOF

# PKM: .gitignore-Einträge des Wiki-Templates anhängen (Dedupe).
if [[ "$PROJECT_KIND" == "pkm" ]]; then
  if [[ -f "$PKM_TEMPLATE/.gitignore" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      [[ -n "$line" ]] || continue
      grep -qxF -- "$line" "${PROJECT_ROOT}/.gitignore" 2>/dev/null || printf '%s\n' "$line" >> "${PROJECT_ROOT}/.gitignore"
    done < "$PKM_TEMPLATE/.gitignore"
  fi
fi

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
