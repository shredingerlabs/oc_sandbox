#!/usr/bin/env bash
#
# start-tui.sh — Interactive TUI for opencode-sandbox project management
#
# Merges tui_demo.sh portable TUI framework with project menu logic.
# Uses gum if available (installed by install.sh), falls back to fzf/whiptail/dialog/plain.
#
# Features:
#   - Display existing projects (name, mode/edition, last_used, running status)
#   - Start existing project (calls scripts/start.sh)
#   - New project init (calls scripts/init-project.sh)
#   - Project registry at ~/.config/opencode-sandbox/projects.json
#
# Usage:
#   scripts/start-tui.sh
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Configuration -------------------------------------------------------------

DEFAULT_INSTALL_PATH=$HOME/.opencode_sandbox
GUM_BIN="${DEFAULT_INSTALL_PATH}/gum"

PROJECT_REGISTRY_DIR="${HOME}/.config/opencode-sandbox"
PROJECT_REGISTRY_FILE="${PROJECT_REGISTRY_DIR}/projects.json"

# TUI backend: "gum" | "fzf" | "whiptail" | "dialog" | "plain"
TUI_BACKEND=""

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
        log_info "Verwende TUI-Backend: gum"
        return 0
    fi

    log_info "gum nicht gefunden – verwende Fallback-Backend."

    if command -v fzf &>/dev/null; then
        TUI_BACKEND="fzf"
    elif command -v whiptail &>/dev/null; then
        TUI_BACKEND="whiptail"
    elif command -v dialog &>/dev/null; then
        TUI_BACKEND="dialog"
    else
        log_warn "Weder whiptail noch dialog verfügbar – nutze einfache read-Prompts."
        TUI_BACKEND="plain"
    fi

    log_info "Verwende TUI-Backend: $TUI_BACKEND"
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

registry_remove() {
    local project_path="$1"

    if [[ ! -f "$PROJECT_REGISTRY_FILE" ]]; then
        return
    fi

    local current
    current=$(cat "$PROJECT_REGISTRY_FILE")

    echo "$current" | jq --arg path "$project_path" '
        if .paths then
            .paths = [.paths[] | select(.path != $path)]
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

    if ! command -v podman &>/dev/null; then
        return 1
    fi

    if podman container exists "${container_name}" 2>/dev/null; then
        if podman inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -q true; then
            return 0
        fi
    fi
    return 1
}

get_running_indicator() {
    local project_name="$1"
    if is_container_running "$project_name"; then
        echo "●"
    else
        echo " "
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
        local edition mode
        edition=$(grep -oP '(?<=--edition )\w+' "$start_script" 2>/dev/null | tail -1 || echo "full")
        if grep -q "\-\-hil_mode" "$start_script" 2>/dev/null; then
            mode="hil_mode"
        else
            mode="standard"
        fi
        echo "${mode}/${edition}"
    else
        echo "unknown"
    fi
}

# --- UI Functions --------------------------------------------------------------

ui_choose() {
    local title="$1"; shift
    local options=("$@")

    case "$TUI_BACKEND" in
        gum)
            "$GUM_BIN" choose --header "$title" "${options[@]}"
            ;;
        fzf)
            printf '%s\n' "${options[@]}" | fzf --header="$title" --prompt="Select > "
            ;;
        whiptail|dialog)
            local menu_items=() i=1
            for opt in "${options[@]}"; do
                menu_items+=("$i" "$opt")
                ((i++))
            done
            local idx
            idx=$("$TUI_BACKEND" --menu "$title" 15 60 "${#options[@]}" \
                "${menu_items[@]}" 3>&1 1>&2 2>&3) || return 1
            echo "${options[$((idx-1))]}"
            ;;
        plain)
            echo "$title" >&2
            local i=1
            for opt in "${options[@]}"; do
                echo "  $i) $opt" >&2
                ((i++))
            done
            local idx
            read -rp "Auswahl [1-${#options[@]}]: " idx
            echo "${options[$((idx-1))]}"
            ;;
    esac
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
            read -rp "$prompt [j/N]: " answer
            [[ "$answer" =~ ^[jJyY]$ ]]
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
    local header
    header=$("$GUM_BIN" style \
        --foreground "#FF6B6B" \
        --border double \
        --border-foreground "#4ECDC4" \
        --padding "1 2" \
        --align center \
        "🚀 OpenCode Sandbox" "" \
        "Choose your workspace")

    echo "$header"
    echo ""

    local action
    action=$("$GUM_BIN" choose \
        --cursor.foreground "#FF6B6B" \
        --selected.foreground "#4ECDC4" \
        "🆕 New Project (Init)" \
        "📂 Start Existing Project" \
        "❌ Exit" \
        --height 3)

    case "$action" in
        "🆕 New"*)
            echo "ACTION:NEW"
            ;;
        "📂 Start"*)
            echo "ACTION:START"
            ;;
        "❌ Exit"*)
            echo "ACTION:EXIT"
            ;;
    esac
}

