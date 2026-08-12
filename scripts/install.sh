#!/usr/bin/env bash
#
# Installiert opencode-sandbox automatisch von GitHub releases.
# Kann per curl/wget one-liner ausgeführt werden oder direkt aufgerufen werden.
#
set -euo pipefail

# --- Konfiguration ------------------------------------------------------------
REPO_OWNER="shredingerlabs"
REPO_NAME="oc_sandbox"
DEFAULT_INSTALL_PATH="$HOME/.oc-sandbox"
MIN_DISK_SPACE_MB=500
GITHUB_API_BASE="https://api.github.com"
TEMP_DIR=""

# --- Globale Variablen ---------------------------------------------------------
INSTALL_PATH="$DEFAULT_INSTALL_PATH"
VERSION=""
FORCE=false
SYMLINKS=false
VERBOSE=false
DOWNLOADED_VERSION=""
USER_AGENT="opencode-sandbox-install-script"
PRESERVE_ALLOWLIST=true

# --- Gum Configuration ---------------------------------------------------------
GUM_VERSION="${GUM_VERSION:-0.17.0}"
GUM_BIN="${DEFAULT_INSTALL_PATH}/gum/gum"
PLATFORM_OS=""
PLATFORM_ARCH=""

# --- Signal-Handling -----------------------------------------------------------
cleanup() {
  if [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]]; then
    if $VERBOSE; then
      echo "Räume temporäres Verzeichnis auf: $TEMP_DIR"
    fi
    rm -rf "$TEMP_DIR"
  fi
}

trap cleanup EXIT INT TERM

# --- Hilfsfunktionen -----------------------------------------------------------
log_verbose() {
  if $VERBOSE; then
    echo "$1"
  fi
}

log_error() {
  echo "Fehler: $1" >&2
}

log_info() {
  echo "$1"
}

log_warn() {
  echo "Warnung: $1" >&2
}

exit_with_error() {
  log_error "$1"
  exit 1
}

exit_with_usage_error() {
  log_error "$1"
  show_help
  exit 1
}

# --- Dependency-Checking -------------------------------------------------------
check_dependencies() {
  local missing_deps=()
  
  # Prüfe curl oder wget
  if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
    missing_deps+=("curl oder wget")
  fi
  
  # Prüfe andere benötigte Tools
  for cmd in tar grep awk sed; do
    if ! command -v "$cmd" &> /dev/null; then
      missing_deps+=("$cmd")
    fi
  done
  
  if [[ ${#missing_deps[@]} -gt 0 ]]; then
    log_error "Fehlende Abhängigkeiten: ${missing_deps[*]}"
    echo "Installieren Sie diese Tools und versuchen Sie es erneut." >&2
    exit 3
  fi
}

# --- Platform Detection --------------------------------------------------------
detect_platform() {
  local kernel arch

  kernel=$(uname -s)
  arch=$(uname -m)

  case "$kernel" in
    Linux*)  PLATFORM_OS="Linux" ;;
    Darwin*) PLATFORM_OS="Darwin" ;;
    MINGW*|MSYS*|CYGWIN*)
      log_error "Natives Windows ohne WSL wird nicht unterstützt."
      return 1
      ;;
    *)
      log_error "Nicht unterstütztes Betriebssystem: $kernel"
      return 1
      ;;
  esac

  case "$arch" in
    x86_64|amd64)   PLATFORM_ARCH="x86_64" ;;
    arm64|aarch64)  PLATFORM_ARCH="arm64" ;;
    *)
      log_error "Nicht unterstützte Architektur: $arch"
      return 1
      ;;
  esac
}

# --- Gum Installation ----------------------------------------------------------
gum_available() {
  if command -v gum &>/dev/null; then
    return 0
  fi
  if [[ -x "$GUM_BIN" ]]; then
    return 0
  fi
  return 1
}

