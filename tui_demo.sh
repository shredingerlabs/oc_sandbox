#!/usr/bin/env bash
#
# tui_demo.sh
#
# Portables TUI-Skript für Linux / macOS / WSL.
# Installiert bei Bedarf automatisch "gum" (mit Checksum-Verifikation)
# und fällt sonst auf whiptail/dialog/reines read zurück.
#
# Nutzung:
#   ./tui_demo.sh
#
# Konfiguration über Umgebungsvariablen möglich:
#   GUM_VERSION           Version, die installiert werden soll (Default unten)
#   DEFAULT_INSTALL_PATH  Zielverzeichnis für das gum-Binary
#   NO_NETWORK=1          Netzwerk-Installation überspringen, direkt Fallback nutzen

set -euo pipefail

# ----------------------------------------------------------------------------
# Konfiguration
# ----------------------------------------------------------------------------

GUM_VERSION="${GUM_VERSION:-0.17.0}"
DEFAULT_INSTALL_PATH="${DEFAULT_INSTALL_PATH:-$HOME/.opencode_sandbox/.gum/bin}"
GUM_BIN="${DEFAULT_INSTALL_PATH}/gum"

# global gesetzt von ensure_tui(): "gum" | "whiptail" | "dialog" | "plain"
TUI_BACKEND=""

# ----------------------------------------------------------------------------
# Hilfsfunktionen: Logging
# ----------------------------------------------------------------------------

log_info()  { printf '\033[1;34m[info]\033[0m %s\n'  "$*" >&2; }
log_warn()  { printf '\033[1;33m[warn]\033[0m %s\n'  "$*" >&2; }
log_error() { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; }

# ----------------------------------------------------------------------------
# Plattform-Erkennung
# ----------------------------------------------------------------------------

# Setzt die Variablen PLATFORM_OS und PLATFORM_ARCH passend zum
# Namensschema der gum-Release-Archive (gum_<version>_<OS>_<ARCH>.tar.gz).
detect_platform() {
    local kernel arch

    kernel=$(uname -s)
    arch=$(uname -m)

    case "$kernel" in
        Linux*)  PLATFORM_OS="Linux" ;;
        Darwin*) PLATFORM_OS="Darwin" ;;
        MINGW*|MSYS*|CYGWIN*)
            # native Windows-Shell ohne WSL wird hier bewusst nicht unterstützt,
            # da dieses Skript für Bash (Linux/macOS/WSL) gedacht ist.
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

# ----------------------------------------------------------------------------
# gum: Installation mit Checksum-Verifikation
# ----------------------------------------------------------------------------

# Prüft, ob bereits eine nutzbare gum-Installation existiert
# (entweder im PATH oder im DEFAULT_INSTALL_PATH).
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

# Lädt das passende Release-Archiv + checksums.txt herunter,
# verifiziert die Prüfsumme und installiert das Binary nach DEFAULT_INSTALL_PATH.
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

    # sha256sum (Linux) oder shasum -a 256 (macOS) verwenden, je nachdem was da ist
    if command -v sha256sum &>/dev/null; then
        sha_cmd="sha256sum"
    elif command -v shasum &>/dev/null; then
        sha_cmd="shasum -a 256"
    else
        log_warn "Kein sha256sum/shasum gefunden – Checksum-Prüfung wird übersprungen."
        sha_cmd=""
    fi

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' RETURN

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

    log_info "Entpacke und installiere nach ${DEFAULT_INSTALL_PATH} ..."
    tar -xzf "$tmpdir/$tarball_name" -C "$tmpdir"

    local extracted_bin
    extracted_bin=$(find "$tmpdir" -type f -name gum | head -n1)
    if [[ -z "$extracted_bin" ]]; then
        log_error "Binary 'gum' im Archiv nicht gefunden."
        return 1
    fi

    mkdir -p "$DEFAULT_INSTALL_PATH"
    cp "$extracted_bin" "$GUM_BIN"
    chmod +x "$GUM_BIN"

    log_info "gum erfolgreich installiert: $GUM_BIN"
}

# ----------------------------------------------------------------------------
# TUI-Backend ermitteln (gum > whiptail > dialog > plain)
# ----------------------------------------------------------------------------

ensure_tui() {
    if [[ "${NO_NETWORK:-0}" != "1" ]]; then
        if gum_available; then
            TUI_BACKEND="gum"
            return 0
        fi

        log_info "gum nicht gefunden – versuche Installation ..."
        if install_gum && gum_available; then
            TUI_BACKEND="gum"
            return 0
        fi
        log_warn "gum-Installation fehlgeschlagen, weiche auf Fallback aus."
    fi

    if command -v whiptail &>/dev/null; then
        TUI_BACKEND="whiptail"
    elif command -v dialog &>/dev/null; then
        TUI_BACKEND="dialog"
    else
        log_warn "Weder whiptail noch dialog verfügbar – nutze einfache read-Prompts."
        TUI_BACKEND="plain"
    fi

    log_info "Verwende TUI-Backend: $TUI_BACKEND"
}

# ----------------------------------------------------------------------------
# Einheitliche UI-Wrapper-Funktionen für alle Backends
# ----------------------------------------------------------------------------

# ui_choose "Titel" "Option A" "Option B" ...  -> Auswahl auf stdout
ui_choose() {
    local title="$1"; shift
    local options=("$@")

    case "$TUI_BACKEND" in
        gum)
            "$GUM_BIN" choose --header "$title" "${options[@]}"
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

# ui_confirm "Frage?" -> Exit-Code 0 = Ja, 1 = Nein
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

# ui_input "Prompt" ["Platzhalter"] -> Eingabe auf stdout
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

# ----------------------------------------------------------------------------
# Demo
# ----------------------------------------------------------------------------

main() {
    ensure_tui

    local name
    name=$(ui_input "Wie heißt du?" "z.B. Max")
    [[ -z "$name" ]] && name="Unbekannt"

    local farbe
    farbe=$(ui_choose "Lieblingsfarbe wählen:" "Rot" "Grün" "Blau" "Lila")

    if ui_confirm "Hallo ${name}, deine Farbe ist ${farbe}. Passt das?"; then
        log_info "Bestätigt. Fertig!"
    else
        log_info "Abgebrochen."
    fi
}

main "$@"
