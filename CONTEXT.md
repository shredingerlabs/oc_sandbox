# Context

## Glossary

**MCP server config** — OpenCode (v2, ADR 0025) uses `mcp.servers.<name>` with `type: "local"` and `command` as an array. The v1 flat `mcp` map is migrated at project start; legacy `enabled: true` is dropped (`disabled: false` is default). Claude Desktop's `mcpServers`/`command`-string/`args` schema is incompatible.

**CBM (codebase-memory-mcp)** — Knowledge-graph indexing tool. Uses `auto_index` and `auto_watch` config (persisted to `.cbm_cache/`) instead of manual entrypoint hooks. UI variant is always installed; `--cbm_ui` flag toggles runtime behavior only.

**TUI (Text-based User Interface)** — Primary user interaction method: the single Go binary `oc-sandbox` (BubbleTea), providing structured navigation, validation, and project management while maintaining terminal compatibility. The former gum/bash TUI (`start-tui.sh`) was deleted after the parity-gate flip (#98, cleanup #99).

**sandbox_config.json** — Per-project configuration file stored in `<project_root>/.opencode_config/` containing container edition, modes, auto-start options, CBM settings, and setup completion status.

**project source** — Where a new project's code comes from: "new empty project" (wizard-created scaffolding) or "clone existing repo via URL" (in-container clone during first-run setup). Avoid: import repo, repo migration, connect repo.

**project kind** — What a project is for, chosen by the New Project wizard's Agent row (identical for both project sources): `code` (default, standard agent rules), `chat` — the "Chat Agent (NO_CODE)" option for a non-code project that behaves like plain AI chat — or `pkm` — the "PKM (Personal Knowledge Wiki)" option, which additionally pins the edition and seeds the PKM templates (see pkm edition, ADR 0027). Decided and recorded once at creation (`project_kind` in sandbox_config.json, absent reads as code); never re-applied or offered in the revisit sequence — later changes are a hand edit of `.opencode_config/AGENTS.md`. Avoid: nocode project, agent template, NO_CODE project.

**pkm edition** — Container edition for Personal Knowledge Wiki work (`opencode-sandbox-pkm` stage in dist/Dockerfile, building from base like every shipped edition): no additional installs, only the `ENTRYPOINT ["/bin/bash", "-l"]` that base lacks. Handled like every other edition everywhere (runtime detection, Build Images / Delete Images, build plan, start, edition mismatch); selectable by the TUI only implicitly — when a new project's project kind is `pkm`, the Edition row is hidden and `container_edition: "pkm"` is forced at creation. Edition switches later (revisit sequence, next-start apply) remain allowed in both directions, but the PKM seeding — `AGENTS.md` (the wiki schema, overriding the code/chat swap), `agents/`/`commands/`/`skills/` templates, `default_agent` + `permissions` appended into opencode.json, pkm `.gitignore` entries appended with dedupe, and the `notes/`/`raw/`/`wiki/` vault skeleton in `project/` — happens only at creation; switching to pkm later yields the image, never a re-seed. Templates also seed over anything an in-container clone brought (same as chat). Avoid: PKM project type (it is a project kind, not a separate type axis), wiki edition.

**repo URL** — The git remote URL recorded at project creation for a `cloned` project; stored in sandbox_config.json and the project registry, and used to derive git tracking and re-check repo reachability. _Avoid_: repo link, clone URL (ambiguous with the act of cloning).

**in-container clone** — Cloning the repo URL inside the project container during first-run setup, after container start and before CBM configuration, using the container-side VCS credentials; chosen so host machines need no git/SSH setup. _Avoid_: host clone, URL import.

**project import** — TUI flow (Settings → Import existing project) that registers an existing folder as a project *only when it is a sandbox project root* (valid `sandbox_config.json` plus a `project/` git worktree; missing infra directories like `.cbm_cache` are no obstacle — start.sh re-creates them at every container start); a summary screen confirms it and only the registry changes. Non-matching or broken-config folders are never migrated/copied — the flow shows an informational prompt and offers a jump into the New Project wizard, where code comes in via the ordinary new-project sources. An already-registered path hits the registered guard regardless of its folder's state. There is no "import" project source; `project/` content of a matching root is left untouched. _Avoid_: folder migration, import repo.

**git init heal** — Host-side `git init` performed at container start when `project/` has no `.git`, guaranteeing OpenCode-Web always finds a worktree; skipped for projects whose project source is `cloned`.

**projects.json** — Global project registry stored in `$HOME/.config/oc-sandbox/` with project metadata including project names, paths, container states, and last-used timestamps for ordering.

**global_config.json** — Global user preferences stored in `$HOME/.config/oc-sandbox/` containing default project paths and user-specific settings not tied to individual projects.

**concurrent containers** — Multiple project containers can run simultaneously using project-specific naming (opencode-sandbox-PROJECTNAME), with TUI using podman exec for accessing running containers instead of starting new ones.

**stop container** — Graceful shutdown of a running project container via `podman stop` (SIGTERM, then SIGKILL after the default timeout), initiated from the TUI Settings menu for a single selected project. Because containers run with `--rm`, a stopped container is removed automatically; the registry's container_status is set to "stopped" only after the container is verified gone.

**start container (Container center)** — Starting a registered project's container from the Container center's Start tab (distinct from the Open Project flow): start.sh runs with the values stored in sandbox_config.json (edition, modes, start_option, proxy, `--detach`; identical arg rebuild to the Open Project start). Already-running containers are reused, not restarted. The registry's container_status is set to "running" only after every started container is verified live via `podman ps`. _Avoid_: restart (a running container is never killed/replaced).

**delete container** — Removing an existing oc-sandbox container from the Container center's Delete tab; the checklist lists every existing container in any state (`podman ps -a` filtered by the `opencode-sandbox-` naming convention), including exited/created leftovers of gone projects, with the project name echoed for registered ones. Running containers are refused with a stop-first hint (`podman stop <name>`) — deleting a running container would be an ungraceful kill. Removal runs `podman rm` (no force needed for non-running), verifies gone against a fresh `podman ps -a` before any registry touch, and then only flips a stale container_status (`running` → `stopped`). It frees the container's writable layer only (real data lives in host bind-mounts; images are untouched). _Avoid_: force delete, image delete.

**build plan (New Project)** — Recorded Build now / Build later radio choice of the New Project form: while the selected edition's image is missing, an inline "Image build" row sits directly under the Editions row (image-existence memoized per edition for row visibility; create re-checks fresh). The create confirm popup lists it ("image build: …") and the Create button executes it: Build now runs build-container.sh, revalidates the image, then proceeds; Build later registers the project stopped (deferred build). While the create chain runs (build, init, credentials), the New Project view streams every step into an embedded log pane below the form card — the form stays visible but inert until the chain ends; the pane stays readable after a failure. Replaces the former create-time Build now / Build later / Go back prompt (Go back dropped — the form itself is home); the start-time prompt keeps its Go back. _Avoid_: build prompt (start-time prompt unchanged), build wizard.

**first-run setup** — Automated container initialization including in-container probe (`git ls-remote`) and full clone for cloned projects, then CBM configuration and skills setup, tracked via `setup_clone_complete`, `setup_cbm_complete`, and `setup_skills_complete` flags in sandbox_config.json, offering granular recovery for partial failures including Retry / Change URL / Exit for clone issues.

**VCS integration** — Version Control System setup (GitHub, GitLab, or custom host) creating host token files (`gh-cli/hosts.yml`, `glab-cli/config.yml` with a `hosts:` block — glab ignores a separate `hosts.yml`, or `vcs/hosts.yml`) in `.git_local/` subdirectories, separate from AI provider configuration.

**AI provider** — The OpenCode model backend available to a project, managed as provider entries inside the project's own config files; which provider's models are used is chosen inside OpenCode at runtime, not by the TUI. A configured provider is an `@opencode/ai/providers/openai-compatible` section in `opencode.json` (v2 `providers.<id>`, ADR 0025) with its credential in the per-provider token file under `.opencode_data/credentials/`. The sandbox_config.json `ai_provider` field is legacy: creation still records it, but the project edit flows neither read nor write it. _Avoid_: GWDG preset (removed; `gwdg-saia` survives as a legacy entry that coexists with others; entries lacking a credential render read-only with an orphan hint), vendor preset, assigned provider.

**custom provider** — Name / Endpoint / Token captured as one block (wizard at creation, Edit Project settings screen for edits). Name is display name and id source; Endpoint is the OpenAI-compatible baseURL; Token is the API key. A project may hold several at the same time. Stored without any vendor preset. _Avoid_: GWDG flow, preset provider.

**provider entry** — One configured provider inside a project: the `providers.<id>` section in opencode.json (v2; the legacy `provider.<id>` shape is read for pre-migration display only, ADR 0025) plus its matching credential file. The smallest manageable unit of AI provider configuration; add, edit and remove act on entries, never across projects. _Avoid_: provider record (ambiguous with the legacy sandbox_config entry), active provider.

**provider list** — The set of provider entries of the currently selected project, shown by the Edit Project settings screen; defined entirely by opencode.json. Not a global or cross-project registry. _Avoid_: global provider list, provider registry.

**provider id** — Slugified key derived from the typed provider name (lowercase, non-alphanumeric runs → `-`, trimmed) used as the credential-file name under `.opencode_data/credentials/` and the `providers.<id>` section key in `opencode.json`. Renaming an entry moves its id (the old section is replaced by the slugified new one). Legacy sandbox_config values may reference `gwdg-saia`. _Avoid_: provider name (the display name is separate), auth key.

**provider endpoint** — The OpenAI-compatible API base URL (scheme `http://`/`https://`, no whitespace, path allowed) offered by a custom provider, mapped to OpenCode `settings.baseURL` (v2). _Avoid_: provider URL (ambiguity with the recorded provider id), host (no scheme/path implied).

**v2 config migration** — One-time in-place rewrite of a project's v1-shaped `opencode.json` (flat `mcp`, `permission` map, `plugin`/`provider` keys) to the OpenCode v2 schema, plus extraction of legacy `auth.json` tokens into per-provider credential files (the old file is renamed `auth.json.v1-migrated`, never deleted). Idempotent; runs before every container start and lazily before each provider subcommand, so a routine image rebuild restores providers instead of silently dropping them. _Avoid_: config upgrade (reads as general TUI updates), re-import.

**credential file** — Project-local VCS or AI secret configuration stored separately from global TUI metadata, with restrictive permissions and excluded from general configuration backups. AI provider tokens live one file per provider id under `.opencode_data/credentials/` and are referenced from `opencode.json` via the v2 `{file:...}` substitution (ADR 0025), not in `auth.json`.

**install script** — Script that downloads and sets up the opencode-sandbox files to a designated installation directory.

**installation folder** — Directory where opencode-sandbox files are stored, defaulting to `$HOME/.oc-sandbox` but configurable via `--install_path`.

**bash one-liner** — Single bash command that downloads and executes the install script from GitHub.

**latest release** — The most recent tagged release on GitHub, used as the default version for installation.

**update mode** — Installation behavior when the installation folder already exists; interactively prompts [y/N] to confirm the update, or runs non-interactively when the caller pre-confirms (TUI-invoked updates).

**version flag** — Optional `--version` parameter to install a specific release tag instead of latest.

**VERSION file** — Install-folder marker holding the installed release tag, rewritten by the install script on every install and update; the version shown in the update menu comes from it, falling back to the binary's compiled version when absent. _Avoid_: version number, release tag (the tag value itself).

**update check** — TUI-side background query of the latest GitHub release, run at every startup and compared against the installed version; failure (offline, rate limit) is silent and leaves no badge, and the result is never persisted between runs. _Avoid_: auto-update (nothing is installed automatically), update detection.

**update menu** — Settings sub-tab directly left of Uninstall, showing installed and available oc-sandbox versions with the Update action behind the confirmation popup (ADR-0021); while an update is available, a footer badge appears that jumps here on click, and after an update the menu notes that restarting oc-sandbox applies it. _Avoid_: update tab, upgrade menu.

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

**convenience artifacts** — The two launcher-bits the install script creates by default so the TUI is reachable everywhere: the entry-point symlink and the desktop shortcut. Both are opt-out, never fail the installation, and can be deselected individually via `--no_symlinks` / `--no_shortcut`. _Avoid_: convenience features, extra flags.

**symlinks** — The symlink artifact of the convenience artifacts: a single symlink `oc-sandbox` → `bin/oc-sandbox` (the Go TUI binary) in `$HOME/.local/bin` for easier command access. Created by default; deselected with `--no_symlinks` (the former opt-in `--symlinks` flag is accepted as a no-op). Desktop shortcut points at the binary too. Default entry point since the parity-gate flip (#98); the former `--bash-tui` escape hatch went away with the bash TUI deletion (#99, ADR-0020).

**color output** — Install script output uses plain text only for maximum compatibility. Helper scripts may use gum for prompts (e.g. the uninstall wizard's red confirmation input), with plain-text fallback; the Go TUI renders via Lip Gloss.

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

**symlink creation** — Creates a single entry-point symlink `oc-sandbox` → `bin/oc-sandbox` (Go TUI binary, default since the parity-gate flip #98) unless deselected with `--no_symlinks`; removes stale symlinks in `$HOME/.local/bin` pointing into the install path first (ADR-0020). Fail-soft: a failing symlink (e.g. unwritable `$HOME/.local/bin`) warns with a manual `ln -s` hint and continues the installation.

**symlink overwriting** — Overwrites existing symlinks without prompting (default behavior; deselect with `--no_symlinks`).

**temporary file naming** — Uses random names with `mktemp` for temporary files to avoid conflicts.

**error message paths** — Uses absolute paths for all error messages to avoid confusion.

**logging** — Only outputs to stdout/stderr; no log files created during installation.

**github API authentication** — Stays unauthenticated for simplicity; handles rate limit failures gracefully with fallback to HTML scraping.

**user agent** — Sets a simple user agent identifying the opencode-sandbox install script for GitHub API requests.

**version comparison** — The update check's comparison of the installed release tag against the latest GitHub release; non-comparable or unknown tags count as outdated (update offered). The interactive install script itself still prompts regardless of versions.

**special file handling** — Only `proxy/allowlist.txt` gets special treatment during updates; all other files are overwritten.

**post-installation message** — Shows brief reminder about system requirements (podman, etc.) and suggests next steps after successful installation.

**setup-complete marker** — Boolean flag in sandbox_config.json tracking whether first-run setup (CBM configuration and skills setup) has been completed for a project.

**atomic config write** — Configuration update method using temporary files and atomic rename operations to prevent corruption during crashes, with automatic backup creation.

**runtime detection** — Dynamic discovery of available container editions and modes at runtime, avoiding hardcoded lists. Editions come from the Dockerfile stage registry (ADR 0019): every `FROM ... AS opencode-sandbox-<name>` stage in `dist/Dockerfile` or `dist/Dockerfile.custom`, grepped live at each menu render. Modes are still parsed from start.sh help text. _Avoid_: hardcoded edition lists, help-text edition parsing.

**base edition** — The parent stage `opencode-sandbox-base` in `dist/Dockerfile` that all shipped editions build `FROM`: infrastructure, not a user-facing edition. The Go TUI never offers it as a selectable edition for a project (New Project wizard, change-settings, re-selection); it remains discoverable and buildable via the Container center Build Images tab, `build-container.sh`, and `start.sh`. Projects stored with `container_edition: "base"` stay valid. _Avoid_: base image edition, selectable base.

**latex edition** — Container edition for LaTeX/TeX document work (`opencode-sandbox-latex` stage in dist/Dockerfile, building from base like every shipped edition). Full compile toolchain: latexmk plus the texlive collections recommended/extra/science/bibtex-extra/publishers/xetex/luatex/pictures/plain-generic/extra-utils/lang-european, biber, chktex, latexdiff, ghostscript, poppler-utils, dvipng, python3-pygments (minted), tex-gyre, lmodern, pandoc, Liberation/Noto fonts, and matplotlib/pandas (apt) for plot generation. Deliberately fat: any document should compile first try, no rebuild-on-complaint. Stands alone from the writing edition — duplicates pandoc and the font packages rather than layering (writing keeps its own pandoc for office/docx conversion), keeping edition images independently rebuildable. Provision-only: tools are shell-invocable, nothing is seeded into opencode.json. _Avoid_: TeX edition, writing edition (the LibreOffice sibling).

**image-existence marker (Build Images)** — Status shown on each edition row of the Container center Build Images tab: `● built` when the edition's image exists in podman, `○ missing` otherwise. Checked once per tab entry and re-checked when a build finishes (a check that fails reads as missing); rows render without any marker before the first check. _Avoid_: live per-render check, missing image (the start-time error about a missing image is a separate concern).

**build images (Container center)** — Container center Build Images tab: lists the live edition registry (ADR 0019, base included) with the image-existence marker, plus select-all and a Build button; building checked editions runs build-container.sh as one sequential op through the task manager, streamed into the embedded log pane. Creates images, never containers. _Avoid_: build container (misnomer — podman builds images), build prompt (the start-time Build now / Build later prompt is a separate flow).

**delete image** — Container center Delete Images tab (directly after Build Images): lists every sandbox-family image as a checklist row with tag, size, and use marker — `● in use` when any container in any state references it **or a child edition image exists** (parent-aware per the Dockerfile stage registry: deleting a parent image, e.g. base, would break a child edition's rebuild, so base reads in use while any edition image exists), `○ unused` otherwise. The sandbox family is every `opencode-sandbox-<edition>` tag (any registry prefix, tag/digest suffix) plus `oc-proxy`; unrelated host images and dangling `<none>` images are never listed, and there is no prune action. In-use images are refused with a remove-containers-first hint (podman rmi never deletes a referenced image); removal runs `podman rmi` per checked image behind the danger confirmation popup, verified gone against a fresh image list. Deletion also refreshes the Build Images tab's existence markers. _Avoid_: image delete (ambiguous with delete container), image prune (no prune action).

**custom edition Dockerfile** — User-editable `dist/Dockerfile.custom` holding user-defined edition stages as standalone build recipes (typically `FROM opencode-sandbox-base`, ending `USER dev` + `ENTRYPOINT ["/bin/bash","-l"]`). Shipped as a commented example template on install; never overwritten on update — recreated only if absent. The main `dist/Dockerfile` is still overwritten on update, so it is not a sanctioned home for user stages. _Avoid_: edition manifest, editions.json.

**native script enhancement** — Strategy of extending existing scripts (start.sh, build-container.sh, init-project.sh) with additional parameters (like --start_opencode) rather than creating parallel TUI-specific implementations, preserving backward compatibility and avoiding code duplication.

**project display name** — Unique user-facing name for a registered project, used in TUI menus and registry selection.

**container identity** — Stable project-specific identifier derived from the canonical project root, persisted with the project, and used to avoid container-name collisions.

**deferred build** — Registered project state in which the container image is not yet built; the project remains stopped until a later build and start.

**setup recovery** — Explicit retry path for incomplete first-run setup while preserving setup-complete as false until CBM and skills setup both succeed.

**credential bridge** — Activated `[credential] helper = store` block in the gitconfig template plus `configure_vcs_credentials` writing a host-scoped `https://<user>:<token>@<host>` entry into `.git_local/credentials` whenever a VCS token is captured — regardless of project source — so HTTPS clone and later push work with the same token in all editions. _Avoid_: credential-store side effect, token persistence.

**retry flow** — Error recovery pattern where users who choose "Retry" after a failure remain in the recovery loop even if intermediate steps (like settings adjustment) fail. Failures show context-aware error messages and return to the retry menu, except for explicit user aborts (exit code 2) which are respected throughout.

**uninstall wizard** — Guided TUI flow in Settings (warning screen with running containers and their stop commands, option rows for symlinks/config/backup toggled via Enter, explicit "Confirm — start uninstall" row that must be selected to proceed, summary screen). Every screen can be cancelled or navigated back; toggling a flag row alone never advances the wizard. The choices are mapped to uninstall.sh flags and run with `--force`.

**UNINSTALL confirmation** — Typed text confirmation required on the uninstall summary before anything is removed; rendered as a red gum input when gum is available, plain `read` fallback otherwise.

**web start option** — Launch style that runs the project container as a web server: the OpenCode v2 background service, pinned via the mounted service config (`service.json` hostname `0.0.0.0` / port 4096, pre-seeded on the host by start.sh before first start) with a single `opencode service start`, then a keep-alive (ADR 0024). Selectable at project creation and in settings; the server runs from the first start (first-run setup executes alongside via exec). Following the detached+exec start architecture, the detached container runs the service as its main process, the host port is auto-scanned upward from 4096 (like the CBM-UI range), and access announces a one-time pair link rather than opening a browser. The server is stopped via normal container stop. Direct CLI usage of start.sh with `--start_web` and without `--detach` runs `opencode serve` foreground, where Ctrl+C stops and removes the container (`--rm`). Implemented via the `--start_web` flag on start.sh, mirroring `--start_opencode`.

**pair link** — One-time web sign-in link printed by `opencode pair` (v2): opening it in a browser signs the session in (30-day session after that; links expire in 5 minutes). The TUI's web access runs `opencode pair --url http://<published-host>` inside the container so the link carries the reachable published authority, and announces it fresh at every web access. Not a stored credential; nothing about it is persisted. If pairing fails (or the access is part of first-run setup), web access falls back to announcing the plain web URL. _Avoid_: web URL (the plain published URL alone does not sign in), login URL.

**shared background service** — The OpenCode v2 client-server runtime model inside a project container: one background server owns sessions, config reload, permissions, and tool execution; every client (TUI, mini, run, web browser) connects to it. Auto-starts on first `opencode` use; the web start option pins and starts it explicitly. Never shipped disabled (`service set disabled true` is forbidden in the image) and `--standalone` (a private server per client) is never the standard. _Avoid_: standalone mode, per-client server.

**console session** — Interactive bash shell attached to a running container via `podman exec -it --user dev <container> bash`, chosen by the TUI at access time. Independent of the configured start option (launch behavior) and of any other session already inside the container.

**squid allowlist scope** — The squid egress proxy governs outbound traffic from inside the container only. Host-browser access to published container ports (web UI, CBM UI) never traverses squid, so allowlist entries are never needed for ingress.

**skipped files** — Files that could not be removed during uninstallation because of missing permissions; tracked during removal and reported at the end with a hint to clean them up manually (e.g. with sudo).

**desktop shortcut** — OS menu launcher for the TUI created by the install script by default (one of the convenience artifacts, opt-out via `--no_shortcut`); exactly one platform-appropriate artifact per OS: a `.desktop` file named `oc-sandbox.desktop` with `Terminal=true` under `$HOME/.local/share/applications/` (Linux), a `.lnk` named `OC Sandbox.lnk` in the current Windows user's Start Menu written from inside WSL (Windows), or a minimal `.app` bundle named `OC Sandbox.app` in `~/Applications` (macOS). Display name is `OC Sandbox` on all platforms. Independent of symlinks.

**shortcut flag** — The former opt-in `--shortcut` parameter on the install script, now accepted as a no-op: the desktop shortcut is created by default and only deselected via `--no_shortcut`; it overwrites an existing artifact silently on re-install.

**shortcut removal** — Uninstall path for desktop shortcuts, offered as a wizard checkbox mapped to an uninstall.sh flag; removes shortcut artifacts pointing into the installation folder.

**shortcut icon** — Icon material for the desktop shortcut, provided under `dist/icons/` in platform subfolders (`linux/`, `windows/`, `macos/`) and processed at shortcut creation time: Linux installs the bundled hicolor PNG set into `$HOME/.local/share/icons/hicolor/` and the `.desktop` file references it by bare theme name (`Icon=oc-sandbox`, no path); Windows copies `icons/windows/oc-sandbox.ico` to `%LOCALAPPDATA%\oc-sandbox\oc-sandbox.ico` and points the `.lnk`'s `IconLocation` there (the Start Menu shell cannot render icons from `\\wsl.localhost\...` UNC paths; UNC is only a fallback); macOS generates the `.icns` from `icons/macos/oc-sandbox.iconset` with `iconutil` at install time and embeds it in the `.app` bundle. Missing icons never fail the shortcut — it is created without one.

**hicolor icon install** — Linux shortcut step copying `icons/linux/share/icons/hicolor/` into `$HOME/.local/share/icons/hicolor/` so the desktop entry resolves `Icon=oc-sandbox` through the icon theme. Best-effort: failures fall back to an icon-less shortcut.

**icns generation** — macOS shortcut step converting the bundled `.iconset` to `AppIcon.icns` using `iconutil` (ships with macOS); on failure the `.app` is created without an icon.

**change project settings** — TUI flow (Settings → Change Project Settings → project list) re-running the full prefilled settings sequence (edition, modes, start option, VCS, AI provider, proxy toggle) for any registered project, including running containers and projects with incomplete first-run setup. Changes are persisted to sandbox_config.json immediately and take effect on next container start; no live-apply. _Avoid_: edit settings, live settings.

**revisit sequence** — The shared prefilled settings chain (edition → modes → start option → VCS → AI provider, plus proxy) used by both the change-project-settings flow and the failure-recovery retry path; one implementation, reachable from either entry point. _Avoid_: settings redo, reconfigure loop.

**proxy toggle** — Per-project yes/no setting recorded as `use_proxy` in sandbox_config.json, passed to start.sh as `--use_proxy` on next start. Governs container-level proxy use only; the global `oc-proxy` container's lifecycle and the allowlist remain start.sh/start.sh-built-image concerns, never edited here. _Avoid_: allowlist editor, proxy settings.

**next-start apply** — Contract that any settings change (including edition switch) is only recorded and never reconfigures a running container; applying requires a later stop/start, where a missing edition image gets the existing Build now / Build later / Go back prompt at start time. If the stored edition differs from a running container's edition, the next Open Project start surfaces the edition-mismatch prompt (see edition mismatch) instead of silently reusing the old container. _Avoid_: live apply, hot reload.

**edition mismatch** — Divergence between the edition recorded in sandbox_config.json and the edition a project's running container actually runs (read from the container's image name `opencode-sandbox-<edition>` via `podman ps`; no labels, nothing stored in the registry). Surfaced only by the Open Project start buttons (Console / OpenCode / Web) as a three-way prompt: **Switch** (explicitly confirmed `podman stop` of the old container, then a normal start with rebuilt args — all current settings ride along — continuing into the requested access), **Use current** (access the old container unchanged), **Cancel**. The old container is never stopped until the new edition's image exists (missing image gets the start-time build prompt first); a switch whose start fails leaves the project stopped with the error in the busy pane. Container center Start is unaffected and keeps reuse-running. _Avoid_: live apply, restart (the switch is an explicit, confirmed replacement, never a silent one).

**BubbleTea TUI** — The TUI: full-window, mouse-first, keyboard-available, single Go binary (`oc-sandbox`). Built on Charm libraries: BubbleTea (application/controller), Bubbles (components), Lip Gloss (styling). Sole TUI since the parity-gate flip (#98) and the bash TUI deletion (#99); gum remains in use by the helper scripts only. _Avoid_: GPU TUI.

**terminal minimum size** — The smallest terminal where the Go TUI renders at all: the too-small overlay guard trips below 80×24. At startup, when the terminal is a TTY and smaller than the layout target 80×41 (the Edit Project settings AI provider pane unfolds with a provider entry opened unscrolled), `oc-sandbox` asks it to grow via the xterm window-size escape (`CSI 8 ; height ; width t`) — emulators that ignore the request fall back to the overlay at 80×24; an already-large terminal is never shrunk. The overlay text names both the minimum (80×24) and the recommended size (115×41). Startup only, no repeat requests mid-session. _Avoid_: window requirement, hard resize, per-screen minimum.

**release-time build** — Cross-compilation of the Go TUI happens in create-release.sh before the orphan release branch is cut: `dist/bin/oc-sandbox_<goos>_<goarch>` (linux/darwin × amd64/arm64, CGO off), tag injected via `-X main.Version`. Go source lives at repo root outside `dist/` and never enters the release branch. `dist/bin/` is gitignored. _Avoid_: committing binaries, duplicating Go source into dist/.

**terminal handoff** — Full-screen terminal suspension where the TUI pauses, releases the grid to a child process (e.g. `podman exec` console via tea.ExecProcess), and re-renders the window on return. Core event for interactive sessions (console/OpenCode); used for long-running external commands that need raw terminal I/O.

**embedded log pane** — TUI layout region streaming live output of long-running container operations (build/start/stop/first-run) while the app stays interactive elsewhere, replacing the current bash "simple status messages" UX. Rendered below the owning view's card; the Container center Build Images tab and the New Project create chain (build/init/credentials steps) both feed it from the task manager's streamed lines.

**background task** — Long-running operation (build/start/stop/export) running in a Go goroutine with progress/status surfaced via footer/health indicators, allowing free navigation while it runs. Completes asynchronously with an inline notification at completion point; queued one-at-a-time per project.

**confirmation popup** — Shared centered modal (shell package) shown over the dimmed card to confirm a triggered action before it runs. Keyboard: ←/→/Tab switch buttons, Enter activates the focused button, esc/q cancels anytime. Pointer: click outside cancels; default focus is Cancel. One-line title + one context line per action; fires only when no op is queued/running. Applied to Create Project (content summary with Confirm/Back), Restore Config, Build (Container view button/card double-click), Uninstall (after the typed confirm, on button click, red Confirm), Update (update menu, plain Confirm, glossary "update menu"), and Remove project from list (Edit Project settings removal pane, red Confirm). Quit keeps the existing guarded-exit footer prompt; start-time Build-now prompt (Build now / Build later / Go back) stays separate. _Avoid_: yes/no dialog (.Create Project uses Confirm/Back), exit prompt (reserved for guarded exit).

**start variant** — The access a single open-project start resolves to: one of console, OpenCode TUI, or Web. The configured start option preselects the variant (configure it in the Settings pane); an explicit card start button or a keyboard preselection can pick a differing variant for just that start (e.g. Web on a console-configured project launches the container with --start_web) without touching the saved settings. Double-click always starts with the configured start option; Enter starts with the visually preselected one. Web picked while a container runs without a published 4096 port adopts web: the running container is replaced with a verified --start_web relaunch (verified stop → start, one attempt per access chain) — a console container can never switch to web live. _Avoid_: access mode, session kind, per-start override.

**card start buttons** — The Open Project card's third content row (Console / OpenCode / Web) that chains start + access in one gesture: clicking a button on the selected card (or Enter, or double-click) starts a stopped container, waits for verified success, and lands in the chosen variant — no separate access prompt. On a running container the chosen variant is entered directly (Web announces a fresh pair link, never a browser). Buttons on unselected cards merely select the card. Left/right on the card grid cycles the preselection; the settings pane's Start option row still changes the configured start option. _Avoid_: access prompt (removed from the card flow), per-card start menu.

**remove project from list** — Deregistering a registered project from the Edit Project settings screen: deletes the project's entry from projects.json only (registry metadata). Untouched: global_config.json, the project's files, sandbox_config.json, and the project's container — a running container keeps running, simply no longer tracked. Gated two-step: the selected card's remove button opens a right pane with info text and warning, and only the pane's "Remove project from list" button behind the shared confirmation popup (red Confirm, default focus Cancel) performs the write. _Avoid_: delete project (reads like delete container or file deletion), unregister project.

**muted card frame** — Frame treatment of unselected cards in the card grids: the rounded border renders in the unfocused border color instead of the accent (project cards via `shell.CardMuted`; the edition cards of the build view style their own muted border). Content is untouched; only the frame is muted. _Avoid_: dimmed card (reserved for modal popup overlay), disabled card (buttons of unselected cards still select).

**websearch** — OpenCode's built-in web-search tool, active in every sandboxed OpenCode instance whose search mode is websearch (see search_mode) via the `OPENCODE_ENABLE_EXA=1` and `OPENCODE_ENABLE_PARALLEL=1` container env vars set by start.sh, with `"websearch": {"provider": "random"}` seeded into the OpenCode config (random backend pick, auto-switch on rate limit). Needs no API key. Backend endpoints are Exa (`https://mcp.exa.ai/mcp`) and Parallel (`https://search.parallel.ai/mcp`); `OPENCODE_WEBSEARCH_PROVIDER=exa`/`parallel` env or config pin overrides the random pick. Under `--use_proxy` it requires the squid allowlist entries `mcp.exa.ai` and `search.parallel.ai`. _Avoid_: Exa search, web search tool.

**Context7** — Remote MCP server (`mcp.context7.com/mcp`) seeded into every sandbox's OpenCode config for documentation search; keyless (rate-limited free tier). Under `--use_proxy` it requires the squid allowlist entry `mcp.context7.com`. _Avoid_: docs MCP, exa MCP (websearch is a built-in tool, not an MCP server).

**oc-searxng** — The single shared SearXNG container for the whole oc-sandbox, mirroring oc-proxy: image built from `dist/searxng/Dockerfile` (outside the edition stage registry, so never offered as a project edition), started on demand by start.sh when the first SearXNG-search project container starts (image auto-built if missing, `--replace`), published at `127.0.0.1:<scanned port>:8080` (scan starts at 8080) and reached from project containers via `host.containers.internal`. Never auto-stopped, also not when the last project container stops. Listed in the Container center Build Images tab as a non-edition row and in Delete Images as part of the sandbox family. Never runs under the squid proxy: SearXNG search is mutually exclusive with `use_proxy` (the SearXNG MCP reads arbitrary URLs, so the allowlist could not bound the agent — incompatible, not unproxied-by-exception). _Avoid_: searxng image (the upstream image), per-project searxng.

**SearXNG MCP** — The npm package `mcp-searxng` (ihor-sokoliuk, pre-installed globally in the base image) registered as a local MCP server inside the project container: tools `searxng_web_search` (query → oc-searxng's JSON API) and `web_url_read` (fetches page content). Seeded into the project's opencode.json when the project's search mode is SearXNG, removed otherwise (applied at next container start). Its base URL is never written into opencode.json — start.sh carries it per container in the `SEARXNG_URL` env var, so port rescans never go stale. _Avoid_: websearch MCP (websearch is the built-in Exa/Parallel tool), fetch MCP (web_url_read is one tool of this server, not the server's identity).

**search_mode** — Per-project web-search setting (one choice row in the settings flows: none / websearch / SearXNG, positioned directly after the proxy toggle) recorded as `search_mode` in sandbox_config.json; absent reads as `websearch` for pre-existing projects. `none` seeds no search tooling at all (no Exa/Parallel env vars, no `"websearch"` config seeding, no MCP); `websearch` is today's built-in Exa/Parallel behavior including the `"websearch": {"provider": "random"}` seeding; `searxng` seeds the SearXNG MCP in opencode.json (no Exa/Parallel env vars, no seeding — SearXNG is the only web search; Context7 stays) and is started via start.sh `--search_mode searxng`. `searxng` is mutually exclusive with `use_proxy` (rejected combination); all modes apply on next container start, never live. _Avoid_: use_searxng toggle (superseded three-way design), websearch toggle.

