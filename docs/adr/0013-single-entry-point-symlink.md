# Single entry-point symlink

## Status

Accepted

## Context

The install script's `--symlinks` flag previously created one symlink per script in `~/.local/bin` (`start.sh`, `build-container.sh`, `init-project.sh`, `start-tui.sh`, `uninstall.sh`). This cluttered the user's bin directory with internal scripts that users rarely call directly, and the script names (`start-tui.sh` etc.) were not branded as sandbox entry points.

## Decision

With `--symlinks`, the install script creates exactly one symlink:

- `~/.local/bin/oc-sandbox` → `<install_path>/scripts/start-tui.sh`

The TUI is the primary user-facing entry point; all other scripts are internal and invoked via the TUI or by path.

Before creating the symlink, the installer removes all existing symlinks in `~/.local/bin` that point into the install path. This cleans up symlinks left by previous installs (including the old per-script names).

`uninstall.sh` removes `oc-sandbox` plus the legacy per-script names, so older installations are cleaned up correctly.

## Consequences

- `~/.local/bin` contains a single branded command (`oc-sandbox`) instead of five generic script names.
- Users upgrading get stale symlinks removed automatically during reinstall with `--symlinks`.
- `uninstall.sh` keeps the legacy names in its removal list until old installs are no longer relevant.
- Users who called `start.sh` etc. from `~/.local/bin` must switch to `oc-sandbox` or call scripts by path.
