#!/usr/bin/env bash
#
# Deinstallationsskript — English-only from the toolchain-sets cutover (#215).
# Removes the opencode-sandbox installation safely and interactively.
#
set -euo pipefail

# --- Configuration -------------------------------------------------------------
DEFAULT_INSTALL_PATH="$HOME/.oc-sandbox"
CONFIG_DIR="$HOME/.config/oc-sandbox"
GUM_BIN="$HOME/.local/share/oc-sandbox/gum"
TEMP_BACKUP_DIR=""

# --- Global variables ----------------------------------------------------------
INSTALL_PATH="$DEFAULT_INSTALL_PATH"
REMOVE_CONFIG=false
REMOVE_SYMLINKS=true
REMOVE_SHORTCUTS=false
BACKUP_CONFIG=true
FORCE=false
DRY_RUN=false
VERBOSE=false
ACTIONS_TAKEN=()
SKIPPED_FILES=()

# --- Helper functions ----------------------------------------------------------
log_verbose() {
  if $VERBOSE; then
    echo "$1" >&2
  fi
}

log_error() {
  echo "Error: $1" >&2
}

log_info() {
  echo "$1"
}

log_warn() {
  echo "Warning: $1" >&2
}

# Tracks files that could not be removed due to missing permissions.
track_skipped() {
  local path="$1"
  local reason="${2:-No permission}"
  SKIPPED_FILES+=("$path ($reason)")
}

# Removes one path, tolerating permission errors (they are reported at the
# end instead of aborting the run).
remove_path_safe() {
  local path="$1"
  if rm -rf "$path" 2>/dev/null; then
    return 0
  fi
  if [[ -e "$path" ]]; then
    track_skipped "$path"
    return 1
  fi
  return 0
}

