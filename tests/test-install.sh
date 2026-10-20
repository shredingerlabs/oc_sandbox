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

# Fixture: Installationsverzeichnis mit plattform-Binary
# im isolierten HOME (Go-Binary ist seit dem Parity-Gate-Flip Default)
setup_shortcut_env() {
  local test_name="$1"
  local test_dir
  test_dir=$(setup_test_env "$test_name")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts" "$install_dir/bin"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox_linux_amd64"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox_linux_amd64" "$install_dir/bin/oc-sandbox"
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

# --- Convenience-Artifact Opt-out-Tests ------------------------------------------

test_help_shows_no_symlinks_flag() {
  local output
  output=$("$INSTALL_SCRIPT" --help 2>&1)

  if [[ "$output" != *"--no_symlinks"* ]]; then
    echo "Help output missing --no_symlinks option"
    return 1
  fi

  return 0
}

test_help_shows_no_shortcut_flag() {
  local output
  output=$("$INSTALL_SCRIPT" --help 2>&1)

  if [[ "$output" != *"--no_shortcut"* ]]; then
    echo "Help output missing --no_shortcut option"
    return 1
  fi

  return 0
}

test_defaults_create_convenience_artifacts() {
  # Symlink + Shortcut sind Default-On; Opt-out nur über --no_*-Flags
  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    echo "SYMLINKS=$SYMLINKS SHORTCUT=$SHORTCUT"
  )

  assert_equals "SYMLINKS=true SHORTCUT=true" "$result" "Default: Symlink + Shortcut aktiv" || return 1
}

test_no_flags_deselect_artifacts() {
  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    parse_arguments --no_symlinks --no_shortcut
    echo "SYMLINKS=$SYMLINKS SHORTCUT=$SHORTCUT"
  )

  assert_equals "SYMLINKS=false SHORTCUT=false" "$result" "--no_* Flags wählen Default-Artifacts ab" || return 1
}

test_symlinks_shortcut_flags_are_noops() {
  # Alte Opt-in-Flags bleiben akzeptiert (Kompatibilität), aber ohne Wirkung
  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    parse_arguments --symlinks --shortcut
    echo "SYMLINKS=$SYMLINKS SHORTCUT=$SHORTCUT"
  )

  assert_equals "SYMLINKS=true SHORTCUT=true" "$result" "--symlinks/--shortcut ohne Wirkung" || return 1
}

