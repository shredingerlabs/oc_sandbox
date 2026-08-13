#!/usr/bin/env bash
#
# OpenCode Sandbox TUI - Interactive Text User Interface
# Provides modern project management, container operations, and configuration
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TUI_ROOT="${TUI_ROOT:-$HOME/.oc-sandbox}"
GUM_BIN="${TUI_ROOT}/gum/gum"
TUI_MODE=""
GLOBAL_CONFIG=""
DEFAULT_PROJECT_PATH=""
AVAILABLE_EDITIONS=()
AVAILABLE_MODES=()

cleanup() {
  :
}

trap cleanup EXIT INT TERM

check_multiple_tui_instances() {
  local instances=$(pgrep -f "start-tui.sh" | wc -l)
  if [[ $instances -gt 1 ]]; then
    echo "Warning: Multiple TUI instances detected"
    echo "Concurrent operations may cause conflicts"
  fi
}

handle_resize() {
  if [[ "$TUI_MODE" == "gum" ]]; then
    local cols=$(tput cols)
    local rows=$(tput lines)

    if [[ $cols -lt 80 ]] || [[ $rows -lt 24 ]]; then
      echo "Warning: Terminal too small for optimal TUI experience"
      echo "Recommended: 80x24 or larger"
    fi
  fi
}

trap handle_resize SIGWINCH

initialize_tui() {
  INSTALL_ROOT="${TUI_ROOT:-$HOME/.oc-sandbox}"

  mkdir -p "$HOME/.config/oc-sandbox/backups"

  if [[ -x "$GUM_BIN" ]] || command -v gum &>/dev/null; then
    [[ -x "$GUM_BIN" ]] || GUM_BIN="$(command -v gum)"
    TUI_MODE="gum"
  else
    TUI_MODE="bash"
    echo "Warning: gum not available, falling back to bash select"
  fi

  if [[ ! -f "$HOME/.config/oc-sandbox/global_config.json" ]]; then
    handle_first_run_setup
  fi

  load_global_config
}

show_welcome_message() {
  show_page "Welcome to OpenCode Sandbox TUI" "This wizard will guide you through the initial setup."
  wait_for_enter
}

prompt_for_path() {
  local prompt="$1"
  local default="$2"
  local result=""

  if [[ "$TUI_MODE" == "gum" ]]; then
    result=$("$GUM_BIN" input --prompt="$prompt (Ctrl+A to replace): " --value="$default") || return 1
  else
    read -e -p "$prompt (Ctrl+A to replace): " -i "$default" result < /dev/tty || return 1
  fi

  if [[ -z "$result" ]]; then
    result="$default"
  fi

  normalize_project_path "$result" "$default"
}