report_skipped_files() {
  if [[ ${#SKIPPED_FILES[@]} -eq 0 ]]; then
    return 0
  fi
  echo ""
  log_warn "The following files could NOT be removed due to missing permissions:"
  local entry
  for entry in "${SKIPPED_FILES[@]}"; do
    echo "  - $entry"
  done
  echo ""
  echo "Clean these up manually with sudo if desired."
}

record_action() {
  ACTIONS_TAKEN+=("$1")
}

# --- Signal handling -----------------------------------------------------------
cleanup() {
  if [[ -n "$TEMP_BACKUP_DIR" && -d "$TEMP_BACKUP_DIR" ]]; then
    log_verbose "Cleaning up the temp backup directory: $TEMP_BACKUP_DIR"
    rm -rf "$TEMP_BACKUP_DIR"
  fi
}

trap cleanup EXIT INT TERM

# --- Dependency checking -------------------------------------------------------
gum_available() {
  if [[ -x "$GUM_BIN" ]]; then
    return 0
  fi
  if command -v gum &>/dev/null; then
    return 0
  fi
  return 1
}

get_gum() {
  if [[ -x "$GUM_BIN" ]]; then
    echo "$GUM_BIN"
  elif command -v gum &>/dev/null; then
    echo "$(command -v gum)"
  else
    echo ""
  fi
}

# --- Safety checks -------------------------------------------------------------
check_running_containers() {
  local containers
  containers=$(podman ps --format "{{.Names}}" 2>/dev/null || echo "")

  if [[ -n "$containers" ]]; then
    echo "The following containers are currently running:"
    echo "$containers" | while read -r container; do
      echo "  - $container"
    done
    echo ""

    if $FORCE; then
      log_warn "Containers keep running during the uninstallation."
      return 0
    fi

    echo -n "Recommendation: stop the containers first. Continue anyway? [y/N] "
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
      echo "Uninstallation aborted. Stop the containers first:"
      echo "  podman stop <container-name>"
      exit 2
    fi
  fi
}

detect_workspace_roots() {
  local found=0

  echo "Checking for registered workspace roots..."

  local workspaces_file="$HOME/.config/oc-sandbox/workspaces.json"
  if [[ -f "$workspaces_file" ]]; then
    local workspaces
    workspaces=$(grep -oP '"path":\s*"\K[^"]+' "$workspaces_file" 2>/dev/null || echo "")

    if [[ -n "$workspaces" ]]; then
      echo ""
      echo "WARNING: Registered workspaces found (NOT removed):"
      echo "$workspaces" | while read -r path; do
        echo "  - $path"
      done
      echo ""
      found=1
    fi
  fi

  if [[ $found -eq 0 ]]; then
    echo "No registered workspaces found."
  fi
  echo ""
}

# --- Backup functions ----------------------------------------------------------
create_config_backup() {
  if ! $BACKUP_CONFIG; then
    return 0
  fi

  if [[ ! -d "$CONFIG_DIR" ]]; then
    return 0
  fi

  local backup_dir="$CONFIG_DIR/backups/config-$(date +%Y%m%d_%H%M%S)"

  log_info "Creating config backup at: $backup_dir"

  mkdir -p "$backup_dir"

  local entry
  local failed=false
  for entry in "$CONFIG_DIR"/* "$CONFIG_DIR"/.[!.]*; do
    [[ -e "$entry" ]] || continue
    [[ "$(basename "$entry")" == "backups" ]] && continue
    if ! cp -r "$entry" "$backup_dir/"; then
      failed=true
    fi
  done

  if ! $failed; then
    log_info "Config backup created: $backup_dir"
    record_action "Config backup created: $backup_dir"
    rotate_config_backups
  else
    log_warn "Config backup failed."
    rm -rf "$backup_dir"
    if $FORCE; then
      log_warn "Backup error ignored (--force set), the uninstallation continues."
      return 0
    fi
    echo -n "Continue without a backup and remove the config? [y/N] "
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
      log_error "Uninstallation aborted — the config was not removed."
      exit 1
    fi
    log_warn "Continuing the uninstallation without a config backup."
  fi
}

rotate_config_backups() {
  local backup_base_dir="$CONFIG_DIR/backups"

  if [[ ! -d "$backup_base_dir" ]]; then
    return 0
  fi

  local backups
  backups=$(find "$backup_base_dir" -type d -name "config-*" 2>/dev/null | sort -r)

  local count
  count=$(echo "$backups" | wc -l)

  if [[ $count -gt 5 ]]; then
    log_info "Rotating config backups (keeping the 5 newest)..."

    local old_backups
    old_backups=$(echo "$backups" | tail -n +6)

    echo "$old_backups" | while read -r old_backup; do
      if [[ -d "$old_backup" ]]; then
        rm -rf "$old_backup"
        log_verbose "Deleted: $old_backup"
      fi
    done
  fi
}

create_backup() {
  if ! $BACKUP_CONFIG; then
    return 0
  fi

  if [[ ! -d "$INSTALL_PATH" ]]; then
    return 0
  fi

  TEMP_BACKUP_DIR=$(mktemp -d)
  local backup_date
  backup_date=$(date +%Y%m%d_%H%M%S)
  local backup_dir="$HOME/.oc-sandbox-backup-$backup_date"

  log_info "Creating backup at: $backup_dir"

  if cp -r "$INSTALL_PATH" "$backup_dir"; then
    log_info "Backup created: $backup_dir"
    record_action "Backup created: $backup_dir"
    echo ""
    echo "You can restore it if needed with:"
    echo "  cp -r $backup_dir/* $INSTALL_PATH/"
    echo ""
  else
    log_warn "Backup failed — the installation is removed anyway."
    rm -rf "$TEMP_BACKUP_DIR"
    TEMP_BACKUP_DIR=""
  fi
}

# --- Cleanup functions ---------------------------------------------------------
remove_config() {
  if ! $REMOVE_CONFIG; then
    return 0
  fi

  if [[ ! -d "$CONFIG_DIR" ]]; then
    return 0
  fi

  log_info "Removing config directory: $CONFIG_DIR"

  if $DRY_RUN; then
    echo "  (the config directory would be removed)"
    return 0
  fi

  local backup_base_dir="$CONFIG_DIR/backups"
  local keep_backups=false
  if [[ -d "$backup_base_dir" ]] && compgen -G "$backup_base_dir/config-*" >/dev/null; then
    keep_backups=true
    mv "$backup_base_dir" "$HOME/.oc-sandbox-config-backups"
  fi

  local entry
  for entry in "$CONFIG_DIR"/* "$CONFIG_DIR"/.[!.]*; do
    [[ -e "$entry" ]] || continue
    [[ "$(basename "$entry")" == "backups" ]] && continue
    remove_path_safe "$entry" || true
  done

  if $keep_backups; then
    mkdir -p "$CONFIG_DIR"
    mv "$HOME/.oc-sandbox-config-backups" "$backup_base_dir"
    record_action "Config backups kept at: $backup_base_dir"
    log_info "Config backups kept at: $backup_base_dir"
  fi

  if [[ -d "$CONFIG_DIR" ]] && ! rmdir "$CONFIG_DIR" 2>/dev/null; then
    track_skipped "$CONFIG_DIR" "not empty (remaining files without permissions)"
  fi

  if [[ ! -d "$CONFIG_DIR" ]]; then
    record_action "Config directory removed: $CONFIG_DIR"
    log_info "Config directory removed: $CONFIG_DIR"
  else
    log_warn "Config directory not fully removed: $CONFIG_DIR"
  fi
}

remove_symlinks() {
  if ! $REMOVE_SYMLINKS; then
    log_info "Symlinks are not removed (--no-symlinks)"
    return 0
  fi

  local bin_dir="$HOME/.local/bin"
  local removed=0

  if [[ ! -d "$bin_dir" ]]; then
    return 0
  fi

  log_info "Checking for symlinks in $bin_dir..."

  # The retired stage builder left this list at the toolchain-sets cutover
  # (#215) — it stopped shipping and install.sh removes the stale script on
  # update.
  local scripts=("oc-sandbox" "configure-workspace.sh" "init-workspace.sh" "start.sh" "start-tui.sh" "create-release.sh" "install.sh" "uninstall.sh")

  for script in "${scripts[@]}"; do
    local link="$bin_dir/$script"
    if [[ -L "$link" ]]; then
      local target
      target=$(readlink "$link")
      if [[ "$target" == "$INSTALL_PATH"* ]]; then
        rm "$link"
        log_verbose "Symlink removed: $link"
        removed=$((removed + 1))
      fi
    fi
  done

  if [[ $removed -gt 0 ]]; then
    log_info "$removed symlink(s) removed."
    record_action "$removed symlink(s) removed from $bin_dir"
  else
    log_info "No symlinks to remove found."
  fi
}

remove_gum() {
  if [[ -d "$GUM_BIN" ]]; then
    log_info "Removing gum installation: $GUM_BIN"
    if remove_path_safe "$(dirname "$GUM_BIN")"; then
      record_action "gum installation removed: $(dirname "$GUM_BIN")"
    fi
  fi
}

remove_shortcuts() {
  if ! $REMOVE_SHORTCUTS; then
    return 0
  fi

  log_info "Checking for desktop shortcuts..."

  local removed_all=true

  # Linux: .desktop file in the application menu
  local desktop_file="$HOME/.local/share/applications/oc-sandbox.desktop"
  if [[ -f "$desktop_file" ]]; then
    if remove_path_safe "$desktop_file"; then
      log_verbose ".desktop shortcut removed: $desktop_file"
      record_action "Desktop shortcut removed: $desktop_file"
    else
      removed_all=false
    fi
  fi

  # Linux: installed Hicolor icons (oc-sandbox.png under */apps/)
  if [[ -d "$HOME/.local/share/icons/hicolor" ]]; then
    while IFS= read -r -d '' icon_file; do
      if remove_path_safe "$icon_file"; then
        log_verbose "Hicolor icon removed: $icon_file"
        record_action "Hicolor icon removed: $icon_file"
      else
        removed_all=false
      fi
    done < <(find "$HOME/.local/share/icons/hicolor" -type f -path "*/apps/oc-sandbox.png" -print0 2>/dev/null)
  fi

  # WSL: .lnk in the Windows Start menu (only when interop is available)
  # macOS: .app bundle in ~/Applications
  if [[ "$(uname -s)" == "Darwin" ]]; then
    local app_dir="$HOME/Applications/OC Sandbox.app"
    if [[ -d "$app_dir" ]]; then
      if remove_path_safe "$app_dir"; then
        log_verbose "App bundle removed: $app_dir"
        record_action "App bundle removed: $app_dir"
      else
        removed_all=false
      fi
    fi
  elif command -v powershell.exe &>/dev/null; then
    local win_start_menu=""
    win_start_menu=$(powershell.exe -NoProfile -Command "[Environment]::GetFolderPath('StartMenu')" 2>/dev/null | tr -d '\r' || true)
    win_start_menu=${win_start_menu%%$'\n'*}
    local lnk_unix=""
    if [[ -n "$win_start_menu" ]]; then
      lnk_unix=$(wslpath -u "${win_start_menu}\\Programs\\OC Sandbox.lnk" 2>/dev/null || true)
    fi
    if [[ -n "$lnk_unix" && -f "$lnk_unix" ]]; then
      if remove_path_safe "$lnk_unix"; then
        log_verbose "Windows shortcut removed: $lnk_unix"
        record_action "Windows shortcut removed: $lnk_unix"
      else
        removed_all=false
      fi
    fi
    # WSL: remove the icon copy in %LOCALAPPDATA% (created by the installer)
    local win_localappdata=""
    win_localappdata=$(powershell.exe -NoProfile -Command "[Environment]::GetFolderPath('LocalApplicationData')" 2>/dev/null | tr -d '\r' || true)
    win_localappdata=${win_localappdata%%$'\n'*}
    local icon_unix=""
    if [[ -n "$win_localappdata" ]]; then
      icon_unix=$(wslpath -u "${win_localappdata}\\oc-sandbox\\oc-sandbox.ico" 2>/dev/null || true)
    fi
    if [[ -n "$icon_unix" && -f "$icon_unix" ]]; then
      if remove_path_safe "$icon_unix"; then
        log_verbose "Icon copy removed: $icon_unix"
        record_action "Icon copy removed: $icon_unix"
      else
        removed_all=false
      fi
    fi
  fi

  if ! $removed_all; then
    log_warn "Some desktop shortcuts could not be removed (see above)."
  fi
}

# --- Interactive prompts -------------------------------------------------------
confirm_removal() {
  echo ""
  echo "Contents to remove:"
  echo "  Install path: $INSTALL_PATH"

  if [[ -d "$INSTALL_PATH" ]]; then
    local size
    size=$(du -sh "$INSTALL_PATH" 2>/dev/null | cut -f1)
    echo "  Size: $size"

    local file_count
    file_count=$(find "$INSTALL_PATH" -type f 2>/dev/null | wc -l)
    echo "  Files: ~$file_count"
  fi

  echo ""

  if $FORCE; then
    log_info "Running the uninstallation (--force set)"
    return 0
  fi

  echo -n "Remove the installation? [y/N] "
  read -r response
  if [[ ! "$response" =~ ^[Yy]$ ]]; then
    echo "Uninstallation aborted."
    exit 0
  fi
}

show_help() {
  cat << EOF
Usage: $0 [options]

Uninstalls opencode-sandbox safely and interactively.

Options:
  --install_path <path>  Installation path (default: $DEFAULT_INSTALL_PATH)
  --remove-config        Also remove the config directory
  --no-backup            Create no backup (also for the config)
  --no-symlinks          Do not remove symlinks
  --remove-shortcuts     Remove desktop shortcuts (.desktop/.lnk/.app)
  --force                No confirmations, remove immediately
  --dry-run              Show what would be removed without deleting
  --verbose              Verbose output
  --help                 Show this help

Examples:
  # Interactive uninstallation (default)
  $0

  # Uninstall with config removal and backup
  $0 --remove-config

  # Uninstall without a config backup
  $0 --remove-config --no-backup

  # Uninstall without removing symlinks
  $0 --no-symlinks

  # Uninstall including desktop shortcuts
  $0 --remove-shortcuts

  # Non-interactive (for scripts/CI)
  $0 --force

  # Dry run (show only)
  $0 --dry-run

  # Combined options
  $0 --install_path ~/my-sandbox --remove-config --force --verbose

EOF
}

# --- Argument parsing ----------------------------------------------------------
parse_arguments() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --install_path)
        INSTALL_PATH="${2:-}"
        if [[ -z "$INSTALL_PATH" ]]; then
          log_error "--install_path requires a path"
          exit 1
        fi
        shift 2
        ;;
      --remove-config)
        REMOVE_CONFIG=true
        shift
        ;;
      --no-backup)
        BACKUP_CONFIG=false
        shift
        ;;
      --no-symlinks)
        REMOVE_SYMLINKS=false
        shift
        ;;
      --remove-shortcuts)
        REMOVE_SHORTCUTS=true
        shift
        ;;
      --force)
        FORCE=true
        shift
        ;;
      --dry-run)
        DRY_RUN=true
        shift
        ;;
      --verbose)
        VERBOSE=true
        shift
        ;;
      --help)
        show_help
        exit 0
        ;;
      *)
        log_error "Unknown argument: $1"
        show_help
        exit 1
        ;;
    esac
  done
}

