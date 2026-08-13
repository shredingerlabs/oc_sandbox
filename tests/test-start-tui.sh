#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "${PROJECT_ROOT}/dist/scripts/start-tui.sh"

assert_path() {
  local expected="$1"
  local actual="$2"
  [[ "$actual" == "$expected" ]] || {
    printf 'expected %s, got %s\n' "$expected" "$actual" >&2
    return 1
  }
}

default_path="/home/user/oc-sandbox"
assert_path "$default_path" "$(normalize_project_path "$default_path" "$default_path")"
assert_path "/home/user/oc-sandbox/test123" "$(normalize_project_path test123 "$default_path")"
assert_path "/home/user/Projects" "$(normalize_project_path /home/user/Projects "$default_path")"
assert_path "/home/user/oc-sandbox/Projects" "$(normalize_project_path ./Projects "$default_path")"
assert_path "/home/user/oc-sandbox/test123" "$(normalize_project_path /home/user/oc-sandboxtest123 "$default_path")"
assert_path "/home/user/Projects" "$(normalize_project_path /home/user/oc-sandbox/home/user/Projects "$default_path")"

printf 'start-tui path tests passed\n'
