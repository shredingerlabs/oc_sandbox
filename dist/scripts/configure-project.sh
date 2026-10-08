#!/usr/bin/env bash
#
# OpenCode Sandbox - Project configuration backend
#
# Per-concern subcommands for project configuration, migrated from the
# inline logic in start-tui.sh. Both TUIs (bash/gum and the upcoming Go
# TUI) call this script instead of holding their own copies; prompts stay
# in the calling TUI, this script only validates and writes.
#
# Nutzung:
#   scripts/configure-project.sh <subcommand> ... (non-interactive flags)
#
# Subcommands:
#   git-identity <project-root> --name <n> --email <e>
#   vcs-credentials github <project-root> --token <t>
#   vcs-credentials gitlab <project-root> --token <t> [--host <h>]
#   vcs-credentials custom <project-root> --token <t> --host <h>
#   ai-provider gwdg <project-root> --token <t>
#   opencode-config gwdg <project-root>
#
# Exit codes: 0 success, 1 error, 2 usage error.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() {
  echo "Error: $1" >&2
  exit "${2:-1}"
}

usage() {
  sed -n '2,25p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 2
}

validate_project_root() {
  local project_path="$1"
  [[ -d "$project_path" ]] || die "Project root does not exist: ${project_path}"
}

# Validates a bare hostname: no scheme, no path, no spaces.
validate_vcs_host() {
  local host="$1"
  [[ "$host" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$ ]]
}