install_gum() {
  local tmpdir tarball_name url checksums_url sha_cmd expected actual

  detect_platform || return 1

  tarball_name="gum_${GUM_VERSION}_${PLATFORM_OS}_${PLATFORM_ARCH}.tar.gz"
  url="https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/${tarball_name}"
  checksums_url="https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/checksums.txt"

  if ! command -v curl &>/dev/null; then
    log_error "curl wird für die Installation benötigt, ist aber nicht vorhanden."
    return 1
  fi

  if command -v sha256sum &>/dev/null; then
    sha_cmd="sha256sum"
  elif command -v shasum &>/dev/null; then
    sha_cmd="shasum -a 256"
  else
    log_warn "Kein sha256sum/shasum gefunden – Checksum-Prüfung wird übersprungen."
    sha_cmd=""
  fi

  tmpdir=$(mktemp -d)

  cleanup_gum() {
    rm -rf "$tmpdir"
  }
  trap cleanup_gum EXIT

  log_info "Lade gum v${GUM_VERSION} für ${PLATFORM_OS}/${PLATFORM_ARCH} herunter ..."
  if ! curl -fsSL "$url" -o "$tmpdir/$tarball_name"; then
    log_error "Download fehlgeschlagen: $url"
    return 1
  fi

  if [[ -n "$sha_cmd" ]]; then
    log_info "Verifiziere Checksum ..."
    if ! curl -fsSL "$checksums_url" -o "$tmpdir/checksums.txt"; then
      log_warn "checksums.txt konnte nicht geladen werden – Prüfung wird übersprungen."
    else
      expected=$(grep " ${tarball_name}\$" "$tmpdir/checksums.txt" | awk '{print $1}')
      if [[ -z "$expected" ]]; then
        log_warn "Kein Checksum-Eintrag für ${tarball_name} gefunden – Prüfung wird übersprungen."
      else
        actual=$($sha_cmd "$tmpdir/$tarball_name" | awk '{print $1}')
        if [[ "$expected" != "$actual" ]]; then
          log_error "Checksum-Mismatch! Erwartet: $expected, erhalten: $actual"
          return 1
        fi
        log_info "Checksum OK."
      fi
    fi
  fi

  log_info "Entpacke und installiere nach ${INSTALL_PATH} ..."
  tar -xzf "$tmpdir/$tarball_name" -C "$tmpdir"

  local extracted_bin
  extracted_bin=$(find "$tmpdir" -type f -name gum | head -n1)
  if [[ -z "$extracted_bin" ]]; then
    log_error "Binary 'gum' im Archiv nicht gefunden."
    return 1
  fi

  mkdir -p "$INSTALL_PATH"
  cp "$extracted_bin" "$GUM_BIN"
  chmod +x "$GUM_BIN"

  log_info "gum erfolgreich installiert: $GUM_BIN"
  trap - EXIT
}

# --- Disk-Space-Checking ------------------------------------------------------
check_disk_space() {
  local path="$1"
  local required_mb="$2"
  
  local available_kb
  available_kb=$(df -k "$path" 2>/dev/null | awk 'NR==2 {print $4}')
  
  if [[ -z "$available_kb" ]]; then
    log_error "Kann Speicherplatz nicht prüfen für: $path"
    return 1
  fi
  
  local available_mb=$((available_kb / 1024))
  
  if [[ $available_mb -lt $required_mb ]]; then
    log_error "Nicht genügend Speicherplatz verfügbar."
    echo "  Benötigt: ${required_mb}MB" >&2
    echo "  Verfügbar: ${available_mb}MB" >&2
    exit 1
  fi
  
  log_verbose "Speicherplatz-Check bestanden: ${available_mb}MB verfügbar"
}

# --- Netzwerk-Operationen mit Retry -------------------------------------------
download_with_retry() {
  local url="$1"
  local output="$2"
  local max_attempts=3
  local attempt=1
  
  while [[ $attempt -le $max_attempts ]]; do
    log_verbose "Download-Versuch $attempt/$max_attempts: $url"
    
    if command -v curl &> /dev/null; then
      if curl -fsSL -A "$USER_AGENT" -o "$output" "$url"; then
        return 0
      fi
    elif command -v wget &> /dev/null; then
      if wget -q --user-agent="$USER_AGENT" -O "$output" "$url"; then
        return 0
      fi
    fi
    
    if [[ $attempt -lt $max_attempts ]]; then
      local wait_time=$((2 ** (attempt - 1)))
      log_verbose "Download fehlgeschlagen, warte ${wait_time}s..."
      sleep "$wait_time"
    fi
    
    ((attempt++))
  done
  
  log_error "Download nach $max_attempts Versuchen fehlgeschlagen: $url"
  return 1
}

