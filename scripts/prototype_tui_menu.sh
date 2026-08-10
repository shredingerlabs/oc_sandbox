#!/usr/bin/env bash
#
# PROTOTYPE: TUI Main Menu Structure
# Question: What should the main menu of start-tui.sh look like?
#
# This demonstrates the main menu structure with:
# - Display of existing projects (name, path, last_used, running status)
# - Selection between "Start existing project" and "New project (init)"
# - Exit option
# - Visual layout using fzf or fallback (select)
#
# Usage: ./scripts/prototype_tui_menu.sh
#

set -euo pipefail

PROTOTYPE_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${PROTOTYPE_DIR}/.." && pwd)"

# --- Mock Project Registry (simulating what will be in ~/.config/opencode-sandbox/projects.json) ---
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
    # Calculate relative time (simplified)
    local now seconds_ago days_ago hours_ago
    now=$(date +%s)
    seconds_ago=$((now - $(date -d "$iso_date" +%s 2>/dev/null || echo "0")))
    
    if [[ $seconds_ago -lt 3600 ]]; then
      echo "${seconds_ago}m ago"
    elif [[ $seconds_ago -lt 86400 ]]; then
      hours_ago=$((seconds_ago / 3600))
      echo "${hours_ago}h ago"
    else
      days_ago=$((seconds_ago / 86400))
      echo "${days_ago}d ago"
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

# --- FZF Version (when available) ---

show_fzf_menu() {
  echo "== OpenCode Sandbox TUI [Prototype] =="
  echo ""
  
  # Transform projects to fzf-friendly format
  local projects_list=""
  local i=0
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    
    local name path created last_used mode edition
    name=$(echo "$line" | jq -r '.name')
    path=$(echo "$line" | jq -r '.path')
    last_used=$(echo "$line" | jq -r '.last_used')
    mode=$(echo "$line" | jq -r '.mode')
    edition=$(echo "$line" | jq -r '.edition')
    
    local running_status="  "
    if is_container_running "$name"; then
      running_status="● "
    fi
    
    local time_ago=$(format_last_used "$last_used")
    local mode_label="${mode}/${edition}"
    
    projects_list+="${running_status}${name}\t${mode_label}\t${time_ago}\t${path}\n"
    ((i++))
  done < <(echo "$MOCK_PROJECTS" | jq -c '.[]')
  
  # Main menu options
  local menu=""
  menu+="[NEW]  New project (init)\n"
  menu+="------------------------\n"
  menu+="$projects_list"
  menu+="------------------------\n"
  menu+="[EXIT] Exit\n"
  
  echo "$menu" | column -t -s $'\t' | fzf \
    --header="OpenCode Sandbox - Main Menu" \
    --prompt="Select action > " \
    --height=20 \
    --layout=reverse \
    --info=inline \
    --border \
    --margin=1,2 \
    --preview-window=right:40% \
    --preview='echo "Preview: {}"' || echo "exit"
}

# --- Fallback Version (pure bash select) ---

show_fallback_menu() {
  while true; do
    clear
    echo "== OpenCode Sandbox TUI [Prototype - Fallback Mode] =="
    echo ""
    
    # Display actions first
    echo "Actions:"
    echo "  1) [NEW]  New project (init)"
    echo ""
    
    # Display projects
    echo "Projects:"
    local i=2
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      
      local name path created last_used mode edition
      name=$(echo "$line" | jq -r '.name')
      path=$(echo "$line" | jq -r '.path')
      last_used=$(echo "$line" | jq -r '.last_used')
      mode=$(echo "$line" | jq -r '.mode')
      edition=$(echo "$line" | jq -r '.edition')
      
      local running_status="  "
      if is_container_running "$name"; then
        running_status="● "
      fi
      
      local time_ago=$(format_last_used "$last_used")
      local mode_label="${mode}/${edition}"
      
      echo "  ${i}) [${running_status}] ${name}"
      echo "       ${mode_label} | ${time_ago} | ${path}"
      ((i++))
    done < <(echo "$MOCK_PROJECTS" | jq -c '.[]')
    
    echo ""
    echo "  ${i}) [EXIT] Exit"
    echo ""
    
    read -p "Select action > " choice
    
    case "$choice" in
      1)
        echo "Selected: New project (init)"
        read -p "Press Enter to continue..."
        ;;
      $i)
        echo "Selected: Exit"
        exit 0
        ;;
      *)
        if [[ "$choice" =~ ^[0-9]+$ ]] && [[ "$choice" -ge 2 ]] && [[ "$choice" -lt "$i" ]]; then
          local project_index=$((choice - 2))
          local selected_project=$(echo "$MOCK_PROJECTS" | jq -r ".[$project_index].name")
          echo "Selected: $selected_project"
          read -p "Press Enter to continue..."
        else
          echo "Invalid choice. Press Enter to continue..."
          read
        fi
        ;;
    esac
  done
}

# --- Main Execution ---

main() {
  if check_fzf; then
    local selection=$(show_fzf_menu)
    case "$selection" in
      "[NEW]"*)
        echo "Prototype: Would trigger init flow"
        echo "Selected: New project (init)"
        ;;
      "[EXIT]"*)
        echo "Prototype: Exiting"
        exit 0
        ;;
      *)
        local project_name=$(echo "$selection" | awk '{print $2, $3, $4, $5, $6, $7, $8, $9, $10}' | sed 's/\s*$//' || echo "$selection" | awk '{print $2}')
        echo "Prototype: Would start project: $project_name"
        echo "Selected project: $selection"
        ;;
    esac
  else
    show_fallback_menu
  fi
}

main "$@"