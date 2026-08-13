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

test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT
export HOME="$test_home"
mkdir -p "$HOME/.config/oc-sandbox/backups"
printf '{"projects": [], "version": "1.0"}\n' > "$HOME/.config/oc-sandbox/projects.json"

empty_dir="$test_home/projects/empty project"
mkdir -p "$empty_dir"
validate_project_path "$empty_dir"
printf 'existing\n' > "$empty_dir/file"
if validate_project_path "$empty_dir"; then
  printf 'non-empty directory was accepted\n' >&2
  exit 1
fi
rm "$empty_dir/file"

path_one="$test_home/one/shared"
path_two="$test_home/two/shared"
mkdir -p "$path_one" "$path_two"
add_project_to_registry "First" "$path_one" "none"
add_project_to_registry "Second" "$path_two" "none"

first_id=$(project_container_identity "$path_one")
second_id=$(project_container_identity "$path_two")
[[ "$first_id" != "$second_id" ]]
[[ "$(get_project_by_id "$first_id" | jq -r '.path')" == "$(canonicalize_project_path "$path_one")" ]]
[[ "$(get_last_used_project)" == "$second_id" ]]

spaced_path="$test_home/path with spaces"
mkdir -p "$spaced_path"
add_project_to_registry "Spaced" "$spaced_path" "none"
[[ "$(get_project_by_name Spaced | jq -r '.path')" == "$(canonicalize_project_path "$spaced_path")" ]]

printf 'start-tui identity and registry tests passed\n'
