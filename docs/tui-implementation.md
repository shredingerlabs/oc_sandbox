# TUI Implementation Specification

## Architecture Overview

The TUI serves as the primary user interaction layer for OpenCode Sandbox, providing a modern, easy-to-use interface while maintaining compatibility with existing scripts. It manages project lifecycle, container operations, and configuration through structured navigation patterns.

## File Structure

```
dist/
├── scripts/
│   ├── start-tui.sh              # Main TUI entry point
│   ├── start.sh                  # (existing - enhanced with --start_opencode flag)
│   ├── build-container.sh        # (existing - used as subprocess)
│   └── init-project.sh           # (existing - used as subprocess)

~/
├── .config/oc-sandbox/
│   ├── global_config.json        # Global user preferences
│   ├── projects.json             # Project registry with metadata
│   └── backups/                  # Automatic config backups
└── .oc-sandbox/gum              # Gum binary installation

<project_root>/
├── .opencode_config/
│   ├── sandbox_config.json      # Per-project settings
│   ├── opencode.json             # OpenCode configuration
│   └── skills/                   # Project-specific skills
├── .opencode_data/
│   └── auth.json                 # AI provider credentials
└── .git_local/
    ├── gitconfig                 # Git configuration
    ├── gh-cli/hosts.yml          # GitHub CLI credentials
     ├── glab-cli/hosts.yml        # GitLab CLI credentials
     └── vcs/hosts.yml             # Custom VCS host credentials
```

## Configuration Schema

### global_config.json
```json
{
  "default_project_path": "/home/user/oc-sandbox",
  "version": "1.0"
}
```

### projects.json
```json
{
  "projects": [
    {
      "name": "my-project",
      "path": "/home/user/oc-sandbox/my-project",
      "last_used": "2025-01-15T10:30:00Z",
      "container_status": "running|stopped",
      "git_tracking": "github|gitlab|custom|none"
    }
  ],
  "version": "1.0"
}
```

### sandbox_config.json
```json
{
  "container_edition": "full|web|embedded|base",
  "container_modes": ["use_proxy", "offline", "hil_mode", "cbm_ui"],
  "start_option": "console|opencode",
  "cbm_auto_index": true,
  "cbm_auto_watch": true,
  "ai_provider": "gwdg-saia|none",
  "setup_complete": false,
  "version": "1.0"
}
```

## TUI State Machine

```
┌─────────────────┐
│   First Run     │
│   Detection     │
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│ Welcome Message │────>│ Default Path    │
└────────┬────────┘     │ Wizard          │
         │             └────────┬────────┘
         ▼                      │
┌─────────────────┐             ▼
│   Main Menu     │<────────────┘
└────────┬────────┘
         │
    ┌────┼────┬────────────┬────────────┬──────────┐
    │    │    │            │            │          │
    ▼    ▼    ▼            ▼            ▼          ▼
┌────────┐ ┌────────┐ ┌────────────┐ ┌───────┐ ┌──────────┐
│  Start │ │  Open  │ │   New      │ │ Build │ │ Settings │
│ Last   │ │Project │ │  Project   │ │Wizard │ │  Menu    │
└────────┘ └───┬────┘ └──────┬─────┘ └───┬───┘ └────┬─────┘
               │             │            │           │
               ▼             ▼            ▼           ▼
         ┌─────────┐   ┌──────────┐ ┌────────┐ ┌────────┐
         │ Project │   │ Init     │ │ Edition │ │ Config │
         │ List    │   │ Wizard   │ │ Wizard │ │ Backup │
         └────┬────┘   └────┬─────┘ └────┬───┘ └────────┘
              │             │            │
              ▼             ▼            ▼
         ┌────────┐   ┌─────────┐  ┌───────┐
         │Running │   │VCS Setup│  │Build  │
         │Handler │   │AI Setup │  │Progress│
         └────────┘   └─────────┘  └───────┘
```

## Core Functions

### TUI Initialization