test_symlink_failure_warns_but_continues() {
  # Unbeschreibbares bin-Verzeichnis: Symlink schlägt fehl, Installation läuft
  # weiter (Fail-Soft wie der Shortcut, warn-and-continue)
  local test_dir
  test_dir=$(setup_binary_env "symlink-fail-soft")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox"
  mkdir -p "$home/.local/bin"
  chmod 0555 "$home/.local/bin"

  local output rc=0
  output=$( (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   PLATFORM_OS="Linux"
   create_symlinks "$install_dir") 2>&1 ) || rc=$?

  chmod 0755 "$home/.local/bin"

  assert_equals "0" "$rc" "Symlink-Fehler bricht nicht ab (warn-and-continue)" || { cleanup_test_env "$test_dir"; return 1; }

  if [[ -L "$home/.local/bin/oc-sandbox" ]]; then
    echo "Symlink wurde trotz unwahrscheinlichem Fehlerfall erstellt"
    cleanup_test_env "$test_dir"
    return 1
  fi

  if [[ "$output" != *"Symlink-Verzeichnis nicht beschreibbar"* ]]; then
    echo "Warnung über unbeschreibbares Verzeichnis fehlt"
    echo "$output"
    cleanup_test_env "$test_dir"
    return 1
  fi

  if [[ "$output" != *"ln -s ${install_dir}/bin/oc-sandbox ${home}/.local/bin/oc-sandbox"* ]]; then
    echo "Manuelle ln -s-Lösung fehlt in der Warnmeldung"
    echo "$output"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
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

# --- Go-Binary-Entry-Point Tests ---------------------------------------------------

# Fixture: Installationsverzeichnis mit plattform-Binary
setup_binary_env() {
  local test_name="$1"
  local test_dir
  test_dir=$(setup_test_env "$test_name")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts" "$install_dir/bin"
  # Plattform passendes Fake-Binary (Linux/x86_64 -> oc-sandbox_linux_amd64)
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox_linux_amd64"
  # Fremdplattform-Binary, darf nie gewählt werden
  printf '#!/usr/bin/env bash\necho WRONG\n' > "$install_dir/bin/oc-sandbox_darwin_arm64"
  chmod +x "$install_dir"/bin/*
  echo "$test_dir"
}

test_help_shows_bin_flag() {
  local output
  output=$("$INSTALL_SCRIPT" --help 2>&1)

  if [[ "$output" != *"--bin"* ]]; then
    echo "Help output missing --bin option"
    return 1
  fi

  return 0
}

test_bin_flag_accepted() {
  # --bin ist ohne Wirkung (Go-Binary ist Default), wird aber akzeptiert
  local test_dir
  test_dir=$(setup_test_env "bin-flag-parse")
  local home="$test_dir/home"
  mkdir -p "$home"

  local rc=0
  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   parse_arguments --bin) || rc=$?

  assert_equals "0" "$rc" "--bin wird ohne Wirkung akzeptiert" || { cleanup_test_env "$test_dir"; return 1; }
  cleanup_test_env "$test_dir"
}

test_bash_tui_flag_rejected() {
  # Die Bash-TUI wurde mit dem Cleanup (#99) gelöscht; ihr Flag ist unbekannt
  local test_dir
  test_dir=$(setup_test_env "bash-tui-flag-parse")
  local home="$test_dir/home"
  mkdir -p "$home"

  local rc=0
  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   parse_arguments --bash-tui) >/dev/null 2>&1 || rc=$?

  assert_equals "1" "$rc" "--bash-tui ist ein Unbekanntes Argument (Exit 1)" || { cleanup_test_env "$test_dir"; return 1; }
  cleanup_test_env "$test_dir"
}

test_default_entry_point_is_binary() {
  # Parity-Gate-Flip (#98): Go-Binary ist Default, ohne Flag
  local test_dir
  test_dir=$(setup_test_env "default-binary")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"

  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    entry_point_path "$install_dir"
  )

  assert_equals "${install_dir}/bin/oc-sandbox" "$result" "Go-Binary ist Default-Entry-Point" || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }
  cleanup_test_env "$test_dir"
}

test_install_dir_binary_uses_go_naming() {
  local test_dir
  test_dir=$(setup_test_env "bin-naming")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir"

  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    PLATFORM_OS="Linux"
    PLATFORM_ARCH="x86_64"
    install_dir_binary "$install_dir"
  )

  if [[ "$result" != "${install_dir}/bin/oc-sandbox_linux_amd64" ]]; then
    echo "install_dir_binary liefert: $result"
    cleanup_test_env "$test_dir"
    return 1
  fi
  cleanup_test_env "$test_dir"
}

test_extract_go_binary_copies_platform_match() {
  local test_dir
  test_dir=$(setup_binary_env "bin-extract")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"

  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    PLATFORM_OS="Linux"
    PLATFORM_ARCH="x86_64"
    extract_go_binary "$install_dir"
    "$install_dir/bin/oc-sandbox"
  ) || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }

  if [[ "$result" != "oc-sandbox-fake" ]]; then
    echo "Falsches Binary kopiert: $result"
    cleanup_test_env "$test_dir"
    return 1
  fi
  cleanup_test_env "$test_dir"
}

test_extract_go_binary_missing_fails_clearly() {
  local test_dir
  test_dir=$(setup_test_env "bin-extract-missing")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/scripts"

  local result rc=0
  result=$({
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    PLATFORM_OS="Darwin"
    PLATFORM_ARCH="arm64"
    extract_go_binary "$install_dir"
    echo "KEIN-ABBRUCH"
  } 2>&1) || rc=$?

  assert_equals "1" "$rc" "Fehlendes release-bin/-Verzeichnis bricht die Installation ab" || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }
  if [[ "$result" == *"KEIN-ABBRUCH"* ]]; then
    echo "Fehlendes Binary hat die Installation nicht abgebrochen"
    echo "$result"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if [[ "$result" != *"Kein Go-Binary im Release"* ]]; then
    echo "Fehlermeldung fehlt: Kein Go-Binary im Release"
    echo "$result"
    cleanup_test_env "$test_dir"
    return 1
  fi
  cleanup_test_env "$test_dir"
}

test_bin_symlink_points_at_binary() {
  local test_dir
  test_dir=$(setup_binary_env "bin-symlink")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"

  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    PLATFORM_OS="Linux"
    PLATFORM_ARCH="x86_64"
    HOME="$home"
    extract_go_binary "$install_dir"
    create_symlinks "$install_dir"
    readlink "$home/.local/bin/oc-sandbox"
  ) || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }

  if [[ "$result" != "${install_dir}/bin/oc-sandbox" ]]; then
    echo "Symlink zeigt nicht auf das Binary: $result"
    cleanup_test_env "$test_dir"
    return 1
  fi
  cleanup_test_env "$test_dir"
}

test_bin_shortcut_points_at_binary() {
  local test_dir
  test_dir=$(setup_binary_env "bin-shortcut")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"

  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    init_shortcut_functions "$home" "Linux"
    PLATFORM_ARCH="x86_64"
    extract_go_binary "$install_dir"
    create_shortcut "$install_dir"
    cat "$home/.local/share/applications/oc-sandbox.desktop"
  ) || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }

  if [[ "$result" != *"Exec=${install_dir}/bin/oc-sandbox"* ]]; then
    echo "Desktop-File Exec zeigt nicht auf das Binary"
    echo "$result"
    cleanup_test_env "$test_dir"
    return 1
  fi
  cleanup_test_env "$test_dir"
}

test_default_symlink_points_at_binary() {
  # Parity-Gate-Flip (#98): Default-Symlink zeigt auf das Go-Binary
  local test_dir
  test_dir=$(setup_binary_env "default-entry-binary")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"

  local result
  result=$(
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    PLATFORM_OS="Linux"
    PLATFORM_ARCH="x86_64"
    HOME="$home"
    extract_go_binary "$install_dir"
    create_symlinks "$install_dir"
    readlink "$home/.local/bin/oc-sandbox"
  ) || { echo "$result"; cleanup_test_env "$test_dir"; return 1; }

  if [[ "$result" != "${install_dir}/bin/oc-sandbox" ]]; then
    echo "Default-Symlink zeigt nicht auf das Go-Binary: $result"
    cleanup_test_env "$test_dir"
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
  if [[ "$content" != *"Exec=${install_dir}/bin/oc-sandbox"* ]]; then
    echo "Desktop-File Exec zeigt nicht auf das Go-Binary"
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
  mkdir -p "$install_dir/icons/linux/share/icons/hicolor/48x48/apps"
  echo "fake-png" > "$install_dir/icons/linux/share/icons/hicolor/48x48/apps/oc-sandbox.png"

  (source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   init_shortcut_functions "$home" "Linux"
   create_shortcut "$install_dir")

  local desktop_file="$home/.local/share/applications/oc-sandbox.desktop"
  assert_file_exists "$desktop_file" || { cleanup_test_env "$test_dir"; return 1; }

  if ! grep -q "^Icon=oc-sandbox$" "$desktop_file"; then
    echo "Desktop-File referenziert Icon nicht per Theme-Namen"
    cat "$desktop_file"
    cleanup_test_env "$test_dir"
    return 1
  fi

  if [[ ! -f "$home/.local/share/icons/hicolor/48x48/apps/oc-sandbox.png" ]]; then
    echo "Hicolor-Icon wurde nicht nach ~/.local/share/icons installiert"
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

  # Fehlender Entry-Point -> Shortcut schlägt fehl, aber create_shortcut
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
  mkdir -p "$install_dir/bin"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox"
  mkdir -p "$install_dir/icons/windows"
  touch "$install_dir/icons/windows/oc-sandbox.ico"

  local test_bin="$test_dir/bin"
  mkdir -p "$test_bin"

  # Fakes Windows-User-Profil unter einem simulierten /mnt/c
  local mnt_c="$test_dir/mnt-c"
  mkdir -p "$mnt_c/Users/testuser/AppData/Roaming/Microsoft/Windows/Start Menu/Programs"
  # Fake %LOCALAPPDATA% (das Icon wird vom Install-Script hierher kopiert)
  mkdir -p "$mnt_c/Users/testuser/AppData/Local"

  # Fake powershell.exe: liefert fake Windows-User und legt die .lnk an
  cat > "$test_bin/powershell.exe" << EOF
#!/usr/bin/bash
echo "POWERSHELL_CALL: \$*" >> "$test_dir/ps-calls"
prev=""
for arg in "\$@"; do
  if [[ "\$prev" == "-Command" ]]; then
    printf '%s\n' "\$arg" >> "$test_dir/ps-commands"
    case "\$arg" in
      *'LocalApplicationData'*)
        echo 'C:\\Users\\testuser\\AppData\\Local'
        exit 0
        ;;
    esac
  fi
  prev="\$arg"
done
echo "testuser"
exit 0
EOF
  chmod +x "$test_bin/powershell.exe"

  # Fake wslpath shim: -w liefert Windows-Pfad, -u übersetzt ins Sandbox-/mnt-c
  cat > "$test_bin/wslpath" << EOF
#!/usr/bin/bash
if [[ "\${1:-}" == "-w" ]]; then
  echo "C:\\\\Fake"
elif [[ "\${1:-}" == "-u" ]]; then
  p="\${2//\\\\//}"
  echo "$test_dir/mnt-c\${p#C:}"
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

  # Fake mkdir/cp shims (ebenfalls nicht im Test-PATH)
  cat > "$test_bin/mkdir" << 'EOF'
#!/usr/bin/bash
exec /usr/bin/mkdir "$@"
EOF
  chmod +x "$test_bin/mkdir"
  cat > "$test_bin/cp" << 'EOF'
#!/usr/bin/bash
exec /usr/bin/cp "$@"
EOF
  chmod +x "$test_bin/cp"

  # shims brauchen absolute Shebangs, weil PATH im Test auf test_bin begrenzt ist
  sed -i "1s|.*|#!/usr/bin/bash|" "$test_bin/powershell.exe" "$test_bin/wslpath" "$test_bin/tr" "$test_bin/mkdir" "$test_bin/cp" 2>/dev/null || true

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

  if ! grep -qF "${install_dir}/bin/oc-sandbox" "$test_dir/ps-commands"; then
    echo ".lnk-Aufruf referenziert das Go-Binary nicht"
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
  if ! grep -qF "\$sc.IconLocation = 'C:\\Users\\testuser\\AppData\\Local\\oc-sandbox\\oc-sandbox.ico'" "$test_dir/ps-commands"; then
    echo ".lnk referenziert das Icon nicht über den Windows-nativen Pfad (%LOCALAPPDATA%) - Startmenu kann UNC-Icons nicht rendern"
    cat "$test_dir/ps-commands"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if [[ ! -f "$mnt_c/Users/testuser/AppData/Local/oc-sandbox/oc-sandbox.ico" ]]; then
    echo ".ico wurde nicht nach %LOCALAPPDATA%\\oc-sandbox kopiert"
    cleanup_test_env "$test_dir"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_wsl_shortcut_resolves_startmenu_via_powershell() {
  local test_dir
  test_dir=$(setup_test_env "shortcut-wsl-relocated")
  local home="$test_dir/home"
  local install_dir="$home/.oc-sandbox"
  mkdir -p "$install_dir/bin"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox"

  local test_bin="$test_dir/bin"
  mkdir -p "$test_bin"

  # Fake powershell.exe: USERNAME passt NICHT zum Profilordner (reloziert/umbenannt),
  # das echte Startmenu-Verzeichnis liefert GetFolderPath
  cat > "$test_bin/powershell.exe" << EOF
#!/bin/bash
for arg in "\$@"; do
  case "\$arg" in
    *'env:USERNAME'*)
      echo "testuser"
      exit 0
      ;;
    *'GetFolderPath'*)
      echo 'C:\\Users\\realuser\\AppData\\Roaming\\Microsoft\\Windows\\Start Menu'
      exit 0
      ;;
  esac
done
prev=""
for arg in "\$@"; do
  if [[ "\$prev" == "-Command" ]]; then
    printf '%s\n' "\$arg" >> "$test_dir/ps-commands"
  fi
  prev="\$arg"
done
exit 0
EOF
  chmod +x "$test_bin/powershell.exe"

  cat > "$test_bin/wslpath" << EOF
#!/bin/bash
if [[ "\${1:-}" == "-w" ]]; then
  echo "C:\\\\Fake"
else
  echo "\$1"
fi
EOF
  chmod +x "$test_bin/wslpath"

  cat > "$test_bin/tr" << 'EOF'
#!/usr/bin/bash
exec /usr/bin/tr "$@"
EOF
  chmod +x "$test_bin/tr"
  sed -i "1s|.*|#!/usr/bin/bash|" "$test_bin/powershell.exe" "$test_bin/wslpath" "$test_bin/tr" 2>/dev/null || true

  # Startmenu existiert nur unter realuser, nicht unter testuser
  local mnt_c="$test_dir/mnt-c"
  mkdir -p "$mnt_c/Users/realuser/AppData/Roaming/Microsoft/Windows/Start Menu/Programs"

  local rc=0
  local output
  output=$((source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
   HOME="$home"
   INSTALL_PATH="$install_dir"
   PLATFORM_OS="Linux"
   OC_SANDBOX_WIN_ROOT="$mnt_c"
   is_wsl() { return 0; }
   PATH="$test_bin:/nonexistent-oc-sandbox-test"
   create_shortcut "$install_dir") 2>&1) || rc=$?

  assert_equals "0" "$rc" "create_shortcut liefert immer 0" || { cleanup_test_env "$test_dir"; return 1; }

  if [[ "$output" == *"übersprungen"* ]]; then
    echo "Shortcut wurde trotz reloziertem Profil übersprungen"
    echo "$output"
    cleanup_test_env "$test_dir"
    return 1
  fi

  # .lnk wurde via powershell.exe/WScript.Shell erstellt
  assert_file_exists "$test_dir/ps-commands" || { cleanup_test_env "$test_dir"; return 1; }
  if ! grep -q "CreateShortcut" "$test_dir/ps-commands"; then
    echo "Kein CreateShortcut-Aufruf: Startmenu-Pfad kommt nicht aus PowerShell"
    cat "$test_dir/ps-commands"
    cleanup_test_env "$test_dir"
    return 1
  fi
  if ! grep -qF "${install_dir}/bin/oc-sandbox" "$test_dir/ps-commands"; then
    echo ".lnk-Aufruf referenziert das Go-Binary nicht"
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
  mkdir -p "$install_dir/bin"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox"

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
  mkdir -p "$install_dir/bin"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox"

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

  if ! grep -qF "${install_dir}/bin/oc-sandbox" "$home/Applications/OC Sandbox.app/Contents/MacOS/OC Sandbox"; then
    echo "App-Stub referenziert das Go-Binary nicht"
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
  mkdir -p "$install_dir/bin"
  printf '#!/usr/bin/env bash\necho oc-sandbox-fake\n' > "$install_dir/bin/oc-sandbox"
  chmod +x "$install_dir/bin/oc-sandbox"

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

# --- Custom-Edition-Dockerfile-Erhalt (Dist/Dockerfile.custom) ---------------

# Fake-Quellverzeichnis mit Template-Datei bauen (wie dist/ im Repo)
setup_custom_dockerfile_source() {
  local src_dir="$1"
  mkdir -p "$src_dir/dist"
  cp "$PROJECT_ROOT/dist/Dockerfile" "$src_dir/dist/Dockerfile"
  cp "$PROJECT_ROOT/dist/Dockerfile.custom" "$src_dir/dist/Dockerfile.custom"
}

run_install_files() {
  local source_dir="$1"
  local target_dir="$2"
  (
    source "$INSTALL_SCRIPT" --help >/dev/null 2>&1
    VERBOSE=false
    PRESERVE_ALLOWLIST=true
    install_files "$source_dir" "$target_dir"
  )
}

test_custom_dockerfile_fresh_install_lands() {
  local test_dir
  test_dir=$(setup_test_env "custom-dockerfile-fresh")
  local src="$test_dir/src"
  local target="$test_dir/target"

  setup_custom_dockerfile_source "$src"

  local rc=0
  run_install_files "$src" "$target" || rc=$?

  assert_equals "0" "$rc" "install_files mit leerem Ziel" || { cleanup_test_env "$test_dir"; return 1; }
  assert_file_exists "$target/dist/Dockerfile.custom" || { cleanup_test_env "$test_dir"; return 1; }
  cmp -s "$PROJECT_ROOT/dist/Dockerfile.custom" "$target/dist/Dockerfile.custom" || {
    echo "Template unterscheidet sich nach Fresh-Install"
    cleanup_test_env "$test_dir"
    return 1
  }
  cmp -s "$PROJECT_ROOT/dist/Dockerfile" "$target/dist/Dockerfile" || {
    echo "dist/Dockerfile abweichend nach Fresh-Install"
    cleanup_test_env "$test_dir"
    return 1
  }
  cleanup_test_env "$test_dir"
}

test_custom_dockerfile_update_preserves_edited_file() {
  local test_dir
  test_dir=$(setup_test_env "custom-dockerfile-preserve")
  local src="$test_dir/src"
  local target="$test_dir/target"

  setup_custom_dockerfile_source "$src"
  run_install_files "$src" "$target" >/dev/null 2>&1

  # User editiert die Datei (eigene Edition ergänzt)
  echo "FROM opencode-sandbox-base AS opencode-sandbox-mytool" >> "$target/dist/Dockerfile.custom"
  cp "$target/dist/Dockerfile.custom" "$test_dir/expected"

  local rc=0
  run_install_files "$src" "$target" || rc=$?

  assert_equals "0" "$rc" "install_files bei Update mit editierter Datei" || { cleanup_test_env "$test_dir"; return 1; }
  cmp -s "$test_dir/expected" "$target/dist/Dockerfile.custom" || {
    echo "Editierte Dockerfile.custom wurde überschrieben (nicht erhalten)"
    cleanup_test_env "$test_dir"
    return 1
  }
  cmp -s "$PROJECT_ROOT/dist/Dockerfile" "$target/dist/Dockerfile" || {
    echo "dist/Dockerfile muss weiterhin aktualisiert werden"
    cleanup_test_env "$test_dir"
    return 1
  }
  cleanup_test_env "$test_dir"
}

test_custom_dockerfile_recreated_if_absent() {
  local test_dir
  test_dir=$(setup_test_env "custom-dockerfile-absent")
  local src="$test_dir/src"
  local target="$test_dir/target"

  setup_custom_dockerfile_source "$src"
  run_install_files "$src" "$target" >/dev/null 2>&1
  rm "$target/dist/Dockerfile.custom"

  local rc=0
  run_install_files "$src" "$target" || rc=$?

  assert_equals "0" "$rc" "install_files nach Löschen der Datei" || { cleanup_test_env "$test_dir"; return 1; }
  assert_file_exists "$target/dist/Dockerfile.custom" || { cleanup_test_env "$test_dir"; return 1; }
  cmp -s "$PROJECT_ROOT/dist/Dockerfile.custom" "$target/dist/Dockerfile.custom" || {
    echo "Fehlende Datei wurde nicht aus Template neu angelegt"
    cleanup_test_env "$test_dir"
    return 1
  }
  cleanup_test_env "$test_dir"
}



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
  run_test "Help shows --no_symlinks" test_help_shows_no_symlinks_flag
  run_test "Help shows --no_shortcut" test_help_shows_no_shortcut_flag
  run_test "Default: Symlink + Shortcut aktiv" test_defaults_create_convenience_artifacts
  run_test "--no_* Flags melden Default ab" test_no_flags_deselect_artifacts
  run_test "--symlinks/--shortcut ohne Wirkung" test_symlinks_shortcut_flags_are_noops
  run_test "Symlink-Fehler warnt nur (Fail-Soft)" test_symlink_failure_warns_but_continues
  run_test "Help shows --bin" test_help_shows_bin_flag
  run_test "--bin-Flag akzeptiert" test_bin_flag_accepted
  run_test "--bash-tui-Flag abgelehnt" test_bash_tui_flag_rejected
  run_test "Default: Go-Binary ist Entry-Point" test_default_entry_point_is_binary
  run_test "install_dir_binary nutzt Go-Naming" test_install_dir_binary_uses_go_naming
  run_test "--bin: Plattform-Binary extrahiert" test_extract_go_binary_copies_platform_match
  run_test "--bin: fehlendes Binary bricht klar ab" test_extract_go_binary_missing_fails_clearly
  run_test "--bin: Symlink zeigt auf Binary" test_bin_symlink_points_at_binary
  run_test "--bin: Shortcut zeigt auf Binary" test_bin_shortcut_points_at_binary
  run_test "Default: Symlink zeigt auf Go-Binary" test_default_symlink_points_at_binary
  run_test "Linux: .desktop-Datei erstellt" test_linux_shortcut_creation
  run_test "Linux: .desktop-Datei mit Icon" test_linux_shortcut_with_icon
  run_test "Linux: fehlgeschlagener Shortcut bricht Installation nicht ab" test_linux_shortcut_failure_warns_but_install_succeeds
  run_test "main verdrahtet detect_platform vor create_shortcut" test_main_wires_detect_platform_before_shortcut
  run_test "Vorhandene Installation ohne TTY: klare Fehlermeldung" test_existing_install_non_tty_fails_clearly
  run_test "Vorhandene Installation mit --force ohne TTY: läuft weiter" test_existing_install_non_tty_with_force_proceeds
  run_test "Vorhandene Installation im TTY: 'y' bestätigt + allowlist erhalten" test_existing_install_tty_confirm_proceeds
  run_test "Vorhandene Installation im TTY: 'n' bricht ab" test_existing_install_tty_decline_aborts
  run_test "WSL: .lnk via powershell.exe (Shim)" test_wsl_shortcut_uses_powershell_shim
  run_test "WSL: Startmenu-Pfad via PowerShell aufgelöst" test_wsl_shortcut_resolves_startmenu_via_powershell
  run_test "WSL: fehlende Interop warnt nur" test_wsl_missing_mnt_c_leaves_install_exit_0
  run_test "macOS: .app-Bundle erstellt (Shim)" test_macos_shortcut_shim
  run_test "macOS: fehlendes osascript warnt nur" test_macos_shortcut_without_osascript_warns

  # Custom-Edition-Dockerfile-Erhalt
  echo ""
  echo "Running custom edition Dockerfile preservation tests..."
  run_test "Fresh install: dist/Dockerfile.custom landet" test_custom_dockerfile_fresh_install_lands
  run_test "Update: editierte dist/Dockerfile.custom bleibt erhalten" test_custom_dockerfile_update_preserves_edited_file
  run_test "Update ohne Datei: wird erneut angelegt" test_custom_dockerfile_recreated_if_absent

  
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
