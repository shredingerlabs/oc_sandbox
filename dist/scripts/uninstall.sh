#!/usr/bin/env bash
#
# Deinstalliert opencode-sandbox sicher und interaktiv.
# Entfernt nur Dateien in $INSTALL_PATH, niemals Projekt-Roots oder User-Daten.
#
set -euo pipefail

# --- Konfiguration ------------------------------------------------------------
REPO_OWNER="shredingerlabs"
REPO_NAME="oc_sandbox"
DEFAULT_INSTALL_PATH="$HOME/.oc-sandbox"
GUM_BIN="${DEFAULT_INSTALL_PATH}/gum/gum"

# --- Globale Variablen ---------------------------------------------------------
INSTALL_PATH="$DEFAULT_INSTALL_PATH"
FORCE=false
BACKUP_CONFIG=true
DRY_RUN=false
TEMP_BACKUP_DIR=""
VERBOSE=false
REMOVE_CONFIG=false
REMOVE_SYMLINKS=true
CONFIG_DIR="$HOME/.config/oc-sandbox"
SKIPPED_FILES=()
ACTIONS_TAKEN=()

# --- Hilfsfunktionen -----------------------------------------------------------
log_info() {
  echo "$1"
}

log_warn() {
  echo "Warnung: $1" >&2
}

log_error() {
  echo "Fehler: $1" >&2
}

log_verbose() {
  if $VERBOSE; then
    echo "$1"
  fi
}

# Tracket Dateien, die wegen fehlender Berechtigungen nicht entfernt wurden.
track_skipped() {
  local path="$1"
  local reason="${2:-Keine Berechtigung}"
  SKIPPED_FILES+=("$path ($reason)")
}

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
  log_warn "Folgende Dateien konnten aufgrund fehlender Berechtigungen NICHT entfernt werden:"
  local entry
  for entry in "${SKIPPED_FILES[@]}"; do
    echo "  - $entry"
  done
  echo ""
  echo "Bereinigen Sie diese manuell mit sudo, falls gewünscht."
}

record_action() {
  ACTIONS_TAKEN+=("$1")
}

# --- Signal-Handling -----------------------------------------------------------
cleanup() {
  if [[ -n "$TEMP_BACKUP_DIR" && -d "$TEMP_BACKUP_DIR" ]]; then
    log_verbose "Räume temporäres Backup-Verzeichnis auf: $TEMP_BACKUP_DIR"
    rm -rf "$TEMP_BACKUP_DIR"
  fi
}

trap cleanup EXIT INT TERM

# --- Dependency-Checking -------------------------------------------------------
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

# --- Safety Checks -------------------------------------------------------------
check_running_containers() {
  local containers
  containers=$(podman ps --format "{{.Names}}" 2>/dev/null || echo "")
  
  if [[ -n "$containers" ]]; then
    echo "Folgende Container laufen aktuell:"
    echo "$containers" | while read -r container; do
      echo "  - $container"
    done
    echo ""
    
    if $FORCE; then
      log_warn "Container bleiben während der Deinstallation aktiv."
      return 0
    fi
    
    echo -n "Empfehlung: Container zuerst stoppen. Trotzdem fortfahren? [y/N] "
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
      echo "Deinstallation abgebrochen. Stoppen Sie zuerst die Container:"
      echo "  podman stop <container-name>"
      exit 2
    fi
  fi
}

detect_project_roots() {
  local found=0
  
  echo "Prüfe auf registrierte Projekt-Roots..."
  
  local projects_file="$HOME/.config/oc-sandbox/projects.json"
  if [[ -f "$projects_file" ]]; then
    local projects
    projects=$(grep -oP '"path":\s*"\K[^"]+' "$projects_file" 2>/dev/null || echo "")
    
    if [[ -n "$projects" ]]; then
      echo ""
      echo "WARNUNG: Registrierte Projekte gefunden (werden NICHT entfernt):"
      echo "$projects" | while read -r path; do
        echo "  - $path"
      done
      echo ""
      found=1
    fi
  fi
  
  if [[ $found -eq 0 ]]; then
    echo "Keine registrierten Projekte gefunden."
  fi
  echo ""
}