```bash
# start-tui.sh
initialize_tui() {
  # Determine installation root ($HOME/.oc-sandbox)
  INSTALL_ROOT="${TUI_ROOT:-$HOME/.oc-sandbox}"

  # Create config directory structure
  mkdir -p "$HOME/.config/oc-sandbox/backups"

  # Check for gum availability
  if command -v gum &>/dev/null || [[ -x "$INSTALL_ROOT/gum" ]]; then
    TUI_MODE="gum"
  else
    TUI_MODE="bash"
    echo "Warning: gum not available, falling back to bash select"
  fi

  # Initialize or load global config
  if [[ ! -f "$HOME/.config/oc-sandbox/global_config.json" ]]; then
    handle_first_run_setup
  fi

  # Load global configuration
  load_global_config
}
```

### First Run Setup

```bash
handle_first_run_setup() {
  show_welcome_message

  # Ask for default project path
  local default_path="$HOME/oc-sandbox"
  local project_path=$(prompt_for_path "Enter default project path:" "$default_path")

  # Create global config
  create_global_config "$project_path"

  # Create projects.json if it doesn't exist
  if [[ ! -f "$HOME/.config/oc-sandbox/projects.json" ]]; then
    create_projects_json
  fi
}
```

### Main Menu

```bash
show_main_menu() {
  while true; do
    local options=("Start last used project" "Open Project" "New Project" "Build Container" "Settings" "Exit")

    # Check if last used project exists
    local last_project=$(get_last_used_project)
    if [[ -z "$last_project" ]]; then
      options=("Open Project" "New Project" "Build Container" "Settings" "Exit")
    fi

    local choice=$(show_menu "OpenCode Sandbox" "${options[@]}")

    case "$choice" in
      "Start last used project")
        handle_start_project "$last_project"
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
```

### Project Selection Wizard

```bash
project_selection_wizard() {
  local projects=($(get_all_projects_ordered))
  local running_projects=($(get_running_containers))

  local menu_items=()
  for project in "${projects[@]}"; do
    local name=$(jq -r '.name' <<< "$project")
    local path=$(jq -r '.path' <<< "$project")
    local status=$(jq -r '.container_status' <<< "$project")
    local status_symbol=$([[ "$status" == "running" ]] && echo "●" || echo "○")

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
```

### New Project Wizard

```bash
init_project_wizard() {
  # Step 1: Project name
  local project_name=$(prompt_for_name "Enter project name:")

  # Step 2: Project path (with default suggestion)
  local default_path="$HOME/oc-sandbox/$project_name"
  local project_path=$(prompt_for_path "Enter project path:" "$default_path")

  # Validate project path
  if ! validate_project_path "$project_path"; then
    echo "Invalid project path"
    return 1
  fi

  # Step 3: Run init-project.sh
  if ! run_init_project "$project_path"; then
    echo "Project initialization failed"
    return 1
  fi

  # Step 4: Container settings
  local edition=$(select_container_edition)
  local modes=(select_container_modes)
  local start_option=$(select_start_option)

  # Step 5: VCS Integration
  local vcs_tracking=$(select_vcs_tracking)
  case "$vcs_tracking" in
    "github")
      setup_github_credentials "$project_path"
      ;;
    "gitlab")
      setup_gitlab_credentials "$project_path"
      ;;
    "none")
      # No VCS setup
      ;;
  esac

  # Step 6: AI Provider
  local ai_provider=$(select_ai_provider)
  if [[ "$ai_provider" == "gwdg-saia" ]]; then
    setup_gwdg_provider "$project_path"
  fi

  # Save sandbox config
  create_sandbox_config "$project_path" "$edition" "$modes" "$start_option" "$ai_provider"

  # Step 7: Check if containers need building
  if ! check_container_images_exist; then
    local build_choice=$(show_menu "Container images not found" ["Build now" "Build later" "Go back"])
    case "$build_choice" in
      "Build now")
        build_container_wizard
        ;;
      "Build later")
        echo "Remember to build containers before starting project"
        ;;
      "Go back")
        return
        ;;
    esac
  fi

  # Start container with setup
  start_container_with_setup "$project_path"

  # Add to projects registry
  add_project_to_registry "$project_name" "$project_path"
}
```

