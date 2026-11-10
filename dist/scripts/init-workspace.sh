#!/usr/bin/env bash
#
# Creates the directory skeleton for a new workspace root:
#
#   <WORKSPACE_ROOT>/
#     workspace/            <- the actual repo (git clone / git init here)
#     .opencode_config/     <- OpenCode config (persistent, per-workspace)
#     .opencode_data/       <- OpenCode data incl. auth/credentials
#     .ssh_local/           <- SSH keys + config for this workspace
#     .git_local/           <- git identity/settings + optional credentials
#     .cbm_cache/           <- CBM graph database (persistent)
#
# Usage:
#   scripts/init-workspace.sh <workspace-root> [--repo_url <url>] [--workspace_kind chat]
#
set -euo pipefail

WORKSPACE_ROOT="${1:-}"
shift 2>/dev/null || true

REPO_URL=""
WORKSPACE_KIND="code"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo_url)
      [[ $# -ge 2 ]] || { echo "--repo_url requires a value" >&2; exit 1; }
      REPO_URL="$2"
      shift 2
      ;;
    --workspace_kind)
      [[ $# -ge 2 ]] || { echo "--workspace_kind requires a value" >&2; exit 1; }
      WORKSPACE_KIND="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$WORKSPACE_ROOT" ]]; then
  echo "Usage: $0 <workspace-root> [--repo_url <url>] [--workspace_kind chat]" >&2
  exit 1
fi

case "$WORKSPACE_KIND" in
  code|chat|pkm) ;;
  *) echo "Unknown --workspace_kind: $WORKSPACE_KIND (code|chat|pkm)" >&2; exit 1 ;;
esac

# With --repo_url the actual repo is cloned from outside: workspace/ is not
# seeded (no template seed, no git init — the source is the cloned repo,
# not an empty repo).
WORKSPACE_SOURCE="empty"
if [[ -n "$REPO_URL" ]]; then
  WORKSPACE_SOURCE="cloned"
fi