show_gum_project_list() {
    local projects
    projects=$(registry_list)

    if [[ "$projects" == "[]" ]] || [[ -z "$projects" ]]; then
        ui_message "No Projects Found" "No existing projects registered."
        return
    fi

    local project_options=()

    while IFS= read -r line; do
        [[ -z "$line" ]] && continue

        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')

        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi

        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")

        local option="${running} ${name} │ ${mode_label} │ ${time_ago}"
        project_options+=("$path")
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)

    project_options+=("BACK")

    local selected
    selected=$("$GUM_BIN" choose \
        --cursor.foreground "#FF6B6B" \
        --selected.foreground "#4ECDC4" \
        "${project_options[@]}" \
        --height 6)

    if [[ "$selected" == "BACK" ]]; then
        echo "ACTION:BACK"
    else
        echo "ACTION:SELECT:$selected"
    fi
}

show_fzf_menu() {
    echo "🚀 OpenCode Sandbox [FZF Mode]"
    echo ""

    local menu_items=(
        "🆕 New Project (Init)"
        "📂 Start Existing Project"
        "❌ Exit"
    )

    local choice
    choice=$(printf '%s\n' "${menu_items[@]}" | fzf \
        --header="OpenCode Sandbox - Main Menu" \
        --prompt="Select action > " \
        --height=8 \
        --layout=reverse \
        --border)

    case "$choice" in
        *"New"*)
            echo "ACTION:NEW"
            ;;
        *"Start"*)
            echo "ACTION:START"
            ;;
        *"Exit"*)
            echo "ACTION:EXIT"
            ;;
    esac
}

show_fzf_project_list() {
    local projects
    projects=$(registry_list)

    if [[ "$projects" == "[]" ]] || [[ -z "$projects" ]]; then
        ui_message "No Projects Found" "No existing projects registered."
        return
    fi

    local menu=""
    local -a path_map=()

    while IFS= read -r line; do
        [[ -z "$line" ]] && continue

        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')

        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi

        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")

        menu+="${running} ${name} │ ${mode_label} │ ${time_ago}"$'\n'
        path_map+=("$path")
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)

    menu+="⬅️  Back to Main Menu"$'\n'
    path_map+=("BACK")

    local choice
    local choice_index
    choice=$(echo -e "$menu" | fzf \
        --header="Select a project to start" \
        --prompt="Project > " \
        --height=8 \
        --layout=reverse \
        --border)

    choice_index=$(echo -e "$menu" | grep -nxF "$choice" | cut -d: -f1)
    choice_index=$((choice_index - 1))

    if [[ "${path_map[$choice_index]}" == "BACK" ]]; then
        echo "ACTION:BACK"
    else
        echo "ACTION:SELECT:${path_map[$choice_index]}"
    fi
}

show_select_menu() {
    echo "🚀 OpenCode Sandbox [Basic Mode]"
    echo "Using basic bash select menu."
    echo ""

    PS3="Select action: "
    select action in "🆕 New Project (Init)" "📂 Start Existing Project" "❌ Exit"; do
        case "$action" in
            *"New"*)
                echo "ACTION:NEW"
                break
                ;;
            *"Start"*)
                echo "ACTION:START"
                break
                ;;
            *"Exit"*)
                echo "ACTION:EXIT"
                break
                ;;
        esac
    done
}

show_select_project_list() {
    local projects
    projects=$(registry_list)

    if [[ "$projects" == "[]" ]] || [[ -z "$projects" ]]; then
        ui_message "No Projects Found" "No existing projects registered."
        read -p "Press Enter to continue..."
        echo "ACTION:BACK"
        return
    fi

    echo ""
    echo "Select a project:"
    echo ""

    local i=1
    declare -A path_map

    while IFS= read -r line; do
        [[ -z "$line" ]] && continue

        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')

        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi

        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")
        local mode_label=$(detect_project_config "$path")

        local option="${running} ${name} │ ${mode_label} │ ${time_ago}"
        echo "  $i) $option"
        path_map[$i]="$path"
        ((i++))
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)

    local back_idx=$i
    echo "  $back_idx) ⬅️  Back to Main Menu"
    echo ""

    read -p "Select option: " choice

    if [[ "$choice" == "$back_idx" ]]; then
        echo "ACTION:BACK"
    elif [[ -n "${path_map[$choice]}" ]]; then
        echo "ACTION:SELECT:${path_map[$choice]}"
    else
        echo "ACTION:INVALID"
    fi
}

