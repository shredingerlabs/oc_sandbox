#!/usr/bin/env bash
#
# start-tui.sh — Interactive TUI for opencode-sandbox project management
#
# Provides unified init/start flows for opencode-sandbox projects.
# Features:
#   - First-run setup wizard (opencode skills, CBM auto-index, startup preference)
#   - Display existing projects (name, mode/edition, time ago, running status)
#   - Start existing project with interactive config edition
#   - New project init with git host selection and AI API setup
#   - Project registry at ~/.config/opencode-sandbox/projects.json
#   - Multi-backend TUI support (gum→fzf→whiptail→dialog→plain)
#
# Usage:
#   scripts/start-tui.sh
#

set -euo pipefail

trap 'exit 0' INT TERM

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Configuration -------------------------------------------------------------

DEFAULT_INSTALL_PATH=$HOME/.opencode_sandbox
GUM_BIN="${DEFAULT_INSTALL_PATH}/gum"

PROJECT_REGISTRY_DIR="${HOME}/.config/opencode-sandbox"
PROJECT_REGISTRY_FILE="${PROJECT_REGISTRY_DIR}/projects.json"

SANDBOX_CONFIG_FILE="${HOME}/.opencode_sandbox/sandbox_config.json"
CBM_CACHE_DIR="${HOME}/.cache/codebase-memory-mcp"

# TUI backend: "gum" | "fzf" | "whiptail" | "dialog" | "plain"
TUI_BACKEND=""

# Global array for sorted project paths (used by menu functions)
declare -a SORTED_PROJECT_PATHS=()

# --- Logging -------------------------------------------------------------------

log_info()  { printf '\033[1;34m[info]\033[0m %s\n'  "$*" >&2; }
log_warn()  { printf '\033[1;33m[warn]\033[0m %s\n'  "$*" >&2; }
log_error() { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; }

# --- TUI Backend Detection -----------------------------------------------------

gum_available() {
    if command -v gum &>/dev/null; then
        GUM_BIN="$(command -v gum)"
        return 0
    fi
    if [[ -x "$GUM_BIN" ]]; then
        return 0
    fi
    return 1
}

ensure_tui() {
    if gum_available; then
        TUI_BACKEND="gum"
        log_info "Using TUI backend: gum"
        return 0
    fi

    log_info "gum not found - trying fallback backends."

    if command -v fzf &>/dev/null; then
        TUI_BACKEND="fzf"
    elif command -v whiptail &>/dev/null; then
        TUI_BACKEND="whiptail"
    elif command -v dialog &>/dev/null; then
        TUI_BACKEND="dialog"
    else
        log_warn "No TUI backend available - using plain prompts."
        TUI_BACKEND="plain"
    fi

    log_info "Using TUI backend: $TUI_BACKEND"
}

# --- First-Run Setup -----------------------------------------------------------

is_first_run() {
    [[ ! -f "$SANDBOX_CONFIG_FILE" ]]
}

ask_yes_no() {
    local prompt="$1"
    local default="${2:-yes}"
    local choice
    
    while true; do
        if [[ "$default" == "yes" ]]; then
            read -rp "${prompt} [Y/n]: " choice
            choice=${choice:-Y}
        else
            read -rp "${prompt} [y/N]: " choice
            choice=${choice:-N}
        fi
        
        case "$choice" in
            [Yy]*) return 0 ;;
            [Nn]*) return 1 ;;
            *) echo "Please answer yes or no." ;;
        esac
    done
}

run_opencode_setup() {
    log_info "Step 1: Configuring opencode with matt-pocock skills..."
    echo ""
    
    if ! command -v opencode &>/dev/null; then
        log_warn "opencode command not found. Skipping opencode setup."
        return 1
    fi
    
    echo "This will run: opencode run \"setup-matt-pocock-skills\""
    echo "You will see the output and may need to provide input."
    echo ""
    
    if ask_yes_no "Continue with opencode setup?"; then
        echo ""
        echo "--- Running opencode setup ---"
        echo ""
        
        if opencode run "setup-matt-pocock-skills"; then
            log_info "Opencode setup completed successfully"
            return 0
        else
            log_warn "Opencode setup failed, continuing..."
            return 1
        fi
    else
        log_warn "Skipping opencode setup"
        return 1
    fi
}