### Container Operations

```bash
handle_project_action() {
  local project_data="$1"
  local project_path=$(jq -r '.path' <<< "$project_data")
  local container_status=$(jq -r '.container_status' <<< "$project_data")
  local config_path="${project_path}/.opencode_config/sandbox_config.json"

  # Update last used timestamp
  update_last_used "$project_path"

  if [[ "$container_status" == "running" ]]; then
    # Handle running container
    access_running_container "$project_path" "$config_path"
  else
    # Start new container
    start_container "$project_path" "$config_path"
  fi
}

start_container() {
  local project_path="$1"
  local config_path="$2"

  local edition=$(jq -r '.container_edition' "$config_path")
  local modes=($(jq -r '.container_modes[]' "$config_path"))
  local start_option=$(jq -r '.start_option' "$config_path")

  # Build start command with flags
  local start_cmd="${SCRIPT_DIR}/start.sh ${project_path}"
  for mode in "${modes[@]}"; do
    start_cmd+=" --${mode}"
  done

  if [[ "$start_option" == "opencode" ]]; then
    start_cmd+=" --start_opencode"
  fi

  # Start container and show progress
  run_command_with_status "Starting container" "$start_cmd"

  # Update project status
  update_project_status "$project_path" "running"
}

access_running_container() {
  local project_path="$1"
  local config_path="$2"
  local project_name=$(basename "$project_path")
  local start_option=$(jq -r '.start_option' "$config_path")

  # Build container name
  local container_name="opencode-sandbox-${project_name}"

  # Access based on start option
  if [[ "$start_option" == "opencode" ]]; then
    # Direct opencode access
    podman exec -it --user dev "$container_name" opencode
  else
    # Access bash first
    podman exec -it --user dev "$container_name" bash
  fi
}
```

### First-Run Setup Handler

```bash
start_container_with_setup() {
  local project_path="$1"
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  local setup_complete=$(jq -r '.setup_complete' "$config_path")

  # Start container first
  start_container "$project_path" "$config_path"

  # Check if setup already complete
  if [[ "$setup_complete" == "true" ]]; then
    return 0
  fi

  # Run first-run setup
  run_first_run_setup "$project_path"
}

run_first_run_setup() {
  local project_path="$1"
  local project_name=$(basename "$project_path")

  # Container name
  local container_name="opencode-sandbox-${project_name}"

  echo "Running first-run setup..."

  # CBM Configuration
  echo "Configuring Codebase Memory..."
  podman exec -it --user dev "$container_name" bash -c '
    codebase-memory-mcp config set auto_index true
    codebase-memory-mcp config set auto_index_limit 50000
    codebase-memory-mcp config set auto_watch true
  '

  # Skills Setup
  echo "Setting up skills..."
  podman exec -it --user dev "$container_name" bash -c 'opencode run "setup-matt-pocock-skills"'

  # Update setup complete flag
  local config_path="${project_path}/.opencode_config/sandbox_config.json"
  update_sandbox_config_field "$config_path" "setup_complete" "true"

  echo "Setup complete!"
  wait_for_enter
}
```

### Build Container Wizard

```bash
build_container_wizard() {
  # Detect available editions
  local editions=($(detect_available_editions))

  local options=("all")
  options+=("${editions[@]}")
  options+=("← Go Back")

  local choice=$(show_menu "Select container edition" "${options[@]}")

  if [[ "$choice" == "← Go Back" ]]; then
    return
  fi

  local edition="$choice"

  # Run build command with progress tracking
  local build_cmd="${SCRIPT_DIR}/build-container.sh ${edition}"

  if run_command_with_live_output "Building ${edition} containers" "$build_cmd"; then
    echo "Build successful!"
  else
    echo "Build failed. Check logs for details."
  fi

  wait_for_enter
}

detect_available_editions() {
  # Parse build-container.sh help text for editions
  "$SCRIPT_DIR/build-container.sh" 2>&1 | grep -oE '(base|web|embedded|full)' | sort -u
}
```

