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

fallback_choice=$(printf '2\n' | bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=bash
  bash_select "Fallback navigation" "Open Project" "New Project"
' _ "$PROJECT_ROOT")
[[ "$fallback_choice" == "New Project" ]]
printf 'start-tui bash fallback navigation passed\n'

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
[[ ${#captured_modes[@]} -eq 4 ]]
[[ "${captured_modes[3]}" == "" ]]

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
printf '%s\n' '#!/usr/bin/env bash' 'printf "Usage: %s --safe --other --start_opencode --start_web --detach\\n" "$0"' > "$discovery_dir/start.sh"
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
credential_output="$test_home/tui-credential-output"
setup_github_credentials "$credentials_project" >"$credential_output"
[[ -f "$credentials_project/.git_local/gh-cli/hosts.yml" ]]
[[ "$(jq -r '.["github.com"].oauth_token' "$credentials_project/.git_local/gh-cli/hosts.yml")" == 'test-secret' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local")" == '700' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local/gh-cli")" == '700' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local/gh-cli/hosts.yml")" == '600' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local/credentials")" == '600' ]]
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://oauth2:test-secret@github.com' ]]
! grep -Fq 'test-secret' "$credential_output"

setup_gitlab_credentials "$credentials_project" >/dev/null
[[ "$(jq -r '.["gitlab.com"].token' "$credentials_project/.git_local/glab-cli/hosts.yml")" == 'test-secret' ]]
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://token:test-secret@gitlab.com' ]]
setup_custom_vcs_credentials "$credentials_project" >/dev/null
[[ "$(jq -r '.["git.example.com"].token' "$credentials_project/.git_local/vcs/hosts.yml")" == 'test-secret' ]]
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://token:test-secret@git.example.com' ]]

prompt_for_text() { printf '%s\n' 'selfhosted.example.com'; }
show_menu() { printf '%s\n' 'Replace'; }
setup_self_hosted_gitlab_credentials "$credentials_project" >/dev/null
[[ "$(jq -r --arg host selfhosted.example.com '.[$host].token' "$credentials_project/.git_local/glab-cli/hosts.yml")" == 'test-secret' ]]
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://token:test-secret@selfhosted.example.com' ]]

prompt_for_text() { printf '%s\n' 'https://invalid.example.com/path'; }
if setup_self_hosted_gitlab_credentials "$credentials_project" >/dev/null; then
  printf 'invalid GitLab host was accepted\n' >&2
  exit 1
fi
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://token:test-secret@selfhosted.example.com' ]]

printf '%s\n' '[user]' '    name = Existing Name' '    email = existing@example.com' '[core]' '    editor = vi' > "$credentials_project/.git_local/gitconfig"
prompt_for_text() {
  case "$1" in
    'Git user.name:') printf '%s\n' 'Project Name' ;;
    'Git user.email:') printf '%s\n' 'project@example.com' ;;
    *) printf '%s\n' 'selfhosted.example.com' ;;
  esac
}
configure_git_identity "$credentials_project"
[[ "$(git config --file "$credentials_project/.git_local/gitconfig" user.name)" == 'Project Name' ]]
[[ "$(git config --file "$credentials_project/.git_local/gitconfig" user.email)" == 'project@example.com' ]]
[[ "$(git config --file "$credentials_project/.git_local/gitconfig" core.editor)" == 'vi' ]]
[[ "$(stat -c '%a' "$credentials_project/.git_local")" == '700' ]]

printf '%s\n' 'original' > "$credentials_project/.git_local/gh-cli/hosts.yml"
show_menu() { printf '%s\n' 'Keep existing'; }
setup_github_credentials "$credentials_project" >/dev/null
[[ "$(<"$credentials_project/.git_local/gh-cli/hosts.yml")" == 'original' ]]
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://token:test-secret@selfhosted.example.com' ]]

show_menu() { printf '%s\n' 'Replace'; }
setup_github_credentials "$credentials_project" >/dev/null
[[ "$(jq -r '.["github.com"].oauth_token' "$credentials_project/.git_local/gh-cli/hosts.yml")" == 'test-secret' ]]
[[ "$(<"$credentials_project/.git_local/credentials")" == 'https://oauth2:test-secret@github.com' ]]

cancelled_project="$test_home/cancelled credentials"
mkdir -p "$cancelled_project"
prompt_for_secret() { return 1; }
if setup_github_credentials "$cancelled_project" >/dev/null; then
  printf 'cancelled credential prompt was accepted\n' >&2
  exit 1
fi
[[ ! -e "$cancelled_project/.git_local/gh-cli/hosts.yml" ]]
[[ ! -e "$cancelled_project/.git_local/credentials" ]]

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

rm -f "$credential_output"
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
  ps)
    [[ -n "${PODMAN_RUNNING:-}" ]] && printf '%s\n' "$PODMAN_RUNNING"
    ;;
  port)
    [[ -n "${PODMAN_NO_PORT:-}" ]] && exit 0
    printf '%s\n' '127.0.0.1:4096'
    ;;
  exec)
    if [[ "${PODMAN_FAIL_CBM:-false}" == true && "$*" == *codebase-memory-mcp* ]]; then
      exit 1
    fi
    if [[ "${PODMAN_FAIL_SKILLS:-false}" == true && "$*" == *'setup-matt-pocock-skills'* ]]; then
      exit 1
    fi
    if [[ "$*" == *'setup-matt-pocock-skills'* ]]; then
      [[ "$*" == *'exec -it '* ]] || exit 1
      [[ "$*" != *'bash -c'* ]] || exit 1
      printf '%s\n' "$*" > "$SKILLS_INPUT_LOG"
      printf 'interactive skills output\n'
    fi
    if [[ -n "${PODMAN_CLONE_DIR:-}" ]]; then
      if [[ "$*" == *'remote.origin.url'* ]]; then
        if [[ -d "$PODMAN_CLONE_DIR/.git" ]] &&
          git -C "$PODMAN_CLONE_DIR" config --get remote.origin.url 2>/dev/null | grep -Fxq "$CLONE_URL" &&
          git -C "$PODMAN_CLONE_DIR" rev-parse --verify -q HEAD >/dev/null 2>&1; then
          exit 0
        fi
        exit 1
      fi
      if [[ "$*" == *'git ls-remote'* ]]; then
        printf 'probe %s\n' "$*" >> "$CLONE_LOG"
        if [[ -f "$PROBE_FAILS" && "$(cat "$PROBE_FAILS")" -gt 0 ]]; then
          printf '%s' $(( $(cat "$PROBE_FAILS") - 1 )) > "$PROBE_FAILS"
          printf 'fatal: could not read from remote repository\n'
          exit 1
        fi
        exit 0
      fi
      if [[ "$*" == *'git clone'* ]]; then
        printf 'clone %s\n' "$*" >> "$CLONE_LOG"
        rm -rf "${PODMAN_CLONE_DIR:?}"/* "${PODMAN_CLONE_DIR:?}"/.[!.]* 2>/dev/null || true
        if [[ -f "$CLONE_FAILS" && "$(cat "$CLONE_FAILS")" -gt 0 ]]; then
          printf '%s' $(( $(cat "$CLONE_FAILS") - 1 )) > "$CLONE_FAILS"
          mkdir -p "${PODMAN_CLONE_DIR:?}/.git"
          printf 'leftover\n' > "${PODMAN_CLONE_DIR:?}/partial.marker"
          printf 'fatal: early EOF\n'
          exit 1
        fi
        rm -rf "${PODMAN_CLONE_DIR:?}"
        mkdir -p "${PODMAN_CLONE_DIR:?}"
        git init -q "${PODMAN_CLONE_DIR:?}"
        git -C "${PODMAN_CLONE_DIR:?}" remote add origin "$CLONE_URL"
        git -C "${PODMAN_CLONE_DIR:?}" -c user.name=t -c user.email=t@e commit --allow-empty -q -m init
        exit 0
      fi
    fi
    exit 0
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$workflow_home/bin/podman"
export PODMAN_LOG="$podman_log" IMAGE_STATE="$image_state"
skills_input_log="$workflow_home/skills-input"
export SKILLS_INPUT_LOG="$skills_input_log"
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
printf '%s\n' "$*" >> "$BUILD_ARGS"
exit 1
EOF
chmod +x "$workflow_home/native/build-container.sh"
export BUILD_ARGS="$workflow_home/build-args"
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
grep -Fxq full "$BUILD_ARGS"
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
show_menu() { printf '%s\n' 'OpenCode (configured)'; }
start_container_with_setup "$workflow_project"
grep -F -- 'opencode' "$podman_log"
SCRIPT_DIR="$PROJECT_ROOT/dist/scripts"

# The web start option is selectable during creation and passed through to
# start.sh; access of a running web project offers the access-time chooser and
# reports the published URL.
# shellcheck disable=SC1091
source "$PROJECT_ROOT/dist/scripts/start-tui.sh"
show_menu() {
  case "$1" in
    'Select access for running container'*) printf '%s\n' "$ACCESS_CHOICE" ;;
    *) printf '%s\n' 'web' ;;
  esac
}
captured_start_choice=""
select_vcs_tracking() { captured_start_choice="${*: -1}"; }
select_start_option /tmp/project Test full
[[ "$captured_start_choice" == web ]]

SCRIPT_DIR="$workflow_home/native"
web_project_log="$workflow_home/web-access-output"
ACCESS_CHOICE='Show Web URL'
export ACCESS_CHOICE
create_sandbox_config "$workflow_project" full web none
start_container_with_setup "$workflow_project" > "$web_project_log"
grep -F -- '--start_web' "$workflow_home/native-start-args"
! grep -F -- '--start_opencode' "$workflow_home/native-start-args"
grep -q 'OpenCode Web: http://127.0.0.1:4096' "$web_project_log"
SCRIPT_DIR="$PROJECT_ROOT/dist/scripts"

# The access-time chooser for a running container offers Console (bash) for
# opencode- and web-configured projects; web projects can fall back to console
# even when no port is published.
console_project="$workflow_home/console-access"
mkdir -p "$console_project"
add_project_to_registry ConsoleAccess "$console_project" none
create_sandbox_config "$console_project" full console none
update_sandbox_config_field "$console_project/.opencode_config/sandbox_config.json" start_option opencode
update_sandbox_config_field "$console_project/.opencode_config/sandbox_config.json" setup_complete true
podman_log="$workflow_home/podman-args"
: > "$podman_log"
CONSOLE_NAME="opencode-sandbox-$(project_container_identity "$console_project")"
export PODMAN_RUNNING="$CONSOLE_NAME"

ACCESS_CHOICE='Console (bash)'
handle_project_action "$(get_project_by_path "$console_project")"
ACCESS_CHOICE='OpenCode (configured)'
handle_project_action "$(get_project_by_path "$console_project")"
grep -c 'exec -it --user dev opencode-sandbox-' "$podman_log" | grep -q '^2$'
grep -q 'exec -it --user dev .* opencode$' "$podman_log"
grep -q 'exec -it --user dev .* bash$' "$podman_log"

# Web-configured running project with no published port still reaches console.
web_no_port="$workflow_home/web-no-port"
mkdir -p "$web_no_port"
add_project_to_registry WebNoPort "$web_no_port" none
create_sandbox_config "$web_no_port" full web none
update_sandbox_config_field "$web_no_port/.opencode_config/sandbox_config.json" setup_complete true
export PODMAN_RUNNING="opencode-sandbox-$(project_container_identity "$web_no_port")"
export PODMAN_NO_PORT=true
: > "$podman_log"
ACCESS_CHOICE='Show Web URL'
no_port_note="$workflow_home/no-port-note"
handle_project_action "$(get_project_by_path "$web_no_port")" 2> "$no_port_note"
grep -q 'not published' "$no_port_note"
unset PODMAN_NO_PORT
: > "$podman_log"
ACCESS_CHOICE='Console (bash)'
handle_project_action "$(get_project_by_path "$web_no_port")"
grep -Fq 'exec -it --user dev' "$podman_log"
unset PODMAN_RUNNING ACCESS_CHOICE

# A failed native start must return to its caller when Go back is selected,
# even though the TUI runs with set -e.
failed_start_dir="$workflow_home/failed-start"
mkdir -p "$failed_start_dir"
cat > "$failed_start_dir/start.sh" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$failed_start_dir/start.sh"
failed_start_project="$workflow_home/failed-start-project"
mkdir -p "$failed_start_project"
add_project_to_registry FailedStart "$failed_start_project" none
create_sandbox_config "$failed_start_project" full console none
failed_start_return="$workflow_home/failed-start-returned"
if bash -c '
  source "$1/dist/scripts/start-tui.sh"
  set -euo pipefail
  SCRIPT_DIR="$2"
  show_menu() { printf "%s\n" "Go back"; }
  if start_container_with_setup "$3"; then
    exit 1
  else
    printf returned > "$4"
    exit 1
  fi
' _ "$PROJECT_ROOT" "$failed_start_dir" "$failed_start_project" "$failed_start_return"; then
  printf 'failed start unexpectedly succeeded\n' >&2
  exit 1
fi
[[ -f "$failed_start_return" ]]
[[ "$(jq -r '.projects[] | select(.name == "FailedStart") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == stopped ]]

# Retry reopens every setting with the current values and keeps existing VCS
# credentials when the integration is unchanged.
retry_project="$workflow_home/retry-settings"
mkdir -p "$retry_project/.git_local/gh-cli"
add_project_to_registry RetrySettings "$retry_project" github.com
create_sandbox_config "$retry_project" full offline console none
update_sandbox_config_field "$retry_project/.opencode_config/sandbox_config.json" vcs_tracking github.com
printf '%s\n' 'existing credentials' > "$retry_project/.git_local/gh-cli/hosts.yml"
prompt_for_text() {
  case "$1" in
    'Git user.name:') printf '%s\n' 'Retry User' ;;
    'Git user.email:') printf '%s\n' 'retry@example.com' ;;
  esac
}
setup_github_credentials() {
  printf 'unchanged credentials were prompted\n' >&2
  return 1
}
show_menu() {
  case "$1" in
    'Select container edition'*) printf '%s\n' full ;;
    'Select container modes'*) printf '%s\n' Done ;;
    'Select start option'*) printf '%s\n' console ;;
    'Select VCS tracking'*) printf '%s\n' github.com ;;
    'Select AI provider'*) printf '%s\n' none ;;
    *) return 1 ;;
  esac
}
revisit_project_settings "$retry_project"
[[ "$(jq -r '.container_edition' "$retry_project/.opencode_config/sandbox_config.json")" == full ]]
[[ "$(jq -r '.container_modes | join(",")' "$retry_project/.opencode_config/sandbox_config.json")" == offline ]]
[[ "$(jq -r '.vcs_tracking' "$retry_project/.opencode_config/sandbox_config.json")" == github.com ]]
[[ "$(<"$retry_project/.git_local/gh-cli/hosts.yml")" == 'existing credentials' ]]

reconcile_project="$workflow_home/reconcile"
mkdir -p "$reconcile_project"
add_project_to_registry Reconcile "$reconcile_project" none
create_sandbox_config "$reconcile_project" full console none
update_sandbox_config_field "$reconcile_project/.opencode_config/sandbox_config.json" setup_complete true
PODMAN_RUNNING="opencode-sandbox-$(project_container_identity "$reconcile_project")"
export PODMAN_RUNNING
handle_project_action "$(get_project_by_path "$reconcile_project")"
[[ "$(jq -r '.projects[] | select(.name == "Reconcile") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == running ]]
unset PODMAN_RUNNING

printf 'start-tui lifecycle tests passed\n'

# Setup recovery tests verify stage persistence and that retries skip completed work.
recovery_project="$workflow_home/recovery"
mkdir -p "$recovery_project"
add_project_to_registry Recovery "$recovery_project" none
create_sandbox_config "$recovery_project" full console none
export PODMAN_FAIL_CBM=true
show_menu() { printf '%s\n' 'Go back'; }
set +e
run_first_run_setup "$recovery_project"
recovery_result=$?
set -e
[[ "$recovery_result" -eq 1 ]]
[[ "$(jq -r '.setup_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == false ]]
[[ "$(jq -r '.setup_cbm_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == false ]]

export PODMAN_FAIL_CBM=false PODMAN_FAIL_SKILLS=true
show_menu() { printf '%s\n' 'Go back'; }
set +e
run_first_run_setup "$recovery_project"
recovery_result=$?
set -e
[[ "$recovery_result" -eq 1 ]]
[[ "$(jq -r '.setup_cbm_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == true ]]
[[ "$(jq -r '.setup_skills_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == false ]]

export PODMAN_FAIL_SKILLS=false
show_menu() { printf '%s\n' 'Retry'; }
skills_output=$(run_first_run_setup "$recovery_project")
[[ "$(jq -r '.setup_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == true ]]
[[ "$(jq -r '.setup_cbm_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == true ]]
[[ "$(jq -r '.setup_skills_complete' "$recovery_project/.opencode_config/sandbox_config.json")" == true ]]
[[ "$(<"$skills_input_log")" == *'opencode --prompt run skill setup-matt-pocock-skills'* ]]
[[ "$(<"$skills_input_log")" == *'-m opencode/big-pickle'* ]]
[[ "$skills_output" == *'interactive skills output'* ]]

# A cancelled recoverable operation returns to its caller rather than exiting the shell.
show_menu() { printf '%s\n' 'Go back'; }
if handle_recoverable_failure "Test operation"; then
  printf 'cancelled operation was retried\n' >&2
  exit 1
fi
show_menu() { printf '%s\n' 'Exit'; }
set +e
handle_recoverable_failure "Test operation"
recovery_result=$?
set -e
[[ "$recovery_result" -eq 2 ]]

printf 'start-tui setup recovery tests passed\n'

# Clone setup tests (#42): probe (git ls-remote) and full clone run inside the
# container before CBM, guarded by project_source/setup_clone_complete.
create_clone_project() {
  local path="$1"
  mkdir -p "$path"
  add_project_to_registry "$2" "$path" none "${3:-https://example.com/repo.git}"
  create_sandbox_config "$path" full console none
  update_sandbox_config_field "$path/.opencode_config/sandbox_config.json" project_source cloned
  update_sandbox_config_field "$path/.opencode_config/sandbox_config.json" \
    repo_url 'https://example.com/repo.git'
  printf '%s\n' "$path/.opencode_config/sandbox_config.json"
}

PATH="$workflow_home/bin:$PATH"
export PATH
podman_log="$workflow_home/podman-args"
export CLONE_LOG="$workflow_home/clone-counter-log"
export PROBE_FAILS="$workflow_home/probe-fails"
export CLONE_FAILS="$workflow_home/clone-fails"
export CLONE_URL='https://example.com/repo.git'

# Fully automated happy path: probe + clone (in this order) before CBM/skills.
clone_project="$workflow_home/clone"
clone_config="$(create_clone_project "$clone_project" CloneTest)"
export PODMAN_CLONE_DIR="$clone_project/project"
: > "$CLONE_LOG"; : > "$podman_log"; : > "$skills_input_log"
printf '0\n' > "$PROBE_FAILS"; printf '0\n' > "$CLONE_FAILS"
run_first_run_setup "$clone_project" >/dev/null
[[ "$(jq -r '.setup_clone_complete' "$clone_config")" == true ]]
[[ "$(jq -r '.setup_complete' "$clone_config")" == true ]]
[[ "$(head -n1 "$CLONE_LOG")" == probe* ]]
[[ "$(grep -c '^clone ' "$CLONE_LOG")" -eq 1 ]]
clone_log_line=$(grep '^clone ' "$CLONE_LOG")
[[ "$clone_log_line" == *'/home/dev/project'* ]]
[[ "$clone_log_line" != *'--depth'* ]]
[[ -x "$clone_project/project/.git" || -f "$clone_project/project/HEAD" || -d "$clone_project/project/.git" ]]
# clone podman-exec happens before the CBM podman-exec
[[ "$(grep -n '^exec' "$podman_log" | grep 'git clone' | cut -d: -f1)" -lt \
   "$(grep -n '^exec' "$podman_log" | grep 'codebase-memory-mcp' | cut -d: -f1)" ]]

# Probe failure: Retry re-probes with the same URL, nothing else re-runs.
retry_probe_project="$workflow_home/retry-probe"
retry_probe_config="$(create_clone_project "$retry_probe_project" CloneRetryProbe)"
: > "$CLONE_LOG"
printf '1\n' > "$PROBE_FAILS"; printf '0\n' > "$CLONE_FAILS"
export PODMAN_CLONE_DIR="$retry_probe_project/project"
show_menu() { printf '%s\n' 'Retry'; }
prompt_for_repo_url() { printf 'unexpected URL prompt\n' >&2; exit 1; }
run_first_run_clone "$retry_probe_project" "$retry_probe_config"
grep -c '^probe ' "$CLONE_LOG" | grep -q '^2$'
! grep -q '^clone ' "$CLONE_LOG"
[[ "$(jq -r '.setup_clone_complete' "$retry_probe_config")" == true ]]

# Probe failure: Change URL re-prompts, persists the new URL, and clones it.
change_url_project="$workflow_home/change-url"
change_url_config="$(create_clone_project "$change_url_project" CloneChangeUrl)"
update_sandbox_config_field "$change_url_config" repo_url 'git@old.example.com:team/old.git'
export PODMAN_CLONE_DIR="$change_url_project/project"
: > "$CLONE_LOG"
printf '1\n' > "$PROBE_FAILS"; printf '0\n' > "$CLONE_FAILS"
prompt_for_repo_url() {
  case "${PROMPT_COUNT:-0}" in
    0) PROMPT_COUNT=1; printf '%s\n' 'https://example.com/new-url.git' ;;
    *) printf 'unexpected extra prompt\n' >&2; exit 1 ;;
  esac
}
show_menu() { printf '%s\n' 'Change URL'; }
run_first_run_clone "$change_url_project" "$change_url_config"
[[ "$(jq -r '.repo_url' "$change_url_config")" == 'https://example.com/new-url.git' ]]
[[ "$(jq -r '.projects[] | select(.name == "CloneChangeUrl") | .repo_url' "$HOME/.config/oc-sandbox/projects.json")" == 'https://example.com/new-url.git' ]]
grep -q 'probe .*git@old.example.com' "$CLONE_LOG"
grep -q 'probe .*new-url.git' "$CLONE_LOG"
[[ "$(jq -r '.setup_clone_complete' "$change_url_config")" == true ]]
unset PROMPT_COUNT

# Clone failure mid-flight: partial state is cleaned before the retry succeeds.
partial_project="$workflow_home/partial"
partial_config="$(create_clone_project "$partial_project" ClonePartial)"
export PODMAN_CLONE_DIR="$partial_project/project"
: > "$CLONE_LOG"
printf '0\n' > "$PROBE_FAILS"
printf '1\n' > "$CLONE_FAILS"
show_menu() { printf '%s\n' 'Retry'; }
run_first_run_clone "$partial_project" "$partial_config"
[[ "$(grep -c '^clone ' "$CLONE_LOG")" -eq 2 ]]
[[ ! -e "$PODMAN_CLONE_DIR/partial.marker" ]]
[[ "$(jq -r '.setup_clone_complete' "$partial_config")" == true ]]

# Clone failure with explicit Exit: user abort (exit 2) is respected.
abort_project="$workflow_home/abort"
abort_config="$(create_clone_project "$abort_project" CloneAbort)"
export PODMAN_CLONE_DIR="$abort_project/project"
printf '0\n' > "$PROBE_FAILS"
printf '1\n' > "$CLONE_FAILS"
if bash -c '
  source "$1/dist/scripts/start-tui.sh"
  set -euo pipefail
  show_menu() { printf "%s\n" "Exit"; }
  run_first_run_clone "$2" "$3"
' _ "$PROJECT_ROOT" "$abort_project" "$abort_config" 2>/dev/null; then
  printf 'clone abort unexpectedly succeeded\n' >&2
  exit 1
fi
[[ "$(jq -r '.setup_clone_complete' "$abort_config")" == false ]]

# Clone succeeded but the flag write failed: Retry setup (menu path "Retry")
# completes the attempt without re-cloning.
recall_project="$workflow_home/recall"
recall_config="$(create_clone_project "$recall_project" CloneRecall)"
export PODMAN_CLONE_DIR="$recall_project/project"
rm -rf "$PODMAN_CLONE_DIR"
mkdir -p "$PODMAN_CLONE_DIR"
git init -q "$PODMAN_CLONE_DIR"
git -C "$PODMAN_CLONE_DIR" remote add origin "$CLONE_URL"
git -C "$PODMAN_CLONE_DIR" -c user.name=t -c user.email=t@e commit --allow-empty -q -m init
: > "$CLONE_LOG"
printf '0\n' > "$PROBE_FAILS"; printf '0\n' > "$CLONE_FAILS"
show_menu() { printf '%s\n' 'Retry'; }
update_sandbox_config_field "$recall_config" setup_clone_complete false
run_first_run_clone "$recall_project" "$recall_config"
[[ "$(jq -r '.setup_clone_complete' "$recall_config")" == true ]]
[[ ! -s "$CLONE_LOG" ]]

# "Retry setup" from the project menu runs the clone stage and then attaches.
menu_project="$workflow_home/menu-clone"
menu_config="$(create_clone_project "$menu_project" CloneMenuPath)"
export PODMAN_CLONE_DIR="$menu_project/project"
: > "$CLONE_LOG"
printf '0\n' > "$PROBE_FAILS"; printf '0\n' > "$CLONE_FAILS"
CONSOLE_NAME="opencode-sandbox-$(project_container_identity "$menu_project")"
export PODMAN_RUNNING="$CONSOLE_NAME"
show_menu() {
  if [[ "$1" == "Setup incomplete" ]]; then
    printf '%s\n' 'Retry setup'
  else
    printf '%s\n' 'Console (bash)'
  fi
}
handle_project_action "$(get_project_by_path "$menu_project")"
[[ "$(jq -r '.setup_clone_complete' "$menu_config")" == true ]]
[[ "$(jq -r '.setup_complete' "$menu_config")" == true ]]
[[ "$(grep -c '^clone ' "$CLONE_LOG")" -eq 1 ]]
unset PODMAN_RUNNING

# Empty-source projects skip probe/clone entirely.
empty_setup_project="$workflow_home/empty-setup"
mkdir -p "$empty_setup_project"
add_project_to_registry EmptySetup "$empty_setup_project" none
create_sandbox_config "$empty_setup_project" full console none
export PODMAN_CLONE_DIR="$empty_setup_project/project"
: > "$CLONE_LOG"; : > "$skills_input_log"
run_first_run_setup "$empty_setup_project" >/dev/null
[[ "$(jq -r '.setup_complete' "$empty_setup_project/.opencode_config/sandbox_config.json")" == true ]]
[[ ! -s "$CLONE_LOG" ]]
[[ "$(jq -r '.setup_clone_complete' "$empty_setup_project/.opencode_config/sandbox_config.json")" == false ]]

# Cleanup of overrides so later sections see the real wizard functions again.
unset CLONE_LOG PROBE_FAILS CLONE_FAILS CLONE_URL PODMAN_CLONE_DIR
unset -f show_menu prompt_for_repo_url
# shellcheck disable=SC1091
source "$PROJECT_ROOT/dist/scripts/start-tui.sh"

printf 'start-tui clone setup tests passed\n'

# Configuration backup/restore tests cover malformed input, safety copies,
# per-file scope, rapid writes, rotation, and secret exclusion.
backup_project="$test_home/backup-project"
mkdir -p "$backup_project/.opencode_config" "$backup_project/.opencode_data"
printf '%s\n' '{"active": true}' > "$HOME/.config/oc-sandbox/global_config.json"
printf '%s\n' '{"projects": []}' > "$HOME/.config/oc-sandbox/projects.json"
projects_before=$(<"$HOME/.config/oc-sandbox/projects.json")
BACKUP_HISTORY_LIMIT=5
for _ in 1 2 3 4 5 6; do
  backup_config "$HOME/.config/oc-sandbox/global_config.json"
done
backup_count=0
for backup in "$HOME/.config/oc-sandbox/backups/global_config.json".*; do
  [[ -f "$backup" ]] && backup_count=$((backup_count + 1))
done
[[ "$backup_count" -eq 5 ]]

printf '%s\n' 'secret' > "$backup_project/.opencode_data/auth.json"
if backup_config "$backup_project/.opencode_data/auth.json"; then
  printf 'secret file was backed up\n' >&2
  exit 1
fi
for backup in "$HOME/.config/oc-sandbox/backups"/*; do
  ! grep -Fq 'secret' "$backup"
done

malformed_backup="$HOME/.config/oc-sandbox/backups/global_config.json.malformed"
printf '%s\n' '{malformed' > "$malformed_backup"
printf '%s\n' 'unchanged' > "$HOME/.config/oc-sandbox/global_config.json"
restore_answers=(global_config.json global_config.json.malformed)
show_menu() {
  if [[ "$1" == "Select configuration to restore" ]]; then
    printf '%s\n' 'global_config.json'
  else
    printf '%s\n' 'global_config.json.malformed'
  fi
}
restore_config >/dev/null || true
[[ "$(<"$HOME/.config/oc-sandbox/global_config.json")" == 'unchanged' ]]

valid_backup="$HOME/.config/oc-sandbox/backups/global_config.json.valid"
printf '%s\n' '{"restored": true}' > "$valid_backup"
restore_answers=(global_config.json global_config.json.valid)
show_menu() {
  if [[ "$1" == "Select configuration to restore" ]]; then
    printf '%s\n' 'global_config.json'
  else
    printf '%s\n' 'global_config.json.valid'
  fi
}
wait_for_enter() { :; }
restore_config >/dev/null
[[ "$(jq -r '.restored' "$HOME/.config/oc-sandbox/global_config.json")" == true ]]
[[ "$(<"$HOME/.config/oc-sandbox/projects.json")" == "$projects_before" ]]
safety_count=0
for backup in "$HOME/.config/oc-sandbox/backups/global_config.json".*; do
  [[ -f "$backup" ]] && safety_count=$((safety_count + 1))
done
[[ "$safety_count" -eq 5 ]]
[[ ! -e "$HOME/.config/oc-sandbox/backups/auth.json" ]]

printf 'start-tui configuration backup and restore tests passed\n'

# Stop Container flow: info page when nothing runs, confirmation gate,
# verified stop with registry update, and failure paths.
stop_home="$test_home/stop-flow"
mkdir -p "$stop_home/bin" "$stop_home/project"
stop_podman_log="$stop_home/podman-args"
stop_state="$stop_home/running"
cat > "$stop_home/bin/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STOP_PODMAN_LOG"
case "$1" in
  ps)
    [[ -f "$STOP_STATE" ]] && cat "$STOP_STATE"
    ;;
  stop)
    [[ "${STOP_FAIL:-false}" == true ]] && exit 1
    [[ "${STOP_STUBBORN:-false}" == true ]] && exit 0
    grep -Fvx -- "$2" "$STOP_STATE" > "$STOP_STATE.tmp" 2>/dev/null || : > "$STOP_STATE.tmp"
    mv "$STOP_STATE.tmp" "$STOP_STATE"
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$stop_home/bin/podman"
export STOP_PODMAN_LOG="$stop_podman_log" STOP_STATE="$stop_state"
PATH="$stop_home/bin:$PATH"
pages_log="$stop_home/pages"
: > "$pages_log"
show_page() { printf 'PAGE: %s\n' "$*" >> "$pages_log"; }
wait_for_enter() { :; }

stop_project="$stop_home/project"
idle_project="$stop_home/idle"
mkdir -p "$idle_project"
add_project_to_registry StopTest "$stop_project" none
add_project_to_registry IdleProject "$idle_project" none
update_project_status "$stop_project" running
update_project_status "$idle_project" stopped
stop_container_name=$(container_name_for_project "$(get_project_by_name StopTest)")

# Nothing running: info page, no podman stop attempted, registry untouched.
: > "$pages_log"; : > "$stop_podman_log"; rm -f "$stop_state"
show_menu() { printf '%s\n' 'Stop'; }
stop_container_menu
grep -q 'PAGE: No running containers' "$pages_log"
! grep -q '^stop ' "$stop_podman_log"
[[ "$(jq -r '.projects[] | select(.name == "StopTest") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == running ]]

# Only the live-running project is offered; the stopped one is filtered out.
selection_menu_log="$stop_home/selection-menu"
: > "$pages_log"; : > "$stop_podman_log"; : > "$selection_menu_log"
printf '%s\n' "$stop_container_name" > "$stop_state"
show_menu() {
  printf '%s\n' "$*" >> "$selection_menu_log"
  if [[ "$1" == "Select container to stop" ]]; then
    printf '%s\n' "● StopTest | $stop_project"
  else
    printf '%s\n' '← Go Back'
  fi
}
stop_container_menu
grep -Fq '● StopTest |' "$selection_menu_log"
! grep -q 'IdleProject' "$selection_menu_log"

# Declining the confirmation leaves the container running.
: > "$pages_log"; : > "$stop_podman_log"
printf '%s\n' "$stop_container_name" > "$stop_state"
show_menu() {
  if [[ "$1" == "Select container to stop" ]]; then
    printf '%s\n' "● StopTest | $stop_project"
  else
    printf '%s\n' '← Go Back'
  fi
}
stop_container_menu
[[ "$(<"$stop_state")" == "$stop_container_name" ]]
! grep -q '^stop ' "$stop_podman_log"
[[ "$(jq -r '.projects[] | select(.name == "StopTest") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == running ]]

# Successful stop: container removed, registry marked stopped, success page.
: > "$pages_log"; : > "$stop_podman_log"
printf '%s\n' "$stop_container_name" > "$stop_state"
show_menu() {
  if [[ "$1" == "Select container to stop" ]]; then
    printf '%s\n' "● StopTest | $stop_project"
  else
    printf '%s\n' 'Stop'
  fi
}
stop_container_menu
[[ ! -s "$stop_state" ]]
grep -Fq "stop $stop_container_name" "$stop_podman_log"
grep -q 'PAGE: Container stopped' "$pages_log"
[[ "$(jq -r '.projects[] | select(.name == "StopTest") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == stopped ]]

# Failed podman stop: error page, registry status unchanged.
: > "$pages_log"; : > "$stop_podman_log"
printf '%s\n' "$stop_container_name" > "$stop_state"
update_project_status "$stop_project" running
STOP_FAIL=true
export STOP_FAIL
stop_container_menu
grep -q 'PAGE: Stop failed' "$pages_log"
[[ "$(<"$stop_state")" == "$stop_container_name" ]]
[[ "$(jq -r '.projects[] | select(.name == "StopTest") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == running ]]
STOP_FAIL=false

# Container survives stop (stubs exit 0 but stays in ps): error page, no status change.
: > "$pages_log"; : > "$stop_podman_log"
printf '%s\n' "$stop_container_name" > "$stop_state"
update_project_status "$stop_project" running
STOP_STUBBORN=true
export STOP_FAIL STOP_STUBBORN
stop_container_menu
grep -q 'PAGE: Stop failed' "$pages_log"
[[ "$(<"$stop_state")" == "$stop_container_name" ]]
[[ "$(jq -r '.projects[] | select(.name == "StopTest") | .container_status' "$HOME/.config/oc-sandbox/projects.json")" == running ]]
STOP_STUBBORN=false
unset STOP_FAIL STOP_STUBBORN

printf 'start-tui stop container tests passed\n'

# Project source step: source selection, URL shape validation with re-prompt,
# name prefill from URL basename, and cloned/empty persistence.
source_home="$test_home/source-step"
mkdir -p "$source_home"
pages_log="$source_home/pages"
: > "$pages_log"
show_page() { printf 'PAGE: %s\n' "$*" >> "$pages_log"; }
wait_for_enter() { :; }

# Go Back from the first step leaves the wizard without further prompts.
name_prompted=0
prompt_for_name() { name_prompted=1; printf '%s\n' 'ShouldNotHappen'; }
show_menu() { printf '%s\n' '← Go Back'; }
init_project_wizard
[[ "$name_prompted" -eq 0 ]]
! grep -q 'PAGE: Invalid' "$pages_log"

# Empty source: wizard proceeds without a prefill (identical to previous flow).
show_menu() { printf '%s\n' 'New empty project'; }
empty_wizard_root="$source_home/empty-root"
mkdir -p "$empty_wizard_root"
captured_prefill="$source_home/prefill-empty"
printf '__unset__\n' > "$captured_prefill"
prompt_for_name() { printf '%s\n' "${2:-}" > "$captured_prefill"; printf '%s\n' 'EmptyProj'; }
prompt_for_path() { printf '%s\n' "$empty_wizard_root"; }
select_container_edition() { captured_edition_args=("$@"); }
init_project_wizard
[[ "$(<"$captured_prefill")" == "" ]]
[[ "${captured_edition_args[0]}" == "$empty_wizard_root" ]]
[[ "${captured_edition_args[1]}" == EmptyProj ]]
[[ "${captured_edition_args[2]}" == "" ]]

# Cloned source: URL prompted, name prefilled from URL basename, editable.
show_menu() { printf '%s\n' 'Clone existing repo via URL'; }
url_answers="$source_home/url-answers"
printf '%s\n' 'https://example.com/re po.git' 'https://example.com/repo.git' > "$url_answers"
prompt_for_text() {
  local head_line
  local rest=()
  IFS= read -r head_line < "$url_answers"
  mapfile -t rest < <(tail -n +2 "$url_answers")
  if [[ ${#rest[@]} -gt 0 ]]; then
    printf '%s\n' "${rest[@]}" > "$url_answers"
  else
    : > "$url_answers"
  fi
  printf '%s\n' "$head_line"
}
cloned_wizard_root="$source_home/cloned-root"
mkdir -p "$cloned_wizard_root"
captured_prefill="$source_home/prefill-cloned"
printf '__unset__\n' > "$captured_prefill"
prompt_for_name() { printf '%s\n' "${2:-}" > "$captured_prefill"; printf '%s\n' 'some_repo'; }
prompt_for_path() { printf '%s\n' "$cloned_wizard_root"; }
init_project_wizard
[[ "$(grep -c 'PAGE: Invalid repo URL' "$pages_log")" -eq 1 ]]
[[ "$(<"$captured_prefill")" == repo ]]
[[ "${captured_edition_args[0]}" == "$cloned_wizard_root" ]]
[[ "${captured_edition_args[1]}" == some_repo ]]
[[ "${captured_edition_args[2]}" == 'https://example.com/repo.git' ]]
[[ ! -s "$url_answers" ]]

# URL shape helper: light check only (non-empty, no spaces), no network probe.
validate_repo_url_shape 'https://github.com/user/repo.git'
validate_repo_url_shape 'git@github.com:user/repo.git'
! validate_repo_url_shape ''
! validate_repo_url_shape 'https://example.com/re po.git'

# Name prefill derivation from URL basename.
[[ "$(derive_project_name_from_repo_url 'https://github.com/user/repo.git')" == repo ]]
[[ "$(derive_project_name_from_repo_url 'https://github.com/user/repo')" == repo ]]
[[ "$(derive_project_name_from_repo_url 'https://gitlab.com/team/my_repo.git/')" == my_repo ]]
[[ "$(derive_project_name_from_repo_url 'git@github.com:user/repo.git')" == repo ]]
[[ "$(derive_project_name_from_repo_url 'ssh://git@host/team/app.git')" == app ]]
[[ "$(derive_project_name_from_repo_url 'git@github.com:app.git')" == app ]]
[[ -z "$(derive_project_name_from_repo_url 'https://host//')" ]]

# VCS tracking derivation from repo URL host (#41): github.com/gitlab.com are
# derived literally from https, scp-like, and ssh forms; other hosts fail so
# the wizard shows the manual picker.
[[ "$(derive_vcs_tracking_from_repo_url 'https://github.com/user/repo.git')" == github.com ]]
[[ "$(derive_vcs_tracking_from_repo_url 'git@github.com:user/repo.git')" == github.com ]]
[[ "$(derive_vcs_tracking_from_repo_url 'ssh://git@github.com/user/repo.git')" == github.com ]]
[[ "$(derive_vcs_tracking_from_repo_url 'https://gitlab.com/team/repo.git')" == gitlab.com ]]
[[ "$(derive_vcs_tracking_from_repo_url 'git@gitlab.com:team/repo.git')" == gitlab.com ]]
[[ "$(derive_vcs_tracking_from_repo_url 'ssh://git@gitlab.com/team/repo.git')" == gitlab.com ]]
! derive_vcs_tracking_from_repo_url 'https://git.example.com/team/repo.git'
! derive_vcs_tracking_from_repo_url 'git@git.example.com:team/repo.git'
! derive_vcs_tracking_from_repo_url ''
! derive_vcs_tracking_from_repo_url 'https://github.com.evil.com/user/repo.git'
[[ "$(derive_vcs_tracking_from_repo_url 'git@GitHub.com:user/repo.git')" == github.com ]]

# Derived tracking skips the VCS picker and reaches select_ai_provider directly.
# re-source to restore the real wizard functions (an earlier stub replaced them).
# shellcheck disable=SC1091
source "$PROJECT_ROOT/dist/scripts/start-tui.sh"
captured_vcs=""
select_ai_provider() { captured_vcs="${*: -1}"; }
select_vcs_tracking /tmp/project Test full 'https://github.com/user/repo.git' console console web
[[ "$captured_vcs" == github.com ]]
select_vcs_tracking /tmp/project Test full 'git@gitlab.com:team/repo.git' console console web
[[ "$captured_vcs" == gitlab.com ]]

# Unknown hosts and empty-source projects keep the manual picker.
derive_vcs_tracking_from_repo_url() { return 1; }
select_ai_provider() { :; }
show_menu() { printf '%s\n' "$1" > "$menu_probe"; printf '%s\n' 'none'; }
menu_probe="$source_home/menu-probe"
: > "$menu_probe"
select_vcs_tracking /tmp/project Test full 'https://git.example.com/team/repo.git' console console web
grep -q 'Select VCS tracking' "$menu_probe"
: > "$menu_probe"
select_vcs_tracking /tmp/project Test full "" console console web
grep -q 'Select VCS tracking' "$menu_probe"
unset menu_probe
# Restore the real wizard functions overridden by the stubs above.
# shellcheck disable=SC1091
source "$PROJECT_ROOT/dist/scripts/start-tui.sh"

printf 'start-tui project source tests passed\n'

# sandbox_config.json records empty/cloned source and repo_url.
source_config_project="$source_home/config-project"
mkdir -p "$source_config_project"
create_sandbox_config "$source_config_project" full console none
source_config="$source_config_project/.opencode_config/sandbox_config.json"
[[ "$(jq -r '.project_source' "$source_config")" == empty ]]
[[ "$(jq -r '.repo_url' "$source_config")" == '' ]]
update_sandbox_config_field "$source_config" "project_source" "cloned"
update_sandbox_config_field "$source_config" "repo_url" 'https://example.com/repo.git'
[[ "$(jq -r '.project_source' "$source_config")" == cloned ]]
[[ "$(jq -r '.repo_url' "$source_config")" == 'https://example.com/repo.git' ]]

# Registry entry carries repo_url; entries without one keep it empty.
source_registry_project="$source_home/registry-project"
mkdir -p "$source_registry_project"
add_project_to_registry SourceRegistry "$source_registry_project" none 'https://example.com/repo.git'
[[ "$(jq -r '.projects[] | select(.name == "SourceRegistry") | .repo_url' "$HOME/.config/oc-sandbox/projects.json")" == 'https://example.com/repo.git' ]]
[[ "$(jq -r '.projects[] | select(.name == "First") | .repo_url' "$HOME/.config/oc-sandbox/projects.json")" == '' ]]

# Cloned projects pass --repo_url to init-project.sh (skips seed + git init).
init_stub_dir="$source_home/init-stub"
mkdir -p "$init_stub_dir"
init_args_log="$source_home/init-args"
cat > "$init_stub_dir/init-project.sh" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" > "$init_args_log"
EOF
chmod +x "$init_stub_dir/init-project.sh"
SCRIPT_DIR="$init_stub_dir"
run_init_project "$source_config_project" --repo_url 'https://example.com/repo.git'
[[ "$(<"$init_args_log")" == "$source_config_project --repo_url https://example.com/repo.git" ]]
run_init_project "$source_config_project"
[[ "$(<"$init_args_log")" == "$source_config_project" ]]
SCRIPT_DIR="$PROJECT_ROOT/dist/scripts"

# select_ai_provider persists source/URL into config and registry for cloned projects.
flow_project="$source_home/flow-project"
mkdir -p "$flow_project"
run_init_project() { :; }
check_and_build_containers() { return 2; }
select_ai_provider "$flow_project" FlowApp full 'https://example.com/repo.git' console console none
[[ "$(jq -r '.project_source' "$flow_project/.opencode_config/sandbox_config.json")" == cloned ]]
[[ "$(jq -r '.repo_url' "$flow_project/.opencode_config/sandbox_config.json")" == 'https://example.com/repo.git' ]]
[[ "$(jq -r '.projects[] | select(.name == "FlowApp") | .repo_url' "$HOME/.config/oc-sandbox/projects.json")" == 'https://example.com/repo.git' ]]

# Empty-source flow records project_source "empty" and no repo_url.
empty_flow_project="$source_home/empty-flow-project"
mkdir -p "$empty_flow_project"
select_ai_provider "$empty_flow_project" EmptyFlow full "" console console none
[[ "$(jq -r '.project_source' "$empty_flow_project/.opencode_config/sandbox_config.json")" == empty ]]
[[ "$(jq -r '.repo_url' "$empty_flow_project/.opencode_config/sandbox_config.json")" == '' ]]
[[ "$(jq -r '.projects[] | select(.name == "EmptyFlow") | .repo_url' "$HOME/.config/oc-sandbox/projects.json")" == '' ]]

printf 'start-tui cloned source persistence tests passed\n'