if [[ -e "$WORKSPACE_ROOT" ]]; then
  if [[ ! -d "$WORKSPACE_ROOT" ]] || [[ -n "$(find "$WORKSPACE_ROOT" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
    echo "Error: $WORKSPACE_ROOT already exists and is not empty." >&2
    exit 1
  fi
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_SSH_CONFIG="${SCRIPT_DIR}/../templates/ssh_local/config"
TEMPLATE_GIT_CONFIG="${SCRIPT_DIR}/../templates/git_local/gitconfig"
TEMPLATE_OPENCODE_CONFIG="${SCRIPT_DIR}/../templates/opencode/opencode-basic.json"
TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/opencode/AGENTS.md"
if [[ "$WORKSPACE_KIND" == "chat" ]]; then
  TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/opencode/AGENTS_chat.md"
elif [[ "$WORKSPACE_KIND" == "pkm" ]]; then
  # pkm always seeds the wiki schema — even from a cloned source (the repo
  # AGENTS.md landing in workspace/ is not overwritten; stable by design:
  # .opencode_config/ survives the in-container clone).
  TEMPLATE_OPENCODE_AGENTS="${SCRIPT_DIR}/../templates/pkm/AGENTS.md"
fi
TEMPLATE_OPENCODE_SKILLS="${SCRIPT_DIR}/../templates/opencode/skills"
TEMPLATE_BASH_PROFILE="${SCRIPT_DIR}/../templates/bash/bash_profile"

mkdir -p \
  "${WORKSPACE_ROOT}/workspace" \
  "${WORKSPACE_ROOT}/.opencode_config" \
  "${WORKSPACE_ROOT}/.opencode_data" \
  "${WORKSPACE_ROOT}/.ssh_local" \
  "${WORKSPACE_ROOT}/.git_local" \
  "${WORKSPACE_ROOT}/.cbm_cache"

# OpenCode web needs a git repo as worktree: without .git in workspace/
# opencode falls back to the "global" project with worktree "/" (web UI:
# "+" disabled, no project can be created).
if [[ "$WORKSPACE_SOURCE" != "cloned" && ! -e "${WORKSPACE_ROOT}/workspace/.git" ]]; then
  git -C "${WORKSPACE_ROOT}/workspace" init -q
fi

chmod 700 "${WORKSPACE_ROOT}/.ssh_local"
chmod 700 "${WORKSPACE_ROOT}/.git_local"

if [[ -f "$TEMPLATE_SSH_CONFIG" ]]; then
  cp "$TEMPLATE_SSH_CONFIG" "${WORKSPACE_ROOT}/.ssh_local/config"
  chmod 600 "${WORKSPACE_ROOT}/.ssh_local/config"
fi

if [[ -f "$TEMPLATE_GIT_CONFIG" ]]; then
  cp "$TEMPLATE_GIT_CONFIG" "${WORKSPACE_ROOT}/.git_local/gitconfig"
  chmod 600 "${WORKSPACE_ROOT}/.git_local/gitconfig"
fi

if [[ -f "$TEMPLATE_OPENCODE_CONFIG" ]]; then
  cp "$TEMPLATE_OPENCODE_CONFIG" "${WORKSPACE_ROOT}/.opencode_config/opencode.json"
fi

if [[ -f "$TEMPLATE_OPENCODE_AGENTS" ]]; then
  cp "$TEMPLATE_OPENCODE_AGENTS" "${WORKSPACE_ROOT}/.opencode_config/AGENTS.md"
fi

if [[ -d "$TEMPLATE_OPENCODE_SKILLS" ]]; then
  cp -r "$TEMPLATE_OPENCODE_SKILLS" "${WORKSPACE_ROOT}/.opencode_config/skills"
fi

if [[ -f "$TEMPLATE_BASH_PROFILE" ]]; then
  mkdir -p "${WORKSPACE_ROOT}/.bash_local"
  cp "$TEMPLATE_BASH_PROFILE" "${WORKSPACE_ROOT}/.bash_local/bash_profile"
  chmod 644 "${WORKSPACE_ROOT}/.bash_local/bash_profile"
fi

# PKM seeding (creation-time only, issue #122): template folders, opencode.json
# key merge, .gitignore append and vault skeleton — before the usual
# provider/search seed chain (configure-workspace.sh runs after this script).
PKM_TEMPLATE="${SCRIPT_DIR}/../templates/pkm"
if [[ "$WORKSPACE_KIND" == "pkm" ]]; then
  if [[ ! -d "$PKM_TEMPLATE" ]]; then
    echo "Error: pkm template missing: $PKM_TEMPLATE" >&2
    exit 1
  fi

  for dir in agents commands skills; do
    [[ -d "$PKM_TEMPLATE/$dir" ]] || continue
    # Merge into the existing target folder (later skill pulls add more)
    mkdir -p "${WORKSPACE_ROOT}/.opencode_config/$dir"
    cp -r "$PKM_TEMPLATE/$dir/." "${WORKSPACE_ROOT}/.opencode_config/$dir/"
  done

  # default_agent from the pkm template (plain JSON, jq-readable); permissions
  # are concatenated (base + pkm) so base rules (.env protection,
  # external_directory) survive like in the other modes.
  OC_JSON="${WORKSPACE_ROOT}/.opencode_config/opencode.json"
  if [[ -f "$PKM_TEMPLATE/opencode.jsonc" && -f "$OC_JSON" ]]; then
    if ! command -v jq >/dev/null 2>&1; then
      echo "Error: jq not found — cannot merge opencode.json" >&2
      exit 1
    fi
    MERGE_TMP="$(mktemp)"
    if jq -s '.[0] * {default_agent: .[1].default_agent} * {permissions: (.[0].permissions + .[1].permissions)}' "$OC_JSON" "$PKM_TEMPLATE/opencode.jsonc" > "$MERGE_TMP"; then
      mv "$MERGE_TMP" "$OC_JSON"
    else
      rm -f "$MERGE_TMP"
      echo "Error: merging pkm keys into $OC_JSON failed" >&2
      exit 1
    fi
  fi

  # Vault skeleton in the workspace repo
  mkdir -p "${WORKSPACE_ROOT}/workspace/notes" "${WORKSPACE_ROOT}/workspace/raw" "${WORKSPACE_ROOT}/workspace/wiki"
fi

SKILLS_TARGET="${WORKSPACE_ROOT}/.opencode_config/skills"
mkdir -p "$SKILLS_TARGET"
TEMP_DIR="$(mktemp -d)"

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
  echo "Warning: could not clone mattpocock/skills (no network?)." >&2
fi

if git clone --depth 1 https://github.com/shredingerlabs/shredinger-skills.git "$TEMP_DIR/shredinger" 2>/dev/null; then
  for skill_dir in "$TEMP_DIR/shredinger/skills"/*/; do
    name="$(basename "$skill_dir")"
    if [[ ! -d "${SKILLS_TARGET}/${name}" ]]; then
      cp -r "$skill_dir" "${SKILLS_TARGET}/${name}"
    fi
  done
else
  echo "Warning: could not clone shredingerlabs/shredinger-skills (no network?)." >&2
fi

rm -rf "$TEMP_DIR"

# Recommended .gitignore in case a repo is accidentally initialized in the
# WORKSPACE_ROOT instead of workspace/. It does not hurt as a safety net.
cat > "${WORKSPACE_ROOT}/.gitignore" <<'EOF'
.opencode_config/
.opencode_data/
.ssh_local/
.git_local/
.cbm_cache/
.bash_local/
EOF

# PKM: append the wiki template's .gitignore entries (dedupe).
if [[ "$WORKSPACE_KIND" == "pkm" ]]; then
  if [[ -f "$PKM_TEMPLATE/.gitignore" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      [[ -n "$line" ]] || continue
      grep -qxF -- "$line" "${WORKSPACE_ROOT}/.gitignore" 2>/dev/null || printf '%s\n' "$line" >> "${WORKSPACE_ROOT}/.gitignore"
    done < "$PKM_TEMPLATE/.gitignore"
  fi
fi

echo "Workspace root created: ${WORKSPACE_ROOT}"