### Configuration Management

```bash
load_global_config() {
  GLOBAL_CONFIG=$(cat "$HOME/.config/oc-sandbox/global_config.json")
  DEFAULT_PROJECT_PATH=$(jq -r '.default_project_path' <<< "$GLOBAL_CONFIG")
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

  # Write to temp file
  echo "$content" > "$temp_file"

  # Backup existing file
  if [[ -f "$filepath" ]]; then
    backup_config "$filepath"
  fi

  # Atomic rename
  mv "$temp_file" "$filepath"
}

backup_config() {
  local filepath="$1"
  local backup_dir="$HOME/.config/oc-sandbox/backups"
  local timestamp=$(date +%Y%m%d_%H%M%S)

  mkdir -p "$backup_dir"

  local filename=$(basename "$filepath")
  cp "$filepath" "${backup_dir}/${filename}.${timestamp}"

  # Rotate backups (keep last 5)
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
```

### Utility Functions

```bash
show_menu() {
  local title="$1"
  shift
  local options=("$@")

  if [[ "$TUI_MODE" == "gum" ]]; then
    gum choose --header="$title" "${options[@]}" --height="${#options[@]}"
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

prompt_for_name() {
  local prompt="$1"

  if [[ "$TUI_MODE" == "gum" ]]; then
    gum input --prompt="$prompt " --placeholder="project-name" --validation.alphanumeric
  else
    read -p "$prompt " input
    # Validate alphanumeric only
    if [[ "$input" =~ ^[a-zA-Z0-9_-]+$ ]]; then
      echo "$input"
    else
      echo "" # Invalid
    fi
  fi
}

prompt_for_path() {
  local prompt="$1"
  local default="$2"

  if [[ "$TUI_MODE" == "gum" ]]; then
    gum input --prompt="$prompt " --value="$default" --file
  else
    read -e -p "$prompt " -i "$default" input
    echo "$input"
  fi
}

run_command_with_status() {
  local status="$1"
  local command="$2"

  if [[ "$TUI_MODE" == "gum" ]]; then
    gum spin --spinner dot --title="$status" -- bash -c "$command"
  else
    echo "$status..."
    bash -c "$command"
  fi
}

run_command_with_live_output() {
  local status="$1"
  local command="$2"

  echo "$status..."
  bash -c "$command" | tee /tmp/tui_build.log
  return ${PIPESTATUS[0]}
}

wait_for_enter() {
  echo
  read -p "Press Enter to continue..."
}

validate_project_path() {
  local path="$1"

  # Check if path exists and is not empty
  if [[ -e "$path" ]] && ! [[ -z "$(ls -A "$path" 2>/dev/null)" ]]; then
    return 1 # Path exists and not empty
  fi

  # Check parent directory exists and is writable
  local parent_dir=$(dirname "$path")
  if [[ ! -d "$parent_dir" ]] || [[ ! -w "$parent_dir" ]]; then
    return 1
  fi

  return 0
}

get_all_projects_ordered() {
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  if [[ ! -f "$projects_json" ]]; then
    return 1
  fi

  # Get projects sorted by last_used
  jq -r '.projects | sort_by(.last_used) | reverse | .[] | @json' "$projects_json"
}

get_last_used_project() {
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  if [[ ! -f "$projects_json" ]]; then
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
  # Get all running opencode-sandbox containers
  podman ps --format '{{.Names}}' --filter "name=opencode-sandbox-"
}

check_container_images_exist() {
  local config_path="$1"
  local edition=$(jq -r '.container_edition' "$config_path")

  # Check if edition image exists
  podman image exists "opencode-sandbox-${edition}"
}

update_last_used() {
  local project_path="$1"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

  jq --arg path "$project_path" --arg timestamp "$timestamp" \
    '.projects |= map(if .path == $path then .last_used = $timestamp else . end)' \
    "$projects_json" | atomic_write "$projects.json"
}

update_project_status() {
  local project_path="$1"
  local status="$2"
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  jq --arg path "$project_path" --arg status "$status" \
    '.projects |= map(if .path == $path then .container_status = $status else . end)' \
    "$projects_json" | atomic_write "$projects_json"
}

add_project_to_registry() {
  local name="$1"
  local path="$2"
  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local projects_json="$HOME/.config/oc-sandbox/projects.json"

  # Check if project already exists
  local existing=$(get_project_by_name "$name")
  if [[ -n "$existing" ]]; then
    echo "Project already exists in registry"
    return 1
  fi

  # Add project
  jq --arg name "$name" --arg path "$path" --arg timestamp "$timestamp" \
    '.projects += [{"name": $name, "path": $path, "last_used": $timestamp, "container_status": "stopped", "git_tracking": "none"}]' \
    "$projects_json" | atomic_write "$projects_json"
}
```