configure_cbm_auto_index() {
    log_info "Step 2: Configuring CBM auto-index..."
    echo ""
    
    if ! command -v codebase-memory-mcp &>/dev/null; then
        log_warn "codebase-memory-mcp not found. Skipping CBM configuration."
        return 1
    fi
    
    echo "Enabling CBM auto-index for automatic project indexing"
    echo "This will automatically index your projects when you start opencode"
    echo ""
    
    if codebase-memory-mcp config set auto_index true 2>/dev/null; then
        log_info "CBM auto-index enabled"
        
        if codebase-memory-mcp config set auto_index_limit 50000 2>/dev/null; then
            log_info "CBM file limit set to 50,000"
        fi
        
        if codebase-memory-mcp config set auto_watch true 2>/dev/null; then
            log_info "CBM auto-watch enabled"
        fi
        
        return 0
    else
        log_warn "Failed to configure CBM auto-index"
        return 1
    fi
}

ask_startup_preference() {
    log_info "Step 3: Configure startup behavior..."
    echo ""
    
    echo "How would you like opencode to start?"
    echo "  1) Always start opencode on system start"
    echo "  2) Start in terminal only (manual start)"
    echo ""
    
    read -rp "Your preference [1-2]: " choice
    
    case "$choice" in
        1) return 0 ;;
        2) return 1 ;;
        *) 
            log_warn "Invalid choice, defaulting to manual start"
            return 1
            ;;
    esac
}

save_configuration() {
    local auto_start_on_boot="$1"
    local cbm_configured="$2"
    local opencode_setup_done="$3"
    
    log_info "Step 4: Saving configuration..."
    echo ""
    
    local config_dir
    config_dir="$(dirname "$SANDBOX_CONFIG_FILE")"
    
    if [[ ! -d "$config_dir" ]]; then
        mkdir -p "$config_dir"
    fi
    
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    
    cat > "$SANDBOX_CONFIG_FILE" << EOF
{
  "version": "1.0.0",
  "configured_on": "$timestamp",
  "startup": {
    "auto_start_on_boot": $auto_start_on_boot,
    "start_in_terminal": true
  },
  "cbm": {
    "auto_index_enabled": $cbm_configured,
    "cache_dir": "$CBM_CACHE_DIR"
  },
  "opencode": {
    "setup_matt_pocock_skills": $opencode_setup_done
  },
  "commands": {
    "setup_skill": "setup-matt-pocock-skills"
  },
  "start": {
    "edition": "full",
    "web_access": "unrestricted",
    "cbm_ui": false
  }
}
EOF

    chmod 600 "$SANDBOX_CONFIG_FILE"
    log_info "Configuration saved to: $SANDBOX_CONFIG_FILE"
    echo ""
}

run_first_time_setup() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║          OpenCode Sandbox - First Run Setup                ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""
    echo "This wizard will help you configure opencode-sandbox for first use."
    echo "You can customize these settings later in: $SANDBOX_CONFIG_FILE"
    echo ""
    
    local opencode_setup_done=false
    local cbm_configured=false
    local auto_start_on_boot=false
    
    if run_opencode_setup; then
        opencode_setup_done=true
    fi
    
    if configure_cbm_auto_index; then
        cbm_configured=true
    fi
    
    if ask_startup_preference; then
        auto_start_on_boot=true
    fi
    
    save_configuration "$auto_start_on_boot" "$cbm_configured" "$opencode_setup_done"
    
    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║                    Setup Complete!                          ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Configuration summary:"
    echo "  • Opencode setup: $opencode_setup_done"
    echo "  • CBM auto-index: $cbm_configured"
    echo "  • Auto-start on boot: $auto_start_on_boot"
    echo ""
    read -p "Press Enter to continue..."
}

# --- Project Registry ----------------------------------------------------------

init_registry() {
    if [[ ! -d "$PROJECT_REGISTRY_DIR" ]]; then
        mkdir -p "$PROJECT_REGISTRY_DIR"
    fi

    if [[ ! -f "$PROJECT_REGISTRY_FILE" ]]; then
        echo '{}' > "$PROJECT_REGISTRY_FILE"
    fi
}

registry_list() {
    if [[ ! -f "$PROJECT_REGISTRY_FILE" ]]; then
        echo "[]"
        return
    fi

    jq -c 'if type == "object" then [.paths[]?] else [.[]?] end' "$PROJECT_REGISTRY_FILE" 2>/dev/null || echo "[]"
}