# Writes content atomically with restrictive permissions. Credential and
# secret files are never backed up (ADR 0009/0010: secrets are excluded
# from general configuration backups). The caller owns the replacement
# decision; existing files are overwritten.
write_secret_file() {
  local filepath="$1"
  local content="$2"
  local parent_dir
  parent_dir=$(dirname "$filepath")
  mkdir -p "$parent_dir"
  chmod 700 "$parent_dir"
  case "$filepath" in
    */.git_local/*) chmod 700 "${filepath%%/.git_local/*}/.git_local" ;;
    */.opencode_data/*) chmod 700 "${filepath%%/.opencode_data/*}/.opencode_data" ;;
  esac
  local temp_file
  temp_file=$(mktemp "${filepath}.tmp.XXXXXX")
  chmod 600 "$temp_file"
  printf '%s\n' "$content" > "$temp_file"
  mv -f -- "$temp_file" "$filepath"
  chmod 600 "$filepath"
}

# Writes a project config file atomically without creating a backup
# (same behavior as the previous inline implementation).
write_config_without_backup() {
  local filepath="$1"
  local content="$2"
  local parent_dir
  parent_dir=$(dirname "$filepath")
  mkdir -p "$parent_dir"
  local temp_file
  temp_file=$(mktemp "${filepath}.tmp.XXXXXX")
  printf '%s\n' "$content" > "$temp_file"
  if [[ -e "$filepath" ]]; then
    chmod "$(stat -c '%a' "$filepath")" "$temp_file"
  fi
  mv -f -- "$temp_file" "$filepath"
}

# git-identity: writes user.name / user.email into the project-local
# gitconfig. Values are supplied by the calling TUI's prompts.
cmd_git_identity() {
  local project_path="" name="" email=""

  if [[ $# -ge 1 ]]; then
    project_path="$1"
    shift
  fi

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --name) [[ $# -ge 2 ]] || die "--name requires a value" 2; name="$2"; shift 2 ;;
      --email) [[ $# -ge 2 ]] || die "--email requires a value" 2; email="$2"; shift 2 ;;
      *) die "Unknown argument: $1" 2 ;;
    esac
  done

  [[ -n "$project_path" ]] || die "Usage: $0 git-identity <project-root> --name <n> --email <e>" 2
  [[ -n "$name" ]] || die "Missing --name" 2
  [[ -n "$email" ]] || die "Missing --email" 2
  [[ "$name" != *$'\n'* && "$email" != *$'\n'* ]] || die "--name and --email must not contain newlines" 2

  local git_config="${project_path}/.git_local/gitconfig"
  mkdir -p "$(dirname "$git_config")" || die "Cannot create config directory under: ${project_path}"
  chmod 700 "${project_path}/.git_local"
  touch "$git_config"
  git config --file "$git_config" user.name "$name"
  git config --file "$git_config" user.email "$email"
  chmod 600 "$git_config"
}

# vcs-credentials: writes the CLI host file plus the git credential-store
# bridge entry for the captured token.
cmd_vcs_credentials() {
  local provider="${1:-}"
  shift 2>/dev/null || true

  local project_path="" token="" host="" replace=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --token) [[ $# -ge 2 ]] || die "--token requires a value" 2; token="$2"; shift 2 ;;
      --host) [[ $# -ge 2 ]] || die "--host requires a value" 2; host="$2"; shift 2 ;;
      --replace) replace=true; shift ;;
      -*) die "Unknown argument: $1" 2 ;;
      *) [[ -z "$project_path" ]] || die "Unexpected argument: $1" 2; project_path="$1"; shift ;;
    esac
  done

  [[ -n "$provider" ]] || die "Usage: $0 vcs-credentials github|gitlab|custom <project-root> --token <t> [--host <h>] [--replace]" 2
  [[ -n "$project_path" ]] || die "Missing project root" 2
  [[ -n "$token" ]] || die "Missing --token" 2
  [[ "$token" != *[[:space:]]* && "$token" != *$'\n'* ]] || die "Token must not contain whitespace or newlines" 2

  local credentials_file git_user config
  case "$provider" in
    github)
      host="github.com"
      credentials_file="${project_path}/.git_local/gh-cli/hosts.yml"
      git_user="oauth2"
      [[ "$replace" != "true" ]] || die "--replace is not needed for github (file is overwritten with a fresh token)" 2
      ;;
    gitlab)
      host="${host:-gitlab.com}"
      credentials_file="${project_path}/.git_local/glab-cli/config.yml"
      git_user="token"
      ;;
    custom)
      [[ -n "$host" ]] || die "custom provider requires --host" 2
      credentials_file="${project_path}/.git_local/vcs/hosts.yml"
      git_user="token"
      ;;
    *) die "Unknown provider: ${provider} (use github|gitlab|custom)" 2 ;;
  esac

  validate_vcs_host "$host" || die "Invalid VCS host: ${host}"

  case "$provider" in
    github) config=$(VCS_TOKEN="$token" jq -n --arg host "$host" \
      '{($host): {user: "oauth2", oauth_token: $ENV.VCS_TOKEN, git_protocol: "https"}}') ;;
    gitlab) config=$(VCS_TOKEN="$token" jq -n --arg host "$host" \
      '{editor: "vi", hosts: {($host): {token: $ENV.VCS_TOKEN}}}') ;;
    custom) config=$(VCS_TOKEN="$token" jq -n --arg host "$host" \
      '{($host): {token: $ENV.VCS_TOKEN}}') ;;
  esac

  write_secret_file "$credentials_file" "$config"

  local git_credentials_file="${project_path}/.git_local/credentials"
  write_secret_file "$git_credentials_file" "https://${git_user}:${token}@${host}"
}

# ai-provider gwdg: merges the GWDG SAIA token into the project-local
# auth.json, then wires the GWDG OpenCode config template. Passing
# --replace overwrites an existing gwdg-saia entry (the calling TUI
# confirms replacement with the user first; without --replace an
# existing entry is refused).
cmd_ai_provider() {
  local target="${1:-}"
  shift 2>/dev/null || true

  [[ "$target" == "gwdg" ]] || die "Unknown provider: ${target:-<none>} (use gwdg)" 2

  local project_path="" token="" replace=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --token) [[ $# -ge 2 ]] || die "--token requires a value" 2; token="$2"; shift 2 ;;
      --host) [[ $# -ge 2 ]] || die "--host requires a value" 2; host="$2"; shift 2 ;;
      --replace) replace=true; shift ;;
      -*) die "Unknown argument: $1" 2 ;;
      *) [[ -z "$project_path" ]] || die "Unexpected argument: $1" 2; project_path="$1"; shift ;;
    esac
  done

  [[ -n "$project_path" ]] || die "Usage: $0 ai-provider gwdg <project-root> --token <t> [--replace]" 2
  [[ -n "$token" ]] || die "Missing --token" 2
  [[ "$token" != *[[:space:]]* ]] || die "Token must not contain whitespace or newlines" 2

  local auth_file="${project_path}/.opencode_data/auth.json"
  local existing=''
  local config

  if [[ -e "$auth_file" ]]; then
    existing=$(jq -c . "$auth_file") || die "Existing auth.json is not valid JSON"
    if jq -e 'has("gwdg-saia")' <<< "$existing" >/dev/null && [[ "$replace" != "true" ]]; then
      die "auth.json already contains a gwdg-saia entry; pass --replace to overwrite"
    fi
  fi

  if [[ -n "$existing" ]]; then
    config=$(GWDG_TOKEN="$token" jq 'del(."gwdg-saia") | . + {"gwdg-saia": {type: "api", key: $ENV.GWDG_TOKEN}}' <<< "$existing")
  else
    config=$(GWDG_TOKEN="$token" jq -n '{"gwdg-saia": {type: "api", key: $ENV.GWDG_TOKEN}}')
  fi
  write_secret_file "$auth_file" "$config"
}

# opencode-config gwdg: merges the shipped GWDG template into the
# project-local opencode.json.
cmd_opencode_config() {
  local target="${1:-}"
  shift

  [[ "$target" == "gwdg" ]] || die "Unknown target: ${target:-<none>} (use gwdg)" 2

  [[ $# -ge 1 ]] || die "Usage: $0 opencode-config gwdg <project-root>" 2
  local project_path="$1"

  local config_file="${project_path}/.opencode_config/opencode.json"
  local template="${SCRIPT_DIR}/../templates/opencode/opencode-gwdg.json"
  [[ -f "$template" ]] || return 0

  local config
  if [[ -f "$config_file" ]]; then
    config=$(jq --slurpfile gwdg "$template" \
      'reduce ($gwdg[0] | keys[]) as $key (.; if $key == "provider" then
        .provider = ((.provider // {}) * $gwdg[0].provider) else .[$key] = $gwdg[0][$key] end)' \
      "$config_file") || die "Merging GWDG template into opencode.json failed"
  else
    config=$(<"$template")
  fi
  write_config_without_backup "$config_file" "$config"
}

main() {
  local subcommand="${1:-}"
  shift 2>/dev/null || true

  case "$subcommand" in
    git-identity) cmd_git_identity "$@" ;;
    vcs-credentials) cmd_vcs_credentials "$@" ;;
    ai-provider) cmd_ai_provider "$@" ;;
    opencode-config) cmd_opencode_config "$@" ;;
    -h|--help|help|"") usage ;;
    *) die "Unknown subcommand: ${subcommand} (see --help)" 2 ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
