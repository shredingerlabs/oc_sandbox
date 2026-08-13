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
printf '%s\n' '{"projects": [], "version": "1.0"}' > "$HOME/.config/oc-sandbox/projects.json"

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

AVAILABLE_MODES=(offline cbm_ui hil_mode)
selected_modes=(offline)
mode_is_selected offline "${selected_modes[@]}"
! mode_is_selected cbm_ui "${selected_modes[@]}"
remove_selected_mode offline selected_modes
[[ ${#selected_modes[@]} -eq 0 ]]

show_page() { :; }
wait_for_enter() { :; }
captured_modes=()
select_start_option() {
  captured_modes=("$@")
}

menu_sequence="$test_home/menu-sequence"
printf '%s\n' "● offline" "● offline" "Done" > "$menu_sequence"
show_menu() {
  local answers=()
  mapfile -t answers < "$menu_sequence"
  local answer="${answers[0]}"
  : > "$menu_sequence"
  if [[ ${#answers[@]} -gt 1 ]]; then
    printf '%s\n' "${answers[@]:1}" > "$menu_sequence"
  fi
  printf '%s\n' "$answer"
}
select_container_modes /tmp/project Test full
[[ ${#captured_modes[@]} -eq 3 ]]

printf '%s\n' "● offline" "● cbm_ui" "Done" "← Go Back" > "$menu_sequence"
captured_modes=()
select_container_edition() { :; }
select_container_modes /tmp/project Test full
[[ ${#captured_modes[@]} -eq 0 ]]

validate_container_modes offline
if validate_container_modes offline cbm_ui; then
  printf 'incompatible modes were accepted\n' >&2
  exit 1
fi

config_project="$test_home/config project"
mkdir -p "$config_project"
add_project_to_registry "Args" "$config_project" "none"
create_sandbox_config "$config_project" full console none
[[ "$(jq -c '.container_modes' "$config_project/.opencode_config/sandbox_config.json")" == '[]' ]]

native_dir="$test_home/native"
args_file="$test_home/native-args"
mkdir -p "$native_dir"
printf '%s\n' '#!/usr/bin/env bash' "printf '%s\\n' \"\$@\" > '$args_file'" > "$native_dir/start.sh"
chmod +x "$native_dir/start.sh"
SCRIPT_DIR="$native_dir"
create_sandbox_config "$config_project" full offline console none
start_container "$config_project" "$config_project/.opencode_config/sandbox_config.json" true
mapfile -t native_args < "$args_file"
[[ "${native_args[0]}" == "$config_project" ]]
[[ "${native_args[*]}" == *"--offline --detach"* ]]

printf 'start-tui mode selection tests passed\n'

discovery_dir="$test_home/discovery"
mkdir -p "$discovery_dir"
printf '%s\n' '#!/usr/bin/env bash' 'printf "Usage: %s [alpha|beta|all]\\n" "$0"' > "$discovery_dir/build-container.sh"
printf '%s\n' '#!/usr/bin/env bash' 'printf "Usage: %s --safe --other --start_opencode --detach\\n" "$0"' > "$discovery_dir/start.sh"
chmod +x "$discovery_dir/build-container.sh" "$discovery_dir/start.sh"
SCRIPT_DIR="$discovery_dir"
detect_available_editions
[[ "${AVAILABLE_EDITIONS[*]}" == "alpha beta" ]]
[[ "${AVAILABLE_MODES[*]}" == "safe other" ]]

printf '%s\n' '#!/usr/bin/env bash' 'exit 1' > "$discovery_dir/build-container.sh"
chmod +x "$discovery_dir/build-container.sh"
if detect_available_editions; then
  printf 'discovery failure was accepted\n' >&2
  exit 1
fi
SCRIPT_DIR="$PROJECT_ROOT/dist/scripts"

printf 'start-tui discovery tests passed\n'

# Credential setup tests use prompt seams so no real terminal or secret is needed.
TUI_MODE=bash
prompt_for_secret() { printf '%s\n' 'test-secret'; }
prompt_for_text() { printf '%s\n' 'git.example.com'; }
show_menu() { printf '%s\n' 'Keep existing'; }

credentials_project="$test_home/credentials project"
mkdir -p "$credentials_project"
setup_github_credentials "$credentials_project" >/tmp/tui-credential-output
[[ -f "$credentials_project/.git_local/gh-cli/hosts.yml" ]]
[[ "$(jq -r '.["github.com"].oauth_token' "$credentials_project/.git_local/gh-cli/hosts.yml")" == 'test-secret' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local")" == '700' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local/gh-cli")" == '700' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local/gh-cli/hosts.yml")" == '600' ]]
! grep -Fq 'test-secret' /tmp/tui-credential-output

setup_gitlab_credentials "$credentials_project" >/dev/null
[[ "$(jq -r '.["gitlab.com"].token' "$credentials_project/.git_local/glab-cli/hosts.yml")" == 'test-secret' ]]
setup_custom_vcs_credentials "$credentials_project" >/dev/null
[[ "$(jq -r '.["git.example.com"].token' "$credentials_project/.git_local/vcs/hosts.yml")" == 'test-secret' ]]

printf '%s\n' 'original' > "$credentials_project/.git_local/gh-cli/hosts.yml"
show_menu() { printf '%s\n' 'Keep existing'; }
setup_github_credentials "$credentials_project" >/dev/null
[[ "$(<"$credentials_project/.git_local/gh-cli/hosts.yml")" == 'original' ]]

show_menu() { printf '%s\n' 'Replace'; }
setup_github_credentials "$credentials_project" >/dev/null
[[ "$(jq -r '.["github.com"].oauth_token' "$credentials_project/.git_local/gh-cli/hosts.yml")" == 'test-secret' ]]

cancelled_project="$test_home/cancelled credentials"
mkdir -p "$cancelled_project"
prompt_for_secret() { return 1; }
if setup_github_credentials "$cancelled_project" >/dev/null; then
  printf 'cancelled credential prompt was accepted\n' >&2
  exit 1
fi
[[ ! -e "$cancelled_project/.git_local/gh-cli/hosts.yml" ]]

prompt_for_secret() { printf '%s\n' 'ai-secret'; }
show_menu() { printf '%s\n' 'Keep existing'; }
setup_gwdg_provider "$credentials_project" >/dev/null
[[ "$(jq -r '."gwdg-saia".key' "$credentials_project/.opencode_data/auth.json")" == 'ai-secret' ]]
[[ "$(jq -r '.provider."gwdg-saia".options.baseURL' "$credentials_project/.opencode_config/opencode.json")" == 'https://chat-ai.academiccloud.de/v1' ]]
[[ "$(stat -c '%a' "$credentials_project/.opencode_data")" == '700' ]]
[[ "$(stat -c '%a' "$credentials_project/.opencode_data/auth.json")" == '600' ]]

printf '%s\n' 'credential-output-safety' > "$HOME/.config/oc-sandbox/projects.json"
if grep -R --exclude='test-start-tui.sh' -Fq 'ai-secret' "$HOME/.config/oc-sandbox"; then
  printf 'secret leaked into general configuration\n' >&2
  exit 1
fi

printf '%s\n' '{"projects": [], "version": "1.0"}' > "$HOME/.config/oc-sandbox/projects.json"

rm -f /tmp/tui-credential-output
printf 'start-tui credential safety tests passed\n'

# Lifecycle tests use mocked Podman and native scripts.
workflow_home="$test_home/workflow"
mkdir -p "$workflow_home/bin" "$workflow_home/native"
image_state="$workflow_home/images"
podman_log="$workflow_home/podman-args"
touch "$image_state"
cat > "$workflow_home/bin/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$PODMAN_LOG"
case "${1:-}" in
  image) grep -Fxq "${3#opencode-sandbox-}" "$IMAGE_STATE" ;;
  ps) : ;;
  exec) exit 0 ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$workflow_home/bin/podman"
export PODMAN_LOG="$podman_log" IMAGE_STATE="$image_state"
PATH="$workflow_home/bin:$PATH"

workflow_project="$workflow_home/project"
mkdir -p "$workflow_project"
add_project_to_registry Workflow "$workflow_project" none
create_sandbox_config "$workflow_project" full console none
show_menu() { printf '%s\n' 'Build later'; }
set +e
check_and_build_containers "$workflow_project"
build_result=$?
set -e
if [[ "$build_result" -eq 0 ]]; then
  printf 'deferred build was accepted as ready\n' >&2
  exit 1
fi
[[ "$build_result" -eq 2 ]]
[[ "$(jq -r '.projects[] | select(.name == "Workflow") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == stopped ]]
[[ ! -e "$workflow_home/native-start-args" ]]
show_menu() { printf '%s\n' 'Go back'; }
set +e
check_and_build_containers "$workflow_project"
build_result=$?
set -e
[[ "$build_result" -eq 3 ]]

cat > "$workflow_home/native/build-container.sh" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$workflow_home/native/build-container.sh"
SCRIPT_DIR="$workflow_home/native"
show_menu() { printf '%s\n' 'Build now'; }
set +e
check_and_build_containers "$workflow_project"
build_result=$?
set -e
if [[ "$build_result" -eq 0 ]]; then
  printf 'failed build was accepted\n' >&2
  exit 1
fi
[[ ! -e "$workflow_home/native-start-args" ]]
SCRIPT_DIR="$PROJECT_ROOT/dist/scripts"

cat > "$workflow_home/native/start.sh" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$workflow_home/native-start-args"
EOF
chmod +x "$workflow_home/native/start.sh"
printf 'full\n' > "$image_state"
SCRIPT_DIR="$workflow_home/native"
create_sandbox_config "$workflow_project" full console none
start_container_with_setup "$workflow_project"
grep -F -- '--detach' "$workflow_home/native-start-args"
[[ "$(jq -r '.setup_complete' "$workflow_project/.opencode_config/sandbox_config.json")" == true ]]

create_sandbox_config "$workflow_project" full opencode none
start_container_with_setup "$workflow_project"
grep -F -- 'opencode' "$podman_log"
SCRIPT_DIR="$PROJECT_ROOT/dist/scripts"

printf 'start-tui lifecycle tests passed\n'
