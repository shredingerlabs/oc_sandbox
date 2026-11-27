# OpenCode Sandbox – Production Distribution

This folder holds the **production-ready files** for the OpenCode Sandbox — a
directly runnable foundation for a unified development sandbox with
content-addressed toolchain sets.

## Included files

```
dist/
├── toolchains/
│   ├── base.Dockerfile             <- The fixed base recipe (content-hashed family identity)
│   ├── manifest.json               <- Bundle map: curated toolchain sets
│   ├── recipes/                    <- Shipped toolchain recipes (overwritten on updates)
│   ├── custom/                     <- User recipes (always kept; example template)
│   └── spot-check.tsv              <- Tier-2 spot-check table (id → invocations/imports)
├── scripts/
│   ├── start.sh                    <- Unified start script (--toolchains flag)
│   ├── build-images.sh             <- The toolchain-sets builder (--base/--tc/--set/--all)
│   ├── lib-identity.sh             <- Shared identity/tag derivation (builder + start.sh)
│   ├── toolchains.sh               <- Discovery/lint helpers over the toolchains tree
│   ├── dump.sh / merge-pm.py       <- The payload-archive dump + stanza-union merge core
│   ├── configure-workspace.sh      <- Workspace configuration (git identity, VCS credentials, AI provider)
│   ├── init-workspace.sh           <- Creates the workspace-root structure
│   └── uninstall.sh                <- Uninstalls opencode-sandbox
├── proxy/
│   ├── Dockerfile                  <- Separate Squid proxy image
│   ├── squid.conf
│   └── allowlist.txt
├── stt/
│   ├── Dockerfile                  <- Separate STT image (whisper.cpp v1.9.5, baked ggml-small model)
│   ├── entrypoint.sh               <- Starts the whisper server (port 8090, --language from STT_LANG)
│   └── public/                     <- Dictation sidecar page (index.html, app.js)
├── udev/
│   └── 99-hil.rules                <- udev rules for HIL devices
├── icons/                          <- Icon material for desktop shortcuts
└── templates/
    ├── ssh_local/config
    ├── git_local/
    │   ├── gitconfig
    │   ├── credentials
    │   ├── gh-cli/
    │   └── glab-cli/
    ├── bash/
    │   └── bash_profile
    ├── opencode/
    │   ├── opencode-basic.json
    │   ├── AGENTS.md
    │   ├── AGENTS_chat.md
    │   ├── skills/
    │   └── plugins/
    │       └── dictation/          <- TUI dictation plugin (index.ts, tui.tsx, dictation.mjs)
    └── pkm/
        ├── AGENTS.md
        ├── .gitignore
        ├── agents/
        ├── commands/
        ├── opencode.jsonc
        └── skills/
```

The WebUI stores each registered workspace's canonical root and a short SHA-256
container identity in `~/.config/oc-sandbox/workspaces.json`. This keeps workspace
names unique while allowing workspaces with identical directory basenames.

During workspace setup, the WebUI can configure no VCS, GitHub, public GitLab,
self-hosted GitLab, or another VCS host. For cloned workspaces whose repo URL
points at `github.com` or `gitlab.com` (https, SSH, or scp-like form), the VCS
host is derived automatically without prompting; other hosts show the manual
chooser. Tokens use hidden inputs and remain in workspace-local `gh-cli/hosts.yml`,
`glab-cli/config.yml` (hosts block; glab ignores a separate `hosts.yml`), or
`.git_local/vcs/hosts.yml`. The AI provider (provider-neutral, OpenAI-compatible
endpoint of your choice) stores its token in `.opencode_data/credentials/<id>`
under a slugified provider id; the v2-shaped `providers.<id>` section in
`opencode.json` references it via OpenCode's `{file:...}` substitution.
Existing credentials are kept unless `Replace` is explicitly selected.
Non-empty workspace-local Git `user.name` and `user.email` are requested for every
non-`none` VCS choice. A failed start offers `Retry`, `Go back`, or `Cancel`; retry
reopens the settings with current values.

Workspace setup also selects a start option: `console` (shell), `opencode`
(OpenCode TUI), or `web` (OpenCode web surface). Every detached start runs
the OpenCode background service (v2) permanently on port 4096, published on
`127.0.0.1` (port scan 4096-4196, except `--offline`); **Web access** opens a
fresh one-time sign-in link (`opencode pair`) in a new tab within the click —
a plain-URL fallback appears when pairing fails.

