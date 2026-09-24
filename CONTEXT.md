# Context

## Glossary

**MCP server config** — OpenCode uses `mcp` (not `mcpServers`) with `type: "local"` and `command` as an array. Claude Desktop's `mcpServers`/`command`-string/`args` schema is incompatible.

**CBM (codebase-memory-mcp)** — Knowledge-graph indexing tool. Uses `auto_index` and `auto_watch` config (persisted to `.cbm_cache/`) instead of manual entrypoint hooks. UI variant is always installed; `--cbm_ui` flag toggles runtime behavior only.

**TUI (Text-based User Interface)** — Primary user interaction method using gum (https://github.com/charmbracelet/gum) with bash select fallback, providing structured navigation, validation, and project management while maintaining terminal compatibility.

**sandbox_config.json** — Per-project configuration file stored in `<project_root>/.opencode_config/` containing container edition, modes, auto-start options, CBM settings, and setup completion status.

**projects.json** — Global project registry stored in `$HOME/.config/oc-sandbox/` with project metadata including project names, paths, container states, and last-used timestamps for ordering.

**global_config.json** — Global user preferences stored in `$HOME/.config/oc-sandbox/` containing default project paths and user-specific settings not tied to individual projects.

**concurrent containers** — Multiple project containers can run simultaneously using project-specific naming (opencode-sandbox-PROJECTNAME), with TUI using podman exec for accessing running containers instead of starting new ones.

**first-run setup** — Automated container initialization including CBM configuration and skills setup, tracked via setup-complete flag in sandbox_config.json, offering granular recovery for partial failures.

**VCS integration** — Version Control System setup (GitHub, GitLab, or custom host) creating credentials/hosts.yml in `.git_local/` subdirectories, separate from AI provider configuration.

**AI provider** — LLM API service configuration like GWDG, stored in auth.json within `.opencode_data/` with provider sections for OpenCode integration.

**install script** — Script that downloads and sets up the opencode-sandbox files to a designated installation directory.

**installation folder** — Directory where opencode-sandbox files are stored, defaulting to `$HOME/.oc-sandbox` but configurable via `--install_path`.

**bash one-liner** — Single bash command that downloads and executes the install script from GitHub.

**latest release** — The most recent tagged release on GitHub, used as the default version for installation.

**update mode** — Installation behavior when the installation folder already exists; prompts user to either override existing files or skip.

**version flag** — Optional `--version` parameter to install a specific release tag instead of latest.

**github API fallback** — Uses GitHub releases API to detect latest release; falls back to HTML scraping if API fails.

**git clone method** — Uses `git clone --depth 1 --branch <tag>` for clean repository handling.

**fallback method** — If git is not available, downloads release tarball from GitHub as alternative.

**cleanup** — Removes `.git` directory after installation to save space and prevent accidental git operations.

**executable permissions** — Automatically sets executable permissions on all `.sh` files during installation.

**update prompt** — When installation folder exists, prompts user with [y/N] (default no) to confirm update.

**temporary directory** — Uses system temp directory (`/tmp` or `$TMPDIR`) for downloads and extraction; cleans up after installation.

**verbose output** — `--verbose` flag enables detailed logging; default is minimal progress information.

**permissions** — Never uses sudo automatically; fails with clear error messages on permission issues.

**network retry** — Retries network operations automatically 3 times with exponential backoff before failing.

**disk space check** — Validates minimum 500MB available disk space before starting installation.

**symlinks** — Optional `--symlinks` flag creates a single symlink `oc-sandbox` → `scripts/start-tui.sh` in `$HOME/.local/bin` for easier command access.

**color output** — Install script output uses plain text only for maximum compatibility. The TUI may use color through gum (e.g. the red `UNINSTALL` confirmation prompt), with plain-text fallback.

**exit codes** — Standard exit codes: 0 (success), 1 (general error), 2 (user abort), 3 (missing dependencies).

**configuration preservation** — During updates, asks before overwriting `proxy/allowlist.txt` with option to skip; overwrites all other files.

**shell compatibility** — Written for bash specifically (`#!/usr/bin/env bash`) for cleaner syntax and better feature support.

**dependency checking** — Checks for required tools: curl or wget, tar, grep, awk, sed; fails with clear error if any missing.

**user prompts** — Uses basic `read -p` prompts with explicit Enter key instructions.

**help documentation** — Supports `--help` flag showing usage, available flags, and examples.

**curl fallback** — Bash one-liner prefers `curl` but falls back to `wget` if curl is not available.

**script execution** — Bash one-liner uses pipe directly (`curl ... | bash`) for simplicity; security implications documented.

**installation validation** — Validates key files exist and are executable after installation; reports issues if found.

**progress indicators** — Uses simple status messages during operations; no progress bars or spinners.

**signal handling** — Handles SIGINT (Ctrl+C) gracefully to clean up temporary files before exiting.

**symlink creation** — Creates a single entry-point symlink `oc-sandbox` → `scripts/start-tui.sh` when `--symlinks` flag is used; removes stale symlinks in `$HOME/.local/bin` pointing into the install path first.

**symlink overwriting** — Overwrites existing symlinks without prompting when using `--symlinks` flag.

**temporary file naming** — Uses random names with `mktemp` for temporary files to avoid conflicts.

**error message paths** — Uses absolute paths for all error messages to avoid confusion.

**logging** — Only outputs to stdout/stderr; no log files created during installation.

**github API authentication** — Stays unauthenticated for simplicity; handles rate limit failures gracefully with fallback to HTML scraping.

**user agent** — Sets a simple user agent identifying the opencode-sandbox install script for GitHub API requests.

**version comparison** — Always prompts user for update regardless of version comparison; no automatic update detection.

**special file handling** — Only `proxy/allowlist.txt` gets special treatment during updates; all other files are overwritten.

**post-installation message** — Shows brief reminder about system requirements (podman, etc.) and suggests next steps after successful installation.

**setup-complete marker** — Boolean flag in sandbox_config.json tracking whether first-run setup (CBM configuration and skills setup) has been completed for a project.

**atomic config write** — Configuration update method using temporary files and atomic rename operations to prevent corruption during crashes, with automatic backup creation.

**runtime detection** — Dynamic parsing of script help text (build-container.sh, start.sh) to discover available container editions and modes, avoiding hardcoded lists and maintaining flexibility for future script enhancements.

**native script enhancement** — Strategy of extending existing scripts (start.sh, build-container.sh, init-project.sh) with additional parameters (like --start_opencode) rather than creating parallel TUI-specific implementations, preserving backward compatibility and avoiding code duplication.

**project display name** — Unique user-facing name for a registered project, used in TUI menus and registry selection.

**container identity** — Stable project-specific identifier derived from the canonical project root, persisted with the project, and used to avoid container-name collisions.

**deferred build** — Registered project state in which the container image is not yet built; the project remains stopped until a later build and start.

**setup recovery** — Explicit retry path for incomplete first-run setup while preserving setup-complete as false until CBM and skills setup both succeed.

**credential file** — Project-local VCS or AI secret configuration stored separately from global TUI metadata, with restrictive permissions and excluded from general configuration backups.

**retry flow** — Error recovery pattern where users who choose "Retry" after a failure remain in the recovery loop even if intermediate steps (like settings adjustment) fail. Failures show context-aware error messages and return to the retry menu, except for explicit user aborts (exit code 2) which are respected throughout.

**uninstall wizard** — Guided TUI flow in Settings (warning screen with running containers and their stop commands, option checkboxes for symlinks/config/backup, summary screen). Every screen can be cancelled or navigated back; the choices are mapped to uninstall.sh flags and run with `--force`.

**UNINSTALL confirmation** — Typed text confirmation required on the uninstall summary before anything is removed; rendered as a red gum input when gum is available, plain `read` fallback otherwise.

**web start option** — Launch style that runs the project container as a web server (`opencode web --port 4096`). Selectable at project creation and in settings; the server runs from the first start (first-run setup executes alongside via exec). Following the detached+exec start architecture, the detached container runs the web server as its main process, the host port is auto-scanned upward from 4096 (like the CBM-UI range), and the actual URL is printed rather than opening a browser. The server is stopped via normal container stop. Direct CLI usage of start.sh with `--start_web` and without `--detach` runs the server foreground, where Ctrl+C stops and removes the container (`--rm`). Implemented via the `--start_web` flag on start.sh, mirroring `--start_opencode`.

**console session** — Interactive bash shell attached to a running container via `podman exec -it --user dev <container> bash`, chosen by the TUI at access time. Independent of the configured start option (launch behavior) and of any other session already inside the container.

**squid allowlist scope** — The squid egress proxy governs outbound traffic from inside the container only. Host-browser access to published container ports (web UI, CBM UI) never traverses squid, so allowlist entries are never needed for ingress.

**skipped files** — Files that could not be removed during uninstallation because of missing permissions; tracked during removal and reported at the end with a hint to clean them up manually (e.g. with sudo).

**desktop shortcut** — OS menu launcher for the TUI created by the install script on request; exactly one platform-appropriate artifact per OS: a `.desktop` file named `oc-sandbox.desktop` with `Terminal=true` under `$HOME/.local/share/applications/` (Linux), a `.lnk` named `OC Sandbox.lnk` in the current Windows user's Start Menu written from inside WSL (Windows), or a minimal `.app` bundle named `OC Sandbox.app` in `~/Applications` (macOS). Display name is `OC Sandbox` on all platforms. Independent of symlinks.

**shortcut flag** — Optional `--shortcut` parameter on the install script; creates the desktop shortcut for the detected platform and overwrites an existing artifact silently on re-install.

**shortcut removal** — Uninstall path for desktop shortcuts, offered as a wizard checkbox mapped to an uninstall.sh flag; removes shortcut artifacts pointing into the installation folder.

**shortcut icon** — Icon material for the desktop shortcut, provided under `dist/icons/` in platform subfolders (`linux/`, `windows/`, `macos/`) and processed at shortcut creation time: Linux installs the bundled hicolor PNG set into `$HOME/.local/share/icons/hicolor/` and the `.desktop` file references it by bare theme name (`Icon=oc-sandbox`, no path); Windows points the `.lnk` at `icons/windows/oc-sandbox.ico` via absolute (translated) path; macOS generates the `.icns` from `icons/macos/oc-sandbox.iconset` with `iconutil` at install time and embeds it in the `.app` bundle. Missing icons never fail the shortcut — it is created without one.

**hicolor icon install** — Linux shortcut step copying `icons/linux/share/icons/hicolor/` into `$HOME/.local/share/icons/hicolor/` so the desktop entry resolves `Icon=oc-sandbox` through the icon theme. Best-effort: failures fall back to an icon-less shortcut.

**icns generation** — macOS shortcut step converting the bundled `.iconset` to `AppIcon.icns` using `iconutil` (ships with macOS); on failure the `.app` is created without an icon.
