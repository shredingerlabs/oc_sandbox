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
TUI_TEMP_FILES=()

cleanup() {
  local temp_file
  for temp_file in "${TUI_TEMP_FILES[@]}"; do
    rm -f -- "$temp_file"
  done
  TUI_TEMP_FILES=()
}

handle_signal() {
  cleanup
  exit 130
}

trap cleanup EXIT
trap handle_signal INT TERM

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

  if ! is_general_config_file "$filepath"; then
    echo "Refusing to back up secret or unsupported configuration: $filepath" >&2
    return 1
  fi

  mkdir -p "$backup_dir"

  local filename
  filename=$(basename "$filepath")
  local backup_file
  backup_file=$(mktemp "${backup_dir}/${filename}.$(date -u +%Y%m%dT%H%M%S%N).XXXXXX")
  cp -- "$filepath" "$backup_file"

  rotate_backups "$backup_dir" "$filename"
}

rotate_backups() {
  local backup_dir="$1"
  local filename="$2"
  local history_limit="${BACKUP_HISTORY_LIMIT:-5}"
  local backups=()
  local backup

  [[ "$history_limit" =~ ^[1-9][0-9]*$ ]] || history_limit=5
  for backup in "$backup_dir/${filename}".*; do
    [[ -f "$backup" ]] && backups+=("$backup")
  done

  while [[ ${#backups[@]} -gt history_limit ]]; do
    local oldest="${backups[0]}"
    local oldest_mtime
    oldest_mtime=$(stat -c '%Y' -- "$oldest")
    local i mtime
    for ((i=1; i<${#backups[@]}; i++)); do
      mtime=$(stat -c '%Y' -- "${backups[$i]}")
      if [[ "$mtime" -lt "$oldest_mtime" ]] ||
         [[ "$mtime" -eq "$oldest_mtime" && "${backups[$i]}" < "$oldest" ]]; then
        oldest="${backups[$i]}"
        oldest_mtime="$mtime"
      fi
    done
    rm -f -- "$oldest"
    local remaining=()
    for backup in "${backups[@]}"; do
      [[ "$backup" != "$oldest" ]] && remaining+=("$backup")
    done
    backups=("${remaining[@]}")
  done
}

is_general_config_file() {
  local filepath
  filepath=$(realpath -m -- "$1") || return 1

  case "$filepath" in
    */.git_local/*|*/.opencode_data/*|*/auth.json|*/credentials|*/hosts.yml)
      return 1
      ;;
  esac

  [[ "$filepath" == "$(realpath -m -- "$HOME/.config/oc-sandbox/global_config.json")" ||
     "$filepath" == "$(realpath -m -- "$HOME/.config/oc-sandbox/projects.json")" ||
     "$filepath" == */.opencode_config/* ]]
}

supported_config_path() {
  case "$1" in
    projects.json) printf '%s\n' "$HOME/.config/oc-sandbox/projects.json" ;;
    global_config.json) printf '%s\n' "$HOME/.config/oc-sandbox/global_config.json" ;;
    *) return 1 ;;
  esac
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

handle_recoverable_failure() {
  local operation="$1"
  local choice

  show_page "${operation} failed" "Choose how to continue."
  choice=$(show_menu "${operation} failed" "Retry" "Go back" "Exit") || choice="Go back"
  case "$choice" in
    Retry) return 0 ;;
    "Go back"|"Go Back"|"← Go Back") return 1 ;;
    Exit) return 2 ;;
    *) return 1 ;;
  esac
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

  if ! detect_available_editions; then
    show_page "Native option discovery failed" "Unable to read supported editions and modes from the native scripts."
    wait_for_enter || true
    return 1
  fi
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

  local selected_modes=()

  while true; do
    local options=()
    local available_mode
    for available_mode in "${AVAILABLE_MODES[@]}"; do
      local status_symbol="○"
      if mode_is_selected "$available_mode" "${selected_modes[@]}"; then
        status_symbol="●"
      fi
      options+=("${status_symbol} ${available_mode}")
    done
    options+=("Done" "← Go Back")

    local mode=$(show_menu "Select container modes (toggle, then Done)" "${options[@]}")

    case "$mode" in
      "Done")
        local modes=()
        local candidate
        for candidate in "${AVAILABLE_MODES[@]}"; do
          if mode_is_selected "$candidate" "${selected_modes[@]}"; then
            modes+=("$candidate")
          fi
        done
        if ! validate_container_modes "${modes[@]}"; then
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
        mode="${mode#* }"
        if mode_is_selected "$mode" "${selected_modes[@]}"; then
          remove_selected_mode "$mode" selected_modes
        else
          selected_modes+=("$mode")
        fi
        ;;
    esac
  done
}

mode_is_selected() {
  local mode="$1"
  shift
  local selected
  for selected in "$@"; do
    [[ "$selected" == "$mode" ]] && return 0
  done
  return 1
}

remove_selected_mode() {
  local mode="$1"
  local array_name="$2"
  local -n selected_ref="$array_name"
  local remaining=()
  local selected
  for selected in "${selected_ref[@]}"; do
    [[ "$selected" == "$mode" ]] || remaining+=("$selected")
  done
  selected_ref=("${remaining[@]}")
}

validate_container_modes() {
  local has_offline=false
  local has_cbm_ui=false
  local mode
  for mode in "$@"; do
    [[ "$mode" == "offline" ]] && has_offline=true
    [[ "$mode" == "cbm_ui" ]] && has_cbm_ui=true
  done

  if [[ "$has_offline" == "true" && "$has_cbm_ui" == "true" ]]; then
    echo "Error: --offline cannot be combined with --cbm_ui." >&2
    show_page "Incompatible modes" "Offline mode cannot be combined with CBM UI."
    return 1
  fi
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

  local options=("none" "github.com" "gitlab.com" "own GitLab" "others" "← Go Back")
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
  update_sandbox_config_field "$project_path/.opencode_config/sandbox_config.json" "vcs_tracking" "$vcs_tracking"

  if [[ "$vcs_tracking" == "github.com" ]]; then
    setup_github_credentials "$project_path" || return 0
  elif [[ "$vcs_tracking" == "gitlab.com" ]]; then
    setup_gitlab_credentials "$project_path" gitlab.com || return 0
  elif [[ "$vcs_tracking" == "own GitLab" ]]; then
    setup_self_hosted_gitlab_credentials "$project_path" || return 0
  elif [[ "$vcs_tracking" == "others" ]]; then
    setup_custom_vcs_credentials "$project_path" || return 0
  fi

  if [[ "$vcs_tracking" != "none" ]]; then
    configure_git_identity "$project_path" || return 0
  fi

  if [[ "$ai_provider" == "gwdg-saia" ]]; then
    setup_gwdg_provider "$project_path" || return 0
  fi

  if check_and_build_containers "$project_path"; then
    add_project_to_registry "$project_name" "$project_path" "$vcs_tracking" || return 0
    echo "Project created successfully!"
    start_container_with_setup "$project_path" || {
      local start_result=$?
      [[ "$start_result" -eq 2 ]] && exit 1
      return 0
    }
  else
    case "$?" in
      2) add_project_to_registry "$project_name" "$project_path" "$vcs_tracking" || return 0
         show_page "Build deferred" "The project was registered stopped. Build its image before starting it." ;;
      3) select_container_edition "$project_path" "$project_name" ;;
      *) show_page "Project creation stopped" "The project was not registered because the container image is unavailable." ;;
    esac
    wait_for_enter || true
  fi
}

create_sandbox_config() {
  local project_path="$1"
  local edition="$2"
  shift 2
  local modes=("$@")
  local ai_provider="${modes[-1]}"
  unset 'modes[-1]'
  local start_option="${modes[-1]}"
  unset 'modes[-1]'

  local config_path="${project_path}/.opencode_config/sandbox_config.json"

  mkdir -p "$(dirname "$config_path")"

  local modes_json='[]'
  if [[ ${#modes[@]} -gt 0 ]]; then
    modes_json=$(printf '%s\n' "${modes[@]}" | jq -R . | jq -s .)
  fi

  local config
  config=$(jq -n \
    --arg edition "$edition" \
    --argjson modes "$modes_json" \
    --arg start_option "$start_option" \
    --arg ai_provider "$ai_provider" \
     '{container_edition: $edition, container_modes: $modes, start_option: $start_option,
       cbm_auto_index: true, cbm_auto_watch: true, ai_provider: $ai_provider,
       setup_cbm_complete: false, setup_skills_complete: false,
       setup_complete: false, version: "1.0"}')

  atomic_write "$config_path" "$config"
}

setup_github_credentials() {
  local project_path="$1"
  configure_vcs_credentials "$project_path" "github" "${project_path}/.git_local/gh-cli/hosts.yml"
}

setup_gitlab_credentials() {
  local project_path="$1"
  local host="${2:-gitlab.com}"
  configure_vcs_credentials "$project_path" "gitlab" "${project_path}/.git_local/glab-cli/hosts.yml" "$host"
}

setup_self_hosted_gitlab_credentials() {
  local project_path="$1"
  local host
  host=$(prompt_for_text "GitLab host (for example gitlab.example.com):") || return 1
  validate_vcs_host "$host" || {
    show_page "Invalid GitLab host" "Use a hostname without a scheme or path."
    return 1
  }
  configure_vcs_credentials "$project_path" "gitlab" "${project_path}/.git_local/glab-cli/hosts.yml" "$host"
}

setup_custom_vcs_credentials() {
  local project_path="$1"
  local host
  host=$(prompt_for_text "VCS host (for example git.example.com):") || return 1
  validate_vcs_host "$host" || {
    show_page "Invalid VCS host" "Use a hostname without a scheme or path."
    return 1
  }
  configure_vcs_credentials "$project_path" "custom" "${project_path}/.git_local/vcs/hosts.yml" "$host"
}

validate_vcs_host() {
  local host="$1"
  [[ "$host" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$ ]]
}

prompt_for_text() {
  local prompt="$1"
  local default="${2:-}"
  if [[ "$TUI_MODE" == "gum" ]] && [[ -x "$GUM_BIN" ]]; then
    "$GUM_BIN" input --prompt="$prompt " --value="$default" || return 1
  else
    local result
    read -r -p "$prompt " -i "$default" result < /dev/tty || return 1
    result="${result:-$default}"
    printf '%s\n' "$result"
  fi
}

configure_git_identity() {
  local project_path="$1"
  local git_config="${project_path}/.git_local/gitconfig"
  local current_name=""
  local current_email=""

  if [[ -f "$git_config" ]]; then
    current_name=$(git config --file "$git_config" --get user.name || true)
    current_email=$(git config --file "$git_config" --get user.email || true)
  fi

  local name email
  name=$(prompt_for_text "Git user.name:" "$current_name") || return 1
  email=$(prompt_for_text "Git user.email:" "$current_email") || return 1
  [[ -n "$name" && -n "$email" ]] || {
    show_page "Invalid Git identity" "user.name and user.email cannot be empty."
    return 1
  }

  mkdir -p "$(dirname "$git_config")"
  chmod 700 "${project_path}/.git_local"
  touch "$git_config"
  git config --file "$git_config" user.name "$name"
  git config --file "$git_config" user.email "$email"
  chmod 600 "$git_config"
}

prompt_for_secret() {
  local prompt="$1"
  if [[ "$TUI_MODE" == "gum" ]] && [[ -x "$GUM_BIN" ]]; then
    "$GUM_BIN" input --password --prompt="$prompt " || return 1
  else
    local result
    read -r -s -p "$prompt " result < /dev/tty || return 1
    printf '\n' >&2
    printf '%s\n' "$result"
  fi
}

confirm_credential_replacement() {
  [[ -e "$1" ]] || return 0
  local choice
  choice=$(show_menu "Credentials already exist" "Keep existing" "Replace") || return 1
  [[ "$choice" == "Replace" ]]
}

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
  TUI_TEMP_FILES+=("$temp_file")
  chmod 600 "$temp_file"
  printf '%s\n' "$content" > "$temp_file"
  mv -f -- "$temp_file" "$filepath"
  chmod 600 "$filepath"
}

configure_vcs_credentials() {
  local project_path="$1"
  local provider="$2"
  local credentials_file="$3"
  local host="${4:-}"

  if [[ -e "$credentials_file" ]] && ! confirm_credential_replacement "$credentials_file"; then
    return 0
  fi

  case "$provider" in
    github) host="github.com"; show_page "GitHub credentials" "Enter a token for github.com." ;;
    gitlab) host="${host:-gitlab.com}"; show_page "GitLab credentials" "Enter a token for ${host}." ;;
    custom) show_page "Custom VCS credentials" "Enter a token for ${host}." ;;
  esac

  local token
  token=$(prompt_for_secret "Token:") || return 1
  [[ -n "$token" ]] || return 1

  local config
  case "$provider" in
    github) config=$(VCS_TOKEN="$token" jq -n --arg host "$host" \
      '{($host): {user: "oauth2", oauth_token: $ENV.VCS_TOKEN, git_protocol: "https"}}') ;;
    gitlab) config=$(VCS_TOKEN="$token" jq -n --arg host "$host" \
      '{($host): {token: $ENV.VCS_TOKEN}}') ;;
    custom) config=$(VCS_TOKEN="$token" jq -n --arg host "$host" \
      '{($host): {token: $ENV.VCS_TOKEN}}') ;;
  esac
  write_secret_file "$credentials_file" "$config"
}

setup_gwdg_provider() {
  local project_path="$1"
  local auth_file="${project_path}/.opencode_data/auth.json"
  local existing=''

  if [[ -e "$auth_file" ]]; then
    existing=$(jq -c . "$auth_file") || {
      show_page "Invalid AI credentials" "Existing auth.json is not valid JSON."
      return 1
    }
    if jq -e 'has("gwdg-saia")' <<< "$existing" >/dev/null && \
      ! confirm_credential_replacement "$auth_file"; then
      return 0
    fi
  fi

  show_page "GWDG SAIA provider" "Enter the GWDG SAIA token."
  local token
  token=$(prompt_for_secret "Token:") || return 1
  [[ -n "$token" ]] || return 1

  local config
  if [[ -n "$existing" ]]; then
    config=$(GWDG_TOKEN="$token" jq '. + {"gwdg-saia": {type: "api", key: $ENV.GWDG_TOKEN}}' <<< "$existing")
  else
    config=$(GWDG_TOKEN="$token" jq -n '{"gwdg-saia": {type: "api", key: $ENV.GWDG_TOKEN}}')
  fi
  write_secret_file "$auth_file" "$config"
  configure_gwdg_opencode_config "$project_path"
}

configure_gwdg_opencode_config() {
  local project_path="$1"
  local config_file="${project_path}/.opencode_config/opencode.json"
  local template="${SCRIPT_DIR}/../templates/opencode/opencode-gwdg.json"
  [[ -f "$template" ]] || return 0

  local config
  if [[ -f "$config_file" ]]; then
    config=$(jq --slurpfile gwdg "$template" \
      '. + {provider: ((.provider // {}) * $gwdg[0].provider), model: $gwdg[0].model,
        small_model: $gwdg[0].small_model, agent: $gwdg[0].agent}' "$config_file") || return 1
  else
    config=$(<"$template")
  fi
  write_config_without_backup "$config_file" "$config"
}

write_config_without_backup() {
  local filepath="$1"
  local content="$2"
  local parent_dir
  parent_dir=$(dirname "$filepath")
  mkdir -p "$parent_dir"
  local temp_file
  temp_file=$(mktemp "${filepath}.tmp.XXXXXX")
  TUI_TEMP_FILES+=("$temp_file")
  printf '%s\n' "$content" > "$temp_file"
  if [[ -e "$filepath" ]]; then
    chmod "$(stat -c '%a' "$filepath")" "$temp_file"
  fi
  mv -f -- "$temp_file" "$filepath"
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

  if check_container_images_exist "$edition"; then
    return 0
  fi

  echo "Container images not found for edition: $edition"
  echo

  local options=("Build now" "Build later" "Go back")
  local choice=$(show_menu "Container images not found" "${options[@]}")

  case "$choice" in
    "Build now")
      while true; do
        if build_container_for_edition "$edition" && check_container_images_exist "$edition"; then
          return 0
        fi
        show_page "Build failed" "The ${edition} image is still unavailable. Check the build output."
        if handle_recoverable_failure "Container build"; then
          continue
        fi
        [[ $? -eq 2 ]] && exit 1
        return 3
      done
      ;;
    "Build later")
      echo "Remember to build containers before starting project"
      return 2
      ;;
    "Go back")
      return 3
      ;;
  esac

  return 1
}

check_container_images_exist() {
  local edition="$1"

  podman image exists "opencode-sandbox-${edition}" 2>/dev/null
}

start_container_with_setup() {
  local project_path="$1"
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local setup_complete=$(jq -r '.setup_complete' "$config_path")

  while true; do
    if start_container "$project_path" "$config_path" true; then
      break
    fi
    local recovery_result
    if handle_recoverable_failure "Starting container"; then
      revisit_project_settings "$project_path" || return 1
    else
      recovery_result=$?
      case "$recovery_result" in
        2) return 2 ;;
        *) return 1 ;;
      esac
    fi
  done

  if [[ "$setup_complete" == "true" ]]; then
    access_running_container "$project_path"
    return $?
  fi

  if ! run_first_run_setup "$project_path"; then
    return 1
  fi

  access_running_container "$project_path"
}

show_menu_prefilled() {
  local title="$1"
  local selected="$2"
  shift 2
  if [[ "$TUI_MODE" == "gum" ]]; then
    "$GUM_BIN" choose --header="$title" --selected="$selected" "$@" || printf '%s\n' "← Go Back"
  else
    local options=("$selected")
    local option
    for option in "$@"; do
      [[ "$option" == "$selected" ]] || options+=("$option")
    done
    show_menu "$title (current: $selected)" "${options[@]}"
  fi
}

revisit_project_settings() {
  local project_path="$1"
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local edition current_start current_ai current_vcs
  edition=$(jq -r '.container_edition' "$config_path")
  current_start=$(jq -r '.start_option' "$config_path")
  current_ai=$(jq -r '.ai_provider' "$config_path")
  current_vcs=$(jq -r '.vcs_tracking // "none"' "$config_path")

  detect_available_editions || return 1
  local edition_choice
  edition_choice=$(show_menu_prefilled "Select container edition" "$edition" "${AVAILABLE_EDITIONS[@]}" "← Go Back")
  [[ "$edition_choice" != "← Go Back" ]] || return 1
  edition="$edition_choice"

  local selected_modes=()
  mapfile -t selected_modes < <(jq -r '.container_modes[]' "$config_path")
  while true; do
    local mode_options=()
    local available_mode status_symbol
    for available_mode in "${AVAILABLE_MODES[@]}"; do
      status_symbol="○"
      mode_is_selected "$available_mode" "${selected_modes[@]}" && status_symbol="●"
      mode_options+=("${status_symbol} ${available_mode}")
    done
    mode_options+=("Done" "← Go Back")
    local mode_choice
    mode_choice=$(show_menu "Select container modes (current selections marked)" "${mode_options[@]}")
    case "$mode_choice" in
      Done) break ;;
      "← Go Back") return 1 ;;
      *)
        mode_choice="${mode_choice#* }"
        if mode_is_selected "$mode_choice" "${selected_modes[@]}"; then
          remove_selected_mode "$mode_choice" selected_modes
        else
          selected_modes+=("$mode_choice")
        fi
        ;;
    esac
  done
  validate_container_modes "${selected_modes[@]}" || return 1

  local start_choice
  start_choice=$(show_menu_prefilled "Select start option" "$current_start" console opencode "← Go Back")
  [[ "$start_choice" != "← Go Back" ]] || return 1

  local vcs_choice
  vcs_choice=$(show_menu_prefilled "Select VCS tracking" "$current_vcs" none "github.com" "gitlab.com" "own GitLab" "others" "← Go Back")
  [[ "$vcs_choice" != "← Go Back" ]] || return 1

  local ai_choice
  ai_choice=$(show_menu_prefilled "Select AI provider" "$current_ai" gwdg-saia none "← Go Back")
  [[ "$ai_choice" != "← Go Back" ]] || return 1

  local vcs_credentials_file=""
  case "$vcs_choice" in
    github.com) vcs_credentials_file="${project_path}/.git_local/gh-cli/hosts.yml" ;;
    gitlab.com|"own GitLab") vcs_credentials_file="${project_path}/.git_local/glab-cli/hosts.yml" ;;
    others) vcs_credentials_file="${project_path}/.git_local/vcs/hosts.yml" ;;
  esac
  if [[ "$vcs_choice" != "$current_vcs" || ( "$vcs_choice" != "none" && ! -e "$vcs_credentials_file" ) ]]; then
    case "$vcs_choice" in
      github.com) setup_github_credentials "$project_path" || return 1 ;;
      gitlab.com) setup_gitlab_credentials "$project_path" gitlab.com || return 1 ;;
      "own GitLab") setup_self_hosted_gitlab_credentials "$project_path" || return 1 ;;
      others) setup_custom_vcs_credentials "$project_path" || return 1 ;;
    esac
  fi
  if [[ "$vcs_choice" != "none" ]]; then
    configure_git_identity "$project_path" || return 1
  fi
  if [[ "$ai_choice" == "gwdg-saia" && ( "$ai_choice" != "$current_ai" || ! -e "${project_path}/.opencode_data/auth.json" ) ]]; then
    setup_gwdg_provider "$project_path" || return 1
  fi

  local modes_json='[]'
  if [[ ${#selected_modes[@]} -gt 0 ]]; then
    modes_json=$(printf '%s\n' "${selected_modes[@]}" | jq -R . | jq -s .)
  fi
  jq --arg edition "$edition" --argjson modes "$modes_json" \
    --arg start "$start_choice" --arg vcs "$vcs_choice" --arg ai "$ai_choice" \
    '.container_edition=$edition | .container_modes=$modes | .start_option=$start |
     .vcs_tracking=$vcs | .ai_provider=$ai' "$config_path" | atomic_write "$config_path"
}

start_container() {
  local project_path="$1"
  local config_path="$2"
  local detached="${3:-false}"

  local edition=$(jq -r '.container_edition' "$config_path")
  local modes=()
  mapfile -t modes < <(jq -r '.container_modes[]' "$config_path")
  local start_option=$(jq -r '.start_option' "$config_path")

  if ! validate_container_modes "${modes[@]}"; then
    echo "Container start aborted due to incompatible modes." >&2
    return 1
  fi

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
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local cbm_complete
  local skills_complete

  echo "Running first-run setup..."

  while true; do
    cbm_complete=$(jq -r '.setup_cbm_complete // false' "$config_path")
    skills_complete=$(jq -r '.setup_skills_complete // false' "$config_path")

    if [[ "$cbm_complete" != "true" ]]; then
      echo "Configuring Codebase Memory..."
      if podman exec --user dev "$container_name" bash -c \
        'codebase-memory-mcp config set auto_index true &&
         codebase-memory-mcp config set auto_index_limit 50000 &&
         codebase-memory-mcp config set auto_watch true'; then
        if ! update_sandbox_config_field "$config_path" "setup_cbm_complete" "true"; then
          show_page "Setup incomplete" "Could not save Codebase Memory setup state."
          if handle_recoverable_failure "Saving setup state"; then
            continue
          fi
          [[ $? -eq 2 ]] && exit 1
          return 1
        fi
      else
        show_page "Setup incomplete" "Codebase Memory configuration failed."
        if handle_recoverable_failure "Codebase Memory setup"; then
          continue
        fi
        [[ $? -eq 2 ]] && exit 1
        return 1
      fi
    fi

    if [[ "$skills_complete" != "true" ]]; then
      echo "Setting up skills..."
      if podman exec -it --user dev "$container_name" opencode run "setup-matt-pocock-skills"; then
        if ! update_sandbox_config_field "$config_path" "setup_skills_complete" "true"; then
          show_page "Setup incomplete" "Could not save skills setup state."
          if handle_recoverable_failure "Saving setup state"; then
            continue
          fi
          [[ $? -eq 2 ]] && exit 1
          return 1
        fi
      else
        show_page "Setup incomplete" "Skills setup failed."
        if handle_recoverable_failure "Skills setup"; then
          continue
        fi
        [[ $? -eq 2 ]] && exit 1
        return 1
      fi
    fi

    if update_sandbox_config_field "$config_path" "setup_complete" "true"; then
      show_page "Setup complete" "The project container is ready."
      wait_for_enter || true
      return 0
    fi

    if handle_recoverable_failure "Saving setup state"; then
      continue
    fi
    local recovery_result=$?
    [[ "$recovery_result" -eq 2 ]] && exit 1
    return 1
  done
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
    update_project_status "$project_path" "running"
  else
    update_project_status "$project_path" "stopped"
  fi

  if [[ "$is_running" == "true" ]]; then
    if [[ "$(jq -r '.setup_complete' "${project_path}/.opencode_config/sandbox_config.json")" != "true" ]]; then
      local setup_choice
      setup_choice=$(show_menu "Setup incomplete" "Retry setup" "Go Back")
      [[ "$setup_choice" == "Retry setup" ]] || return 0
      run_first_run_setup "$project_path" || return 1
    fi
    access_running_container "$project_path"
  else
    local config_path="${project_path}/.opencode_config/sandbox_config.json"
    check_and_build_containers "$project_path" || return 1
    start_container_with_setup "$project_path" || {
      local start_result=$?
      [[ "$start_result" -eq 2 ]] && exit 1
      return 0
    }
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
  if ! detect_available_editions; then
    show_page "Native option discovery failed" "Unable to read supported editions and modes from the native scripts."
    wait_for_enter || true
    return 1
  fi

  local options=("all" "${AVAILABLE_EDITIONS[@]}" "← Go Back")
  local choice=$(show_menu "Select container edition to build" "${options[@]}")

  if [[ "$choice" == "← Go Back" ]]; then
    return
  fi

  local edition="$choice"
  if build_container_for_edition "$edition"; then
    echo "Build successful!"
  else
    echo "Build failed. Check logs for details."
  fi

  wait_for_enter
}

build_container_for_edition() {
  local edition="$1"
  echo "Building ${edition} containers..."
  bash "${SCRIPT_DIR}/build-container.sh" "$edition"
}

detect_available_editions() {
  local help
  if ! help=$(bash "${SCRIPT_DIR}/build-container.sh" --help 2>&1); then
    echo "Error: unable to discover container editions from build-container.sh." >&2
    return 1
  fi
  AVAILABLE_EDITIONS=()
  while read -r edition; do
    [[ -n "$edition" ]] && AVAILABLE_EDITIONS+=("$edition")
  done < <(sed -nE 's/.*\[([^]]+)\].*/\1/p' <<< "$help" | tr '|' '\n' | grep -Ev '^all$' | awk '!seen[$0]++')
  if [[ ${#AVAILABLE_EDITIONS[@]} -eq 0 ]]; then
    echo "Error: build-container.sh help did not list any supported editions." >&2
    return 1
  fi

  local start_help
  if ! start_help=$(bash "${SCRIPT_DIR}/start.sh" --help 2>&1); then
    echo "Error: unable to discover container modes from start.sh." >&2
    return 1
  fi
  AVAILABLE_MODES=()
  while read -r mode; do
    [[ -n "$mode" ]] && AVAILABLE_MODES+=("$mode")
  done < <(grep -oE -- '(^|[[:space:]])--[a-zA-Z0-9_-]+' <<< "$start_help" | sed -E 's/^[[:space:]]*--//' | grep -Ev '^(start_opencode|edition|detach|container-id|help)$' | awk '!seen[$0]++')
  if [[ ${#AVAILABLE_MODES[@]} -eq 0 ]]; then
    echo "Error: start.sh help did not list any supported modes." >&2
    return 1
  fi
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

  if [[ -f "$projects_json" ]]; then
    backup_config "$projects_json" || return 1
  fi
  if [[ -f "$global_config" ]]; then
    backup_config "$global_config" || return 1
  fi

  echo "Configuration backed up successfully"
  wait_for_enter
}

restore_config() {
  local backup_dir="$HOME/.config/oc-sandbox/backups"
  local configs=("projects.json" "global_config.json")
  local available_configs=()
  local config backup

  for config in "${configs[@]}"; do
    for backup in "$backup_dir/${config}".*; do
      if [[ -f "$backup" ]]; then
        available_configs+=("$config")
        break
      fi
    done
  done

  if [[ ${#available_configs[@]} -eq 0 ]]; then
    show_page "No configuration backups" "Create a backup before restoring configuration."
    wait_for_enter || true
    return 1
  fi

  local selected_config
  selected_config=$(show_menu "Select configuration to restore" "${available_configs[@]}" "← Go Back") || return 1
  [[ "$selected_config" != "← Go Back" ]] || return 1

  local target
  target=$(supported_config_path "$selected_config") || return 1
  local available_backups=()
  for backup in "$backup_dir/${selected_config}".*; do
    [[ -f "$backup" ]] && available_backups+=("$(basename "$backup")")
  done
  [[ ${#available_backups[@]} -gt 0 ]] || return 1

  local selected_backup
  selected_backup=$(show_menu "Select backup for ${selected_config}" "${available_backups[@]}" "← Go Back") || return 1
  [[ "$selected_backup" != "← Go Back" ]] || return 1
  local backup_path="$backup_dir/$selected_backup"
  [[ "$backup_path" == "$backup_dir/${selected_config}".* && -f "$backup_path" ]] || return 1

  # Validate before creating the safety backup or touching the active file.
  if ! jq empty "$backup_path" >/dev/null 2>&1; then
    show_page "Invalid configuration backup" "The selected backup is not valid JSON."
    wait_for_enter || true
    return 1
  fi

  local content
  content=$(<"$backup_path")

  if [[ -f "$target" ]] && ! backup_config "$target"; then
    show_page "Restore failed" "Could not create a safety backup."
    wait_for_enter || true
    return 1
  fi

  if ! write_config_without_backup "$target" "$content"; then
    show_page "Restore failed" "The active configuration was not changed."
    wait_for_enter || true
    return 1
  fi

  echo "Restored ${selected_config} from ${selected_backup}"
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