## Enhanced Features

### Signal Handling and Cleanup

```bash
cleanup() {
    echo "Cleaning up..."
    # Kill any background processes
    # Restore terminal state
}

trap cleanup EXIT INT TERM
```

### Terminal Resize Handling

```bash
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
```

### Concurrent Operations Warning

```bash
check_multiple_tui_instances() {
  local instances=$(pgrep -f "start-tui.sh" | wc -l)
  if [[ $instances -gt 1 ]]; then
    echo "Warning: Multiple TUI instances detected"
    echo "Concurrent operations may cause conflicts"
  fi
}
```

## Error Handling

```bash
handle_error() {
  local operation="$1"
  local error_code="$2"

  echo "Error during $operation (exit code: $error_code)"

  local options=("Retry" "Go back" "Exit")
  local choice=$(show_menu "Error handling" "${options[@]}")

  case "$choice" in
    "Retry")
      return 1 # Retry the operation
      ;;
    "Go back")
      return 2 # Go back to previous menu
      ;;
    "Exit")
      exit 1 # Exit TUI
      ;;
  esac
}
```

## Installation and Distribution

The TUI script (`start-tui.sh`) follows the same distribution pattern as existing scripts and is installed via `install.sh`. It can create symlinks when `--symlinks` flag is used and is automatically made executable.

### Install Script Integration

```bash
# In install.sh main()
echo "  5. Interaktive TUI starten:"
echo "     ${INSTALL_PATH}/scripts/start-tui.sh"
```

## Testing Strategy

The shell tests use isolated temporary `HOME` and project roots. Native scripts and
Podman are replaced with process-boundary mocks that record arguments and return
controlled failures; no network, credentials, or real container is required.

Run the complete suite with:

```bash
bash -n dist/scripts/*.sh tests/*.sh
bash tests/test-start-tui.sh
bash tests/test-tui-gum.sh
bash tests/test-install.sh
```

`test-start-tui.sh` exercises the bash fallback and asserts externally visible
workflow state: project identity, mode toggles, credentials and permissions,
deferred/failed builds, setup retry, runtime reconciliation, and atomic restore.
`test-tui-gum.sh` runs the bundled gum v0.17.0 binary in a PTY and covers input,
choose/toggle, cancellation, hidden token input, and navigation. Assertions avoid
private implementation details and verify JSON, registry state, command arguments,
permissions, and secret non-disclosure.

## Future Enhancements

1. **Enhanced project migration**: Import existing projects not in registry
2. **Advanced container management**: Resource limits, custom networks
3. **Skill marketplace integration**: Browse and install skills from community
4. **Template system**: Create projects from predefined templates
5. **Log viewer**: Integration with container logs and TUI output