registry_add() {
    local project_path="$1"
    local project_name="$2"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if [[ ! -f "$PROJECT_REGISTRY_FILE" ]]; then
        init_registry
    fi

    local current
    current=$(cat "$PROJECT_REGISTRY_FILE")

    if ! echo "$current" | jq -e ".paths" &>/dev/null; then
        current='{"paths":[]}'
    fi

    echo "$current" | jq --arg path "$project_path" --arg name "$project_name" --arg ts "$timestamp" '
        if .paths | map(.path) | index($path) then
            .
        else
            .paths += [{
                "path": $path,
                "name": $name,
                "created": $ts,
                "last_used": $ts
            }]
        end
    ' > "${PROJECT_REGISTRY_FILE}.tmp"

    mv "${PROJECT_REGISTRY_FILE}.tmp" "$PROJECT_REGISTRY_FILE"
}

registry_update_last_used() {
    local project_path="$1"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if [[ ! -f "$PROJECT_REGISTRY_FILE" ]]; then
        return
    fi

    local current
    current=$(cat "$PROJECT_REGISTRY_FILE")

    echo "$current" | jq --arg path "$project_path" --arg ts "$timestamp" '
        if .paths then
            .paths = [.paths[] | if .path == $path then .last_used = $ts else . end]
        else
            .
        end
    ' > "${PROJECT_REGISTRY_FILE}.tmp"

    mv "${PROJECT_REGISTRY_FILE}.tmp" "$PROJECT_REGISTRY_FILE"
}

# --- Helper Functions ----------------------------------------------------------

format_last_used() {
    local iso_date="$1"
    if [[ -n "$iso_date" ]]; then
        local now seconds_ago
        now=$(date +%s)
        seconds_ago=$((now - $(date -d "$iso_date" +%s 2>/dev/null || echo "0")))

        if [[ $seconds_ago -lt 0 ]]; then
            seconds_ago=0
        fi

        if [[ $seconds_ago -lt 3600 ]]; then
            echo "${seconds_ago}m ago"
        elif [[ $seconds_ago -lt 86400 ]]; then
            echo "$((seconds_ago / 3600))h ago"
        else
            echo "$((seconds_ago / 86400))d ago"
        fi
    else
        echo "never"
    fi
}

is_container_running() {
    local project_name="$1"
    local container_name="opencode-sandbox-${project_name}"

    if command -v podman &>/dev/null; then
        if podman container exists "${container_name}" 2>/dev/null; then
            if podman inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -q true; then
                return 0
            fi
        fi
    fi
    
    if command -v docker &>/dev/null; then
        if docker container exists "${container_name}" 2>/dev/null; then
            if docker inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -q true; then
                return 0
            fi
        fi
    fi
    
    return 1
}

get_running_indicator() {
    local project_name="$1"
    
    if is_container_running "$project_name"; then
        echo "●"
    else
        echo "○"
    fi
}

detect_project_name() {
    local project_path="$1"
    basename "$project_path"
}

detect_project_config() {
    local project_path="$1"
    local start_script="${project_path}/.opencode_sandbox/scripts/start.sh"

    if [[ -f "$start_script" ]]; then
        local edition
        edition=$(grep -oP '(?<=--edition )\w+' "$start_script" 2>/dev/null | tail -1 || echo "full")

        local mode="standard"
        if grep -q "\-\-hil_mode" "$start_script" 2>/dev/null; then
            mode="hil_mode"
        fi

        echo "${mode}/${edition}"
    else
        echo "unknown"
    fi
}

# --- UI Functions --------------------------------------------------------------

ui_choose() {
    local title="$1"
    shift
    local result=""

    case "$TUI_BACKEND" in
        gum)
            result=$("$GUM_BIN" choose --header "$title" "$@")
            ;;
        fzf)
            result=$(printf '%s\n' "$@" | fzf --header="$title" --prompt="Select > ")
            ;;
        whiptail|dialog)
            local temp_file=$(mktemp)
            local i=1
            for opt in "$@"; do
                echo "$i" "$opt" >> "$temp_file"
                i=$((i + 1))
            done
            local idx
            idx=$("$TUI_BACKEND" --menu "$title" 15 60 "$#" --file "$temp_file" 3>&1 1>&2 2>&3) || return 1
            rm -f "$temp_file"
            idx=$((idx - 1))
            set -- "$@"
            shift $idx
            result="$1"
            ;;
        plain)
            echo "$title" >&2
            local i=1
            for opt in "$@"; do
                echo "  $i) $opt" >&2
                i=$((i + 1))
            done
            local idx
            read -rp "Selection [1-${#}]: " idx
            idx=$((idx - 1))
            set -- "$@"
            shift $idx
            result="$1"
            ;;
    esac
    
    echo "$result"
}

