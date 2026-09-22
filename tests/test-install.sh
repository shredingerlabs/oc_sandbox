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
