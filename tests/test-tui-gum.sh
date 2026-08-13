#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUM_BIN="${PROJECT_ROOT}/tests/gum_bin/gum"
PTY_RUNNER="${PROJECT_ROOT}/tests/run-pty.py"

[[ -x "$GUM_BIN" ]] || { printf 'bundled gum is missing\n' >&2; exit 1; }
command -v python3 >/dev/null || { printf 'python3 is required for PTY tests\n' >&2; exit 1; }

run_gum() {
  local input="$1"
  local submit="$2"
  shift 2
  python3 "$PTY_RUNNER" --input "$input" --submit "$submit" -- "$GUM_BIN" "$@"
}

run_gum_result() {
  local input="$1"
  local submit="$2"
  shift 2
  python3 "$PTY_RUNNER" --input "$input" --submit "$submit" -- bash -c '
    result=$("$1" "${@:2}")
    printf "RESULT=%s\\n" "$result"
  ' _ "$GUM_BIN" "$@"
}

input_result=$(run_gum_result hello $'\r' input --prompt='Name: ')
[[ "$input_result" == *$'RESULT=hello\r'* ]] || {
  printf 'gum input did not return the entered value\n' >&2
  exit 1
}

choose_result=$(run_gum_result $'\033[B' $'\r' choose one two)
[[ "$choose_result" == *$'RESULT=two\r'* ]] || {
  printf 'gum choose did not select the second option\n' >&2
  exit 1
}

toggle_result=$(run_gum_result $' \033[B ' $'\r' choose --no-limit one two)
[[ "$toggle_result" == *$'RESULT=one\r\ntwo\r'* ]] || {
  printf 'gum toggle selection did not return both options\n' >&2
  exit 1
}

cancel_result=$(run_gum $'\033' '' choose one two)
if [[ "$cancel_result" != *"nothing selected"* ]]; then
  printf 'gum cancellation was not observable\n' >&2
  exit 1
fi

secret='gum-hidden-token'
secret_result=$(python3 "$PTY_RUNNER" --input "$secret" --submit $'\r' -- bash -c '
  result=$("$1" input --password --prompt="Token: ")
  printf "SECRET_LENGTH=%s\\n" "${#result}"
' _ "$GUM_BIN")
[[ "$secret_result" == *'SECRET_LENGTH=16'* ]] || {
  printf 'gum password input did not accept the token\n' >&2
  exit 1
}
[[ "$secret_result" != *"$secret"* ]] || {
  printf 'gum password input disclosed the token\n' >&2
  exit 1
}

test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT
export HOME="$test_home"
mkdir -p "$test_home/.config/oc-sandbox"
printf '%s\n' '{"projects": [], "version": "1.0"}' > "$test_home/.config/oc-sandbox/projects.json"

menu_result=$(python3 "$PTY_RUNNER" --input $'\033[B' --submit $'\r' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$1/tests/gum_bin/gum"
  show_menu "Navigation" "Open Project" "New Project"
' _ "$PROJECT_ROOT")
[[ "$menu_result" == *$'New Project\r'* ]] || {
  printf 'TUI navigation did not return the selected item\n' >&2
  exit 1
}

printf 'real-gum TUI harness tests passed\n'
