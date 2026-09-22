#!/usr/bin/env bash
#
# Tests für uninstall.sh Script
# Testet externes Verhalten (Exit-Codes, Dateisystem-Änderungen)
#

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$TEST_DIR/.." && pwd)"
UNINSTALL_SCRIPT="${PROJECT_ROOT}/dist/scripts/uninstall.sh"

TEMP_BASE="${TMPDIR:-/tmp}/uninstall-tests-$$"
mkdir -p "$TEMP_BASE"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

RED='\033[0;31m'
GREEN='\033[0;32m'
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
    if [[ -n "$message" ]]; then
      echo "  Assertion fehlgeschlagen: $message"
    fi
    echo "  Erwartet: '$expected', erhalten: '$actual'"
    return 1
  fi
}

assert_dir_exists() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    echo "  Verzeichnis existiert nicht: $dir"
    return 1
  fi
}

assert_file_exists() {
  local file="$1"
  if [[ ! -f "$file" ]]; then
    echo "  Datei existiert nicht: $file"
    return 1
  fi
}

assert_dir_not_exists() {
  local dir="$1"
  if [[ -d "$dir" ]]; then
    echo "  Verzeichnis existiert: $dir"
    return 1
  fi
}

# Fixture: simuliert eine Installation + Config in isoliertem HOME
setup_installed_home() {
  local test_dir="$1"
  local home="$test_dir/home"
  local install_path="$home/.oc-sandbox"

  mkdir -p "$install_path" "$home/.config/oc-sandbox" "$home/.local/bin"
  echo "fake" > "$install_path/some-file.txt"
  echo '{}' > "$home/.config/oc-sandbox/projects.json"

  # Ein Symlink, der auf die Installation zeigt
  ln -s "$install_path/uninstall.sh" "$home/.local/bin/uninstall.sh"

  echo "$home"
}

run_uninstall() {
  local home="$1"
  shift
  HOME="$home" bash "$UNINSTALL_SCRIPT" --force "$@" >/dev/null 2>&1
}

run_uninstall_capture() {
  local home="$1"
  shift
  HOME="$home" bash "$UNINSTALL_SCRIPT" --force "$@" 2>&1
}

# --- Tests ---------------------------------------------------------------------