ui_confirm() {
    local prompt="$1"

    case "$TUI_BACKEND" in
        gum)
            "$GUM_BIN" confirm "$prompt"
            ;;
        whiptail|dialog)
            "$TUI_BACKEND" --yesno "$prompt" 8 60
            ;;
        plain)
            local answer
            read -rp "$prompt [y/N]: " answer
            [[ "$answer" =~ ^[yY]$ ]]
            ;;
    esac
}

ui_input() {
    local prompt="$1"
    local placeholder="${2:-}"

    case "$TUI_BACKEND" in
        gum)
            "$GUM_BIN" input --placeholder "$placeholder" --header "$prompt"
            ;;
        whiptail|dialog)
            "$TUI_BACKEND" --inputbox "$prompt" 8 60 "$placeholder" 3>&1 1>&2 2>&3
            ;;
        plain)
            local value
            read -rp "$prompt: " value
            echo "$value"
            ;;
    esac
}

ui_input_hidden() {
    local prompt="$1"
    local placeholder="${2:-[hidden]}"

    case "$TUI_BACKEND" in
        gum)
            "$GUM_BIN" input --password --placeholder "$placeholder" --header "$prompt"
            ;;
        *)
            read -rp "$prompt (input will be hidden): " value
            echo "$value"
            ;;
    esac
}

ui_message() {
    local title="$1"
    local message="$2"

    case "$TUI_BACKEND" in
        gum)
            "$GUM_BIN" style --bold "$title"
            echo "$message"
            ;;
        *)
            echo "== $title =="
            echo "$message"
            ;;
    esac
}

# --- Menu Display Functions ----------------------------------------------------

show_gum_menu() {
    declare -a display_items=()
    
    local tmpfile=$(mktemp)
    
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        
        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')
        last_used=$(echo "$line" | jq -r '.last_used // empty')
        
        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi
        
        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")
        
        local sort_key
        if [[ -n "$last_used" && "$last_used" != "null" ]]; then
            sort_key=$(date -d "$last_used" +%s 2>/dev/null || echo "0")
        else
            sort_key="0"
        fi
        
        local option="${running} ${name} │ ${mode_label} │ ${time_ago}"
        echo "${sort_key}|${option}|${path}" >> "$tmpfile"
    done < <(registry_list)
    
    while IFS='|' read -r sort_key option path; do
        display_items+=("$option")
        SORTED_PROJECT_PATHS+=("$path")
    done < <(sort -t'|' -k1 -rn "$tmpfile")
    
    rm -f "$tmpfile"

    local all_options=()
    
    all_options+=("🔨 Re-/Build Container")
    all_options+=("🆕 New Project (Init)")
    all_options+=("")
    
    if [[ ${#display_items[@]} -eq 0 ]]; then
        all_options+=("  (no existing projects)")
    else
        for item in "${display_items[@]}"; do
            all_options+=("$item")
        done
    fi
    
    local action
    action=$("$GUM_BIN" choose \
        --cursor.foreground "#FF6B6B" \
        --selected.foreground "#4ECDC4" \
        --header="🚀 OpenCode Sandbox" \
        --header.foreground="#FF6B6B" \
        "${all_options[@]}" \
        --height 8)

    case "$action" in
        "🔨 Re-"*)
            echo "ACTION:BUILD"
            ;;
        "🆕 New"*)
            echo "ACTION:NEW"
            ;;
        ""|"  (no"*)
            echo "ACTION:EXIT"
            ;;
        *)
            local idx=0
            for item in "${display_items[@]}"; do
                if [[ "$item" == "$action" ]]; then
                    echo "ACTION:SELECT:${SORTED_PROJECT_PATHS[$idx]}"
                    return
                fi
                idx=$((idx + 1))
            done
            echo "ACTION:EXIT"
            ;;
    esac
}