# --- Main removal logic --------------------------------------------------------
perform_removal() {
  if [[ ! -d "$INSTALL_PATH" ]]; then
    echo "Installation not found: $INSTALL_PATH"
    echo "No uninstallation required."
    exit 0
  fi

  if $DRY_RUN; then
    echo "=== DRY-RUN MODE ==="
    echo "The following contents WOULD be removed:"
    echo ""
    find "$INSTALL_PATH" -maxdepth 3 2>/dev/null | head -50
    if [[ $(find "$INSTALL_PATH" -maxdepth 3 2>/dev/null | wc -l) -gt 50 ]]; then
      echo "... (hidden)"
    fi
    echo ""
    echo "Symlinks in $HOME/.local/bin:"
    local bin_dir="$HOME/.local/bin"
    if [[ -d "$bin_dir" ]]; then
      find "$bin_dir" -type l 2>/dev/null | grep "$INSTALL_PATH" || echo "  (none)"
    fi
    echo ""
    echo "gum installation: $GUM_BIN"
    if [[ -d "$GUM_BIN" ]]; then
      echo "  (would be removed)"
    else
      echo "  (not found)"
    fi
    echo ""
    echo "Desktop shortcuts:"
    if $REMOVE_SHORTCUTS; then
      local desktop_file="$HOME/.local/share/applications/oc-sandbox.desktop"
      if [[ -f "$desktop_file" ]]; then
        echo "  $desktop_file"
        echo "  (would be removed)"
      fi
      if [[ "$(uname -s)" == "Darwin" ]] && [[ -d "$HOME/Applications/OC Sandbox.app" ]]; then
        echo "  $HOME/Applications/OC Sandbox.app"
        echo "  (would be removed)"
      fi
    else
      echo "  (left untouched — --remove-shortcuts not set)"
    fi
    echo ""
    if $REMOVE_CONFIG; then
      echo "Config directory: $CONFIG_DIR"
      if [[ -d "$CONFIG_DIR" ]]; then
        echo "  (the config directory would be removed)"
        if $BACKUP_CONFIG; then
          echo "  (a backup would be created: $CONFIG_DIR/backups/config-$(date +%Y%m%d_%H%M%S))"
        fi
      else
        echo "  (not found)"
      fi
    else
      echo "Config directory: $CONFIG_DIR (left untouched)"
    fi
    echo ""
    echo "=== END DRY-RUN ==="
    exit 0
  fi

  # Safety checks
  check_running_containers
  detect_workspace_roots

  # Backup config if requested
  if $REMOVE_CONFIG; then
    create_config_backup
  fi

  # Backup
  create_backup

  # Confirm
  confirm_removal

  # Perform removal
  echo ""
  echo "Removing the installation..."

  rm -rf "$INSTALL_PATH" 2>/dev/null || true
  if [[ -e "$INSTALL_PATH" ]]; then
    local remaining
    while IFS= read -r remaining; do
      track_skipped "$remaining"
    done < <(find "$INSTALL_PATH" 2>/dev/null)
    log_warn "Installation not fully removed: $INSTALL_PATH"
  else
    record_action "Installation removed: $INSTALL_PATH"
    log_info "Installation removed: $INSTALL_PATH"
  fi

  # Cleanup symlinks
  remove_symlinks

  # Cleanup desktop shortcuts if requested
  remove_shortcuts

  # Cleanup gum
  remove_gum

  # Cleanup config directory if requested
  remove_config

  echo ""
  echo "==> Uninstallation complete!"
  echo ""
  echo "Actions taken:"
  local action
  for action in "${ACTIONS_TAKEN[@]}"; do
    echo "  - $action"
  done

  report_skipped_files

  echo ""
  echo "Remaining artifacts (manual cleanup optional):"
  echo "  - Container images: podman images | grep opencode-sandbox"
  echo "  - Workspace roots: your workspace directories (untouched)"

  if ! $REMOVE_CONFIG; then
    echo "  - Config: \$HOME/.config/oc-sandbox/ (if present)"
  fi

  echo ""
  echo "To remove all opencode-sandbox traces:"
  echo "  podman rmi $(podman images -q | grep opencode-sandbox) 2>/dev/null || true"
  if ! $REMOVE_CONFIG; then
    echo "  rm -rf \$HOME/.config/oc-sandbox/"
  fi
}

# --- Main ----------------------------------------------------------------------
main() {
  echo "==> opencode-sandbox uninstallation"
  echo ""

  parse_arguments "$@"

  perform_removal
}

main "$@"
