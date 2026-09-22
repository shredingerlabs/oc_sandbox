# Enhanced Deinstallation Implementation Spec

## Overview

Implement enhanced deinstallation behavior with CLI flags and TUI integration per ADR 0012.

## Part 1: CLI (uninstall.sh)

### New Global Variables

```bash
REMOVE_SYMLINKS=true
REMOVE_CONFIG=false
BACKUP_CONFIG_ONLY=false  # Only backup config, not INSTALL_PATH
CONFIG_BACKUP_DIR="$HOME/.config/oc-sandbox/backups"
CONFIG_BACKUP_LIMIT=5
SKIPPED_FILES=()  # Track permission-denied files
```

### New Functions

#### `backup_config_directory()`
- Creates backup of `~/.config/oc-sandbox/` to `~/.config/oc-sandbox/backups/config-<timestamp>/`
- Excludes the `backups/` subdirectory itself
- Returns 0 on success, 1 on failure
- Called before config removal if `--remove-config` is set

#### `rotate_config_backups()`
- Lists all `config-*` backups in `CONFIG_BACKUP_DIR`
- Keeps only the 5 most recent (by timestamp)
- Removes older backups
- Called after creating new backup

#### `remove_config_directory()`
- Removes `~/.config/oc-sandbox/` except `backups/` subdirectory
- Tracks permission-denied files in `SKIPPED_FILES`
- Reports skipped files at end

#### `remove_symlinks()` (enhanced)
- Already exists, now respects `REMOVE_SYMLINKS` flag
- Returns early if flag is false

#### `perform_removal()` (modified)
- New flow:
  1. Check running containers (existing)
  2. Detect project roots (existing)
  3. Backup config if `--remove-config` and backup enabled (new)
  4. Confirm removal (existing, enhanced with config info)
  5. Remove INSTALL_PATH (existing)
  6. Remove symlinks if flag set (enhanced)
  7. Remove config if flag set (new)
  8. Report skipped files (new)
  9. Show completion message (enhanced)

### Modified Argument Parsing

Add cases for:
```bash
--no-symlinks) REMOVE_SYMLINKS=false; shift ;;
--remove-config) REMOVE_CONFIG=true; shift ;;
--no-backup) BACKUP_CONFIG=false; shift ;;
```

### Help Text Updates

Add new options:
```
--no-symlinks        Keep symlinks in ~/.local/bin/ (default: remove)
--remove-config      Remove ~/.config/oc-sandbox/ directory
--no-backup          Disable config backup (with --remove-config)
```

## Part 2: TUI Integration (start-tui.sh)

### New Function: `deinstallation_wizard()`

Main entry point, called from Settings menu.

**Flow:**
```bash
deinstallation_wizard() {
  if ! show_deinstallation_warning; then
    return  # User cancelled
  fi
  
  local options=$(select_deinstallation_options)
  if [[ -z "$options" ]]; then
    return  # User cancelled
  fi
  
  if ! show_deinstallation_summary "$options"; then
    return  # User cancelled or wrong confirmation
  fi
  
  run_deinstallation "$options"
}
```

### `show_deinstallation_warning()`

**UI:**
```
⚠️  Deinstallation Warning

This will permanently remove oc-sandbox from your system.

Running containers found:
  - opencode-sandbox-project1
  - opencode-sandbox-project2

Stop containers before deinstallation:
  podman stop opencode-sandbox-project1
  podman stop opencode-sandbox-project2

[Continue] [Cancel]
```

**Implementation:**
- Call `get_running_containers()` to list running containers
- Show podman stop commands for each
- Use `show_menu()` with Continue/Cancel
- Return 1 if Cancel selected

### `select_deinstallation_options()`

**UI:**
```
Select what to remove:

☑ Remove symlinks from ~/.local/bin/
  Deletes: build-container.sh, init-project.sh, start.sh, etc.

☐ Remove user config files (~/.config/oc-sandbox/)
  Deletes: projects.json, global_config.json, backups/

☑ Create backup of config before removal
  Backup location: ~/.config/oc-sandbox/backups/config-<timestamp>/
  Keeps last 5 backups

[Continue] [Back]
```

**Implementation:**
- Use gum choose with multiple selections (or bash select loop)
- "Remove config" must be checked to enable "Create backup"
- Default: symlinks=checked, config=unchecked, backup=checked
- Return selected options as string

### `show_deinstallation_summary()`

**UI:**
```
Summary - The following will be REMOVED:

Installation directory:
  /home/user/.oc-sandbox/

Symlinks:
  5 symlinks from ~/.local/bin/

Config files:
  ~/.config/oc-sandbox/ (excluding backups/)

Config backup will be created:
  ~/.config/oc-sandbox/backups/config-20260907_143022/

Type "DEINSTALL" to confirm:
[________________________________]

[Confirm] [Back]
```

**Implementation:**
- Show INSTALL_PATH always
- Show symlinks count if option selected
- Show config path if option selected
- Show backup path if backup enabled
- Use gum input (red color) or bash read for confirmation
- Validate input == "DEINSTALL"
- Return 1 if wrong or cancelled

### `run_deinstallation()`

**Implementation:**
- Call `uninstall.sh` with appropriate flags based on options
- Show progress during execution
- Capture and display output
- Show completion message
- Handle errors gracefully

**Flags mapping:**
```bash
local flags=()
[[ "$REMOVE_SYMLINKS" == "true" ]] || flags+=("--no-symlinks")
[[ "$REMOVE_CONFIG" == "true" ]] && flags+=("--remove-config")
[[ "$BACKUP_CONFIG" == "false" ]] && flags+=("--no-backup")
```

### Modified `settings_menu()`

**Before:**
```bash
options=("Config Backup" "Config Restore" "← Back to Main Menu")
```

**After:**
```bash
options=("Config Backup" "Config Restore" "Deinstallation" "← Back to Main Menu")
```

**Case handler:**
```bash
"Deinstallation")
  deinstallation_wizard
  ;;
```

## Part 3: Testing

### Unit Tests (uninstall.sh)

```bash
test_remove_config_flag()
test_backup_config_rotation()
test_no_symlinks_flag()
test_dry_run_with_config()
test_permission_error_handling()
test_running_container_detection()
```

### Integration Tests (TUI)

```bash
test_deinstallation_wizard_flow()
test_deinstallation_cancel_at_warning()
test_deinstallation_cancel_at_options()
test_deinstallation_wrong_confirmation()
test_deinstallation_success()
test_deinstallation_with_config_removal()
```

## File Changes

**Modified:**
- `dist/scripts/uninstall.sh` (~100 lines added/modified)
- `dist/scripts/start-tui.sh` (~200 lines added)

**New:**
- `tests/test-uninstall.sh` (new test file)
- `tests/test-tui-deinstallation.sh` (new test file)

## Acceptance Criteria

- [ ] `uninstall.sh --remove-config` removes config and creates backup
- [ ] `uninstall.sh --no-symlinks` keeps symlinks
- [ ] `uninstall.sh --no-backup` skips backup creation
- [ ] Config backup rotation keeps only 5 most recent
- [ ] Permission errors are reported but don't abort
- [ ] TUI Settings menu shows "Deinstallation" option
- [ ] TUI wizard shows running containers with stop commands
- [ ] TUI wizard requires text confirmation "DEINSTALL"
- [ ] TUI wizard passes correct flags to uninstall.sh
- [ ] Running containers are detected and warned about
- [ ] Dry-run mode shows what would be removed
