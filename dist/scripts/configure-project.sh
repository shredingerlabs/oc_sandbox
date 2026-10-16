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
#   ai-provider <project-root> --name <n> --endpoint <u> --token <t> [--replace]
#   opencode-config <project-root> --id <i> --name <n> --endpoint <u>
#   ai-provider-remove <project-root> --id <i>
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
  sed -n '2,26p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
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

# slugify_provider_id derives the provider id from the typed display name:
# lowercase, every run of non-[a-z0-9] becomes '-', leading/trailing '-' trimmed.
slugify_provider_id() {
  local name="$1"
  local id
  id=$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]' | sed -e 's/[^a-z0-9]\+/-/g' -e 's/^-\+//' -e 's/-\+$//')
  printf '%s' "$id"
}

# Validates provider inputs shared by ai-provider/opencode-config.
validate_provider_name() {
  local name="$1"
  [[ -n "$name" ]] || die "Provider name must not be empty" 2
  [[ "$name" == *[a-zA-Z0-9]* ]] || die "Provider name must contain at least one letter or digit" 2
  [[ "$name" != *$'\n'* ]] || die "Provider name must not contain newlines" 2
}

validate_provider_endpoint() {
  local endpoint="$1"
  [[ "$endpoint" =~ ^https?://[^[:space:]]+$ ]] ||
    die "Endpoint must be an http:// or https:// URL without whitespace (e.g. https://api.example.com/v1)" 2
}

validate_provider_token() {
  local token="$1"
  [[ -n "$token" ]] || die "Missing --token" 2
  [[ "$token" != *[[:space:]]* && "$token" != *$'\n'* ]] || die "Token must not contain whitespace or newlines" 2
}

# ai-provider: writes the OpenAI-compatible provider's credential into the
# project-local auth.json under the slugified provider id. --replace is
# required when the id already has an entry (the calling TUI confirms
# replacement with the user first).
cmd_ai_provider() {
  local project_path="" name="" endpoint="" token="" replace=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --name) [[ $# -ge 2 ]] || die "--name requires a value" 2; name="$2"; shift 2 ;;
      --endpoint) [[ $# -ge 2 ]] || die "--endpoint requires a value" 2; endpoint="$2"; shift 2 ;;
      --token) [[ $# -ge 2 ]] || die "--token requires a value" 2; token="$2"; shift 2 ;;
      --replace) replace=true; shift ;;
      *) [[ -z "$project_path" ]] || die "Unexpected argument: $1" 2; project_path="$1"; shift ;;
    esac
  done

  [[ -n "$project_path" ]] || die "Usage: $0 ai-provider <project-root> --name <n> --endpoint <u> --token <t> [--replace]" 2
  [[ -n "$name" ]] || die "Missing --name" 2
  [[ -n "$endpoint" ]] || die "Missing --endpoint" 2
  validate_provider_name "$name"
  validate_provider_endpoint "$endpoint"
  validate_provider_token "$token"

  local provider_id
  provider_id=$(slugify_provider_id "$name")
  [[ -n "$provider_id" ]] || die "Provider name has no usable characters: ${name}" 2

  local auth_file="${project_path}/.opencode_data/auth.json"
  local existing='' config

  if [[ -e "$auth_file" ]]; then
    existing=$(jq -c . "$auth_file") || die "Existing auth.json is not valid JSON"
    if jq -e --arg id "$provider_id" 'has($id)' <<< "$existing" >/dev/null && [[ "$replace" != "true" ]]; then
      die "auth.json already contains a ${provider_id} entry; pass --replace to overwrite" 2
    fi
  fi

  if [[ -n "$existing" ]]; then
    config=$(PROVIDER_ID="$provider_id" PROVIDER_TOKEN="$token" jq \
      'del(.[$ENV.PROVIDER_ID]) | . + {($ENV.PROVIDER_ID): {type: "api", key: $ENV.PROVIDER_TOKEN}}' <<< "$existing")
  else
    config=$(PROVIDER_ID="$provider_id" PROVIDER_TOKEN="$token" jq -n \
      '{($ENV.PROVIDER_ID): {type: "api", key: $ENV.PROVIDER_TOKEN}}')
  fi
  write_secret_file "$auth_file" "$config"
}

# opencode-config: merges the models-discovery plugin entry plus a generated
# provider.<id> block into the project-local opencode.json; when the file
# does not exist it is built from the basic template plus plugin plus the
# provider block. The modelsDiscovery options keep the unchanged GWDG-era
# settings.
cmd_opencode_config() {
  local project_path="" p_id="" name="" endpoint=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --id) [[ $# -ge 2 ]] || die "--id requires a value" 2; p_id="$2"; shift 2 ;;
      --name) [[ $# -ge 2 ]] || die "--name requires a value" 2; name="$2"; shift 2 ;;
      --endpoint) [[ $# -ge 2 ]] || die "--endpoint requires a value" 2; endpoint="$2"; shift 2 ;;
      -*) die "Unknown argument: $1" 2 ;;
      *) [[ -z "$project_path" ]] || die "Unexpected argument: $1" 2; project_path="$1"; shift ;;
    esac
  done

  [[ -n "$project_path" ]] || die "Usage: $0 opencode-config <project-root> --id <i> --name <n> --endpoint <u>" 2
  [[ -n "$p_id" ]] || die "Missing --id" 2
  [[ -n "$name" ]] || die "Missing --name" 2
  [[ -n "$endpoint" ]] || die "Missing --endpoint" 2
  validate_provider_endpoint "$endpoint"

  local config_file="${project_path}/.opencode_config/opencode.json"
  local basic_template="${SCRIPT_DIR}/../templates/opencode/opencode-basic.json"
  local config
  local provider_block
  provider_block=$(PROVIDER_ID="$p_id" PROVIDER_NAME="$name" PROVIDER_ENDPOINT="$endpoint" jq -n '
    {plugin: ["opencode-models-discovery@1.5.3"],
     provider: {($ENV.PROVIDER_ID): {
       npm: "@ai-sdk/openai-compatible",
       name: $ENV.PROVIDER_NAME,
       options: {
         baseURL: $ENV.PROVIDER_ENDPOINT,
         timeout: false,
         modelsDiscovery: {
           enabled: true,
           smartModelName: true,
           models: {
             excludeBy: [
               {field: "id", match: "embed|e5-|bge-|gte-|nomic-|instructor|rerank"}
             ]
           },
           cache: {enabled: true, ttlSeconds: 3600}
         }
       }
     }}}')

  if [[ -f "$config_file" ]]; then
    config=$(jq --argjson block "$provider_block" '
      .plugin = ((.plugin // []) + $block.plugin | unique) |
      .provider = ((.provider // {}) * $block.provider)
    ' "$config_file") || die "Merging provider block into opencode.json failed"
  else
    if [[ ! -f "$basic_template" ]]; then
      die "Basic OpenCode template not found: ${basic_template}"
    fi
    config=$(jq -c '. * $block' --argjson block "$provider_block" "$basic_template") ||
      die "Merging provider block into opencode.json failed"
  fi
  write_config_without_backup "$config_file" "$config"
}

# ai-provider-remove: deletes one provider entry from the project's own
# config files — the credential key in auth.json and the provider.<id>
# section in opencode.json. Unknown ids fail loudly; other entries,
# unrelated sections and the legacy sandbox_config record are untouched
# (ADR-0023).
cmd_ai_provider_remove() {
  local project_path="" p_id=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --id) [[ $# -ge 2 ]] || die "--id requires a value" 2; p_id="$2"; shift 2 ;;
      -*) die "Unknown argument: $1" 2 ;;
      *) [[ -z "$project_path" ]] || die "Unexpected argument: $1" 2; project_path="$1"; shift ;;
    esac
  done

  [[ -n "$project_path" ]] || die "Usage: $0 ai-provider-remove <project-root> --id <i>" 2
  [[ -n "$p_id" ]] || die "Missing --id" 2

  local auth_file="${project_path}/.opencode_data/auth.json"
  local config_file="${project_path}/.opencode_config/opencode.json"
  local existing='' in_auth=false in_config=false

  if [[ -e "$auth_file" ]]; then
    existing=$(jq -c . "$auth_file") || die "Existing auth.json is not valid JSON"
    jq -e --arg id "$p_id" 'has($id)' <<< "$existing" >/dev/null && in_auth=true
  fi
  if [[ -f "$config_file" ]]; then
    jq -e --arg id "$p_id" '.provider | type == "object" and has($id)' \
      "$config_file" >/dev/null 2>&1 && in_config=true
  fi

  if [[ "$in_auth" != "true" && "$in_config" != "true" ]]; then
    die "Unknown provider id: ${p_id}" 2
  fi

  if [[ "$in_auth" == "true" ]]; then
    local auth_config
    auth_config=$(jq --arg id "$p_id" 'del(.[$id])' <<< "$existing") ||
      die "Removing ${p_id} from auth.json failed"
    write_secret_file "$auth_file" "$auth_config"
  fi

  if [[ "$in_config" == "true" ]]; then
    local config
    config=$(jq --arg id "$p_id" 'del(.provider[$id])' "$config_file") ||
      die "Removing provider.${p_id} from opencode.json failed"
    write_config_without_backup "$config_file" "$config"
  fi
}

main() {
  local subcommand="${1:-}"
  shift 2>/dev/null || true

  case "$subcommand" in
    git-identity) cmd_git_identity "$@" ;;
    vcs-credentials) cmd_vcs_credentials "$@" ;;
    ai-provider) cmd_ai_provider "$@" ;;
    opencode-config) cmd_opencode_config "$@" ;;
    ai-provider-remove) cmd_ai_provider_remove "$@" ;;
    -h|--help|help|"") usage ;;
    *) die "Unknown subcommand: ${subcommand} (see --help)" 2 ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