# --- Backup Functions ----------------------------------------------------------
create_config_backup() {
  if ! $BACKUP_CONFIG; then
    return 0
  fi
  
  if [[ ! -d "$CONFIG_DIR" ]]; then
    return 0
  fi

  local backup_dir="$CONFIG_DIR/backups/config-$(date +%Y%m%d_%H%M%S)"
  
  log_info "Erstelle Config-Backup nach: $backup_dir"
  
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
    log_info "Config-Backup erstellt: $backup_dir"
    record_action "Config-Backup erstellt: $backup_dir"
    rotate_config_backups
  else
    log_warn "Config-Backup fehlgeschlagen."
    rm -rf "$backup_dir"
    if $FORCE; then
      log_warn "Backup-Fehler ignoriert (--force gesetzt), Deinstallation läuft weiter."
      return 0
    fi
    echo -n "Ohne Backup fortfahren und Config entfernen? [y/N] "
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
      log_error "Deinstallation abgebrochen – Config wurde nicht entfernt."
      exit 1
    fi
    log_warn "Deinstallation wird ohne Config-Backup fortgesetzt."
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
    log_info "Rotiere Config-Backups (behalte 5 neueste)..."
    
    local old_backups
    old_backups=$(echo "$backups" | tail -n +6)
    
    echo "$old_backups" | while read -r old_backup; do
      if [[ -d "$old_backup" ]]; then
        rm -rf "$old_backup"
        log_verbose "Gelöscht: $old_backup"
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
  
  log_info "Erstelle Backup nach: $backup_dir"
  
  if cp -r "$INSTALL_PATH" "$backup_dir"; then
    log_info "Backup erstellt: $backup_dir"
    record_action "Backup erstellt: $backup_dir"
    echo ""
    echo "Sie können bei Bedarf wiederherstellen mit:"
    echo "  cp -r $backup_dir/* $INSTALL_PATH/"
    echo ""
  else
    log_warn "Backup fehlgeschlagen – Installation wird trotzdem entfernt."
    rm -rf "$TEMP_BACKUP_DIR"
    TEMP_BACKUP_DIR=""
  fi
}

# --- Cleanup Functions ---------------------------------------------------------
remove_config() {
  if ! $REMOVE_CONFIG; then
    return 0
  fi
  
  if [[ ! -d "$CONFIG_DIR" ]]; then
    return 0
  fi
  
  log_info "Entferne Config-Verzeichnis: $CONFIG_DIR"
  
  if $DRY_RUN; then
    echo "  (Config-Verzeichnis würde entfernt werden)"
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
    record_action "Config-Backups erhalten unter: $backup_base_dir"
    log_info "Config-Backups erhalten unter: $backup_base_dir"
  fi

  if [[ -d "$CONFIG_DIR" ]] && ! rmdir "$CONFIG_DIR" 2>/dev/null; then
    track_skipped "$CONFIG_DIR" "Nicht leer (übrige Dateien ohne Berechtigung)"
  fi

  if [[ ! -d "$CONFIG_DIR" ]]; then
    record_action "Config-Verzeichnis entfernt: $CONFIG_DIR"
    log_info "Config-Verzeichnis entfernt: $CONFIG_DIR"
  else
    log_warn "Config-Verzeichnis nicht vollständig entfernt: $CONFIG_DIR"
  fi
}