show_whiptail_menu() {
    local menu_items=()
    local i=1

    menu_items+=("1" "🆕 New Project (Init)")
    menu_items+=("2" "📂 Start Existing Project")
    menu_items+=("3" "❌ Exit")

    local idx
    idx=$("$TUI_BACKEND" --menu "OpenCode Sandbox - Main Menu" 15 60 3 \
        "${menu_items[@]}" 3>&1 1>&2 2>&3) || {
        echo "ACTION:EXIT"
        return
    }

    case "$idx" in
        1) echo "ACTION:NEW" ;;
        2) echo "ACTION:START" ;;
        3) echo "ACTION:EXIT" ;;
    esac
}

show_whiptail_project_list() {
    local projects
    projects=$(registry_list)

    if [[ "$projects" == "[]" ]] || [[ -z "$projects" ]]; then
        ui_message "No Projects Found" "No existing projects registered."
        read -p "Press Enter to continue..."
        echo "ACTION:BACK"
        return
    fi

    local menu_items=()
    local -a path_map=()
    local i=1

    while IFS= read -r line; do
        [[ -z "$line" ]] && continue

        local name path last_used
        name=$(echo "$line" | jq -r '.name // empty')
        path=$(echo "$line" | jq -r '.path')

        if [[ -z "$name" ]]; then
            name=$(detect_project_name "$path")
        fi

        local running=$(get_running_indicator "$name")
        local time_ago=$(format_last_used "$last_used")

        menu_items+=("$i" "[${running}] ${name} (${time_ago})")
        path_map+=("$path")
        ((i++))
    done < <(echo "$projects" | jq -c '.[]' 2>/dev/null)

    local back_idx=$i
    menu_items+=("$back_idx" "⬅️  Back to Main Menu")
    path_map+=("BACK")

    local idx
    idx=$("$TUI_BACKEND" --menu "Select a project" 15 60 "$((i-1))" \
        "${menu_items[@]}" 3>&1 1>&2 2>&3) || {
        echo "ACTION:BACK"
        return
    }

    if [[ "${path_map[$((idx-1))]}" == "BACK" ]]; then
        echo "ACTION:BACK"
    else
        echo "ACTION:SELECT:${path_map[$((idx-1))]}"
    fi
}

# --- Action Handlers -----------------------------------------------------------

handle_new_project() {
    log_info "Starting new project initialization..."

    if [[ -x "${SCRIPT_DIR}/init-project.sh" ]]; then
        "${SCRIPT_DIR}/init-project.sh"
    else
        log_error "init-project.sh not found or not executable"
        return 1
    fi
}

handle_start_project() {
    local project_path="$1"

    if [[ ! -d "$project_path" ]]; then
        log_error "Project path does not exist: $project_path"
        read -p "Press Enter to continue..."
        return 1
    fi

    local project_name
    project_name=$(detect_project_name "$project_path")

    log_info "Starting project: $project_name"

    if [[ -x "${SCRIPT_DIR}/start.sh" ]]; then
        "${SCRIPT_DIR}/start.sh" "$project_path"
    else
        log_error "start.sh not found or not executable"
        return 1
    fi
}

# --- Main Menu Loop ------------------------------------------------------------

main_menu() {
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

project_list_menu() {
    local action

    case "$TUI_BACKEND" in
        gum)
            action=$(show_gum_project_list)
            ;;
        fzf)
            action=$(show_fzf_project_list)
            ;;
        whiptail|dialog)
            action=$(show_whiptail_project_list)
            ;;
        plain)
            action=$(show_select_project_list)
            ;;
    esac

    echo "$action"
}

run_tui() {
    while true; do
        local action
        action=$(main_menu)

        case "$action" in
            ACTION:NEW)
                handle_new_project
                ;;
            ACTION:START)
                while true; do
                    local project_action
                    project_action=$(project_list_menu)

                    if [[ "$project_action" == "ACTION:BACK" ]]; then
                        break
                    elif [[ "$project_action" == ACTION:SELECT:* ]]; then
                        local project_path="${project_action#ACTION:SELECT:}"
                        handle_start_project "$project_path"
                        registry_update_last_used "$project_path"
                    elif [[ "$project_action" == "ACTION:INVALID" ]]; then
                        log_warn "Invalid selection"
                    fi
                done
                ;;
            ACTION:EXIT)
                log_info "Exiting OpenCode Sandbox TUI"
                exit 0
                ;;
        esac
    done
}

# --- Entry Point ---------------------------------------------------------------

main() {
    init_registry
    ensure_tui
    run_tui
}

main "$@"