# --- GitHub Release Detection --------------------------------------------------
get_latest_release() {
  local api_url="${GITHUB_API_BASE}/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
  local temp_file
  temp_file=$(mktemp)
  
  if download_with_retry "$api_url" "$temp_file"; then
    local tag
    tag=$(grep -oP '"tag_name":\s*"\K[^"]+' "$temp_file" | head -1)
    if [[ -n "$tag" ]]; then
      rm -f "$temp_file"
      echo "$tag"
      return 0
    fi
  fi
  
  rm -f "$temp_file"
  
  # Fallback: HTML Scraping
  log_verbose "GitHub API fehlgeschlagen, nutze HTML-Scraping als Fallback"
  local html_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest"
  local html_file
  html_file=$(mktemp)
  
  if download_with_retry "$html_url" "$html_file"; then
    local tag
    tag=$(grep -oP 'tag/\K[^"]+' "$html_file" | head -1)
    rm -f "$html_file"
    if [[ -n "$tag" ]]; then
      echo "$tag"
      return 0
    fi
  fi
  
  rm -f "$html_file"
  return 1
}

# --- Download-Methoden ---------------------------------------------------------
download_via_git() {
  local tag="$1"
  local target_dir="$2"
  
  local repo_url="https://github.com/${REPO_OWNER}/${REPO_NAME}.git"
  local max_attempts=3
  local attempt=1
  
  log_verbose "Klone Repository mit Tag: $tag"
  
  while [[ $attempt -le $max_attempts ]]; do
    log_verbose "Git-Klon-Versuch $attempt/$max_attempts"
    
    # Entferne existierendes Verzeichnis falls vorhanden
    if [[ -d "$target_dir" ]]; then
      rm -rf "$target_dir"
    fi
    
    if git clone --depth 1 --branch "$tag" "$repo_url" "$target_dir" 2>&1; then
      return 0
    fi
    
    if [[ $attempt -lt $max_attempts ]]; then
      local wait_time=$((2 ** (attempt - 1)))
      log_verbose "Git-Klon fehlgeschlagen, warte ${wait_time}s..."
      sleep "$wait_time"
    fi
    
    ((attempt++))
  done
  
  log_verbose "Git-Klon nach $max_attempts Versuchen fehlgeschlagen"
  return 1
}