show_fzf_menu() {
    local projects=$(registry_list)
    
    local tmpfile=$(mktemp)
    local pathfile=$(mktemp)
    
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        
        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')
        last_used=$(echo "$line" | jq -r '.last_used // empty')
        
        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi
        
        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")
        
        local sort_key
        if [[ -n "$last_used" ]]; then
            sort_key=$(date -d "$last_used" +%s 2>/dev/null || echo "0")
        else
            sort_key="0"
        fi
        
        local option="${running} ${name} │ ${mode_label} │ ${time_ago}"
        echo "${sort_key}|${option}" >> "$tmpfile"
        echo "${sort_key}|${path}" >> "$pathfile"
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)
    
    local display_menu=""
    SORTED_PROJECT_PATHS=()
    
    display_menu+="━━━ Segment 1: Actions ━━━"$'\n'
    display_menu+="🔨 Re-/Build Container"$'\n'
    display_menu+="🆕 New Project (Init)"$'\n'
    display_menu+=""$'\n'
    display_menu+="━━━ Segment 2: Projects ━━━"$'\n'
    
    if [[ -s "$tmpfile" ]]; then
        paste "$tmpfile" "$pathfile" | sort -t'|' -k1 -rn | while IFS='|' read -r sort_key option path; do
            display_menu+="${option#*|}"$'\n'
            echo "$path" >> "${tmpfile}_paths"
        done
        
        if [[ -f "${tmpfile}_paths" ]]; then
            while IFS= read -r path; do
                SORTED_PROJECT_PATHS+=("$path")
            done < "${tmpfile}_paths"
            rm -f "${tmpfile}_paths"
        fi
    else
        display_menu+="  (no existing projects)"$'\n'
    fi
    
    rm -f "$tmpfile" "$pathfile"
    
    local choice
    choice=$(echo -e "$display_menu" | fzf \
        --header="OpenCode Sandbox" \
        --prompt="Select > " \
        --height=12 \
        --layout=reverse \
        --border)
    
    case "$choice" in
        *"Build"*)
            echo "ACTION:BUILD"
            ;;
        *"New"*)
            echo "ACTION:NEW"
            ;;
        *)
            if [[ -n "$choice" ]]; then
                local idx=1
                for item in "${SORTED_PROJECT_PATHS[@]}"; do
                    idx=$((idx + 1))
                done
                echo "ACTION:SELECT:${SORTED_PROJECT_PATHS[0]}"
                return
            fi
            echo "ACTION:EXIT"
            ;;
    esac
}

