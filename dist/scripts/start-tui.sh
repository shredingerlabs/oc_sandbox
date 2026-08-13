#!/usr/bin/env bash
#
# OpenCode Sandbox TUI - Interactive Text User Interface
# Provides modern project management, container operations, and configuration
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TUI_ROOT="${TUI_ROOT:-$HOME/.oc-sandbox}"
GUM_BIN="${TUI_ROOT}/gum/gum"
TUI_MODE=""
GLOBAL_CONFIG=""
DEFAULT_PROJECT_PATH=""

cleanup() {
  echo "Cleaning up..."
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
  cat << 'EOF'
╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║              Welcome to OpenCode Sandbox TUI                 ║
║                                                              ║
║     Modern project management for containerized development  ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝

This wizard will guide you through the initial setup.

EOF
  wait_for_enter
}

prompt_for_path() {
  local prompt="$1"
  local default="$2"
  local result=""

  if [[ "$TUI_MODE" == "gum" ]]; then
    if [[ -x "$GUM_BIN" ]]; then
      # Use gum file for directory selection (path is positional argument)
      if result=$("$GUM_BIN" file --directory "$default" 2>/dev/null); then
        # Success - result contains the selected path
        :
      else
        # gum file failed, fall back to input
        result=$("$GUM_BIN" input --prompt="$prompt " --value="$default" 2>/dev/null) || result=""
      fi
    else
      read -e -p "$prompt " -i "$default" result < /dev/tty
    fi
  else
    read -e -p "$prompt " -i "$default" result < /dev/tty
  fi

  if [[ -z "$result" ]]; then
    result="$default"
  fi

  echo "$result"
}

create_global_config() {
  local project_path="$1"

  local config='{
    "default_project_path": "'"$project_path"'",
    "version": "1.0"
  }'

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
  local content="$2"
  local temp_file="${filepath}.tmp.$$"

  echo "$content" > "$temp_file"

  if [[ -f "$filepath" ]]; then
    backup_config "$filepath"
  fi

  mv "$temp_file" "$filepath"
}

backup_config() {
  local filepath="$1"
  local backup_dir="$HOME/.config/oc-sandbox/backups"
  local timestamp=$(date +%Y%m%d_%H%M%S)

  mkdir -p "$backup_dir"

  local filename=$(basename "$filepath")
  cp "$filepath" "${backup_dir}/${filename}.${timestamp}"

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
  # Show welcome message without waiting
  cat << 'EOF'
╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║              Welcome to OpenCode Sandbox TUI                 ║
║                                                              ║
║     Modern project management for containerized development  ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝

This wizard will guide you through the initial setup.

EOF

  local default_path="$HOME/oc-sandbox"
  local project_path=""

  while [[ -z "$project_path" ]]; do
    project_path=$(prompt_for_path "Enter default project path:" "$default_path")
    if [[ -z "$project_path" ]]; then
      echo "Path cannot be empty"
    fi
  done

  mkdir -p "$project_path"

  create_global_config "$project_path"

  if [[ ! -f "$HOME/.config/oc-sandbox/projects.json" ]]; then
    create_projects_json
  fi

  wait_for_enter
}

load_global_config() {
  GLOBAL_CONFIG=$(cat "$HOME/.config/oc-sandbox/global_config.json")
  DEFAULT_PROJECT_PATH=$(jq -r '.default_project_path' <<< "$GLOBAL_CONFIG")
}

wait_for_enter() {
  echo
  read -p "Press Enter to continue..." < /dev/tty
}