When a workspace is opened while its container is already running, the chosen
variant is entered directly: `opencode`/`console` open a host terminal window
(`podman exec` inside; on a headless/SSH machine the browser shows the exact
copyable command instead); `web` opens a fresh sign-in link — and when the container runs without a published
web port, it is replaced by a verified restart with `--start_web` (auto-adopt).
A console session is
an independent bash shell attached
via `podman exec` and does not change the configured start option.

If the workspace's set image is missing, the start flow offers `Build now`,
`Build later`, or `Go back` (Build now rides `build-images.sh` — the missing
components build first, then the assembly). New workspaces run setup in a
detached container and then attach to the selected console or OpenCode session
(with the `web` start option, the web surface URL is opened instead).

First-run setup for workspaces created from a repo URL probes reachability with
`git ls-remote` inside the container and then performs a full clone (no
`--depth`, no submodule/LFS recursion) into the mounted `workspace/` directory
before CBM and skills setup. A probe or clone failure shows the context and
offers `Retry` (same URL, after cleaning partial clone state), `Change URL`
(re-prompts and stores the new URL), or `Cancel setup` (partial progress is
preserved); empty-source workspaces skip this stage. First-run setup records clone, CBM, and skills progress separately. CBM
runs without a terminal; the skills stage (code workspaces only — `chat` and
`pkm` workspaces skip it and their setup completes after CBM) opens a host
terminal window automatically (through an attached
`podman exec -it`, so its interactive input and output stay connected to the
user — on a headless/SSH machine the browser shows the exact copyable command
instead). A stage is marked complete only after success, and a failure keeps the
workspace registered and offers `Retry` or `Cancel setup`; retries only
repeat the incomplete setup stage. "Retry setup" from the workspace settings also
finishes a partial clone attempt. SIGINT and SIGTERM clean up and exit safely.

Constraints for cloned workspaces: HTTPS URLs plus a wizard-captured VCS token
work in every toolchain set (the token is also written to `.git_local/credentials` as
a host-scoped credential-store entry, so later HTTPS push from the container is
authenticated with the same token, whether or not the workspace is cloned). SSH
URLs require manually placed keys in the workspace's `.ssh_local/` directory
(mounted read-only). There is no automatic submodule/LFS recursion.

Settings can back up and restore `workspaces.json`, `global_config.json`
or `catalogs.json` one file
at a time. Restore validates JSON and creates a safety backup before the atomic
replacement. Credential files under `.git_local/` and `.opencode_data/` are
never included in general configuration backups.

The Settings menu also offers **Uninstall**: a guided wizard with a warning
screen (running containers with their `podman stop` commands), option checkboxes
for symlinks/config/backup ("Create backup" is only available when "Remove
config" is selected), a summary with a typed `uninstall` confirmation, and
cancel/back navigation at every screen.

## Toolchain sets

Every workspace's image is composed from its stored toolchain set:

- **base** (`toolchains/base.Dockerfile`): Ubuntu 24.04, Python 3 + pip/venv,
  git/gh/glab, pinned OpenCode + codebase-memory-mcp, node + mcp-searxng, the
  dev user — the parent of every component and set image.
- **component images** (`toolchains/recipes/<id>.Dockerfile`, one pinned
  recipe per toolchain — cpp, go, rust, java, playwright, ros2, latex, …):
  built content-addressed, cached and shared by every workspace that needs
  them. User recipes live in `toolchains/custom/` and always win discovery
  collisions in the user's favor (a user recipe shadows nothing — shipped
  ids keep their shipped recipe; your own ids are free).
- **set images** (`opencode-sandbox-set:<hash12>`): assembled from the
  workspace's member list via the payload-archive dump + stanza-union merge —
  `dpkg` status unioned with the members' PM dumps, pip RECORD paths
  re-registered, the `~/.profile.d/` env hooks re-emitted.

`manifest.json` maps the curated **bundles** (software dev, web, embedded,
ROS 2, LaTeX, …) to their member ids; the wizard's bundle chips and the
`start.sh --edition <name>` alias both read it. Nothing auto-builds on update:
recipes are rewritten wholesale (that is how pins bump), and every image
re-addresses at its next build.