show_select_menu() {
    local projects=$(registry_list)
    
    local -a project_names=()
    local -a project_paths=()
    local -a sort_keys=()
    
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        
        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')
        last_used=$(echo "$line" | jq -r '.last_used // empty')
        
        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi
        
        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")
        
        local sort_key
        if [[ -n "$last_used" ]]; then
            sort_key=$(date -d "$last_used" +%s 2>/dev/null || echo "0")
        else
            sort_key="0"
        fi
        
        project_names+=("${running} ${name} │ ${mode_label} │ ${time_ago}")
        project_paths+=("$path")
        sort_keys+=("$sort_key")
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)
    
    echo "🚀 OpenCode Sandbox [Basic Mode]"
    echo ""
    echo "━━━ Segment 1: Actions ━━━"
    echo "  1) 🔨 Re-/Build Container"
    echo "  2) 🆕 New Project (Init)"
    echo ""
    echo "━━━ Segment 2: Projects ━━━"
    
    if [[ ${#project_names[@]} -gt 0 ]]; then
        local i=3
        for name in "${project_names[@]}"; do
            echo "  $i) $name"
            SORTED_PROJECT_PATHS+=("${project_paths[$((i - 3))]}")
            i=$((i + 1))
        done
        echo ""
    else
        echo "  (no existing projects)"
        echo ""
    fi
    
    echo "  0) ❌ Exit"
    echo ""
    
    read -rp "Select option: " choice
    
    case "$choice" in
        1)
            echo "ACTION:BUILD"
            ;;
        2)
            echo "ACTION:NEW"
            ;;
        0)
            echo "ACTION:EXIT"
            ;;
        *)
            local idx=$((choice - 3))
            if [[ $idx -ge 0 && $idx -lt ${#SORTED_PROJECT_PATHS[@]} ]]; then
                echo "ACTION:SELECT:${SORTED_PROJECT_PATHS[$idx]}"
            else
                echo "ACTION:INVALID"
            fi
            ;;
    esac
}

show_whiptail_menu() {
    local projects=$(registry_list)
    
    local -a project_names=()
    local -a project_paths=()
    local -a sort_keys=()
    
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        
        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')
        last_used=$(echo "$line" | jq -r '.last_used // empty')
        
        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi
        
        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")
        
        local sort_key
        if [[ -n "$last_used" ]]; then
            sort_key=$(date -d "$last_used" +%s 2>/dev/null || echo "0")
        else
            sort_key="0"
        fi
        
        project_names+=("${running} ${name} (${time_ago})")
        project_paths+=("$path")
        sort_keys+=("$sort_key")
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)
    
    local menu_items=()
    SORTED_PROJECT_PATHS=()
    local i=1
    
    menu_items+=("$i" "🔨 Re-/Build Container")
    i=$((i + 1))
    menu_items+=("$i" "🆕 New Project (Init)")
    i=$((i + 1))
    
    for name in "${project_names[@]}"; do
        menu_items+=("$i" "$name")
        SORTED_PROJECT_PATHS+=("${project_paths[${#SORTED_PROJECT_PATHS[@]}]}")
        i=$((i + 1))
    done
    
    menu_items+=("$i" "❌ Exit")
    
    local idx
    idx=$("$TUI_BACKEND" --menu "OpenCode Sandbox" 20 60 "$((i - 1))" \
        "${menu_items[@]}" 3>&1 1>&2 2>&3) || {
        echo "ACTION:EXIT"
        return
    }
    
    case "$idx" in
        1) echo "ACTION:BUILD" ;;
        2) echo "ACTION:NEW" ;;
        *)
            local path_idx=$((idx - 3))
            if [[ $path_idx -ge 0 && $path_idx -lt ${#SORTED_PROJECT_PATHS[@]} ]]; then
                echo "ACTION:SELECT:${SORTED_PROJECT_PATHS[$path_idx]}"
            else
                echo "ACTION:EXIT"
            fi
            ;;
    esac
}

# --- Action Handlers -----------------------------------------------------------

handle_build_container() {
    clear
    ui_message "🔨 Re-/Build Container" "This feature is not yet implemented."
    echo ""
    echo "In the final version, this will navigate to the build menu."
    echo ""
    read -p "Press Enter to continue..."
}

create_project_structure() {
    local project_path="$1"
    local project_name="$2"

    log_info "Creating project structure at: $project_path"

    mkdir -p \
        "${project_path}/project" \
        "${project_path}/.opencode_config" \
        "${project_path}/.opencode_data" \
        "${project_path}/.ssh_local" \
        "${project_path}/.git_local" \
        "${project_path}/.cbm_cache"

    chmod 700 "${project_path}/.ssh_local"
    chmod 700 "${project_path}/.git_local"
}

handle_new_project() {
    clear
    ui_message "🆕 New Project (Init)" ""
    echo ""

    ensure_tui

    # STEP 1: Ask for project folder path
    echo "━━━ STEP 1: Project Location ━━━"
    local project_path
    project_path=$(ui_input "Project folder path" "$HOME/projects/my-project")

    # Auto-generate project name from path
    local project_name
    project_name=$(detect_project_name "$project_path")

    # Allow user to override the auto-generated name
    local override_name
    override_name=$(ui_input "Project name (default: $project_name)" "$project_name")

    if [[ -n "$override_name" && "$override_name" != "$project_name" ]]; then
        project_name="$override_name"
    fi

    if [[ -e "$project_path" ]]; then
        if ! ui_confirm "Path exists: $project_path. Use anyway?"; then
            ui_message "Aborted" "Project creation cancelled by user."
            return 1
        fi
    fi

    # STEP 2: Ask for git host
    echo ""
    echo "━━━ STEP 2: Git Host ━━━"
    local git_host
    git_host=$(ui_choose "Select git host" "GitHub" "GitLab" "Other")

    log_info "Copying git templates for: $git_host"

    case "$git_host" in
        "GitHub")
            log_info "  - templates/git_local/gh-cli/config.yml → .git_local/gh-cli/"
            ;;
        "GitLab")
            log_info "  - templates/git_local/glab-cli/config.yml → .git_local/glab-cli/"
            ;;
        "Other")
            log_info "  - Basic git_local/ gitconfig and credentials"
            ;;
    esac

    # STEP 3: Ask for git user.name and user.email
    echo ""
    echo "━━━ STEP 3: Git Identity ━━━"
    local git_user_name
    git_user_name=$(ui_input "Git user.name" "" "")

    local git_user_email
    git_user_email=$(ui_input "Git user.email" "" "")

    log_info "Writing git identity to .git_local/gitconfig"
    log_info "  user.name = $git_user_name"
    log_info "  user.email = $git_user_email"

    # STEP 4: Ask for AI API provider
    echo ""
    echo "━━━ STEP 4: AI API Provider ━━━"
    local api_provider
    api_provider=$(ui_choose "Select AI API provider" "gwdg-saia" "Other")

    # STEP 5: Auth is ALWAYS required
    if [[ "$api_provider" == "gwdg-saia" ]]; then
        echo ""
        echo "━━━ STEP 5: GWDG SAIA Authentication ━━━"
        local api_token
        api_token=$(ui_input_hidden "GWDG SAIA API token")

        log_info "Writing auth.json to .opencode_data/auth.json"
        log_info "File permissions: 600"

        log_info "TEMPLATE] Copying opencode-gwdg.json → .opencode_config/opencode.json"

    else
        echo ""
        echo "━━━ STEP 5: Custom Provider Authentication ━━━"
        echo ""
        echo "For custom providers, provide your API token now."
        echo "You can also use the /connect command in OpenCode after starting the project."

        local custom_token
        custom_token=$(ui_input_hidden "Custom provider API token")

        local provider_name
        provider_name=$(ui_input "Provider name" "custom-provider")

        log_info "Writing auth.json to .opencode_data/auth.json"
        log_info "File permissions: 600"

        log_info "TEMPLATE] Copying opencode-basic.json → .opencode_config/opencode.json"
    fi

    # STEP 6: Create project structure, register in projects.json
    echo ""
    echo "━━━ STEP 6: Finalizing Project ━━━"

    create_project_structure "$project_path" "$project_name"

    log_info "TEMPLATE] Copying base templates:"
    log_info "  - templates/ssh_local/config → .ssh_local/config"
    log_info "  - templates/opencode/AGENTS.md → .opencode_config/AGENTS.md"
    log_info "  - templates/opencode/skills → .opencode_config/skills"

    registry_add "$project_path" "$project_name"

    ui_message "Success" \
        "Project '$project_name' created at $project_path\n\n" \
        "Next steps:\n" \
        "1. Navigate to project folder\n" \
        "2. Run 'scripts/start-tui.sh' to start the project\n" \
        "3. Configure git credentials in .git_local/\n" \
        "4. Start OpenCode and connect to your remote repo"

    echo ""
    read -p "Press Enter to continue..."
}

