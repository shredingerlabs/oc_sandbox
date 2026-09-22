#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUM_BIN="${PROJECT_ROOT}/tests/gum_bin/gum"
PTY_RUNNER="${PROJECT_ROOT}/tests/run-pty.py"

[[ -x "$GUM_BIN" ]] || { printf 'bundled gum is missing\n' >&2; exit 1; }
command -v python3 >/dev/null || { printf 'python3 is required for PTY tests\n' >&2; exit 1; }

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

STUB_BIN="$(mktemp -d)"
STUB_HOME="$(mktemp -d)"
trap 'rm -rf "$STUB_BIN" "$STUB_HOME"' EXIT

cat > "$STUB_BIN/podman" << 'EOF'
#!/usr/bin/env bash
printf 'opencode-sandbox-test1\nopencode-sandbox-test2\n'
EOF
chmod +x "$STUB_BIN/podman"

# 1. Warning screen lists running containers with stop commands; cancel aborts.
warning_result=$(PATH="$STUB_BIN:$PATH" python3 "$PTY_RUNNER" --input $'\033' --submit '' -- bash -c '
  export HOME="$1"
  source "$2/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$3"
  show_deinstallation_warning
' _ "$STUB_HOME" "$PROJECT_ROOT" "$GUM_BIN") || warning_status=$? || true
[[ "${warning_result:-}" == *"podman stop opencode-sandbox-test1"* ]] || fail "warning screen did not show stop command"
[[ "${warning_result:-}" == *"opencode-sandbox-test2"* ]] || fail "warning screen did not list all containers"
[[ "${warning_status:-0}" -ne 0 ]] || fail "warning screen cancel did not abort"

# 2. Options screen: config toggle preserves default symlink selection.
opts_result=$(python3 "$PTY_RUNNER" --input $'\033[B \r' --submit '' -- bash -c '
  export HOME="$1"
  source "$2/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$3"
  remove_symlinks=false remove_config=false create_backup=false
  select_deinstallation_options remove_symlinks remove_config create_backup
  printf "OPTS=%s %s %s\n" "$remove_symlinks" "$remove_config" "$create_backup"
' _ "$STUB_HOME" "$PROJECT_ROOT" "$GUM_BIN")
[[ "$opts_result" == *'OPTS=true true false'* ]] || fail "options screen did not toggle config (got: $opts_result)"

# 3. Backup only enabled when config removal is selected.
backup_blocked=$(python3 "$PTY_RUNNER" --input $'\033[B\033[B \r' --submit '' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$2"
  remove_symlinks=false remove_config=false create_backup=false
  select_deinstallation_options remove_symlinks remove_config create_backup
  printf "OPTS=%s %s %s\n" "$remove_symlinks" "$remove_config" "$create_backup"
' _ "$PROJECT_ROOT" "$GUM_BIN") || true
[[ "$backup_blocked" == *'must be selected'* ]] || fail "backup gating did not show warning"
[[ "$backup_blocked" == *'OPTS=true false false'* ]] || fail "backup not gated behind config removal (got: $backup_blocked)"
backup_allowed=$(python3 "$PTY_RUNNER" --input $'\033[B \033[B \r' --submit '' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$2"
  remove_symlinks=false remove_config=false create_backup=false
  select_deinstallation_options remove_symlinks remove_config create_backup
  printf "OPTS=%s %s %s\n" "$remove_symlinks" "$remove_config" "$create_backup"
' _ "$PROJECT_ROOT" "$GUM_BIN")
[[ "$backup_allowed" == *'OPTS=true true true'* ]] || fail "backup not selectable when config selected (got: $backup_allowed)"

# 4. Summary: wrong confirmation text aborts.
if python3 "$PTY_RUNNER" --input 'deinstall' --submit $'\r' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$2"
  show_deinstallation_summary true true false
' _ "$PROJECT_ROOT" "$GUM_BIN"; then
  fail "summary accepted wrong confirmation text"
fi

# 5. Summary: DEINSTALL confirms; red prompt is requested in gum mode.
confirm_result=$(python3 "$PTY_RUNNER" --input 'DEINSTALL' --submit $'\r' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$2"
  show_deinstallation_summary true true false
  printf "CONFIRMED=%s\n" "$?"
' _ "$PROJECT_ROOT" "$GUM_BIN")
[[ "$confirm_result" == *'CONFIRMED=0'* ]] || fail "summary did not accept DEINSTALL"
[[ "$confirm_result" == *"Type DEINSTALL to confirm"* ]] || fail "summary did not show confirmation prompt"

# 6. Bash fallback: summary reads via bash read.
bash_confirm=$(python3 "$PTY_RUNNER" --input 'DEINSTALL' --submit $'\r' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=bash
  show_deinstallation_summary true true false
  printf "CONFIRMED=%s\n" "$?"
' _ "$PROJECT_ROOT")
[[ "$bash_confirm" == *'CONFIRMED=0'* ]] || fail "bash fallback summary did not accept DEINSTALL"

# 7. Bash fallback: options toggling.
bash_opts=$(python3 "$PTY_RUNNER" --input $'2\n4\n' --submit '' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=bash
  remove_symlinks=false remove_config=false create_backup=false
  select_deinstallation_options remove_symlinks remove_config create_backup
  printf "OPTS=%s %s %s\n" "$remove_symlinks" "$remove_config" "$create_backup"
' _ "$PROJECT_ROOT") || true
[[ "$bash_opts" == *'OPTS=true true false'* ]] || fail "bash fallback options toggling wrong (got: $bash_opts)"

# 8. Options screen: Esc cancels the wizard.
if python3 "$PTY_RUNNER" --input $'\033' --submit '' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$2"
  remove_symlinks=false remove_config=false create_backup=false
  select_deinstallation_options remove_symlinks remove_config create_backup
' _ "$PROJECT_ROOT" "$GUM_BIN"; then
  fail "options screen Esc did not cancel"
fi

# 9. Summary screen: Esc cancels confirmation.
if python3 "$PTY_RUNNER" --input $'\033' --submit '' -- bash -c '
  source "$1/dist/scripts/start-tui.sh"
  TUI_MODE=gum
  GUM_BIN="$2"
  show_deinstallation_summary true true false
' _ "$PROJECT_ROOT" "$GUM_BIN"; then
  fail "summary screen Esc did not cancel"
fi

# 10. Settings menu offers Deinstallation entry.
menu_source="$(mktemp)"
trap 'rm -rf "$STUB_BIN" "$STUB_HOME" "$menu_source"' EXIT
grep -n "Deinstallation" "$PROJECT_ROOT/dist/scripts/start-tui.sh" | grep -q "options=(" || fail "settings menu missing Deinstallation option"

printf 'deinstallation wizard tests passed\n'
