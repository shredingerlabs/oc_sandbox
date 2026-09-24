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
SHORTCUT=false
SHORTCUT_CREATED=false
VERBOSE=false
DOWNLOADED_VERSION=""
USER_AGENT="opencode-sandbox-install-script"
PRESERVE_ALLOWLIST=true

# --- Gum Configuration ---------------------------------------------------------
GUM_VERSION="${GUM_VERSION:-0.17.0}"
GUM_BIN="${DEFAULT_INSTALL_PATH}/gum/gum"
PLATFORM_OS=""
PLATFORM_ARCH=""
# Temp-Verzeichnis der gum-Installation – global, damit der EXIT-Trap cleanup_gum
# nach Funktionsende noch darauf zugreifen kann (set -u).
tmpdir=""

# --- Signal-Handling -----------------------------------------------------------
cleanup() {
  if [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]]; then
    if $VERBOSE; then
      echo "Räume temporäres Verzeichnis auf: $TEMP_DIR"
    fi
    rm -rf "$TEMP_DIR"
  fi
  if [[ -n "$tmpdir" && -d "$tmpdir" ]]; then
    rm -rf "$tmpdir"
    tmpdir=""
  fi
}

trap cleanup EXIT INT TERM

# --- Hilfsfunktionen -----------------------------------------------------------
log_verbose() {
  if $VERBOSE; then
    echo "$1" >&2
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

# Liest ein Bestätigungszeichen (Y/N) für Update-Prompts.
# Bevorzugt stdin, wenn es ein Terminal ist; sonst /dev/tty – wichtig für den
# bash one-liner ('curl ... | bash'), wo stdin die Script-Pipe ist und ein
# 'read' von stdin Script-Bytes verschlucken würde.
# Liefert 1, wenn keine interaktive Eingabe möglich ist (kein TTY).
ask_confirm() {
  local prompt_text="$1"
  local reply=""

  if [[ -t 0 ]]; then
    read -p "$prompt_text" -n 1 -r reply
    echo
    REPLY="$reply"
    return 0
  fi

  if { exec 3</dev/tty; } 2>/dev/null; then
    read -u 3 -p "$prompt_text" -n 1 -r reply
    exec 3<&-
    echo >&2
    REPLY="$reply"
    return 0
  fi

  return 1
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

# --- Gum Installation ----------------------------------------------------------
# Wählt die Basis für das gum-Install-Temp-Verzeichnis. In WSL können TMPDIR/
# TEMP/TMP über WSLENV auf ein Windows-Laufwerk (/mnt/c/...) zeigen; auf drvfs/
# 9p schlägt das Entpacken mit "Function not implemented" fehl. Daher nur
# Verzeichnisse auf Linux-Dateisystemen zulassen.
select_gum_tmpdir_parent() {
  local candidate
  for candidate in "${TMPDIR:-}" "${TEMP:-}" "${TMP:-}" /tmp "$HOME"; do
    if [[ -n "$candidate" && -d "$candidate" && ! "$candidate" =~ ^/mnt/[a-z]($|/) ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  printf '/tmp\n'
}

install_gum() {
  local tarball_name url checksums_url sha_cmd expected actual

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

  # Temp-Verzeichnis nur auf Linux-Dateisystemen extrahieren (siehe
  # select_gum_tmpdir_parent). Das Aufräumen übernimmt der globale cleanup-Trap.
  tmpdir=$(mktemp -d "$(select_gum_tmpdir_parent)/gum-install.XXXXXX")

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
  if ! tar -xzf "$tmpdir/$tarball_name" -C "$tmpdir"; then
    log_error "Entpacken des gum-Archivs fehlgeschlagen (tmpdir: ${tmpdir})."
    return 1
  fi

  local extracted_bin
  extracted_bin=$(find "$tmpdir" -type f -name gum | head -n1)
  if [[ -z "$extracted_bin" ]]; then
    log_error "Binary 'gum' im Archiv nicht gefunden."
    return 1
  fi

  mkdir -p "$INSTALL_PATH"
  mkdir -p "$(dirname "$GUM_BIN")"
  cp "$extracted_bin" "$GUM_BIN"
  chmod +x "$GUM_BIN"

  log_info "gum erfolgreich installiert: $GUM_BIN"
  tmpdir=""
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
  
  # Entferne veraltete Symlinks, die in die Installation zeigen
  while IFS= read -r -d '' link; do
    local target
    target=$(readlink "$link")
    if [[ "$target" == "$install_dir"* ]]; then
      rm "$link"
      log_verbose "Entferne veralteten Symlink: $link -> $target"
    fi
  done < <(find "$bin_dir" -maxdepth 1 -type l -print0 2>/dev/null)
  
  # Erzeuge einzelnen Entry-Point-Symlink
  local script="${install_dir}/scripts/start-tui.sh"
  
  if [[ ! -f "$script" ]]; then
    log_error "Skript nicht gefunden: $script"
    return 1
  fi
  
  local link_target="${bin_dir}/oc-sandbox"
  
  # Entferne existierenden Symlink
  if [[ -L "$link_target" ]]; then
    rm "$link_target"
    log_verbose "Entferne existierenden Symlink: $link_target"
  fi
  
  # Erstelle neuen Symlink
  ln -s "$script" "$link_target"
  log_verbose "Erzeuge Symlink: $link_target -> $script"
}

# --- Desktop-Shortcut-Erstellung ------------------------------------------------
SHORTCUT_COMMENT="Interaktive OpenCode-Sandbox TUI"

# Erstellt die plattform-spezifische Desktop-Verknüpfung für die TUI.
# Fehler werden nur gewarnt – die Installation selbst schlägt nie fehl.
create_shortcut() {
  local install_dir="$1"

  log_verbose "Erstelle Desktop-Shortcut für $PLATFORM_OS"

  local ok=false
  case "$PLATFORM_OS" in
    Linux)
      if is_wsl; then
        create_shortcut_wsl "$install_dir" && ok=true
      else
        create_shortcut_linux "$install_dir" && ok=true
      fi
      ;;
    Darwin)
      create_shortcut_macos "$install_dir" && ok=true
      ;;
  esac

  if $ok; then
    SHORTCUT_CREATED=true
    log_verbose "Desktop-Shortcut erstellt."
  fi
  return 0
}

is_wsl() {
  grep -qi microsoft /proc/version 2>/dev/null
}

# Installiert die gebündelten Hicolor-Icons nach $HOME/.local/share/icons/hicolor,
# damit das .desktop-File per Theme-Namen (Icon=oc-sandbox) aufgelöst wird.
install_hicolor_icons() {
  local install_dir="$1"
  local icon_source="${install_dir}/icons/linux/share/icons/hicolor"
  local icon_target="$HOME/.local/share/icons/hicolor"

  if [[ ! -d "$icon_source" ]]; then
    log_verbose "Keine Hicolor-Icons gefunden (${icon_source}) – Shortcut wird ohne Icon erstellt."
    return 1
  fi

  if ! mkdir -p "$icon_target" 2>/dev/null; then
    log_verbose "Verzeichnis nicht beschreibbar: $icon_target"
    return 1
  fi

  if ! cp -R "${icon_source}/." "$icon_target/" 2>/dev/null; then
    log_verbose "Hicolor-Icons konnten nicht kopiert werden nach: $icon_target"
    return 1
  fi

  # Best-Effort: Icon-Cache aktualisieren (nicht alle Umgebungen haben das Tool)
  if command -v gtk-update-icon-cache &>/dev/null; then
    gtk-update-icon-cache -f -t "$icon_target" >/dev/null 2>&1 || true
  fi

  log_verbose "Hicolor-Icons installiert: $icon_target"
  return 0
}

create_shortcut_linux() {
  local install_dir="$1"
  local apps_dir="$HOME/.local/share/applications"
  local desktop_file="${apps_dir}/oc-sandbox.desktop"
  local script="${install_dir}/scripts/start-tui.sh"

  if [[ ! -f "$script" ]]; then
    log_warn "Desktop-Shortcut übersprungen: Skript nicht gefunden: $script"
    return 1
  fi

  if ! mkdir -p "$apps_dir" 2>/dev/null; then
    log_warn "Desktop-Shortcut übersprungen: Verzeichnis nicht beschreibbar: $apps_dir"
    return 1
  fi

  local icon_line="Icon=oc-sandbox"
  if ! install_hicolor_icons "$install_dir"; then
    log_verbose "Hicolor-Icons konnten nicht installiert werden – Shortcut wird ohne Icon erstellt."
    icon_line=""
  fi

  {
    cat << EOF
[Desktop Entry]
Type=Application
Name=OC Sandbox
Comment=${SHORTCUT_COMMENT}
Exec=${script}
Terminal=true
Categories=Development;
EOF
    [[ -z "$icon_line" ]] || echo "$icon_line"
  } > "$desktop_file" 2>/dev/null || {
    log_warn "Desktop-Shortcut übersprungen: Datei nicht beschreibbar: $desktop_file"
    return 1
  }

  log_info "Desktop-Shortcut erstellt: $desktop_file"
  return 0
}

create_shortcut_wsl() {
  local install_dir="$1"
  local script="${install_dir}/scripts/start-tui.sh"

  if [[ ! -f "$script" ]]; then
    log_warn "Desktop-Shortcut übersprungen: Skript nicht gefunden: $script"
    return 1
  fi

  local mnt_c="${OC_SANDBOX_WIN_ROOT:-/mnt/c}"
  if [[ ! -d "$mnt_c" ]]; then
    log_warn "Desktop-Shortcut übersprungen: /mnt/c nicht gefunden – Windows-Laufwerke sind nicht gemountet."
    echo "  Manuelle Lösung: Start-Menu-Verknüpfung von Hand anlegen, die folgendes aufruft:" >&2
    echo "  wsl.exe -e bash ${script}" >&2
    return 1
  fi

  if ! command -v powershell.exe &>/dev/null; then
    log_warn "Desktop-Shortcut übersprungen: powershell.exe nicht verfügbar (WSL-Interop deaktiviert?)."
    echo "  Manuelle Lösung: Start-Menu-Verknüpfung von Hand anlegen, die folgendes aufruft:" >&2
    echo "  wsl.exe -e bash ${script}" >&2
    return 1
  fi

  local win_user
  win_user=$(powershell.exe -NoProfile -Command '$env:USERNAME' 2>/dev/null | tr -d '\r')
  local start_menu_dir=""

  if [[ -n "$win_user" && -d "$mnt_c/Users/$win_user" ]]; then
    start_menu_dir=$(wslpath "$mnt_c/Users/$win_user/AppData/Roaming/Microsoft/Windows/Start Menu/Programs" 2>/dev/null || true)
  fi

  if [[ -z "${start_menu_dir:-}" || ! -d "$start_menu_dir" ]]; then
    log_warn "Desktop-Shortcut übersprungen: Windows-Startmenu-Verzeichnis nicht gefunden."
    echo "  Manuelle Lösung: Start-Menu-Verknüpfung von Hand anlegen, die folgendes aufruft:" >&2
    echo "  wsl.exe -e bash ${script}" >&2
    return 1
  fi

  local lnk_path="${start_menu_dir}/OC Sandbox.lnk"
  local lnk_path_win
  lnk_path_win=$(wslpath -w "$lnk_path")

  local icon_block=""
  local icon_file="${install_dir}/icons/windows/oc-sandbox.ico"
  if [[ -f "$icon_file" ]]; then
    local icon_path_win
    icon_path_win=$(wslpath -w "$icon_file")
    icon_block="\$sc.IconLocation = '${icon_path_win}'; "
    log_verbose "Icon referenziert: $icon_path_win"
  else
    log_verbose "Kein Icon gefunden (${icon_file}) – Shortcut wird ohne Icon erstellt."
  fi
  local ps_block
  ps_block='$sc = (New-Object -ComObject WScript.Shell).CreateShortcut('"'"''"${lnk_path_win}"''"'"'); $sc.TargetPath = '"'"'%SystemRoot%\System32\wsl.exe'"'"'; $sc.Arguments = '"'"'-e bash '"${script}"''"'"'; '"$icon_block"'$sc.Save()'

  log_verbose "Erstelle .lnk über powershell.exe: $lnk_path"
  if ! powershell.exe -NoProfile -Command "$ps_block" 2>/dev/null; then
    log_warn "Desktop-Shortcut übersprungen: powershell.exe konnte die Verknüpfung nicht erstellen."
    return 1
  fi

  log_info "Windows-Startmenu-Verknüpfung erstellt: OC Sandbox.lnk"
  return 0
}

create_shortcut_macos() {
  local install_dir="$1"
  local script="${install_dir}/scripts/start-tui.sh"
  local app_dir="$HOME/Applications/OC Sandbox.app"
  local contents="${app_dir}/Contents"

  if [[ ! -f "$script" ]]; then
    log_warn "Desktop-Shortcut übersprungen: Skript nicht gefunden: $script"
    return 1
  fi

  if ! mkdir -p "${contents}/MacOS" "${contents}/Resources" 2>/dev/null; then
    log_warn "Desktop-Shortcut übersprungen: Verzeichnis nicht beschreibbar: $app_dir"
    return 1
  fi

  if ! command -v osascript &>/dev/null; then
    log_warn "Desktop-Shortcut übersprungen: osascript nicht gefunden."
    echo "  Manuelle Lösung: App-Bundle unter ${app_dir} von Hand anlegen, das start-tui.sh in einem Terminal öffnet." >&2
    return 1
  fi

  local stub="${contents}/MacOS/OC Sandbox"
  {
    cat << APPLESCRIPT
#!/usr/bin/env bash
exec /usr/bin/osascript -e 'tell application "Terminal" to do script "${script}"'
APPLESCRIPT
  } > "$stub" 2>/dev/null || {
    log_warn "Desktop-Shortcut übersprungen: App-Bundle konnte nicht geschrieben werden: $app_dir"
    return 1
  }
  chmod +x "$stub"

  {
    cat << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>OC Sandbox</string>
  <key>CFBundleDisplayName</key>
  <string>OC Sandbox</string>
  <key>CFBundleIdentifier</key>
  <string>io.github.oc-sandbox.tui</string>
  <key>CFBundleExecutable</key>
  <string>OC Sandbox</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
</dict>
PLIST
  } > "${contents}/Info.plist" 2>/dev/null || {
    log_warn "Desktop-Shortcut übersprungen: Info.plist konnte nicht geschrieben werden: $app_dir"
    return 1
  }

  local iconset_dir="${install_dir}/icons/macos/oc-sandbox.iconset"
  if [[ -d "$iconset_dir" ]]; then
    local generated_icns="${TEMP_DIR}/oc-sandbox.icns"
    if command -v iconutil &>/dev/null \
      && iconutil -c icns "$iconset_dir" -o "$generated_icns" 2>/dev/null \
      && cp "$generated_icns" "${contents}/Resources/AppIcon.icns" 2>/dev/null; then
      printf '  <key>CFBundleIconFile</key>\n  <string>AppIcon</string>\n' >> "${contents}/Info.plist"
    else
      log_verbose "Icon konnte nicht aus dem Iconset erzeugt werden – App wird ohne Icon erstellt."
    fi
  else
    log_verbose "Kein Iconset gefunden (${iconset_dir}) – Shortcut wird ohne Icon erstellt."
  fi

  log_info "App-Bundle erstellt: $app_dir"
  return 0
}

remove_shortcut() {
  local install_dir="$1"
  log_verbose "Entferne Desktop-Shortcuts für $PLATFORM_OS"

  local removed=0
  case "$PLATFORM_OS" in
    Linux)
      if is_wsl; then
        # WSL: .lnk-Datei im Windows-Startmenu versuchen zu entfernen
        if command -v powershell.exe &>/dev/null && [[ -d /mnt/c ]]; then
          local win_user
          win_user=$(powershell.exe -NoProfile -Command '$env:USERNAME' 2>/dev/null | tr -d '\r')
          local start_menu_dir
          start_menu_dir=$(wslpath "/mnt/c/Users/$win_user/AppData/Roaming/Microsoft/Windows/Start Menu/Programs" 2>/dev/null || true)
          if [[ -n "${start_menu_dir:-}" && -f "${start_menu_dir}/OC Sandbox.lnk" ]]; then
            rm -f "${start_menu_dir}/OC Sandbox.lnk"
            removed=$((removed + 1))
          fi
        fi
      else
        local desktop_file="$HOME/.local/share/applications/oc-sandbox.desktop"
        if [[ -f "$desktop_file" ]]; then
          rm -f "$desktop_file"
          removed=$((removed + 1))
        fi
      fi
      ;;
    Darwin)
      local app_dir="$HOME/Applications/OC Sandbox.app"
      if [[ -d "$app_dir" ]]; then
        rm -rf "$app_dir"
        removed=$((removed + 1))
      fi
      ;;
  esac

  if [[ $removed -gt 0 ]]; then
    log_info "Desktop-Shortcut(s) entfernt."
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
    "scripts/uninstall.sh"
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
    
    if ! ask_confirm "Überschreiben? [y/N] "; then
      log_error "Vorhandene Installation gefunden, aber kein interaktives Terminal verfügbar (z.B. bei 'curl ... | bash' ohne Terminal)."
      log_error "Zum Überschreiben --force anhängen oder das Script interaktiv in einem Terminal ausführen:"
      log_error "  curl -sL https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/scripts/install.sh | bash -s -- --force"
      exit 2
    fi
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
      # Frage nach allowlist.txt
      if [[ -f "$install_path/proxy/allowlist.txt" ]]; then
        echo "Vorhandene proxy/allowlist.txt gefunden."
        
        if ! ask_confirm "Proxy-Konfiguration erhalten? [Y/n] "; then
          log_error "Kein interaktives Terminal verfügbar – bitte --force verwenden."
          exit 2
        fi
        
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
  --shortcut             Desktop-Shortcut für die TUI erstellen (Linux: .desktop,
                         WSL: Windows-Startmenu .lnk, macOS: .app in ~/Applications)
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

  # Mit Desktop-Shortcut im Startmenu (unabhängig von --symlinks)
  $0 --shortcut

  # Kombinierte Optionen
  $0 --install_path ~/sandbox --version v1.0.0 --symlinks --verbose

Bash one-liner:
  curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash

  oder falls curl nicht verfügbar:
  wget -qO- https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash

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
      --shortcut)
        SHORTCUT=true
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
  
  # Erkenne Plattform ( PLATFORM_OS/PLATFORM_ARCH ), z.B. für Shortcut-Erstellung
  detect_platform || exit_with_error "Plattform konnte nicht erkannt werden"
  
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

  # Erstelle Desktop-Shortcut wenn gewünscht (Fehler brechen die Installation nie ab)
  if $SHORTCUT; then
    create_shortcut "$INSTALL_PATH"
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
    echo "Symlink erstellt in \$HOME/.local/bin/."
    echo "Sie können die Sandbox jetzt von überall starten:"
    echo "  oc-sandbox"
  fi

  if $SHORTCUT_CREATED; then
    echo ""
    echo "Desktop-Shortcut erstellt – die TUI ist ab jetzt über Ihr Startmenu/Launcher"
    echo "verfügbar (Eintrag \"OC Sandbox\")."
  elif $SHORTCUT; then
    echo ""
    echo "Desktop-Shortcut konnte nicht erstellt werden – Details oben."
  fi
}

# --- Start ----------------------------------------------------------------------
# Start via Datei: BASH_SOURCE[0] == $0. Start via 'curl ... | bash': Script
# kommt von stdin, BASH_SOURCE[0] ist leer (und $0 ist 'bash').
if [[ "${BASH_SOURCE[0]:-}" == "$0" ]] || [[ -z "${BASH_SOURCE[0]:-}" ]]; then
  main "$@"
fi
