#!/usr/bin/env bash
#
# Tests für install.sh Script
# Testet externes Verhalten (Exit-Codes, Dateisystem-Änderungen, User-Prompts)
# statt Implementierungsdetails
#

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$TEST_DIR/.." && pwd)"
INSTALL_SCRIPT="${PROJECT_ROOT}/scripts/install.sh"

# Test-Konfiguration
TEMP_BASE="${TMPDIR:-/tmp}/install-tests-$$"
mkdir -p "$TEMP_BASE"

# Test-Counter
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Farben für Test-Ausgabe (deaktiviert für CI/Testing)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Hilfsfunktionen
setup_test_env() {
  local test_name="$1"
  local test_dir="${TEMP_BASE}/${test_name}"
  mkdir -p "$test_dir"
  echo "$test_dir"
}

cleanup_test_env() {
  local test_dir="$1"
  if [[ -d "$test_dir" ]]; then
    rm -rf "$test_dir"
  fi
}

run_test() {
  local test_name="$1"
  local test_function="$2"
  
  ((TESTS_RUN++))
  printf "Test: ${test_name}... "
  
  if $test_function; then
    ((TESTS_PASSED++))
    printf "${GREEN}PASSED${NC}\n"
  else
    ((TESTS_FAILED++))
    printf "${RED}FAILED${NC}\n"
  fi
}

assert_equals() {
  local expected="$1"
  local actual="$2"
  local message="${3:-}"
  
  if [[ "$expected" != "$actual" ]]; then
    echo -e "${RED}Assertion failed${NC}"
    if [[ -n "$message" ]]; then
      echo "  Message: $message"
    fi
    echo "  Expected: '$expected'"
    echo "  Actual:   '$actual'"
    return 1
  fi
  return 0
}

assert_success() {
  local exit_code="$1"
  
  if [[ $exit_code -ne 0 ]]; then
    echo -e "${RED}Command failed with exit code ${exit_code}${NC}"
    return 1
  fi
  return 0
}

assert_failure() {
  local exit_code="$1"
  
  if [[ $exit_code -eq 0 ]]; then
    echo -e "${RED}Command succeeded but should have failed${NC}"
    return 1
  fi
  return 0
}

assert_file_exists() {
  local file_path="$1"
  
  if [[ ! -f "$file_path" ]]; then
    echo -e "${RED}File does not exist: ${file_path}${NC}"
    return 1
  fi
  return 0
}

assert_file_executable() {
  local file_path="$1"
  
  if [[ ! -x "$file_path" ]]; then
    echo -e "${RED}File is not executable: ${file_path}${NC}"
    return 1
  fi
  return 0
}

assert_dir_exists() {
  local dir_path="$1"
  
  if [[ ! -d "$dir_path" ]]; then
    echo -e "${RED}Directory does not exist: ${dir_path}${NC}"
    return 1
  fi
  return 0
}