show_menu() {
  local title="$1"
  shift
  local options=("$@")

  if [[ "$TUI_MODE" == "gum" ]]; then
    "$GUM_BIN" choose --header="$title" "${options[@]}" --height="${#options[@]}"
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

  jq -r '.projects | sort_by(.last_used) | reverse | .[0].path' "$projects_json"
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

    local last_project=$(get_last_used_project)
    if [[ -n "$last_project" ]]; then
      local last_project_name=$(basename "$last_project")
      options+=("Start last used project ($last_project_name)")
    fi

    options+=("Open Project" "New Project" "Build Container" "Settings" "Exit")

    local choice=$(show_menu "OpenCode Sandbox" "${options[@]}")

    case "$choice" in
      "Start last used project"*)
        local project_name=$(echo "$choice" | sed 's/Start last used project (//' | sed 's/)$//')
        local project_data=$(get_project_by_name "$project_name")
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
  local projects=($(get_all_projects_ordered))

  if [[ ${#projects[@]} -eq 0 ]]; then
    echo "No projects found. Please create a new project first."
    wait_for_enter
    return
  fi

  local running_projects=($(get_running_containers))

  local menu_items=()
  for project in "${projects[@]}"; do
    local name=$(jq -r '.name' <<< "$project")
    local path=$(jq -r '.path' <<< "$project")
    local status=$(jq -r '.container_status' <<< "$project")
    local status_symbol="○"

    for running in "${running_projects[@]}"; do
      if [[ "$running" == "opencode-sandbox-${name}" ]]; then
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
  echo "New Project Wizard"
  echo "=================="
  echo

  local project_name=""
  while [[ -z "$project_name" ]]; do
    echo "Enter project name (alphanumeric, dashes, underscores only):"
    read -r project_name < /dev/tty
    if [[ ! "$project_name" =~ ^[a-zA-Z0-9_-]+$ ]]; then
      echo "Invalid project name. Please use only letters, numbers, dashes, and underscores."
      project_name=""
    fi
  done

  local default_path="$DEFAULT_PROJECT_PATH/$project_name"
  local project_path=$(prompt_for_path "Enter project path:" "$default_path")

  if ! validate_project_path "$project_path"; then
    echo "Invalid project path"
    wait_for_enter
    return 1
  fi

  echo "Initializing project at: $project_path"
  if ! run_init_project "$project_path"; then
    echo "Project initialization failed"
    wait_for_enter
    return 1
  fi

  select_container_edition "$project_path"
}

prompt_for_name() {
  local prompt="$1"
  local result=""

  if [[ "$TUI_MODE" == "gum" ]]; then
    if [[ -x "$GUM_BIN" ]]; then
      result=$("$GUM_BIN" input --prompt="$prompt " --placeholder="project-name" --validation.alphanumeric)
    else
      while [[ -z "$result" ]]; do
        read -p "$prompt " result < /dev/tty
        if [[ ! "$result" =~ ^[a-zA-Z0-9_-]+$ ]]; then
          echo "Invalid name. Use only letters, numbers, dashes, and underscores."
          result=""
        fi
      done
    fi
  else
    while [[ -z "$result" ]]; do
      read -p "$prompt " result < /dev/tty
      if [[ ! "$result" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "Invalid name. Use only letters, numbers, dashes, and underscores."
        result=""
      fi
    done
  fi

  echo "$result"
}

validate_project_path() {
  local path="$1"

  if [[ -e "$path" ]] && ! [[ -z "$(ls -A "$path" 2>/dev/null)" ]]; then
    return 1
  fi

  local parent_dir=$(dirname "$path")
  if [[ ! -d "$parent_dir" ]] || [[ ! -w "$parent_dir" ]]; then
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

  local options=("full" "web" "embedded" "base" "← Go Back")
  local edition=$(show_menu "Select container edition" "${options[@]}")

  if [[ "$edition" == "← Go Back" ]]; then
    return 1
  fi

  select_container_modes "$project_path" "$edition"
}

select_container_modes() {
  local project_path="$1"
  local edition="$2"

  local options=("use_proxy" "offline" "hil_mode" "cbm_ui" "Done" "← Go Back")
  local modes=()

  while true; do
    local mode=$(show_menu "Select container modes (Done when finished)" "${options[@]}")

    case "$mode" in
      "use_proxy"|"offline"|"hil_mode"|"cbm_ui")
        modes+=("$mode")
        ;;
      "Done")
        if [[ ${#modes[@]} -eq 0 ]]; then
          echo "Please select at least one mode"
        else
          select_start_option "$project_path" "$edition" "${modes[@]}"
          return
        fi
        ;;
      "← Go Back")
        select_container_edition "$project_path"
        return
        ;;
    esac
  done
}

select_start_option() {
  local project_path="$1"
  local edition="$2"
  shift 2
  local modes=("$@")

  local options=("console" "opencode" "← Go Back")
  local start_option=$(show_menu "Select start option" "${options[@]}")

  if [[ "$start_option" == "← Go Back" ]]; then
    select_container_modes "$project_path" "$edition"
    return
  fi

  select_vcs_tracking "$project_path" "$edition" "${modes[@]}" "$start_option"
}

select_vcs_tracking() {
  local project_path="$1"
  local edition="$2"
  shift 2
  local modes=("$@")
  local start_option="${modes[-1]}"
  unset 'modes[-1]'

  local options=("none" "github" "gitlab" "← Go Back")
  local vcs_tracking=$(show_menu "Select VCS tracking" "${options[@]}")

  if [[ "$vcs_tracking" == "← Go Back" ]]; then
    select_start_option "$project_path" "$edition" "${modes[@]}"
    return
  fi

  select_ai_provider "$project_path" "$edition" "${modes[@]}" "$start_option" "$vcs_tracking"
}

select_ai_provider() {
  local project_path="$1"
  local edition="$2"
  shift 2
  local modes=("$@")
  local start_option="${modes[-1]}"
  unset 'modes[-1]'
  local vcs_tracking="${modes[-1]}"
  unset 'modes[-1]'

  local options=("gwdg-saia" "none" "← Go Back")
  local ai_provider=$(show_menu "Select AI provider" "${options[@]}")

  if [[ "$ai_provider" == "← Go Back" ]]; then
    select_vcs_tracking "$project_path" "$edition" "${modes[@]}" "$start_option"
    return
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

  local project_name=$(basename "$project_path")
  add_project_to_registry "$project_name" "$project_path"

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

  echo "GitHub CLI credentials setup"
  echo "============================"
  echo "Please run 'gh auth login' inside the container to authenticate."
  echo
}

setup_gitlab_credentials() {
  local project_path="$1"
  local git_dir="${project_path}/.git_local/glab-cli"

  mkdir -p "$git_dir"

  echo "GitLab CLI credentials setup"
  echo "============================"
  echo "Please run 'glab auth login' inside the container to authenticate."
  echo
}

setup_gwdg_provider() {
  local project_path="$1"
  local data_dir="${project_path}/.opencode_data"

  mkdir -p "$data_dir"

  echo "GWDG SAIA provider setup"
  echo "======================="
  echo "Please configure your credentials in .opencode_data/auth.json"
  echo
}

add_project_to_registry() {
  local name="$1"
  local path="$2"
  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  local existing=$(get_project_by_name "$name")
  if [[ -n "$existing" ]]; then
    echo "Project already exists in registry"
    return 1
  fi

  jq --arg name "$name" --arg path "$path" --arg timestamp "$timestamp" \
    '.projects += [{"name": $name, "path": $path, "last_used": $timestamp, "container_status": "stopped", "git_tracking": "none"}]' \
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

  start_container "$project_path" "$config_path"

  if [[ "$setup_complete" == "true" ]]; then
    return 0
  fi

  run_first_run_setup "$project_path"
}

start_container() {
  local project_path="$1"
  local config_path="$2"

  local edition=$(jq -r '.container_edition' "$config_path")
  local modes=($(jq -r '.container_modes[]' "$config_path"))
  local start_option=$(jq -r '.start_option' "$config_path")

  local start_cmd="${SCRIPT_DIR}/start.sh ${project_path}"
  for mode in "${modes[@]}"; do
    start_cmd+=" --${mode}"
  done

  if [[ "$start_option" == "opencode" ]]; then
    start_cmd+=" --start_opencode"
  fi

  echo "Starting container..."
  if bash -c "$start_cmd"; then
    update_project_status "$project_path" "running"
  else
    echo "Failed to start container"
    return 1
  fi
}

run_first_run_setup() {
  local project_path="$1"
  local project_name=$(basename "$project_path")
  local container_name="opencode-sandbox-${project_name}"

  echo "Running first-run setup..."

  echo "Configuring Codebase Memory..."
  podman exec -it --user dev "$container_name" bash -c '
    opencode run "codebase-memory-mcp config set auto_index true" 2>/dev/null || true
    opencode run "codebase-memory-mcp config set auto_index_limit 50000" 2>/dev/null || true
    opencode run "codebase-memory-mcp config set auto_watch true" 2>/dev/null || true
  ' || true

  echo "Setting up skills..."
  podman exec -it --user dev "$container_name" bash -c 'opencode run "setup-matt-pocock-skills"' 2>/dev/null || true

  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  update_sandbox_config_field "$config_path" "setup_complete" "true"

  echo "Setup complete!"
  wait_for_enter
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

  jq --arg path "$project_path" --arg status "$status" \
    '.projects |= map(if .path == $path then .container_status = $status else . end)' \
    "$projects_json" | atomic_write "$projects_json"
}

update_last_used() {
  local project_path="$1"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

  jq --arg path "$project_path" --arg timestamp "$timestamp" \
    '.projects |= map(if .path == $path then .last_used = $timestamp else . end)' \
    "$projects_json" | atomic_write "$projects_json"
}

handle_project_action() {
  local project_data="$1"
  local project_path=$(jq -r '.path' <<< "$project_data")

  update_last_used "$project_path"

  local project_name=$(basename "$project_path")
  local container_name="opencode-sandbox-${project_name}"

  local is_running=false
  local running_containers=($(get_running_containers))
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
  local project_name=$(basename "$project_path")
  local container_name="opencode-sandbox-${project_name}"
  local start_option=$(jq -r '.start_option' "$config_path")

  if [[ "$start_option" == "opencode" ]]; then
    podman exec -it --user dev "$container_name" opencode
  else
    podman exec -it --user dev "$container_name" bash
  fi
}

build_container_wizard() {
  detect_available_editions

  local options=("all" "full" "web" "embedded" "base" "← Go Back")
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
  echo "Detecting available container editions..."
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

main "$@"