# Deinstallation routine

## Status

Accepted

## Context

A standalone `uninstall.sh` script in `dist/scripts/` provides safe, interactive removal of the opencode-sandbox installation. The original implementation removed only the installation directory and symlinks, but lacked flexibility for config file management and TUI integration.

Users need:
1. **Config file handling**: Option to remove user config files (`~/.config/oc-sandbox/`) with automatic backup
2. **TUI integration**: Guided deinstallation process through the Settings menu
3. **Granular control**: Flags to control what gets removed (symlinks, config, backup)
4. **Safety**: Running container detection, text confirmation, backup rotation

## Decision

**The deinstaller removes:**
- Entire `$INSTALL_PATH` (default `$HOME/.oc-sandbox`) - always removed
- Symlinks from `~/.local/bin/` (configurable via `--no-symlinks`, default: remove)
- User config files `~/.config/oc-sandbox/` (configurable via `--remove-config`, default: false)
- Config backup created before removal (automatic when `--remove-config` is set, disable with `--no-backup`)

**Safety features:**
- Running container detection with warnings and stop commands
- Text confirmation ("DEINSTALL") in TUI wizard
- Explicit confirmation in CLI mode (unless `--force` used)
- Backup rotation: keep last 5 config backups
- Permission errors: skip affected files, report to user at end

**CLI flags:**
- `--install_path <pfad>` - Custom installation path
- `--no-symlinks` - Keep symlinks in ~/.local/bin/ (default: remove)
- `--remove-config` - Remove ~/.config/oc-sandbox/ directory
- `--no-backup` - Disable config backup (only relevant with `--remove-config`)
- `--force` - Non-interactive mode, skip confirmations
- `--dry-run` - Show what would be removed without deleting
- `--verbose` - Detailed output
- `--help` - Show help text

**TUI Integration:**
- Settings menu → "Deinstallation" option (direct, not submenu)
- Multi-screen wizard:
  1. **Warning**: Lists running containers, shows stop commands
  2. **Options**: Checkboxes for symlinks/config/backup with descriptions
  3. **Summary**: Shows what will be removed, text confirmation input ("DEINSTALL")
  4. **Progress**: Step-by-step removal with status
  5. **Completion**: Summary and manual cleanup reminders

**Config backup:**
- Location: `~/.config/oc-sandbox/backups/config-<timestamp>/`
- Rotation: Keep last 5 backups (separate from TUI backup rotation)
- Does NOT remove the `~/.config/oc-sandbox/backups/` folder itself

**Error handling:**
- Config backup fails: Ask user (abort/continue)
- Partial removal: No recovery mechanism, user manually cleans up
- Permission denied: Skip files, list them at end for manual removal

## Considered Options

**No deinstaller**: Users manually `rm -rf` their installation. Simple, but error-prone and doesn't clean up symlinks or warn about running containers.

**Install.sh with --uninstall flag**: Single script for install and uninstall. Tight coupling; the install script is already 750+ lines.

**Standalone uninstall.sh** (chosen): Separate script in `dist/scripts/`, invoked from TUI or command line. Clear separation of concerns, easier to test independently, TUI can call it directly.

**Config removal strategy**:
- Always remove config: Too destructive, users might want to keep settings
- Never remove config: Leaves orphaned config files, inconsistent state
- Optional with backup (chosen): Gives users control while protecting against accidents

## Consequences

### Positive
- Users get fine-grained control over deinstallation scope
- Config files are safely backed up before removal
- TUI provides guided, safe deinstallation experience
- Running containers are detected and warned about
- Backup rotation prevents disk space issues
- Permission errors don't block entire process
- Backward compatible: existing behavior unchanged when no new flags used

### Negative
- Script complexity increases (more flags, more logic paths)
- TUI wizard adds ~150-200 lines to start-tui.sh
- Need to test multiple flag combinations
- Users might accidentally remove config they wanted to keep (mitigated by backup)

### Neutral
- Config backup rotation is separate from TUI backup rotation (different code paths)
- Text confirmation ("DEINSTALL") adds friction but prevents accidents
- Symlinks removal is now optional (default: remove)
- Script lives in `dist/` so it's available in releases and can be called from the TUI

## Implementation Notes

**uninstall.sh changes:**
- Add `REMOVE_SYMLINKS=true`, `REMOVE_CONFIG=false` flags
- Add `backup_config_directory()` function (separate from current `create_backup()`)
- Add `rotate_config_backups()` function (keeps last 5)
- Modify `perform_removal()` to handle config removal
- Add permission error tracking and reporting
- Update help text with new flags

**start-tui.sh changes:**
- Add `deinstallation_wizard()` function
- Add `show_deinstallation_warning()` screen
- Add `select_deinstallation_options()` screen (checkboxes)
- Add `show_deinstallation_summary()` screen (text confirmation)
- Add `run_deinstallation()` caller
- Update `settings_menu()` to include "Deinstallation" option

**Testing:**
- Test all flag combinations
- Test TUI wizard flow (gum + bash fallback)
- Test backup creation and rotation
- Test permission error handling
- Test running container detection
- Test dry-run mode

## Migration Path

Existing users calling `uninstall.sh` without flags get:
- Same behavior as before (removes `$INSTALL_PATH` and symlinks)
- No config removal (backward compatible)
- Backup still created for INSTALL_PATH (current behavior)

To enable new behavior, users must explicitly:
- `--remove-config` to remove config files
- `--no-symlinks` to keep symlinks
- `--no-backup` to skip backup

## Dependencies

- gum (optional, for TUI wizard)
- bash 4.x (indexed arrays for tracking; namerefs in the TUI options screen)
- jq (used by the TUI for config restore; uninstall.sh parses projects.json paths with grep)