mock_git_command() {
  local mock_dir="$1"
  
  # Erstelle mock git Befehl
  mkdir -p "${mock_dir}/bin"
  cat > "${mock_dir}/bin/git" << 'EOF'
#!/usr/bin/env bash
if [[ "$1" == "clone" ]]; then
  shift
  local depth_arg=""
  local branch_arg=""
  local repo_url=""
  local target_dir=""
  
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --depth)
        depth_arg="$1"
        shift 2
        ;;
      --branch)
        branch_arg="$1"
        branch_value="$2"
        shift 2
        ;;
      *)
        if [[ -z "$repo_url" ]]; then
          repo_url="$1"
        elif [[ -z "$target_dir" ]]; then
          target_dir="$1"
        fi
        shift
        ;;
    esac
  done
  
  # Erstelle mock Repository-Struktur
  mkdir -p "$target_dir"
  
  # Kopiere aktuelle Projektstruktur (ohne .git)
  cp -r /home/dev/project/* "$target_dir/" 2>/dev/null || true
  
  # Erstelle mock .git Verzeichnis
  mkdir -p "$target_dir/.git"
  echo "ref: refs/heads/main" > "$target_dir/.git/HEAD"
  
  exit 0
fi

# Git Befehle simulieren
case "$1" in
  --version)
    echo "git version 2.34.1"
    ;;
  *)
    echo "Mock git: $*" >&2
    ;;
esac
EOF
  chmod +x "${mock_dir}/bin/git"
}

# --- Tests ---------------------------------------------------------------------

test_help_shows_usage() {
  local output
  output=$("$INSTALL_SCRIPT" --help 2>&1)
  
  if [[ ! "$output" =~ "Nutzung:" ]]; then
    echo "Help output doesn't contain usage information"
    return 1
  fi
  
  if [[ ! "$output" =~ "--install_path" ]]; then
    echo "Help output missing --install_path option"
    return 1
  fi
  
  if [[ ! "$output" =~ "--version" ]]; then
    echo "Help output missing --version option"
    return 1
  fi
  
  if [[ ! "$output" =~ "--force" ]]; then
    echo "Help output missing --force option"
    return 1
  fi
  
  if [[ ! "$output" =~ "--symlinks" ]]; then
    echo "Help output missing --symlinks option"
    return 1
  fi
  
  return 0
}

test_help_exits_successfully() {
  if "$INSTALL_SCRIPT" --help >/dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

test_missing_arguments_show_error() {
  local output
  output=$("$INSTALL_SCRIPT" --install_path 2>&1 || true)
  
  if [[ ! "$output" =~ "benötigt einen Pfad" ]]; then
    echo "Error message for missing --install_path value not found"
    return 1
  fi
  
  return 0
}

test_invalid_argument_shows_error() {
  local output
  output=$("$INSTALL_SCRIPT" --invalid-option 2>&1 || true)
  
  if [[ ! "$output" =~ "Unbekanntes Argument" ]]; then
    echo "Error message for invalid argument not found"
    return 1
  fi
  
  return 0
}

test_dependency_check() {
  # Da curl/wget/tar/grep/awk/sed auf System vorhanden sein sollten,
  # testen wir nur dass das Skript keine Fehler meldet
  if "$INSTALL_SCRIPT" --help >/dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

# --- Desktop-Shortcut-Tests ------------------------------------------------------

# Fixture: Installationsverzeichnis mit start-tui.sh im isolierten HOME
setup_shortcut_env() {
  local test_name="$1"
  local test_dir
  test_dir=$(setup_test_env "$test_name")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts"
  echo "#!/usr/bin/env bash" > "$install_dir/scripts/start-tui.sh"
  chmod +x "$install_dir/scripts/start-tui.sh"
  echo "$home"
}

# Lädt install.sh, ohne main() auszuführen (quittiert mit --help-Exit).
# PLATFORM_OS/is_wsl werden anschließend vom Aufrufer gesetzt.
init_shortcut_functions() {
  local fake_home="$1"
  local platform="$2"

  HOME="$fake_home"
  INSTALL_PATH="${fake_home}/.oc-sandbox"
  PLATFORM_OS="$platform"
}

test_help_shows_shortcut_flag() {
  local output
  output=$("$INSTALL_SCRIPT" --help 2>&1)

  if [[ "$output" != *"--shortcut"* ]]; then
    echo "Help output missing --shortcut option"
    return 1
  fi

  return 0
}

test_shortcut_flag_accepted() {
  # Führe flag parsing direkt aus (source without running main)
  local test_dir
  test_dir=$(setup_test_env "shortcut-flag")
  local home="$test_dir/home"
  mkdir -p "$home"

  if ! (HOME="$home" bash -c "source '$INSTALL_SCRIPT' --help" >/dev/null 2>&1); then
    echo "Source aufgerufen mit --help sollte 0 liefern"
    return 1
  fi
  cleanup_test_env "$test_dir"
}

test_linux_shortcut_creation() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-linux")
  local home
  home=$(setup_shortcut_env "shortcut-linux-2")
  local install_dir="$home/.oc-sandbox"

  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   init_shortcut_functions "$home" "Linux"
   create_shortcut "$install_dir")

  local desktop_file="$home/.local/share/applications/oc-sandbox.desktop"
  assert_file_exists "$desktop_file" || { cleanup_test_env "$test_dir"; return 1; }

  local content
  content=$(cat "$desktop_file")
  if [[ "$content" != *"Exec=${install_dir}/scripts/start-tui.sh"* ]]; then
    echo "Desktop-File Exec zeigt nicht auf start-tui.sh"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if [[ "$content" != *"Terminal=true"* ]]; then
    echo "Desktop-File hat Terminal=true nicht gesetzt"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if [[ "$content" != *"Name=OC Sandbox"* ]]; then
    echo "Desktop-File hat Name=OC Sandbox nicht"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_linux_shortcut_with_icon() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-icon")
  local home
  home=$(setup_shortcut_env "shortcut-icon-2")
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/icons"
  echo "fake-png" > "$install_dir/icons/opencode-sandbox.png"

  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   init_shortcut_functions "$home" "Linux"
   create_shortcut "$install_dir")

  local desktop_file="$home/.local/share/applications/oc-sandbox.desktop"
  assert_file_exists "$desktop_file" || { cleanup_test_env "$test_dir"; return 1; }

  if ! grep -q "^Icon=${install_dir}/icons/opencode-sandbox.png$" "$desktop_file"; then
    echo "Desktop-File referenziert Icon nicht korrekt"
    cat "$desktop_file"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_linux_shortcut_failure_warns_but_install_succeeds() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-fail")
  local home="$test_dir/home"
  mkdir -p "$home/.local/share/applications"

  # Fehlendes start-tui.sh -> Shortcut schlägt fehl, aber create_shortcut
  # liefert trotzdem Exit 0 (warn-and-continue Nemessis: Installation Exit-Code 0)
  local rc=0
  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   init_shortcut_functions "$home" "Linux"
   create_shortcut "$home/.oc-sandbox-no-script") || rc=$?

  assert_equals "0" "$rc" "create_shortcut liefert immer 0 (warn-and-continue)" || { cleanup_test_env "$test_dir"; return 1; }

  cleanup_test_env "$test_dir"
}

test_main_wires_detect_platform_before_shortcut() {
  # Regression: main() rief detect_platform nie auf – PLATFORM_OS blieb leer,
  # create_shortcut matchte in keinem case-Zweig und schlug stumm fehl
  # ("Desktop-Shortcut konnte nicht erstellt werden" ohne Details).
  local test_dir
  test_dir=$(setup_test_env "shortcut-main-wiring")
  local home
  home=$(setup_shortcut_env "shortcut-main-wiring-2")
  local install_dir="$home/.oc-sandbox"

  local rc=0
  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$install_dir"
   VERBOSE=false
   check_dependencies() { return 0; }
   check_disk_space() { return 0; }
   get_latest_release() { echo "v0.0.34"; }
   check_existing_installation() { return 0; }
   install_files() { return 0; }
   set_executable_permissions() { return 0; }
   validate_installation() { return 0; }
   install_gum() { return 0; }
   gum_available() { return 1; }
   main --shortcut >/dev/null 2>&1) || rc=$?

  assert_equals "0" "$rc" "main --shortcut liefert 0" || { cleanup_test_env "$test_dir"; return 1; }

  assert_file_exists "$home/.local/share/applications/oc-sandbox.desktop" || {
    echo "main hat keinen Desktop-Shortcut erstellt (PLATFORM_OS-Verdrahtung fehlt)"
    cleanup_test_env "$test_dir"
    return 1
  }

  cleanup_test_env "$test_dir"
}

test_existing_install_non_tty_fails_clearly() {
  # Regression: ohne TTY (z.B. 'curl ... | bash' ohne Terminal) schlug der
  # read-Prompt still fehl (set -e, Exit 1 ohne Meldung). Jetzt: klare Fehler-
  # meldung mit --force-Hinweis, Exit 2.
  local test_dir
  test_dir=$(setup_test_env "existing-nontty")
  local home="$test_dir/home"
  mkdir -p "$home/.oc-sandbox"

  local output rc=0
  output=$( (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
             HOME="$home"
             INSTALL_PATH="$home/.oc-sandbox"
             FORCE=false
             VERBOSE=false
             check_existing_installation "$INSTALL_PATH") </dev/null 2>&1 ) || rc=$?

  assert_equals "2" "$rc" "Exit 2 ohne TTY bei vorhandener Installation" || { cleanup_test_env "$test_dir"; return 1; }

  if [[ "$output" != *"--force"* ]]; then
    echo "Fehlermeldung ohne --force-Hinweis"
    echo "$output"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_existing_install_non_tty_with_force_proceeds() {
  # --force umgeht den Prompt auch ohne TTY (Update-Pfad für CI/curl|bash)
  local test_dir
  test_dir=$(setup_test_env "existing-force")
  local home="$test_dir/home"
  mkdir -p "$home/.oc-sandbox"

  local rc=0
  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$home/.oc-sandbox"
   FORCE=true
   VERBOSE=false
   PRESERVE_ALLOWLIST=false
   check_existing_installation "$INSTALL_PATH") </dev/null 2>&1 || rc=$?

  assert_equals "0" "$rc" "--force überschreibt ohne Nachfrage" || { cleanup_test_env "$test_dir"; return 1; }

  cleanup_test_env "$test_dir"
}

test_existing_install_tty_confirm_proceeds() {
  # PTY: Bestätigung mit 'y' + allowlist-Erhalt → PRESERVE_ALLOWLIST=true
  if ! command -v python3 >/dev/null 2>&1; then
    echo "SKIP (python3 fehlt)"
    return 0
  fi
  local test_dir
  test_dir=$(setup_test_env "existing-tty")
  local home="$test_dir/home"
  mkdir -p "$home/.oc-sandbox/proxy"
  echo "*" > "$home/.oc-sandbox/proxy/allowlist.txt"

  local result rc=0
  result=$(python3 "$PROJECT_ROOT/tests/run-pty.py" --input 'yy' --submit '' --timeout 10 -- bash -c '
    source "$1" --help >/dev/null 2>&1
    HOME="$2"
    INSTALL_PATH="$2/.oc-sandbox"
    FORCE=false
    VERBOSE=false
    PRESERVE_ALLOWLIST=false
    check_existing_installation "$INSTALL_PATH"
    echo "PRESERVE=$PRESERVE_ALLOWLIST"
  ' _ "$INSTALL_SCRIPT" "$home" 2>&1) || rc=$?

  assert_equals "0" "$rc" "Bestätigung mit 'y' läuft weiter" || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }
  if [[ "$result" != *"PRESERVE=true"* ]]; then
    echo "allowlist-Prompt hat PRESERVE_ALLOWLIST nicht auf true gesetzt"
    echo "$result"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_existing_install_tty_decline_aborts() {
  # PTY: 'n' bricht ab (Exit 2, "Installation abgebrochen.")
  if ! command -v python3 >/dev/null 2>&1; then
    echo "SKIP (python3 fehlt)"
    return 0
  fi
  local test_dir
  test_dir=$(setup_test_env "existing-tty-decline")
  local home="$test_dir/home"
  mkdir -p "$home/.oc-sandbox"

  local result rc=0
  result=$(python3 "$PROJECT_ROOT/tests/run-pty.py" --input 'n' --submit '' --timeout 10 -- bash -c '
    source "$1" --help >/dev/null 2>&1
    HOME="$2"
    INSTALL_PATH="$2/.oc-sandbox"
    FORCE=false
    VERBOSE=false
    check_existing_installation "$INSTALL_PATH"
    echo "KEIN-ABBRUCH"
  ' _ "$INSTALL_SCRIPT" "$home" 2>&1) || rc=$?

  assert_equals "2" "$rc" "'n' bricht Installation mit Exit 2 ab" || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }
  if [[ "$result" == *"KEIN-ABBRUCH"* ]]; then
    echo "'n' hat die Installation nicht abgebrochen"
    echo "$result"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_wsl_shortcut_uses_powershell_shim() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-wsl")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts"
  echo "#!/usr/bin/env bash" > "$install_dir/scripts/start-tui.sh"
  chmod +x "$install_dir/scripts/start-tui.sh"

  local test_bin="$test_dir/bin"
  mkdir -p "$test_bin"

  # Fakes Windows-User-Profil unter einem simulierten /mnt/c
  local mnt_c="$test_dir/mnt-c"
  mkdir -p "$mnt_c/Users/testuser/AppData/Roaming/Microsoft/Windows/Start Menu/Programs"

  # Fake powershell.exe: liefert fake Windows-User und legt die .lnk an
  cat > "$test_bin/powershell.exe" << EOF
#!/usr/bin/bash
echo "POWERSHELL_CALL: \$*" >> "$test_dir/ps-calls"
prev=""
for arg in "\$@"; do
  if [[ "\$prev" == "-Command" ]]; then
    printf '%s\n' "\$arg" >> "$test_dir/ps-commands"
  fi
  prev="\$arg"
done
echo "testuser"
exit 0
EOF
  chmod +x "$test_bin/powershell.exe"

  # Fake wslpath shim: -w liefert Windows-Pfad, sonst Unix-Pfad
  cat > "$test_bin/wslpath" << EOF
#!/usr/bin/bash
if [[ "\${1:-}" == "-w" ]]; then
  echo "C:\\\\Fake"
else
  echo "\$1"
fi
EOF
  chmod +x "$test_bin/wslpath"

  # Fake tr shim (test_bin ist der einzige PATH-Eintrag im Test)
  cat > "$test_bin/tr" << 'EOF'
#!/usr/bin/bash
exec /usr/bin/tr "$@"
EOF
  chmod +x "$test_bin/tr"

  # shims brauchen absolute Shebangs, weil PATH im Test auf test_bin begrenzt ist
  sed -i "1s|.*|#!/usr/bin/bash|" "$test_bin/powershell.exe" "$test_bin/wslpath" "$test_bin/tr" 2>/dev/null || true

  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$install_dir"
   PLATFORM_OS="Linux"
   OC_SANDBOX_WIN_ROOT="$mnt_c"
   is_wsl() { return 0; }
   PATH="$test_bin:/nonexistent-oc-sandbox-test"
   create_shortcut "$install_dir")
  local rc=$?

  assert_equals "0" "$rc" "create_shortcut liefert immer 0" || { cleanup_test_env "$test_dir"; return 1; }

  # Der powershell.exe-Shim wurde aufgerufen und hat die .lnk-Kommandos empfangen
  assert_file_exists "$test_dir/ps-commands" || { cleanup_test_env "$test_dir"; return 1; }
  assert_file_exists "$test_dir/ps-calls" || { cleanup_test_env "$test_dir"; return 1; }

  if ! grep -q "start-tui.sh" "$test_dir/ps-commands"; then
    echo ".lnk-Aufruf referenziert start-tui.sh nicht"
    cat "$test_dir/ps-commands"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if ! grep -q "WScript.Shell" "$test_dir/ps-commands"; then
    echo ".lnk-Aufruf nutzt WScript.Shell COM nicht"
    cat "$test_dir/ps-commands"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if ! grep -q "CreateShortcut" "$test_dir/ps-commands"; then
    echo ".lnk-Aufruf nutzt CreateShortcut nicht"
    cat "$test_dir/ps-commands"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_wsl_missing_mnt_c_leaves_install_exit_0() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-wsl-nointero")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts"
  echo "#!/usr/bin/env bash" > "$install_dir/scripts/start-tui.sh"
  chmod +x "$install_dir/scripts/start-tui.sh"

  # Kein /mnt/c vorhanden und kein powershell.exe -> nur Warnung, Exit 0
  local rc=0
  local output
  output=$((source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$install_dir"
   PLATFORM_OS="Linux"
   is_wsl() { return 0; }
   PATH="/nonexistent-oc-sandbox-test"
   create_shortcut "$install_dir") 2>&1) || rc=$?

  assert_equals "0" "$rc" "fehlende WSL-Interop bricht Installation nicht" || { cleanup_test_env "$test_dir"; return 1; }
  if [[ "$output" != *"/mnt/c"* && "$output" != *"powershell.exe"* ]]; then
    echo "Fehlermeldung nennt Ursache nicht"
    echo "$output"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_macos_shortcut_shim() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-macos")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts"
  echo "#!/usr/bin/env bash" > "$install_dir/scripts/start-tui.sh"
  chmod +x "$install_dir/scripts/start-tui.sh"

  local test_bin="$test_dir/bin"
  mkdir -p "$test_bin"

  # Fake osascript: emuliert App-Bundle-Erstellung (wie echtes macOS)
  cat > "$test_bin/osascript" << EOF
#!/usr/bin/env bash
echo "OSASCRIPT_CALL: \$*" >> "$test_dir/osascript-calls"
exit 0
EOF
  chmod +x "$test_bin/osascript"

  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$install_dir"
   PLATFORM_OS="Darwin"
   PATH="$test_bin:$PATH"
   create_shortcut "$install_dir")
  local rc=$?

  assert_equals "0" "$rc" "create_shortcut liefert immer 0" || { cleanup_test_env "$test_dir"; return 1; }

  # App-Bundle-Struktur vorhanden?
  assert_dir_exists "$home/Applications/OC Sandbox.app/Contents/MacOS" || { cleanup_test_env "$test_dir"; return 1; }
  assert_file_exists "$home/Applications/OC Sandbox.app/Contents/Info.plist" || { cleanup_test_env "$test_dir"; return 1; }
  assert_file_exists "$home/Applications/OC Sandbox.app/Contents/MacOS/OC Sandbox" || { cleanup_test_env "$test_dir"; return 1; }

  if ! grep -q "start-tui.sh" "$home/Applications/OC Sandbox.app/Contents/MacOS/OC Sandbox"; then
    echo "App-Stub referenziert start-tui.sh nicht"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if ! grep -q "io.github.oc-sandbox.tui" "$home/Applications/OC Sandbox.app/Contents/Info.plist"; then
    echo "Info.plist hat CFBundleIdentifier nicht"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_macos_shortcut_without_osascript_warns() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-macos-nosc")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts"
  echo "#!/usr/bin/env bash" > "$install_dir/scripts/start-tui.sh"
  chmod +x "$install_dir/scripts/start-tui.sh"

  local rc=0
  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$install_dir"
   PLATFORM_OS="Darwin"
   PATH="/nonexistent-oc-sandbox-test"
   create_shortcut "$install_dir") >/dev/null 2>&1 || rc=$?

  assert_equals "0" "$rc" "fehlendes osascript bricht Installation nicht" || { cleanup_test_env "$test_dir"; return 1; }

  cleanup_test_env "$test_dir"
}

# --- Main Test Runner ----------------------------------------------------------

main() {
  echo "========================================="
  echo "Install Script Tests"
  echo "========================================="
  echo ""
  
  # Hilfe Tests
  echo "Running help tests..."
  run_test "Help shows usage" test_help_shows_usage
  run_test "Help exits successfully" test_help_exits_successfully
  
  # Argument Parsing Tests
  echo ""
  echo "Running argument parsing tests..."
  run_test "Missing arguments show error" test_missing_arguments_show_error
  run_test "Invalid argument shows error" test_invalid_argument_shows_error
  
  # Dependency Tests
  echo ""
  echo "Running dependency tests..."
  run_test "Dependency check passes" test_dependency_check
  
  # Desktop-Shortcut-Tests
  echo ""
  echo "Running desktop shortcut tests..."
  run_test "Help shows --shortcut" test_help_shows_shortcut_flag
  run_test "Linux: .desktop-Datei erstellt" test_linux_shortcut_creation
  run_test "Linux: .desktop-Datei mit Icon" test_linux_shortcut_with_icon
  run_test "Linux: fehlgeschlagener Shortcut bricht Installation nicht ab" test_linux_shortcut_failure_warns_but_install_succeeds
  run_test "main verdrahtet detect_platform vor create_shortcut" test_main_wires_detect_platform_before_shortcut
  run_test "Vorhandene Installation ohne TTY: klare Fehlermeldung" test_existing_install_non_tty_fails_clearly
  run_test "Vorhandene Installation mit --force ohne TTY: läuft weiter" test_existing_install_non_tty_with_force_proceeds
  run_test "Vorhandene Installation im TTY: 'y' bestätigt + allowlist erhalten" test_existing_install_tty_confirm_proceeds
  run_test "Vorhandene Installation im TTY: 'n' bricht ab" test_existing_install_tty_decline_aborts
  run_test "WSL: .lnk via powershell.exe (Shim)" test_wsl_shortcut_uses_powershell_shim
  run_test "WSL: fehlende Interop warnt nur" test_wsl_missing_mnt_c_leaves_install_exit_0
  run_test "macOS: .app-Bundle erstellt (Shim)" test_macos_shortcut_shim
  run_test "macOS: fehlendes osascript warnt nur" test_macos_shortcut_without_osascript_warns
  
  # Zusammenfassung
  echo ""
  echo "========================================="
  echo "Results:"
  echo "  Total:  $TESTS_RUN"
  echo -e "  ${GREEN}Passed: $TESTS_PASSED${NC}"
  if [[ $TESTS_FAILED -gt 0 ]]; then
    echo -e "  ${RED}Failed: $TESTS_FAILED${NC}"
  else
    echo "  Failed: $TESTS_FAILED"
  fi
  echo "========================================="
  
  # Aufräumen
  rm -rf "$TEMP_BASE"
  
  if [[ $TESTS_FAILED -gt 0 ]]; then
    exit 1
  fi
}

# Start
main "$@"