download_via_tarball() {
  local tag="$1"
  local target_dir="$2"
  
  local tarball_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/archive/refs/tags/${tag}.tar.gz"
  local temp_tarball
  temp_tarball=$(mktemp)
  
  log_verbose "Lade Tarball herunter: $tarball_url"
  
  if download_with_retry "$tarball_url" "$temp_tarball"; then
    mkdir -p "$target_dir"
    
    # Extrahiere mit Retry-Logik
    local max_attempts=3
    local attempt=1
    
    while [[ $attempt -le $max_attempts ]]; do
      log_verbose "Extrahiere Tarball (Versuch $attempt/$max_attempts)"
      
      if tar -xzf "$temp_tarball" -C "$(dirname "$target_dir")" 2>/dev/null; then
        local extracted_dir
        extracted_dir=$(find "$(dirname "$target_dir")" -maxdepth 1 -type d -name "${REPO_NAME}-${tag}" 2>/dev/null | head -1)
        
        if [[ -n "$extracted_dir" && -d "$extracted_dir" ]]; then
          # Verschiebe Inhalt in target_dir
          mv "${extracted_dir}"/* "$target_dir/"
          mv "${extracted_dir}"/.[^.]* "$target_dir/" 2>/dev/null || true
          rm -rf "$extracted_dir"
          rm -f "$temp_tarball"
          log_verbose "Tarball erfolgreich extrahiert"
          return 0
        else
          log_verbose "Extrahiertes Verzeichnis nicht gefunden: ${extracted_dir:-'null'}"
        fi
      fi
      
      if [[ $attempt -lt $max_attempts ]]; then
        local wait_time=$((2 ** (attempt - 1)))
        log_verbose "Extrahieren fehlgeschlagen, warte ${wait_time}s..."
        sleep "$wait_time"
      fi
      
      ((attempt++))
    done
    
    log_verbose "Tarball-Extraktion nach $max_attempts Versuchen fehlgeschlagen"
  fi
  
  rm -f "$temp_tarball"
  return 1
}

# --- Installationslogik --------------------------------------------------------
prepare_installation() {
  local version="$1"
  
  TEMP_DIR=$(mktemp -d)
  log_verbose "Temporäres Verzeichnis: $TEMP_DIR"
  
  local source_dir="${TEMP_DIR}/opencode-sandbox"
  mkdir -p "$source_dir"
  
  # Versuche zuerst git clone
  if command -v git &> /dev/null; then
    echo "Verwende git clone Methode"
    if download_via_git "$version" "$source_dir"; then
      DOWNLOADED_VERSION="$version"
      return 0
    fi
    echo "Git-Methode fehlgeschlagen, nutze Tarball-Fallback"
  else
    echo "Git nicht gefunden, nutze direkt Tarball-Methode"
  fi
  
  # Fallback zu Tarball
  log_verbose "Nutze Tarball-Methode"
  if download_via_tarball "$version" "$source_dir"; then
    DOWNLOADED_VERSION="$version"
    return 0
  fi
  
  exit_with_error "Download für Version $version fehlgeschlagen"
}

install_files() {
  local source_dir="$1"
  local target_dir="$2"
  
  log_verbose "Kopiere Dateien nach: $target_dir"
  
  # Prüfe ob allowlist.txt erhalten werden muss (basierend auf User-Entscheidung)
  local allowlist_backup=""
  if $PRESERVE_ALLOWLIST && [[ -d "$target_dir" && -f "$target_dir/proxy/allowlist.txt" ]]; then
    allowlist_backup=$(mktemp)
    cp "$target_dir/proxy/allowlist.txt" "$allowlist_backup"
    log_verbose "Sichere proxy/allowlist.txt (User-Entscheidung: erhalten)"
  elif ! $PRESERVE_ALLOWLIST && [[ -d "$target_dir" && -f "$target_dir/proxy/allowlist.txt" ]]; then
    log_verbose "proxy/allowlist.txt wird überschrieben (User-Entscheidung: nicht erhalten)"
  fi
  
  # Erstelle Zielverzeichnis
  mkdir -p "$target_dir"
  
  # Kopiere alle Dateien
  cp -r "$source_dir"/* "$target_dir/"
  
  # Stelle allowlist.txt wieder her wenn gewünscht
  if $PRESERVE_ALLOWLIST && [[ -n "$allowlist_backup" && -f "$allowlist_backup" ]]; then
    cp "$allowlist_backup" "$target_dir/proxy/allowlist.txt"
    rm -f "$allowlist_backup"
    log_verbose "proxy/allowlist.txt wiederhergestellt"
  fi
  
  # Entferne .git Verzeichnis um Platz zu sparen
  if [[ -d "$target_dir/.git" ]]; then
    rm -rf "$target_dir/.git"
    log_verbose ".git Verzeichnis entfernt"
  fi
}

set_executable_permissions() {
  local install_dir="$1"
  
  log_verbose "Setze ausführbare Berechtigungen für .sh Dateien"
  
  while IFS= read -r -d '' file; do
    chmod +x "$file"
    log_verbose "Berechtigung gesetzt: $file"
  done < <(find "$install_dir" -type f -name "*.sh" -print0 2>/dev/null)
}

create_symlinks() {
  local install_dir="$1"
  local bin_dir="$HOME/.local/bin"
  
  log_verbose "Erzeuge Symlinks in: $bin_dir"
  
  mkdir -p "$bin_dir"
  
  local scripts_dir="${install_dir}/scripts"
  if [[ -d "$scripts_dir" ]]; then
    while IFS= read -r -d '' script; do
      local script_name
      script_name=$(basename "$script")
      
      local link_target="${bin_dir}/${script_name}"
      
      # Entferne existierenden Symlink
      if [[ -L "$link_target" ]]; then
        rm "$link_target"
        log_verbose "Entferne existierenden Symlink: $link_target"
      fi
      
      # Erstelle neuen Symlink
      ln -s "$script" "$link_target"
      log_verbose "Erzeuge Symlink: $link_target -> $script"
    done < <(find "$scripts_dir" -type f -name "*.sh" -print0 2>/dev/null)
  fi
}

validate_installation() {
  local install_dir="$1"
  
  log_verbose "Validiere Installation"
  
  local errors=0
  
  # Prüfe wichtige Dateien
  local required_files=(
    "scripts/start.sh"
    "scripts/build-container.sh"
    "scripts/init-project.sh"
    "scripts/start-tui.sh"
    "Dockerfile"
  )
  
  for file in "${required_files[@]}"; do
    local file_path="${install_dir}/${file}"
    if [[ ! -f "$file_path" ]]; then
      log_error "Fehlende Datei: ${file_path}"
      ((errors++))
    elif [[ "$file" == *.sh && ! -x "$file_path" ]]; then
      log_error "Datei nicht ausführbar: ${file_path}"
      ((errors++))
    fi
  done
  
  if [[ $errors -gt 0 ]]; then
    exit_with_error "Validierung fehlgeschlagen: $errors Fehler gefunden"
  fi
  
  log_verbose "Installation validiert"
}

check_existing_installation() {
  local install_path="$1"
  
  if [[ -d "$install_path" ]]; then
    if $FORCE; then
      log_verbose "Überschreibe vorhandene Installation (--force gesetzt)"
      return 0
    fi
    
    echo "Vorhandene Installation gefunden: $install_path"
    read -p "Überschreiben? [y/N] " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
      # Frage nach allowlist.txt
      if [[ -f "$install_path/proxy/allowlist.txt" ]]; then
        echo "Vorhandene proxy/allowlist.txt gefunden."
        read -p "Proxy-Konfiguration erhalten? [Y/n] " -n 1 -r
        echo
        
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
          log_verbose "proxy/allowlist.txt wird erhalten"
          PRESERVE_ALLOWLIST=true
        else
          log_verbose "proxy/allowlist.txt wird überschrieben"
          PRESERVE_ALLOWLIST=false
        fi
      fi
      return 0
    else
      echo "Installation abgebrochen."
      exit 2
    fi
  fi
  
  return 0
}

# --- Hilfe und Verwendung ------------------------------------------------------
show_help() {
  cat << EOF
Nutzung: $0 [Optionen]

Installiert opencode-sandbox automatisch von GitHub releases.

Optionen:
  --install_path <pfad>  Installationspfad (default: $DEFAULT_INSTALL_PATH)
  --version <tag>        Spezifische Version installieren (default: latest)
  --force                Vorhandene Installation ohne Nachfrage überschreiben
  --symlinks             Symlinks in \$HOME/.local/bin erstellen
  --verbose              Detaillierte Ausgabe aktivieren
  --help                 Diese Hilfe anzeigen und beenden

Beispiele:
  # Installation mit Standardpfad und latest Version
  $0

  # Installation mit benutzerdefiniertem Pfad
  $0 --install_path ~/mein-sandbox

  # Spezifische Version installieren
  $0 --version v1.0.0

  # Update erzwingen
  $0 --force

  # Mit Symlinks für einfacheren Zugriff
  $0 --symlinks

  # Kombinierte Optionen
  $0 --install_path ~/sandbox --version v1.0.0 --symlinks --verbose

Bash one-liner:
  curl -sL https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/install.sh | bash

  oder falls curl nicht verfügbar:
  wget -qO- https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/install.sh | bash

EOF
}

# --- Argument Parsing ---------------------------------------------------------
parse_arguments() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --install_path)
        INSTALL_PATH="${2:-}"
        if [[ -z "$INSTALL_PATH" ]]; then
          exit_with_usage_error "--install_path benötigt einen Pfad"
        fi
        shift 2
        ;;
      --version)
        VERSION="${2:-}"
        if [[ -z "$VERSION" ]]; then
          exit_with_usage_error "--version benötigt einen Tag"
        fi
        shift 2
        ;;
      --force)
        FORCE=true
        shift
        ;;
      --symlinks)
        SYMLINKS=true
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
        exit_with_usage_error "Unbekanntes Argument: $1"
        ;;
    esac
  done
}

# --- Hauptprogramm -------------------------------------------------------------
main() {
  echo "==> opencode-sandbox Installation"
  
  parse_arguments "$@"
  check_dependencies
  
  # Prüfe Disk-Space
  check_disk_space "$(dirname "$INSTALL_PATH")" "$MIN_DISK_SPACE_MB"
  
  # Bestimme Version
  if [[ -z "$VERSION" ]]; then
    echo "Ermittle letzte Release-Version..."
    VERSION=$(get_latest_release)
    if [[ -z "$VERSION" ]]; then
      exit_with_error "Konnte letzte Release-Version nicht ermitteln"
    fi
    echo "Gefundene Version: $VERSION"
  else
    echo "Installiere spezifische Version: $VERSION"
  fi
  
  # Prüfe vorhandene Installation
  check_existing_installation "$INSTALL_PATH"
  
  # Bereite Installation vor
  echo "Lade opencode-sandbox $VERSION herunter..."
  prepare_installation "$VERSION"
  
  local source_dir="${TEMP_DIR}/opencode-sandbox"
  
  # Installiere Dateien
  echo "Installiere nach: $INSTALL_PATH"
  install_files "$source_dir" "$INSTALL_PATH"
  
  # Setze Berechtigungen
  set_executable_permissions "$INSTALL_PATH"
  
  # Validiere Installation
  validate_installation "$INSTALL_PATH"
  
  # Erstelle Symlinks wenn gewünscht
  if $SYMLINKS; then
    create_symlinks "$INSTALL_PATH"
  fi

  # Installiere gum für TUI-Unterstützung
  echo "Installiere gum für interaktive TUI..."
  if ! gum_available; then
    if install_gum; then
      echo "gum erfolgreich installiert."
    else
      log_warn "gum-Installation fehlgeschlagen – TUI wird auf Fallback-Modi zurückgreifen."
    fi
  else
    log_verbose "gum ist bereits installiert."
  fi

  # Erfolgsmeldung
  echo ""
  echo "==> Installation erfolgreich!"
  echo "    Pfad: $INSTALL_PATH"
  echo "    Version: $DOWNLOADED_VERSION"
  echo ""
  if [[ -x "$GUM_BIN" ]]; then
    echo "    gum: $GUM_BIN (installiert)"
  fi
  echo ""
  echo "Nächste Schritte:"
  echo "  1. Podman installieren (falls nicht vorhanden):"
  echo "     sudo apt-get install -y podman pasta fuse-overlayfs"
  echo ""
  echo "  2. Images bauen:"
  echo "     ${INSTALL_PATH}/scripts/build-container.sh full"
  echo ""
  echo "  3. Projekt initialisieren:"
  echo "     ${INSTALL_PATH}/scripts/init-project.sh ~/projects/dein-projekt"
  echo ""
  echo "  4. Sandbox starten:"
  echo "     ${INSTALL_PATH}/scripts/start.sh ~/projects/dein-projekt"
  echo ""
  echo "  5. Interaktive TUI starten:"
  echo "     ${INSTALL_PATH}/scripts/start-tui.sh"

  if $SYMLINKS; then
    echo ""
    echo "Symlinks erstellt in \$HOME/.local/bin/."
    echo "Sie können die Skripte jetzt von überall aufrufen:"
    echo "  build-container.sh"
    echo "  init-project.sh"
    echo "  start.sh"
    echo "  start-tui.sh"
  fi
}

# --- Start ----------------------------------------------------------------------
main "$@"
