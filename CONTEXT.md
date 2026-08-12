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

**VCS integration** — Version Control System setup (GitHub/GitLab) creating credentials/hosts.yml in `.git_local/` subdirectories, separate from AI provider configuration.

**AI provider** — LLM API service configuration like GWDG, stored in auth.json within `.opencode_data/` with provider sections for OpenCode integration.

**install script** — Script that downloads and sets up the opencode-sandbox files to a designated installation directory.

**installation folder** — Directory where opencode-sandbox files are stored, defaulting to `$HOME/.opencode_sandbox` but configurable via `--install_path`.

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

**symlinks** — Optional `--symlinks` flag creates symlinks in `$HOME/.local/bin` for easier command access.

**color output** — Uses plain text output only for maximum compatibility; no colored output.

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

**symlink creation** — Creates symlinks for all executable scripts from `scripts/` directory when `--symlinks` flag is used.

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
