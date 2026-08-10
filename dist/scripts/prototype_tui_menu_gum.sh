#!/usr/bin/env bash
#
# PROTOTYPE: TUI Main Menu Structure (Gum Version)
# Question: Modern look and feel using gum TUI framework
#
# This demonstrates a modern TUI main menu with:
 # - Visual hierarchy using gum styles
 # - Interactive project list with running status indicators  
 # - Modern color scheme and spacing
 # - Gum choose/select for better UX
#
 # Usage: ./scripts/prototype_tui_menu_gum.sh
 # Requires: gum (https://github.com/charmbracelet/gum)
 # Install: brew install gum  # macOS
 #          curl https://github.com/charmbracelet/gum/releases/latest/download/gum_Linux_arm64.tar.gz | tar xz && sudo mv gum /usr/local/bin/
 #          Falls back to fzf if gum not available
#

set -euo pipefail

PROTOTYPE_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- Mock Project Registry ---
MOCK_PROJECTS='[
  {
    "name": "customer-x-api",
    "path": "/home/dev/projects/customer-x",
    "created": "2025-08-01T10:30:00Z",
    "last_used": "2025-08-10T14:22:00Z",
    "mode": "full",
    "edition": "web"
  },
  {
    "name": "embedded-fw", 
    "path": "/home/dev/projects/firmware",
    "created": "2025-07-15T09:00:00Z",
    "last_used": "2025-08-09T11:45:00Z",
    "mode": "hil_mode",
    "edition": "embedded"
  },
  {
    "name": "experimental-ai",
    "path": "/home/dev/projects/ai-research", 
    "created": "2025-08-05T16:20:00Z",
    "last_used": "2025-08-10T09:15:00Z",
    "mode": "full",
    "edition": "full"
  }
]'

# --- Helper Functions ---

check_gum() {
  if command -v gum &>/dev/null; then
    return 0
  else
    return 1
  fi
}

check_fzf() {
  if command -v fzf &>/dev/null; then
    return 0
  else
    return 1
  fi
}

format_last_used() {
  local iso_date="$1"
  if [[ -n "$iso_date" ]]; then
    local now seconds_ago
    now=$(date +%s)
    seconds_ago=$((now - $(date -d "$iso_date" +%s 2>/dev/null || echo "0")))
    
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
  if podman container exists "${container_name}" 2>/dev/null; then
    if podman inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -q true; then
      return 0
    fi
  fi
  return 1
}

get_running_status() {
  local project_name="$1"
  if is_container_running "$project_name"; then
    echo "🟢"
  else
    echo "⚪"
  fi
}

# --- Modern Gum Version ---

show_gum_menu() {
  # Header with modern styling
  gum style \
    --foreground "#FF6B6B" \
    --border double \
    --border-foreground "#4ECDC4" \
    --padding "1 2" \
    --align center \
    "🚀 OpenCode Sandbox" "" \
    "Choose your workspace"

  echo ""
  
  # Main action buttons
  local action=$(gum choose \
    --cursor.foreground "#FF6B6B" \
    --selected.foreground "#4ECDC4" \
    "🆕 New Project (Init)" \
    "📂 Start Existing Project" \
    "❌ Exit" \
    --height 3)
  
  case "$action" in
    "🆕 New"*|"📂 Start"*)
      if [[ "$action" == *"New"* ]]; then
        echo "Prototype: Would trigger init flow"
      else
        show_project_list_gum
      fi
      ;;
    "❌ Exit")
      echo "Prototype: Exiting"
      exit 0
      ;;
  esac
}

show_project_list_gum() {
  echo ""
  gum style \
    --foreground "#4ECDC4" \
    --bold "Select a project to start:"
  echo ""
  
  # Build project list with gum formatting
  local project_options=()
  local i=1
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    
    local name path last_used mode edition
    name=$(echo "$line" | jq -r '.name')
    path=$(echo "$line" | jq -r '.path')
    last_used=$(echo "$line" | jq -r '.last_used')
    mode=$(echo "$line" | jq -r '.mode')
    edition=$(echo "$line" | jq -r '.edition')
    
    local running=$(get_running_status "$name")
    local time_ago=$(format_last_used "$last_used")
    local mode_label="${mode}/${edition}"
    
    # Create modern styled option with embedded metadata
    local option="${running} ${name} │ ${mode_label} │ ${time_ago}"
    project_options+=("$option")
    ((i++))
  done < <(echo "$MOCK_PROJECTS" | jq -c '.[]')
  
  # Add back option
  project_options+=("⬅️  Back to Main Menu")
  
  local selected=$(gum choose \
    --cursor.foreground "#FF6B6B" \
    --selected.foreground "#4ECDC4" \
    "${project_options[@]}" \
    --height 6)
  
  case "$selected" in
    "⬅️  "*)
      show_gum_menu
      ;;
    *)
      local project_name=$(echo "$selected" | awk '{print $2}')
      echo "Prototype: Would start project: $project_name"
      echo "Selected: $selected"
      ;;
  esac
}