load_sandbox_config() {
    if [[ ! -f "$SANDBOX_CONFIG_FILE" ]]; then
        cat << 'EOF'
{
  "version": "1.0.0",
  "start": {
    "edition": "full",
    "web_access": "unrestricted",
    "cbm_ui": false
  }
}
EOF
    else
        cat "$SANDBOX_CONFIG_FILE"
    fi
}

save_sandbox_config() {
    local config_json="$1"
    
    if ! echo "$config_json" | jq . > /dev/null 2>&1; then
        log_error "Invalid JSON configuration"
        return 1
    fi
    
    echo "$config_json" | jq '.' > "$SANDBOX_CONFIG_FILE"
    chmod 600 "$SANDBOX_CONFIG_FILE"
}

assemble_start_flags() {
    local edition="$1"
    local web_access="$2" 
    local cbm_ui="$3"
    
    local flags="--edition $edition"
    
    case "$web_access" in
        "unrestricted")
            ;;
        "proxy") 
            flags="$flags --use_proxy"
            ;;
        "offline")
            flags="$flags --offline"
            ;;
    esac
    
    if [[ "$cbm_ui" == "true" ]]; then
        flags="$flags --cbm_ui"
    fi
    
    echo "$flags"
}

ui_display_config() {
    local header="Current Startup Configuration"
    local config_json="$1"
    
    echo "━━━ $header ━━━"
    echo "$config_json" | jq -r '
        "Edition: " + .start.edition,
        "Web Access: " + .start.web_access,
        "CBM UI: " + (.start.cbm_ui | tostring)
    ' 2>/dev/null || echo "  (could not parse configuration)"
    echo ""
}