remove_symlinks() {
  if ! $REMOVE_SYMLINKS; then
    log_info "Symlinks werden nicht entfernt (--no-symlinks)"
    return 0
  fi
  
  local bin_dir="$HOME/.local/bin"
  local removed=0
  
  if [[ ! -d "$bin_dir" ]]; then
    return 0
  fi
  
  log_info "Prüfe auf Symlinks in $bin_dir..."
  
  local scripts=("build-container.sh" "init-project.sh" "start.sh" "start-tui.sh" "create-release.sh" "install.sh" "uninstall.sh")
  
  for script in "${scripts[@]}"; do
    local link="$bin_dir/$script"
    if [[ -L "$link" ]]; then
      local target
      target=$(readlink "$link")
      if [[ "$target" == "$INSTALL_PATH"* ]]; then
        rm "$link"
        log_verbose "Symlink entfernt: $link"
        removed=$((removed + 1))
      fi
    fi
  done
  
  if [[ $removed -gt 0 ]]; then
    log_info "$removed Symlink(s) entfernt."
    record_action "$removed Symlink(s) entfernt aus $bin_dir"
  else
    log_info "Keine Symlinks zum Entfernen gefunden."
  fi
}

remove_gum() {
  if [[ -d "$GUM_BIN" ]]; then
    log_info "Entferne gum-Installation: $GUM_BIN"
    if remove_path_safe "$(dirname "$GUM_BIN")"; then
      record_action "gum-Installation entfernt: $(dirname "$GUM_BIN")"
    fi
  fi
}

# --- Interactive Prompts -------------------------------------------------------
confirm_removal() {
  echo ""
  echo "Zu entfernende Inhalte:"
  echo "  Installationspfad: $INSTALL_PATH"
  
  if [[ -d "$INSTALL_PATH" ]]; then
    local size
    size=$(du -sh "$INSTALL_PATH" 2>/dev/null | cut -f1)
    echo "  Größe: $size"
    
    local file_count
    file_count=$(find "$INSTALL_PATH" -type f 2>/dev/null | wc -l)
    echo "  Dateien: ~$file_count"
  fi
  
  echo ""
  
  if $FORCE; then
    log_info "Deinstallation wird ausgeführt (--force gesetzt)"
    return 0
  fi
  
  echo -n "Installation entfernen? [y/N] "
  read -r response
  if [[ ! "$response" =~ ^[Yy]$ ]]; then
    echo "Deinstallation abgebrochen."
    exit 0
  fi
}

show_help() {
  cat << EOF
Nutzung: $0 [Optionen]

Deinstalliert opencode-sandbox sicher und interaktiv.

Optionen:
  --install_path <pfad>  Installationspfad (default: $DEFAULT_INSTALL_PATH)
  --remove-config        Config-Verzeichnis ebenfalls entfernen
  --no-backup            Kein Backup erstellen (auch für Config)
  --no-symlinks          Symlinks nicht entfernen
  --force                Keine Bestätigungen, sofort entfernen
  --dry-run              Zeige was entfernt würde, ohne zu löschen
  --verbose              Detaillierte Ausgabe
  --help                 Diese Hilfe anzeigen

Beispiele:
  # Interaktive Deinstallation (Standard)
  $0

  # Deinstallation mit Config-Entfernung und Backup
  $0 --remove-config

  # Deinstallation ohne Config-Backup
  $0 --remove-config --no-backup

  # Deinstallation ohne Symlinks zu entfernen
  $0 --no-symlinks

  # Nicht-interaktiv (für Skripte/CI)
  $0 --force

  # Dry-Run (nur anzeigen)
  $0 --dry-run

  # Kombinierte Optionen
  $0 --install_path ~/mein-sandbox --remove-config --force --verbose

EOF
}

# --- Argument Parsing ---------------------------------------------------------
parse_arguments() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --install_path)
        INSTALL_PATH="${2:-}"
        if [[ -z "$INSTALL_PATH" ]]; then
          log_error "--install_path benötigt einen Pfad"
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
        log_error "Unbekanntes Argument: $1"
        show_help
        exit 1
        ;;
    esac
  done
}