Own toolchains: add a recipe file to `toolchains/custom/` (grammar and pin
rules in `docs/porting-dockerfile-custom.md`) — it appears automatically in
the WebUI's toolchain multi-select, in `build-images.sh --tc <id>`, and in
`start.sh --toolchains <ids>`. The directory is never overwritten on updates.

### Build-time egress

Toolchain and image builds fetch directly from their sources — **build-time
only**: running workspaces never build, and the per-workspace squid proxy
never sees a build (builds ride `--network host` over the host's own proxy
environment, so no allowlist entries are ever needed for them). The complete
list of build-time source hosts, for restricted-host administrators:
`archive.ubuntu.com`, `security.ubuntu.com`, `docker.io`, `pypi.org`,
`files.pythonhosted.org`, `registry.npmjs.org`, `deb.nodesource.com`,
`sh.rustup.rs`, `static.rust-lang.org`, `github.com`,
`objects.githubusercontent.com`, `raw.githubusercontent.com`, `gitlab.com`,
`opencode.ai`, `cdn.playwright.dev`, `packages.ros.org`, `labs.picotech.com`,
`downloads.arduino.cc` — plus the infra-only hosts (the shared infrastructure
images build outside `build-images.sh`'s contract, same build-time-only rule):
`dl-cdn.alpinelinux.org`, `huggingface.co`.

## Installation & Update

Install or update via the bash one-liner (downloads the latest release from
GitHub and installs it to `~/.oc-sandbox`):

```bash
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

or, when curl is unavailable:

```bash
wget -qO- https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

The command can be repeated at any time to update to the newest version: before
overwriting an existing installation it asks, and your `proxy/allowlist.txt`
edits are preserved (unless you explicitly decline). `toolchains/custom/` +
`toolchains/snapshots/` always survive updates; the shipped
`toolchains/recipes/`, `manifest.json`, and `base.Dockerfile` are overwritten
(pins arrive via these rewrites). Your legacy `Dockerfile.custom` is never
touched. The script never uses `sudo` automatically. **Restart `oc-sandbox`
after an update** — between install and restart the old binary may refuse
against the new toolchains world.

**Symlink and desktop shortcut are the default:** the installation creates the
symlink `oc-sandbox` → `bin/oc-sandbox` (the Go binary, single entry point) and
a desktop shortcut for oc-sandbox (Linux: `.desktop`, WSL: Windows Start-menu
`.lnk`, macOS: `.app` in `~/Applications`). Failures warn only and never abort
the installation.

**Options:**
- `--install_path <path>` – Installation path (default: `$HOME/.oc-sandbox`)
- `--version <tag>` – Install a specific version (default: latest)
- `--force` – Overwrite an existing installation without asking
- `--yes` – Alias of `--force`: non-interactive update (e.g. from the WebUI's update screen)
- `--no_symlinks` – Skip the default symlink
- `--no_shortcut` – Skip the default desktop shortcut
- `--symlinks` / `--shortcut` – No effect (compatibility: the
  convenience artifacts are default-on)
- `--verbose` – Detailed output

For the uninstallation see [below](#uninstallation).

## Quickstart

The preferred way is the WebUI — it walks you through image builds, workspace
setup and start, without calling scripts by hand. Direct script usage remains
as the [legacy path](#direct-script-usage-legacy).

### 1. Prerequisites

```bash
sudo apt-get update
sudo apt-get install -y podman pasta fuse-overlayfs git curl jq slirp4netns

# Check podman rootless
podman info --format '{{.Host.Security.Rootless}}'   # should print "true"
```

### 2. Start oc-sandbox

With the default symlink, from any directory:

```bash
oc-sandbox
```

Without the symlink, via the install path or from this folder:

```bash
~/.oc-sandbox/bin/oc-sandbox
./bin/oc-sandbox
```

oc-sandbox is a single Go binary with the embedded WebUI (`go:embed`).
On start it launches the web server on `127.0.0.1` behind a one-time
token URL and opens the browser tab automatically; the terminal
shows a minimal launcher view with the full URL, the
**Open WebUI** button (re-opens the tab) and **Quit**. A second start
points at the running instance and exits; on quit it asks whether
running containers should be stopped (default: keep them running).
The first start on a pre-rename installation migrates it once
(`projects.json`/`project/` → `workspaces.json`/`workspace/`, backups first —
ADR-0032). Via the WebUI (sidebar: **Workspace → Open**, **Workspace → New**,
**Settings** with Workspace Settings, Proxy, VCS Tracker, AI-Provider,
Import Workspace, Export / Restore Config, Images, Containers, Update,
Uninstall) all further steps run: building the toolchain images, creating the
workspace root, collecting VCS/tokens hidden, choosing the start option
(`console`, `opencode` or `web`) and starting the sandbox. Running containers
are reused via `podman exec` instead of being restarted.

## Direct script usage (legacy)

All WebUI steps can also be run directly via the scripts.

### 1. Install the udev rules (for HIL tests)

```bash
sudo cp udev/99-hil.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
```

### 2. Build images

```bash
./scripts/build-images.sh --base
./scripts/build-images.sh --tc cpp,rust,java,go
./scripts/build-images.sh --set cpp,rust,java,go   # or: --all for base + every component
```

### 3. Set up the workspace root

```bash
./scripts/init-workspace.sh ~/workspaces/my-workspace
```

Optional: with `--repo_url <url>` the workspace is created as "cloned" —
`workspace/` stays empty (no template seed, no `git init`), the repo is cloned
later from `<url>`.

### 4. Start the sandbox

```bash
./scripts/start.sh ~/workspaces/my-workspace --toolchains cpp,rust,java,go
```

**Options:**
- `--toolchains <ids>` – Comma-separated toolchain selection (validated against
  live discovery; `""` = base-only set)
- `--edition <name>` – Manifest-bundle alias of `--toolchains` (refuses together
  with it)
- `--use_proxy` – Egress proxy with allowlist (per-pod Squid under `127.0.0.1:3128`)
- `--search_mode <none|websearch|searxng>` – Per-workspace web search: `websearch`
  (Exa/Parallel, default), `searxng` (own SearXNG instance as a pod member, reached
  via `127.0.0.1:8080` — incompatible with `--use_proxy`, ADR 0026), `none`
- `--offline` – Completely offline (no network)
- `--hil_mode` – HIL tests with USB devices (oscilloscope, MCU)
- `--cbm_ui` – CBM knowledge-graph UI (port 9749)
- `--start_opencode` – Start OpenCode right after the container starts
- `--start_web` – Start the OpenCode web surface in the foreground (v2 background
  service on container port 4096, published on 127.0.0.1, port scan 4096-4196; without
  `--detach` `opencode serve` runs foreground, Ctrl+C stops and removes the container;
  in detached mode the service runs regardless of this flag)
- `--detach` – Start the container in the background (main process always:
  OpenCode-v2 background service + keep-alive, `cd /home/dev/workspace && HOME=/home/dev/workspace opencode service start && exec sleep infinity`, so the
  service starts in the workspace and the web surface does not show the workspace
  picker; port 4096 is published on every detached start — except with `--offline`)
- `--container-id <id>` – Set the persisted container identity from the workspace
  registry (otherwise: basename of the workspace root)

## Uninstallation

In the WebUI (Settings → **Uninstall**) the same flow runs as a guided
wizard with warning, option selection and `UNINSTALL` confirmation.

Directly via script:

```bash
./scripts/uninstall.sh
```

**Options:**
- `--install_path <path>` – Installation path (default: `$HOME/.oc-sandbox`)
- `--remove-config` – Also remove the config directory (with backup)
- `--no-backup` – Create no backup (also for the config)
- `--no-symlinks` – Do not remove symlinks
- `--remove-shortcuts` – Remove desktop shortcuts (`.desktop`/`.lnk`/`.app`) and
  the installed Hicolor icons
- `--force` – No confirmations (for scripts/CI)
- `--dry-run` – Show what would be removed without deleting
- `--verbose` – Detailed output

**Safety:**
- Checks for running containers and warns
- Offers a backup before removal
- Never removes workspace roots or user data
- Removes symlinks automatically (a legacy gum-installation cleanup rides along)

## Usage

After starting, inside the container:

```bash
opencode
```

## Documentation

- **Full guide**: see README.md in the repository root
- **Templates**: `templates/` holds the git, SSH, OpenCode config templates

## License & origin

Part of [opencode-sandbox](https://github.com/shredingerlabs/oc_sandbox).