# --- Fallback Version (fzf when available, otherwise select) ---

show_fallback_menu() {
  if check_fzf; then
    show_fzf_menu
  else
    show_select_menu
  fi
}

show_fzf_menu() {
  echo "🚀 OpenCode Sandbox [Prototype - FZF Mode]"
  echo ""
  
  local menu="🆕 New Project (Init)\n📂 Start Existing Project\n❌ Exit"
  
  local choice=$(echo -e "$menu" | fzf \
    --header="OpenCode Sandbox - Main Menu" \
    --prompt="Select action > " \
    --height=8 \
    --layout=reverse \
    --border \
    --margin=1,2)
  
  case "$choice" in
    *"New"*)
      echo "Prototype: Would trigger init flow"
      echo "Selected: $choice"
      ;;
    *"Start"*)
      show_project_list_fzf
      ;;
    *"Exit"*)
      echo "Prototype: Exiting"
      exit 0
      ;;
  esac
}

show_select_menu() {
  echo "🚀 OpenCode Sandbox [Prototype - Basic Mode]"
  echo "Gum and fzf not installed. Using basic menu."
  echo ""
  
  local options=(
    "🆕 New Project (Init)"
    "📂 Start Existing Project" 
    "❌ Exit"
  )
  
  select action in "${options[@]}"; do
    case "$action" in
      *"New"*)
        echo "Prototype: Would trigger init flow"
        break
        ;;
      *"Start"*)
        show_project_list_select
        break
        ;;
      *"Exit"*)
        echo "Prototype: Exiting"
        exit 0
        ;;
    esac
  done
}

show_project_list_fzf() {
  echo ""
  local project_list=""
  
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    
    local name path last_used mode edition
    name=$(echo "$line" | jq -r '.name')
    path=$(echo "$line" | jq -r '.path')
    last_used=$(echo "$line" | jq -r '.last_used')
    mode=$(echo "$line" | jq -r '.mode')
    edition=$(echo "$line" | jq -r '.edition')
    
    local running=$(get_running_status "$name")
    local time_ago=$(format_last_used "$last_used")
    local mode_label="${mode}/${edition}"
    
    project_list+="$running $name │ $mode_label │ $time_ago\n"
  done < <(echo "$MOCK_PROJECTS" | jq -c '.[]')
  
  local menu="$project_list\n⬅️  Back to Main Menu"
  
  local choice=$(echo -e "$menu" | fzf \
    --header="Select a project to start" \
    --prompt="Project > " \
    --height=8 \
    --layout=reverse \
    --border \
    --margin=1,2)
  
  case "$choice" in
    "⬅️  "*)
      show_fzf_menu
      ;;
    *)
      local project_name=$(echo "$choice" | awk '{print $2}')
      echo "Prototype: Would start project: $project_name"
      echo "Selected: $choice"
      ;;
  esac
}

show_project_list_select() {
  echo ""
  echo "Select a project:"
  echo ""
  
  local i=1
  declare -A project_map
  
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    
    local name path last_used mode edition
    name=$(echo "$line" | jq -r '.name')
    path=$(echo "$line" | jq -r '.path')
    last_used=$(echo "$line" | jq -r '.last_used')
    mode=$(echo "$line" | jq -r '.mode')
    edition=$(echo "$line" | jq -r '.edition')
    
    local running=$(get_running_status "$name")
    local time_ago=$(format_last_used "$last_used")
    local mode_label="${mode}/${edition}"
    
    local option="${running} ${name} │ ${mode_label} │ ${time_ago}"
    echo "  $i) $option"
    project_map[$i]="$name"
    ((i++))
  done < <(echo "$MOCK_PROJECTS" | jq -c '.[]')
  
  echo "  $i) ⬅️  Back to Main Menu"
  project_map[$i]="back"
  
  echo ""
  read -p "Select option: " choice
  
  if [[ "${project_map[$choice]}" == "back" ]]; then
    show_select_menu
  elif [[ -n "${project_map[$choice]}" ]]; then
    local project_name="${project_map[$choice]}"
    echo "Prototype: Would start project: $project_name"
  else
    echo "Invalid choice"
  fi
}

# --- Main Execution ---

main() {
  if check_gum; then
    show_gum_menu
  else
    show_fallback_menu
  fi
}

main "$@"