# Desktop shortcuts via --shortcut flag

## Status

Accepted

## Context

Users want to start the TUI from their OS start menu / app launcher instead of typing `oc-sandbox` or running `start-tui.sh` by path. The install script must remain a bash script (Linux, WSL, macOS), and it never uses sudo.

## Decision

The install script gains a `--shortcut` flag (independent of `--symlinks`) that creates one platform-appropriate artifact:

- **Linux**: `~/.local/share/applications/oc-sandbox.desktop` with `Terminal=true`, `Name=OC Sandbox`, absolute-path `Exec` and `Icon`.
- **Windows (from inside WSL)**: `OC Sandbox.lnk` in the current Windows user's Start Menu, created via `powershell.exe` (WScript.Shell COM). The Start-Menu directory is resolved by Windows itself (`[Environment]::GetFolderPath('StartMenu')`), so it is independent of `USERNAME`, the profile path under `/mnt/c/Users`, and locale. The `.lnk` target is `wsl.exe -e bash <install>/scripts/start-tui.sh` in the default distro. `IconLocation` requires a **Windows-native** file: the Start Menu shell cannot render icons from `\\wsl.localhost\...` UNC paths (and tmpfs drive-letter paths from `wslpath -w` are invalid in the Start-Menu context), so the installer copies the `.ico` to `%LOCALAPPDATA%\oc-sandbox\oc-sandbox.ico` and points `IconLocation` there (UNC path as fallback; the icon copy is removed again by `--remove-shortcuts`).
- **macOS**: minimal `~/Applications/OC Sandbox.app` whose stub is an AppleScript wrapper (created via `osascript`) that opens a Terminal window running `start-tui.sh`; `CFBundleIdentifier=io.github.oc-sandbox.tui`; icon as `.icns` in `Contents/Resources/`.

Icons are provided under `dist/icons/` in platform-native formats (`.png`, `.ico`, `.icns`) because bash cannot convert between them; the installer references whichever exists and otherwise omits the icon gracefully (verbose hint).

Shortcut creation failures (no powershell.exe interop, unwritable directory) warn and continue — the core installation is never failed by shortcut creation. On re-install with `--shortcut`, existing artifacts are overwritten silently. Removal is flag-gated: a new uninstall.sh flag (wired to an uninstall-wizard checkbox) removes shortcut artifacts; uninstall does not passively detect them.

Considered alternatives:

- **WSLg `.desktop` entry instead of `.lnk`** — works on modern WSL, but the `.lnk` is distro-independent from the user's perspective and works even where WSLg app menus are unreliable.
- **Bash stub for the macOS `.app`** — pure bash, but Terminal does not reliably open a window for a plain shell-script bundle; the AppleScript wrapper is the standard minimal-`.app` trick.
- **Passive stale-artifact cleanup in uninstall.sh** — rejected to keep uninstall behavior symmetric with the `--symlinks` flag (ADR 0013).

## Consequences

- The installer depends on WSL interop (`powershell.exe`) for Windows shortcuts; without it, installs succeed but shortcuts are skipped with a clear error.
- Icons require per-platform native files in `dist/icons/`; a png alone does not render on Windows or macOS.
- The AppleScript wrapper requires `osascript` (always present on macOS).