normalize_project_path() {
  local path="$1"
  local default="$2"

  if [[ "$path" == "$default"* && "$path" != "$default" ]]; then
    path="${path#"$default"}"
    if [[ "$path" == /*/* ]]; then
      printf '%s\n' "$path"
      return
    fi
    printf '%s\n' "${default%/}/${path#./}"
    return
  fi

  if [[ "$path" == /* ]]; then
    printf '%s\n' "$path"
  else
    printf '%s\n' "${default%/}/${path#./}"
  fi
}

create_global_config() {
  local project_path="$1"

  local config
  config=$(jq -n --arg path "$project_path" '{default_project_path: $path, version: "1.0"}')

  atomic_write "$HOME/.config/oc-sandbox/global_config.json" "$config"
}

create_projects_json() {
  local projects='{
    "projects": [],
    "version": "1.0"
  }'

  atomic_write "$HOME/.config/oc-sandbox/projects.json" "$projects"
}

atomic_write() {
  local filepath="$1"
  local content=""
  if [[ $# -ge 2 ]]; then
    content="$2"
  else
    content=$(cat)
  fi
  mkdir -p "$(dirname "$filepath")"
  local temp_file
  temp_file=$(mktemp "${filepath}.tmp.XXXXXX")
  printf '%s\n' "$content" > "$temp_file"

  if [[ -f "$filepath" ]]; then
    backup_config "$filepath"
  fi

  mv "$temp_file" "$filepath"
}

backup_config() {
  local filepath="$1"
  local backup_dir="$HOME/.config/oc-sandbox/backups"
  mkdir -p "$backup_dir"

  local filename=$(basename "$filepath")
  local backup_file
  backup_file=$(mktemp "${backup_dir}/${filename}.XXXXXX")
  cp -- "$filepath" "$backup_file"

  rotate_backups "$backup_dir" "$filename"
}

rotate_backups() {
  local backup_dir="$1"
  local filename="$2"

  local backups=($(ls -t "${backup_dir}/${filename}".* 2>/dev/null))
  if [[ ${#backups[@]} -gt 5 ]]; then
    local excess=$(( ${#backups[@]} - 5 ))
    for ((i=5; i<${#backups[@]}; i++)); do
      rm "${backups[$i]}"
    done
  fi
}

handle_first_run_setup() {
  show_page "Welcome to OpenCode Sandbox TUI" "This wizard needs a default project path."

  local default_path="$HOME/oc-sandbox"
  local project_path=""

  while [[ -z "$project_path" ]]; do
    project_path=$(prompt_for_path "Default project path:" "$default_path") || return 1
    if [[ -z "$project_path" ]]; then
      echo "Path cannot be empty"
    fi
  done

  mkdir -p "$project_path"

  create_global_config "$project_path"

  if [[ ! -f "$HOME/.config/oc-sandbox/projects.json" ]]; then
    create_projects_json
  fi

  wait_for_enter || return 1
}

load_global_config() {
  GLOBAL_CONFIG=$(cat "$HOME/.config/oc-sandbox/global_config.json")
  DEFAULT_PROJECT_PATH=$(jq -r '.default_project_path' <<< "$GLOBAL_CONFIG")
}

wait_for_enter() {
  if [[ "$TUI_MODE" == "gum" ]]; then
    "$GUM_BIN" input --prompt="Press Enter to continue... " --placeholder="" >/dev/null || return 1
  else
    read -p "Press Enter to continue..." < /dev/tty || return 1
  fi
}

show_page() {
  local title="$1"
  shift
  if [[ "$TUI_MODE" == "gum" ]]; then
    "$GUM_BIN" style --border rounded --padding "1 2" -- "$title" "$@"
  else
    printf '\n%s\n' "$title"
    printf '%s\n' "$@"
  fi
}

show_menu() {
  local title="$1"
  shift
  local options=("$@")

  if [[ "$TUI_MODE" == "gum" ]]; then
    "$GUM_BIN" choose --header="$title" "${options[@]}" --height="${#options[@]}" || printf '%s\n' "← Go Back"
  else
    bash_select "$title" "${options[@]}"
  fi
}

bash_select() {
  local title="$1"
  shift
  local options=("$@")
  local PS3="$title
> "

  select choice in "${options[@]}"; do
    if [[ -n "$choice" ]]; then
      echo "$choice"
      break
    fi
  done
}

get_all_projects_ordered() {
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  if [[ ! -f "$projects_json" ]]; then
    return 1
  fi

  jq -r '.projects | sort_by(.last_used) | reverse | .[] | @json' "$projects_json"
}

get_last_used_project() {
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  if [[ ! -f "$projects_json" ]]; then
    return 1
  fi

  local count=$(jq -r '.projects | length' "$projects_json")
  if [[ "$count" -eq 0 ]]; then
    return 1
  fi

  jq -r '.projects | sort_by(.last_used) | reverse | .[0].container_id // empty' "$projects_json"
}

get_project_by_id() {
  local container_id="$1"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  jq -r --arg id "$container_id" '.projects[] | select(.container_id == $id) | @json' "$projects_json"
}

get_project_by_path() {
  local path
  path=$(canonicalize_project_path "$1") || return 1
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  jq -r --arg path "$path" '.projects[] | select(.path == $path) | @json' "$projects_json"
}

canonicalize_project_path() {
  local path="$1"
  realpath -m -- "$path"
}

project_container_identity() {
  local path
  path=$(canonicalize_project_path "$1") || return 1
  printf '%s' "$path" | sha256sum | cut -c1-12
}

container_name_for_project() {
  local project_data="$1"
  printf 'opencode-sandbox-%s\n' "$(jq -r '.container_id' <<< "$project_data")"
}

get_project_by_name() {
  local name="$1"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  jq -r --arg name "$name" '.projects[] | select(.name == $name) | @json' "$projects_json"
}

get_running_containers() {
  podman ps --format '{{.Names}}' --filter "name=opencode-sandbox-" 2>/dev/null || true
}

main_menu() {
  while true; do
    local options=()

    local last_project_id=$(get_last_used_project)
    if [[ -n "$last_project_id" ]]; then
      local last_project_data=$(get_project_by_id "$last_project_id")
      local last_project_name=$(jq -r '.name' <<< "$last_project_data")
      options+=("Start last used project ($last_project_name)")
    fi

    options+=("Open Project" "New Project" "Build Container" "Settings" "Exit")

    local choice=$(show_menu "OpenCode Sandbox" "${options[@]}")

    case "$choice" in
      "Start last used project"*)
        local project_data=$(get_project_by_id "$last_project_id")
        handle_project_action "$project_data"
        ;;
      "Open Project")
        project_selection_wizard
        ;;
      "New Project")
        init_project_wizard
        ;;
      "Build Container")
        build_container_wizard
        ;;
      "Settings")
        settings_menu
        ;;
      "Exit")
        exit 0
        ;;
    esac
  done
}

project_selection_wizard() {
  local projects=()
  mapfile -t projects < <(get_all_projects_ordered)

  if [[ ${#projects[@]} -eq 0 ]]; then
    show_page "No projects" "Create a new project first."
    wait_for_enter || true
    return
  fi

  local running_projects=()
  mapfile -t running_projects < <(get_running_containers)

  local menu_items=()
  for project in "${projects[@]}"; do
    local name=$(jq -r '.name' <<< "$project")
    local path=$(jq -r '.path' <<< "$project")
    local status=$(jq -r '.container_status' <<< "$project")
    local status_symbol="○"

    for running in "${running_projects[@]}"; do
      local container_id=$(jq -r '.container_id' <<< "$project")
      if [[ "$running" == "opencode-sandbox-${container_id}" ]]; then
        status_symbol="●"
        break
      fi
    done

    menu_items+=("${status_symbol} ${name} | ${path}")
  done

  menu_items+=("← Go Back")

  local choice=$(show_menu "Select Project" "${menu_items[@]}")

  if [[ "$choice" == "← Go Back" ]]; then
    return
  fi

  local selected_name=$(echo "$choice" | sed 's/^[●○] //' | sed 's/ |.*//')
  local project_data=$(get_project_by_name "$selected_name")

  handle_project_action "$project_data"
}

init_project_wizard() {
  show_page "New Project Wizard" "Choose a name and path, then configure the sandbox."

  local project_name
  project_name=$(prompt_for_name "Project name:") || return 0

  local default_path="$DEFAULT_PROJECT_PATH/$project_name"
  local project_path
  project_path=$(prompt_for_path "Project path:" "$default_path") || return 0
  project_path=$(canonicalize_project_path "$project_path") || return 0

  if [[ -n "$(get_project_by_name "$project_name")" ]]; then
    show_page "Duplicate project name" "Choose a unique display name."
    wait_for_enter || true
    return 0
  fi

  if ! validate_project_path "$project_path"; then
    show_page "Invalid project path" "The path must be empty or not exist, with a writable parent."
    wait_for_enter || true
    return 0
  fi

  select_container_edition "$project_path" "$project_name"
}

prompt_for_name() {
  local prompt="$1"
  local result=""

  while [[ ! "$result" =~ ^[a-zA-Z0-9_-]+$ ]]; do
    result=""
    if [[ "$TUI_MODE" == "gum" ]] && [[ -x "$GUM_BIN" ]]; then
      result=$("$GUM_BIN" input --prompt="$prompt " --placeholder="project-name") || return 1
    else
      read -p "$prompt " result < /dev/tty || return 1
    fi
    if [[ ! "$result" =~ ^[a-zA-Z0-9_-]+$ ]]; then
      echo "Invalid name. Use only letters, numbers, dashes, and underscores."
    fi
  done

  echo "$result"
}

validate_project_path() {
  local path="$1"

  if [[ -e "$path" ]] && { [[ ! -d "$path" ]] || [[ -n "$(find "$path" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; }; then
    return 1
  fi

  local parent_dir=$(dirname "$path")
  if [[ -e "$path" ]]; then
    [[ -w "$path" ]] || return 1
  elif [[ ! -d "$parent_dir" ]] || [[ ! -w "$parent_dir" ]]; then
    return 1
  fi

  return 0
}

run_init_project() {
  local project_path="$1"
  local init_script="${SCRIPT_DIR}/init-project.sh"

  if [[ ! -x "$init_script" ]]; then
    echo "Error: init-project.sh not found or not executable"
    return 1
  fi

  "$init_script" "$project_path"
}

select_container_edition() {
  local project_path="$1"
  local project_name="$2"

  detect_available_editions
  local options=("${AVAILABLE_EDITIONS[@]}" "← Go Back")
  local edition=$(show_menu "Select container edition" "${options[@]}")

  if [[ "$edition" == "← Go Back" ]]; then
    return 1
  fi

  select_container_modes "$project_path" "$project_name" "$edition"
}

select_container_modes() {
  local project_path="$1"
  local project_name="$2"
  local edition="$3"

  local options=("${AVAILABLE_MODES[@]}" "Done" "← Go Back")
  local modes=()

  while true; do
    local mode=$(show_menu "Select container modes (Done when finished)" "${options[@]}")

    case "$mode" in
      "Done")
        if [[ ${#modes[@]} -eq 0 ]]; then
          show_page "No mode selected" "Select at least one mode, or go back."
          wait_for_enter || true
        else
          select_start_option "$project_path" "$project_name" "$edition" "${modes[@]}"
          return
        fi
        ;;
      "← Go Back")
        select_container_edition "$project_path" "$project_name"
        return
        ;;
      *)
        modes+=("$mode")
        ;;
    esac
  done
}

select_start_option() {
  local project_path="$1"
  local project_name="$2"
  local edition="$3"
  shift 3
  local modes=("$@")

  local options=("console" "opencode" "← Go Back")
  local start_option=$(show_menu "Select start option" "${options[@]}")

  if [[ "$start_option" == "← Go Back" ]]; then
    select_container_modes "$project_path" "$project_name" "$edition"
    return
  fi

  select_vcs_tracking "$project_path" "$project_name" "$edition" "${modes[@]}" "$start_option"
}

select_vcs_tracking() {
  local project_path="$1"
  local project_name="$2"
  local edition="$3"
  shift 3
  local modes=("$@")
  local start_option="${modes[-1]}"
  unset 'modes[-1]'

  local options=("none" "github" "gitlab" "← Go Back")
  local vcs_tracking=$(show_menu "Select VCS tracking" "${options[@]}")

  if [[ "$vcs_tracking" == "← Go Back" ]]; then
    select_start_option "$project_path" "$project_name" "$edition" "${modes[@]}"
    return
  fi

  select_ai_provider "$project_path" "$project_name" "$edition" "${modes[@]}" "$start_option" "$vcs_tracking"
}

select_ai_provider() {
  local project_path="$1"
  local project_name="$2"
  local edition="$3"
  shift 3
  local modes=("$@")
  local vcs_tracking="${modes[-1]}"
  unset 'modes[-1]'
  local start_option="${modes[-1]}"
  unset 'modes[-1]'

  local options=("gwdg-saia" "none" "← Go Back")
  local ai_provider=$(show_menu "Select AI provider" "${options[@]}")

  if [[ "$ai_provider" == "← Go Back" ]]; then
    select_vcs_tracking "$project_path" "$project_name" "$edition" "${modes[@]}" "$start_option"
    return
  fi

  if ! run_init_project "$project_path"; then
    show_page "Project initialization failed" "No project state was registered."
    wait_for_enter || true
    return 0
  fi

  create_sandbox_config "$project_path" "$edition" "${modes[@]}" "$start_option" "$ai_provider"

  if [[ "$vcs_tracking" == "github" ]]; then
    setup_github_credentials "$project_path"
  elif [[ "$vcs_tracking" == "gitlab" ]]; then
    setup_gitlab_credentials "$project_path"
  fi

  if [[ "$ai_provider" == "gwdg-saia" ]]; then
    setup_gwdg_provider "$project_path"
  fi

  check_and_build_containers "$project_path"

  add_project_to_registry "$project_name" "$project_path" "$vcs_tracking"

  echo "Project created successfully!"
  start_container_with_setup "$project_path"
}

create_sandbox_config() {
  local project_path="$1"
  local edition="$2"
  shift 2
  local modes=("$@")
  local start_option="${modes[-1]}"
  unset 'modes[-1]'
  local ai_provider="${modes[-1]}"
  unset 'modes[-1]'

  local config_path="${project_path}/.opencode_config/sandbox_config.json"

  mkdir -p "$(dirname "$config_path")"

  local modes_json=$(printf '%s\n' "${modes[@]}" | jq -R . | jq -s .)

  local config=$(cat <<EOF
{
  "container_edition": "${edition}",
  "container_modes": ${modes_json},
  "start_option": "${start_option}",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "${ai_provider}",
  "setup_complete": false,
  "version": "1.0"
}
EOF
)

  atomic_write "$config_path" "$config"
}

setup_github_credentials() {
  local project_path="$1"
  local git_dir="${project_path}/.git_local/gh-cli"

  mkdir -p "$git_dir"

  show_page "GitHub credentials" "Run 'gh auth login' inside the container to authenticate."
}

setup_gitlab_credentials() {
  local project_path="$1"
  local git_dir="${project_path}/.git_local/glab-cli"

  mkdir -p "$git_dir"

  show_page "GitLab credentials" "Run 'glab auth login' inside the container to authenticate."
}

setup_gwdg_provider() {
  local project_path="$1"
  local data_dir="${project_path}/.opencode_data"

  mkdir -p "$data_dir"

  show_page "GWDG SAIA provider" "Configure credentials in .opencode_data/auth.json."
}

add_project_to_registry() {
  local name="$1"
  local path="$2"
  local vcs_tracking="$3"
  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local projects_json="$HOME/.config/oc-sandbox/projects.json"
  if [[ ! "$name" =~ ^[a-zA-Z0-9_-]+$ ]]; then
    echo "Invalid project name" >&2
    return 1
  fi
  path=$(canonicalize_project_path "$path") || return 1
  local container_id
  container_id=$(project_container_identity "$path") || return 1

  local existing=$(get_project_by_name "$name")
  if [[ -n "$existing" ]]; then
    echo "Project already exists in registry"
    return 1
  fi

  if [[ -n "$(get_project_by_id "$container_id")" ]]; then
    echo "Project path already exists in registry"
    return 1
  fi

  jq --arg name "$name" --arg path "$path" --arg id "$container_id" --arg timestamp "$timestamp" --arg vcs "$vcs_tracking" \
    '.projects += [{"name": $name, "path": $path, "container_id": $id, "last_used": $timestamp, "container_status": "stopped", "git_tracking": $vcs}]' \
    "$projects_json" | atomic_write "$projects_json"
}

check_and_build_containers() {
  local project_path="$1"
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local edition=$(jq -r '.container_edition' "$config_path")

  if ! check_container_images_exist "$edition"; then
    echo "Container images not found for edition: $edition"
    echo

    local options=("Build now" "Build later" "Go back")
    local choice=$(show_menu "Container images not found" "${options[@]}")

    case "$choice" in
      "Build now")
        build_container_wizard
        ;;
      "Build later")
        echo "Remember to build containers before starting project"
        ;;
      "Go back")
        return 1
        ;;
    esac
  fi
}

check_container_images_exist() {
  local edition="$1"

  podman image exists "opencode-sandbox-${edition}" 2>/dev/null
}

start_container_with_setup() {
  local project_path="$1"
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local setup_complete=$(jq -r '.setup_complete' "$config_path")

  start_container "$project_path" "$config_path" true

  if [[ "$setup_complete" == "true" ]]; then
    return 0
  fi

  run_first_run_setup "$project_path"
}

start_container() {
  local project_path="$1"
  local config_path="$2"
  local detached="${3:-false}"

  local edition=$(jq -r '.container_edition' "$config_path")
  local modes=()
  mapfile -t modes < <(jq -r '.container_modes[]' "$config_path")
  local start_option=$(jq -r '.start_option' "$config_path")

  local project_data
  project_data=$(get_project_by_path "$project_path")
  local container_id=$(jq -r '.container_id' <<< "$project_data")
  local start_args=("$project_path" "--edition" "$edition" "--container-id" "$container_id")
  for mode in "${modes[@]}"; do
    start_args+=("--${mode}")
  done

  if [[ "$start_option" == "opencode" && "$detached" != "true" ]]; then
    start_args+=("--start_opencode")
  fi
  [[ "$detached" == "true" ]] && start_args+=("--detach")

  echo "Starting container..."
  if bash "${SCRIPT_DIR}/start.sh" "${start_args[@]}"; then
    update_project_status "$project_path" "running"
  else
    echo "Failed to start container"
    return 1
  fi
}

run_first_run_setup() {
  local project_path="$1"
  local project_data=$(get_project_by_path "$project_path")
  local container_name=$(container_name_for_project "$project_data")

  echo "Running first-run setup..."

  echo "Configuring Codebase Memory..."
  if ! podman exec -it --user dev "$container_name" bash -c \
    'codebase-memory-mcp config set auto_index true &&
     codebase-memory-mcp config set auto_index_limit 50000 &&
     codebase-memory-mcp config set auto_watch true'; then
    show_page "Setup incomplete" "Codebase Memory configuration failed."
    wait_for_enter || true
    return 1
  fi

  echo "Setting up skills..."
  if ! podman exec -it --user dev "$container_name" bash -c 'opencode run "setup-matt-pocock-skills"'; then
    show_page "Setup incomplete" "Skills setup failed. Retry from the project menu."
    wait_for_enter || true
    return 1
  fi

  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  update_sandbox_config_field "$config_path" "setup_complete" "true"

  show_page "Setup complete" "The project container is ready."
  wait_for_enter || true
}

update_sandbox_config_field() {
  local config_path="$1"
  local field="$2"
  local value="$3"

  if [[ "$value" == "true" ]] || [[ "$value" == "false" ]]; then
    jq --arg field "$field" --argjson value "$value" '.[$field] = $value' "$config_path" | atomic_write "$config_path"
  else
    jq --arg field "$field" --arg value "$value" '.[$field] = $value' "$config_path" | atomic_write "$config_path"
  fi
}

update_project_status() {
  local project_path="$1"
  local status="$2"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  local container_id
  container_id=$(project_container_identity "$project_path")

  jq --arg id "$container_id" --arg status "$status" \
    '.projects |= map(if .container_id == $id then .container_status = $status else . end)' \
    "$projects_json" | atomic_write "$projects_json"
}

update_last_used() {
  local project_path="$1"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local container_id
  container_id=$(project_container_identity "$project_path")

  jq --arg id "$container_id" --arg timestamp "$timestamp" \
    '.projects |= map(if .container_id == $id then .last_used = $timestamp else . end)' \
    "$projects_json" | atomic_write "$projects_json"
}

handle_project_action() {
  local project_data="$1"
  local project_path=$(jq -r '.path' <<< "$project_data")

  update_last_used "$project_path"

  local container_name=$(container_name_for_project "$project_data")

  local is_running=false
  local running_containers=()
  mapfile -t running_containers < <(get_running_containers)
  for running in "${running_containers[@]}"; do
    if [[ "$running" == "$container_name" ]]; then
      is_running=true
      break
    fi
  done

  if [[ "$is_running" == "true" ]]; then
    access_running_container "$project_path"
  else
    local config_path="${project_path}/.opencode_config/sandbox_config.json"
    start_container "$project_path" "$config_path"
  fi
}

access_running_container() {
  local project_path="$1"
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local project_data
  project_data=$(get_project_by_path "$project_path")
  local container_name=$(container_name_for_project "$project_data")
  local start_option=$(jq -r '.start_option' "$config_path")

  if [[ "$start_option" == "opencode" ]]; then
    podman exec -it --user dev "$container_name" opencode
  else
    podman exec -it --user dev "$container_name" bash
  fi
}

build_container_wizard() {
  detect_available_editions

  local options=("all" "${AVAILABLE_EDITIONS[@]}" "← Go Back")
  local choice=$(show_menu "Select container edition to build" "${options[@]}")

  if [[ "$choice" == "← Go Back" ]]; then
    return
  fi

  local edition="$choice"
  local build_cmd="${SCRIPT_DIR}/build-container.sh ${edition}"

  echo "Building ${edition} containers..."
  if bash -c "$build_cmd"; then
    echo "Build successful!"
  else
    echo "Build failed. Check logs for details."
  fi

  wait_for_enter
}

detect_available_editions() {
  local help
  help=$(bash "${SCRIPT_DIR}/build-container.sh" --help 2>&1 || true)
  AVAILABLE_EDITIONS=()
  while read -r edition; do
    [[ -n "$edition" ]] && AVAILABLE_EDITIONS+=("$edition")
  done < <(grep -oE 'base|web|embedded|full' <<< "$help" | awk '!seen[$0]++')
  [[ ${#AVAILABLE_EDITIONS[@]} -gt 0 ]] || AVAILABLE_EDITIONS=(base web embedded full)

  local start_help
  start_help=$(bash "${SCRIPT_DIR}/start.sh" --help 2>&1 || true)
  AVAILABLE_MODES=()
  while read -r mode; do
    [[ -n "$mode" ]] && AVAILABLE_MODES+=("$mode")
  done < <(grep -oE -- '--[a-z_]+\s' <<< "$start_help" | sed 's/^--//; s/[[:space:]]*$//' | grep -Ev '^(start_opencode|edition|detach)$' | awk '!seen[$0]++')
  [[ ${#AVAILABLE_MODES[@]} -gt 0 ]] || AVAILABLE_MODES=(use_proxy offline hil_mode cbm_ui)
}

settings_menu() {
  while true; do
    local options=("Config Backup" "Config Restore" "← Back to Main Menu")
    local choice=$(show_menu "Settings" "${options[@]}")

    case "$choice" in
      "Config Backup")
        backup_config_manually
        ;;
      "Config Restore")
        restore_config
        ;;
      "← Back to Main Menu")
        return
        ;;
    esac
  done
}

backup_config_manually() {
  echo "Backing up configuration files..."
  local projects_json="$HOME/.config/oc-sandbox/projects.json"
  local global_config="$HOME/.config/oc-sandbox/global_config.json"

  backup_config "$projects_json"
  backup_config "$global_config"

  echo "Configuration backed up successfully"
  wait_for_enter
}

restore_config() {
  local backup_dir="$HOME/.config/oc-sandbox/backups"
  local configs=("projects.json" "global_config.json")

  echo "Available backups:"
  echo

  for config in "${configs[@]}"; do
    local backups=($(ls -t "${backup_dir}/${config}".* 2>/dev/null))
    if [[ ${#backups[@]} -gt 0 ]]; then
      echo "$config:"
      for backup in "${backups[@]}"; do
        echo "  - $(basename "$backup")"
      done
    fi
  done

  echo
  echo "To restore, manually copy files from $backup_dir"
  wait_for_enter
}

main() {
  check_multiple_tui_instances
  initialize_tui
  main_menu
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