test_help_shows_new_flags() {
  local test_dir
  test_dir=$(setup_test_env "help")
  local home
  home=$(setup_installed_home "$test_dir")

  local output
  output=$(HOME="$home" bash "$UNINSTALL_SCRIPT" --help 2>&1)

  assert_equals "0" "$?" "exit code" || return 1

  if [[ "$output" != *"--remove-config"* ]]; then
    echo "  --remove-config fehlt in Hilfe"
    return 1
  fi
  if [[ "$output" != *"--no-backup"* ]]; then
    echo "  --no-backup fehlt in Hilfe"
    return 1
  fi
  if [[ "$output" != *"--no-symlinks"* ]]; then
    echo "  --no-symlinks fehlt in Hilfe"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_default_keeps_config_and_removes_symlinks() {
  local test_dir
  test_dir=$(setup_test_env "default")
  local home
  home=$(setup_installed_home "$test_dir")

  run_uninstall "$home"

  assert_dir_not_exists "$home/.oc-sandbox" || return 1
  assert_dir_exists "$home/.config/oc-sandbox" "Config soll unberührt bleiben" || return 1
  if [[ -L "$home/.local/bin/uninstall.sh" ]]; then
    echo "  Symlink sollte entfernt sein"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_remove_config_removes_dir_and_creates_backup() {
  local test_dir
  test_dir=$(setup_test_env "remove-config")
  local home
  home=$(setup_installed_home "$test_dir")

  run_uninstall "$home" --remove-config

  assert_dir_not_exists "$home/.oc-sandbox" || return 1
  assert_dir_not_exists "$home/.config/oc-sandbox/projects.json" "Config soll entfernt werden" || return 1
  assert_dir_exists "$home/.config/oc-sandbox/backups" "Backup soll erhalten bleiben" || return 1

  # Backup-Verzeichnis mit Timestamp-Schema
  local backup_count
  backup_count=$(find "$home/.config/oc-sandbox/backups" -maxdepth 1 -type d -name "config-*" | wc -l)
  assert_equals "1" "$backup_count" "genau ein Backup erwartet" || return 1

  # Backup enthält die Config
  assert_file_exists "$(find "$home/.config/oc-sandbox/backups" -maxdepth 1 -type d -name "config-*" | head -1)/projects.json" || return 1

  cleanup_test_env "$test_dir"
}

test_remove_config_with_no_backup_creates_no_backup() {
  local test_dir
  test_dir=$(setup_test_env "remove-config-no-backup")
  local home
  home=$(setup_installed_home "$test_dir")

  run_uninstall "$home" --remove-config --no-backup

  assert_dir_not_exists "$home/.config/oc-sandbox" || return 1

  local backup_count
  backup_count=$(find "$home/.config/oc-sandbox/backups" -maxdepth 1 -type d -name "config-*" 2>/dev/null | wc -l)
  assert_equals "0" "$backup_count" "kein Backup erwartet" || return 1

  cleanup_test_env "$test_dir"
}

test_backup_rotation_keeps_five() {
  local test_dir
  test_dir=$(setup_test_env "rotation")
  local home
  home=$(setup_installed_home "$test_dir")

  # 7 alte Backups anlegen
  local backups_dir="$home/.config/oc-sandbox/backups"
  mkdir -p "$backups_dir"
  for i in 1 2 3 4 5 6 7; do
    mkdir -p "$backups_dir/config-2020010${i}_000000"
  done

  run_uninstall "$home" --remove-config

  local backup_count
  backup_count=$(find "$backups_dir" -maxdepth 1 -type d -name "config-*" | wc -l)
  assert_equals "5" "$backup_count" "Rotation soll 5 neueste behalten" || return 1

  # Neueste (incl. frischem Backup) müssen überlebt haben
  assert_dir_exists "$backups_dir/config-20200107_000000" || return 1

  cleanup_test_env "$test_dir"
}

test_no_symlinks_keeps_symlinks() {
  local test_dir
  test_dir=$(setup_test_env "no-symlinks")
  local home
  home=$(setup_installed_home "$test_dir")

  run_uninstall "$home" --no-symlinks

  assert_dir_not_exists "$home/.oc-sandbox" || return 1
  if [[ ! -L "$home/.local/bin/uninstall.sh" ]]; then
    echo "  Symlink sollte erhalten bleiben"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_unknown_flag_fails() {
  local test_dir
  test_dir=$(setup_test_env "unknown-flag")
  local home
  home=$(setup_installed_home "$test_dir")

  if HOME="$home" bash "$UNINSTALL_SCRIPT" --bogus-flag >/dev/null 2>&1; then
    echo "  Unbekanntes Flag sollte fehlschlagen"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_dry_run_shows_config_removal() {
  local test_dir
  test_dir=$(setup_test_env "dry-run-config")
  local home
  home=$(setup_installed_home "$test_dir")

  local output
  output=$(HOME="$home" bash "$UNINSTALL_SCRIPT" --dry-run --remove-config 2>&1)

  if [[ "$output" != *"Config-Verzeichnis würde entfernt"* ]]; then
    echo "  Dry-Run zeigt Config-Entfernung nicht an"
    return 1
  fi
  assert_dir_exists "$home/.oc-sandbox" "Dry-Run darf nichts löschen" || return 1
  assert_file_exists "$home/.config/oc-sandbox/projects.json" || return 1

  cleanup_test_env "$test_dir"
}

test_completion_message_lists_actions() {
  local test_dir
  test_dir=$(setup_test_env "completion")
  local home
  home=$(setup_installed_home "$test_dir")

  local output
  output=$(run_uninstall_capture "$home")

  if [[ "$output" != *"Durchgeführte Aktionen"* ]]; then
    echo "  Abschlussmeldung listet Aktionen nicht auf"
    return 1
  fi
  if [[ "$output" != *"Installation entfernt: $home/.oc-sandbox"* ]]; then
    echo "  'Installation entfernt' fehlt in Abschlussmeldung"
    return 1
  fi
  if [[ "$output" != *"Symlink"* ]]; then
    echo "  Symlink-Aktion fehlt in Abschlussmeldung"
    return 1
  fi

  cleanup_test_env "$test_dir"
}

test_backup_failure_with_force_continues() {
  if [[ $EUID -eq 0 ]]; then
    return 0
  fi
  local test_dir
  test_dir=$(setup_test_env "backup-failure-force")
  local home
  home=$(setup_installed_home "$test_dir")

  # Unlesbare Datei -> cp schlägt fehl -> Backup fehlschlägt
  echo "secret" > "$home/.config/oc-sandbox/unreadable.json"
  chmod 000 "$home/.config/oc-sandbox/unreadable.json"

  run_uninstall "$home" --remove-config --force

  assert_dir_not_exists "$home/.oc-sandbox" || return 1
  assert_dir_not_exists "$home/.config/oc-sandbox/projects.json" || return 1
  chmod 600 "$home/.config/oc-sandbox/unreadable.json" 2>/dev/null || true

  cleanup_test_env "$test_dir"
}

test_backup_failure_without_force_aborts() {
  if [[ $EUID -eq 0 ]]; then
    return 0
  fi
  local test_dir
  test_dir=$(setup_test_env "backup-failure-abort")
  local home
  home=$(setup_installed_home "$test_dir")

  echo "secret" > "$home/.config/oc-sandbox/unreadable.json"
  chmod 000 "$home/.config/oc-sandbox/unreadable.json"

  # Nein-Antwort auf Fortfahren-Prompt -> Abbruch
  HOME="$home" bash "$UNINSTALL_SCRIPT" --remove-config <<< "n" >/dev/null 2>&1

  # Config muss noch vorhanden sein (Abbruch vor Entfernung)
  assert_file_exists "$home/.config/oc-sandbox/projects.json" "Abbruch darf Config nicht entfernen" || return 1
  chmod 600 "$home/.config/oc-sandbox/unreadable.json" 2>/dev/null || true

  cleanup_test_env "$test_dir"
}

test_permission_error_reports_skipped_files() {
  if [[ $EUID -eq 0 ]]; then
    return 0
  fi
  local test_dir
  test_dir=$(setup_test_env "permission-install")
  local home
  home=$(setup_installed_home "$test_dir")

  # Unbeschreibbares Unterverzeichnis -> rm -rf schlägt fehl -> Dateien werden getrackt
  mkdir -p "$home/.oc-sandbox/locked"
  echo "protected" > "$home/.oc-sandbox/locked/keep.txt"
  chmod 555 "$home/.oc-sandbox/locked"

  local output
  output=$(run_uninstall_capture "$home")
  chmod -R u+w "$home" 2>/dev/null || true

  if [[ "$output" != *"fehlender Berechtigungen"* ]]; then
    echo "  Ausgabe nennt fehlende Berechtigungen nicht"
    return 1
  fi
  if [[ "$output" != *"$home/.oc-sandbox"* ]]; then
    echo "  Geschützte Dateien werden nicht aufgelistet"
    return 1
  fi
  assert_dir_exists "$home/.oc-sandbox" "Installation soll wegen Berechtigungen bestehen bleiben" || return 1

  cleanup_test_env "$test_dir"
}

test_config_not_empty_permission_tracked() {
  if [[ $EUID -eq 0 ]]; then
    return 0
  fi
  local test_dir
  test_dir=$(setup_test_env "permission-config")
  local home
  home=$(setup_installed_home "$test_dir")

  # Unbeschreibbares Unterverzeichnis in der Config -> rmdir schlägt fehl -> getrackt
  mkdir -p "$home/.config/oc-sandbox/locked"
  echo "protected" > "$home/.config/oc-sandbox/locked/keep.txt"
  chmod 555 "$home/.config/oc-sandbox/locked"

  local output
  output=$(run_uninstall_capture "$home" --remove-config --force)
  chmod -R u+w "$home" 2>/dev/null || true

  if [[ "$output" != *"Nicht leer (übrige Dateien ohne Berechtigung)"* ]]; then
    echo "  Config-Verzeichnis nicht als 'Nicht leer' getrackt"
    return 1
  fi
  if [[ "$output" != *"$home/.config/oc-sandbox"* ]]; then
    echo "  Config-Pfad fehlt in Ausgabe"
    return 1
  fi
  assert_dir_exists "$home/.config/oc-sandbox" "Config soll wegen Berechtigungen bestehen bleiben" || return 1

  cleanup_test_env "$test_dir"
}

test_running_container_detection_still_works() {
  local test_dir
  test_dir=$(setup_test_env "containers")
  local home
  home=$(setup_installed_home "$test_dir")

  # podman nicht verfügbar/keine Container -> Deinstallation läuft durch
  run_uninstall "$home"

  assert_dir_not_exists "$home/.oc-sandbox" || return 1

  cleanup_test_env "$test_dir"
}

# --- Main ----------------------------------------------------------------------

run_test "Hilfe zeigt neue Flags" test_help_shows_new_flags
run_test "Default: Config bleibt, Symlinks werden entfernt" test_default_keeps_config_and_removes_symlinks
run_test "--remove-config: Config entfernt, Backup erstellt" test_remove_config_removes_dir_and_creates_backup
run_test "--remove-config --no-backup: kein Backup" test_remove_config_with_no_backup_creates_no_backup
run_test "Backup-Rotation behält 5 neueste" test_backup_rotation_keeps_five
run_test "--no-symlinks: Symlinks bleiben" test_no_symlinks_keeps_symlinks
run_test "Unbekanntes Flag schlägt fehl" test_unknown_flag_fails
run_test "Dry-Run zeigt Config-Entfernung" test_dry_run_shows_config_removal
run_test "Abschlussmeldung listet Aktionen" test_completion_message_lists_actions
run_test "Backup-Fehler mit --force läuft weiter" test_backup_failure_with_force_continues
run_test "Backup-Fehler ohne Fortfahren bricht ab" test_backup_failure_without_force_aborts
run_test "Container-Erkennung blockiert nicht bei keiner Ausgabe" test_running_container_detection_still_works
run_test "Berechtigungsfehler werden getrackt und berichtet" test_permission_error_reports_skipped_files
run_test "Config nicht leer wegen Berechtigungen wird getrackt" test_config_not_empty_permission_tracked

echo ""
echo "Tests gesamt: $TESTS_RUN, bestanden: $TESTS_PASSED, fehlgeschlagen: $TESTS_FAILED"

rm -rf "$TEMP_BASE"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
exit 0