# --- Main Removal Logic --------------------------------------------------------
perform_removal() {
  if [[ ! -d "$INSTALL_PATH" ]]; then
    echo "Installation nicht gefunden: $INSTALL_PATH"
    echo "Keine Deinstallation erforderlich."
    exit 0
  fi
  
  if $DRY_RUN; then
    echo "=== DRY-RUN MODE ==="
    echo "Folgende Inhalte WÜRDEN entfernt:"
    echo ""
    find "$INSTALL_PATH" -maxdepth 3 2>/dev/null | head -50
    if [[ $(find "$INSTALL_PATH" -maxdepth 3 2>/dev/null | wc -l) -gt 50 ]]; then
      echo "... (ausgeblendet)"
    fi
    echo ""
    echo "Symlinks in $HOME/.local/bin:"
    local bin_dir="$HOME/.local/bin"
    if [[ -d "$bin_dir" ]]; then
      find "$bin_dir" -type l 2>/dev/null | grep "$INSTALL_PATH" || echo "  (keine)"
    fi
    echo ""
    echo "gum-Installation: $GUM_BIN"
    if [[ -d "$GUM_BIN" ]]; then
      echo "  (würde entfernt)"
    else
      echo "  (nicht gefunden)"
    fi
    echo ""
    if $REMOVE_CONFIG; then
      echo "Config-Verzeichnis: $CONFIG_DIR"
      if [[ -d "$CONFIG_DIR" ]]; then
        echo "  (Config-Verzeichnis würde entfernt)"
        if $BACKUP_CONFIG; then
          echo "  (Backup würde erstellt: $CONFIG_DIR/backups/config-$(date +%Y%m%d_%H%M%S))"
        fi
      else
        echo "  (nicht gefunden)"
      fi
    else
      echo "Config-Verzeichnis: $CONFIG_DIR (bleibt unberührt)"
    fi
    echo ""
    echo "=== ENDE DRY-RUN ==="
    exit 0
  fi
  
  # Safety checks
  check_running_containers
  detect_project_roots
  
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
  echo "Entferne Installation..."
  
  rm -rf "$INSTALL_PATH" 2>/dev/null || true
  if [[ -e "$INSTALL_PATH" ]]; then
    local remaining
    while IFS= read -r remaining; do
      track_skipped "$remaining"
    done < <(find "$INSTALL_PATH" 2>/dev/null)
    log_warn "Installation nicht vollständig entfernt: $INSTALL_PATH"
  else
    record_action "Installation entfernt: $INSTALL_PATH"
    log_info "Installation entfernt: $INSTALL_PATH"
  fi

  # Cleanup symlinks
  remove_symlinks

  # Cleanup gum
  remove_gum

  # Cleanup config directory if requested
  remove_config

  echo ""
  echo "==> Deinstallation abgeschlossen!"
  echo ""
  echo "Durchgeführte Aktionen:"
  local action
  for action in "${ACTIONS_TAKEN[@]}"; do
    echo "  - $action"
  done

  report_skipped_files

  echo ""
  echo "Verbleibende Artefakte (manuelle Bereinigung optional):"
  echo "  - Container-Images: podman images | grep opencode-sandbox"
  echo "  - Projekt-Roots: Ihre Projekt-Verzeichnisse (unberührt)"
  
  if ! $REMOVE_CONFIG; then
    echo "  - Config: \$HOME/.config/oc-sandbox/ (falls vorhanden)"
  fi
  
  echo ""
  echo "Um alle opencode-sandbox Bezüge zu entfernen:"
  echo "  podman rmi $(podman images -q | grep opencode-sandbox) 2>/dev/null || true"
  if ! $REMOVE_CONFIG; then
    echo "  rm -rf \$HOME/.config/oc-sandbox/"
  fi
}

# --- Main ----------------------------------------------------------------------
main() {
  echo "==> opencode-sandbox Deinstallation"
  echo ""
  
  parse_arguments "$@"
  
  perform_removal
}

main "$@"