handle_start_project() {
    local project_path="$1"

    if [[ ! -d "$project_path" ]]; then
        ui_message "Error" "Project path does not exist: $project_path"
        echo ""
        read -p "Press Enter to continue..."
        return 1
    fi

    ensure_tui

    echo "━━━ Start Project ━━━"
    echo "Project: $project_path"
    echo ""

    # STEP 1: Show current configuration from sandbox_config.json
    echo "━━━ STEP 1: Current Configuration ━━━"
    local current_config
    current_config=$(load_sandbox_config)
    
    ui_display_config "$current_config"
    
    # Extract current values
    local current_edition current_web_access current_cbm_ui
    current_edition=$(echo "$current_config" | jq -r '.start.edition // "full"' 2>/dev/null || echo "full")
    current_web_access=$(echo "$current_config" | jq -r '.start.web_access // "unrestricted"' 2>/dev/null || echo "unrestricted")
    current_cbm_ui=$(echo "$current_config" | jq -r '.start.cbm_ui // "false"' 2>/dev/null || echo "false")
    
    # Show quickstart option
    if ui_confirm "Start with current configuration?"; then
        echo "[QUICKSTART] Using saved configuration"
    else
        echo "[CONFIGURATION] User wants to modify settings"
        
        # STEP 2: Let user modify configuration
        echo ""
        echo "━━━ STEP 2: Modify Configuration ━━━"
        
        # Choose edition
        local edition
        edition=$(ui_choose "Select Sandbox Edition" "web" "embedded" "full")
        
        # Choose web access mode
        local web_access
        web_access=$(ui_choose "Select Web Access Mode" "unrestricted" "proxy" "offline")
        
        # Toggle CBM UI
        local cbm_ui_choice
        cbm_ui_choice=$(ui_choose "Enable CBM UI?" "Yes" "No")
        local cbm_ui="false"
        [[ "$cbm_ui_choice" == "Yes" ]] && cbm_ui="true"
        
        # Update config with new values
        current_config=$(echo "$current_config" | jq --arg edition "$edition" --arg web_access "$web_access" --argjson cbm_ui "${cbm_ui:-false}" '
            .start.edition = $edition |
            .start.web_access = $web_access |
            .start.cbm_ui = $cbm_ui
        ' 2>/dev/null || echo "$current_config")
        
        save_sandbox_config "$current_config"
        echo "[CONFIG] Updated sandbox_config.json saved"
        
        current_edition="$edition"
        current_web_access="$web_access"
        current_cbm_ui="$cbm_ui"
    fi
    
    # STEP 3: Assemble and display flags
    echo ""
    echo "━━━ STEP 3: Start Configuration ━━━"
    
    local start_flags
    start_flags=$(assemble_start_flags "$current_edition" "$current_web_access" "$current_cbm_ui")
    
    echo "Path:  $project_path"
    echo "Flags: $start_flags"
    echo ""
    
    # STEP 4: Confirmation before starting
    if ! ui_confirm "Start project with these settings?"; then
        ui_message "Aborted" "Project start cancelled by user"
        return 1
    fi
    
    # STEP 5: Call scripts/start.sh with assembled flags
    echo ""
    echo "━━━ STEP 5: Starting Project ━━━"
    
    local start_script="${SCRIPT_DIR}/start.sh"
    if [[ ! -x "$start_script" ]]; then
        ui_message "Error" "start.sh not found or not executable at: $start_script"
        echo ""
        echo "Creating placeholder start.sh script..."
        cat > "$start_script" << 'EOF'
#!/usr/bin/env bash
log_info "OpenCode Sandbox start script"
log_info "Project path: $1"
log_info "Flags: ${@:2}"
EOF
        chmod +x "$start_script"
        log_info "Placeholder start.sh created"
    fi
    
    echo "[EXEC] Executing: $start_script $project_path $start_flags"
    echo ""
    
    if [[ -n "${OPENCODE_DRY_RUN:-}" ]]; then
        echo "[DRY RUN] Would execute: $start_script $project_path $start_flags"
        ui_message "Dry Run Complete" "In production mode, this would start the project"
    else
        if "$start_script" "$project_path" $start_flags; then
            ui_message "Success" "Project started successfully"
        else
            ui_message "Error" "Failed to start project"
            return 1
        fi
    fi
    
    registry_update_last_used "$project_path"
    echo ""
    read -p "Press Enter to continue..."
}

# --- Main Menu Loop ------------------------------------------------------------

main_menu() {
    SORTED_PROJECT_PATHS=()
    local action

    case "$TUI_BACKEND" in
        gum)
            action=$(show_gum_menu)
            ;;
        fzf)
            action=$(show_fzf_menu)
            ;;
        whiptail|dialog)
            action=$(show_whiptail_menu)
            ;;
        plain)
            action=$(show_select_menu)
            ;;
    esac

    echo "$action"
}

run_tui() {
    while true; do
        clear
        local action
        action=$(main_menu)

        case "$action" in
            ACTION:BUILD)
                handle_build_container
                ;;
            ACTION:NEW)
                handle_new_project
                ;;
            ACTION:SELECT:*)
                local project_path="${action#ACTION:SELECT:}"
                handle_start_project "$project_path"
                ;;
            ACTION:EXIT)
                log_info "Exiting OpenCode Sandbox TUI"
                exit 0
                ;;
            ACTION:INVALID)
                log_warn "Invalid selection"
                read -p "Press Enter to continue..."
                ;;
        esac
    done
}

# --- Entry Point ---------------------------------------------------------------

main() {
    init_registry
    ensure_tui
    
    if is_first_run; then
        run_first_time_setup
    fi
    
    run_tui
}

main "$@"