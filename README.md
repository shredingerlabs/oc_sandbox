# OpenCode Sandbox

A multi-edition development sandbox running OpenCode in rootless Podman containers
(Ubuntu 24.04), managed through a terminal UI.

**⚠ BETA version — extensive tests are still needed.**

## Features

- **Sandbox** — rootless Podman containers with hardened settings
  (`--cap-drop=ALL`, `no-new-privileges`), one container per project, multiple
  sandboxes in parallel
- **Editions** — prebuilt environments for different use cases (web, embedded,
  software dev, LaTeX, ROS 2, and more), auto-discovered from the Dockerfile
- **Search tools** — per-project web search: OpenCode `websearch` (Exa/Parallel,
  no API key), a shared SearXNG container, or none; Context7-MCP for docs search
  in every mode
- **Hardware in Loop (HIL) mode** — USB oscilloscope (PicoScope) and
  microcontroller device passthrough for HIL tests
- **Project & git handling** — project wizard with VCS setup (GitHub, GitLab,
  custom host), hidden token storage, git identity, in-container cloning
- **Prepared skills** — first-run setup installs an engineering skill set for
  OpenCode automatically
- **All manageable with the TUI** — the Go binary `oc-sandbox` covers build,
  projects, images, containers, settings, update, and uninstall

For details on direct script usage, see the
[legacy script guide](docs/legacy-script-usage.md).

## Starting Guide

[https://shredingerlabs.github.io/oc_sandbox/](https://shredingerlabs.github.io/oc_sandbox/)
— step-by-step walkthrough of installation, first project, and daily TUI workflow.

## Installation

Prerequisites (Ubuntu 24.04 or similar):

```bash
sudo apt-get update
sudo apt-get install -y git curl podman passt fuse-overlayfs jq
podman info --format '{{.Host.Security.Rootless}}'   # should print "true"
```

Install via one-liner (downloads the latest GitHub release to `~/.oc-sandbox`):

```bash
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash
```

To pass flags, use `bash -s --` (everything after `--` goes to the script):

```bash
# Custom install path + fixed version
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --install_path ~/my-sandbox --version v1.0.0

# Skip convenience artifacts (no symlink, no desktop shortcut)
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --no_symlinks --no_shortcut

# Overwrite an existing installation without asking
curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash -s -- --force
```

By default the installer creates the symlink `oc-sandbox` (→ Go TUI binary in
`~/.local/bin`) and a desktop shortcut; both are opt-out via `--no_symlinks` /
`--no_shortcut`. The script never uses `sudo` automatically. Further options:
`--version <tag>`, `--yes` (non-interactive update), `--verbose`, `--help`.

## TUI

Start with:

```bash
oc-sandbox          # or: ~/.oc-sandbox/bin/oc-sandbox
```

The TUI is a standalone Go application (`oc-sandbox`), built at release time
(ADR-0020).

### Main menu

| Menu item    | Function                                                                                                                                                                    |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Open Project** | Pick a registered project and start it — project cards (sorted by last use) with start buttons **Console / OpenCode / Web**                                               |
| **New Project**  | Create a project root (project wizard)                                                                                                                                  |
| **Container**    | Container center with sub-tabs **Build Images** (`● built` / `○ missing` markers), **Delete Images** (`● in use` / `○ unused`, with sizes), **Start / Stop / Delete Container** |
| **Settings**     | **Project settings** (edition, modes, start option, VCS/AI provider), Import project, Export Config, Restore Config, Update, Uninstall                                       |
| **Exit**         | Quit the TUI                                                                                                                                                             |

### New Project

The wizard asks for the **project source** ("New empty project" or "Clone
existing repo via URL" — clones happen inside the container during first-run
setup), creates the project root, collects VCS host (`none`, GitHub, public
GitLab, own GitLab, others) and tokens (hidden input, stored project-locally in
`.git_local/`), configures the git identity, and picks a start option:

- `console` — interactive shell in the container
- `opencode` — OpenCode TUI
- `web` — OpenCode web UI (background service on port 4096, published on
  `127.0.0.1`; the TUI shows a one-time sign-in link via `opencode pair`

The **Agent** row picks the project kind: `Code` (default) or chat/PKM
variants. The **AI provider** selection offers `none` and `Custom` (name,
endpoint (OpenAI-compatible API), token — written to `opencode.json` v2 and
`.opencode_data/credentials/`, see ADR-0022/0025). Existing credentials are
kept unless **Replace** is chosen.

While the edition's image is missing, an **Image build** row offers
**Build now** / **Build later** (default: Build now).

First-run setup runs automatically in the background (CBM configuration, then
interactive skills setup via an OpenCode TUI session). On failure the TUI
offers `Retry` and `Exit` (plus `Change URL` when a clone failed); a retry
repeats only the incomplete step.
**OpenCode must be exited via `/exit`.**

### Open Project

Opening a project whose container is already running enters it directly:
`opencode`/`console` via `podman exec`, `web` via a one-time pair link. Each
project card has start buttons **Console / OpenCode / Web** (start + access in
one click). If a running container has no published web port, it is replaced
by a verified `--start_web` restart (auto-adopt).

### Images and containers

The container center's **Build Images** tab builds any edition (plus the
optional `oc-proxy` and `oc-searxng` images); **Delete Images** lists the
sandbox family with sizes and refuses in-use images. Containers can be
stopped (graceful) and deleted (running containers are refused with a
stop-first hint).

### Change project settings

**Settings → Change Project Settings** re-runs the full prefilled sequence
(edition, modes, start option, VCS, AI provider, proxy toggle) for any
registered project — including running containers. Changes are persisted to
`sandbox_config.json` and apply on next container start, never live.

### Update / Uninstall

**Settings → Update** shows installed vs. latest release version, checks for
updates at every TUI start (footer badge when available), and runs the install
script non-interactively, fetched fresh from the repository's `main` branch —
`proxy/allowlist.txt`, `dist/Dockerfile.custom`, and user configs are
preserved; a failed update leaves a persistent warning in the pane; restart
`oc-sandbox` afterwards.

**Settings → Uninstall** runs a guided wizard (warning → options → typed
`uninstall` confirmation). It removes only the installation, never project
roots or user data. Details: `dist/scripts/uninstall.sh --help`.

## Editions

Editions are **auto-discovered** from the `FROM ... AS opencode-sandbox-<name>`
stages of `dist/Dockerfile` and `dist/Dockerfile.custom` (see ADR-0019) — no
hardcoded whitelist. Sizes are approximate.

| Edition    | What's installed                                                                                | ~Size | Use case                                              |
| ---------- | ----------------------------------------------------------------------------------------------- | ----- | ----------------------------------------------------- |
| **base**   | Python 3 + pip/venv, build-essential, cmake, git, gh, glab, OpenCode, codebase-memory-mcp        | ~1 GB | Minimal Python; parent of all editions                |
| **web**    | base + Node 22, TypeScript/Jest/Playwright, pnpm, Chromium + Firefox                             | ~2.5 GB | TS/JS web development, browser automation            |
| **embedded** | base + ARM toolchains (gcc-arm-none-eabi), Arduino CLI (AVR/ESP32), MicroPython (mpremote, esptool), PlatformIO, PicoScope SDK | ~4 GB | Arduino/ESP32, MicroPython firmware, HIL tests        |
| **swdev**  | base + C/C++ (clang/gdb), GoogleTest/Catch2, Java (OpenJDK 21/Maven), Rust (rustup), Go          | ~3.5 GB | General software development in C/C++, Java, Rust, Go |
| **matlab** | base + GNU Octave + numpy/scipy/matplotlib/pandas                                                | ~2.5 GB | License-free numerical computing (headless)           |
| **ros2**   | base + ROS 2 Jazzy (ros-base, RViz2, CycloneDDS), colcon/rosdep/vcstool                          | ~3 GB | ROS 2 development in C++/Python                       |
| **writing** | base + LibreOffice + pandoc + Liberation/Noto fonts                                             | ~2.5 GB | Office docs (odt/odp, docx/pptx), PDF export          |
| **latex**  | base + latexmk + extensive TeX Live collections, biber/chktex/latexdiff, ghostscript, pandoc     | ~4 GB | LaTeX document compilation, any document compiles first try |
| **pkm**    | base only (PKM wiki templates are seeded at project creation)                                    | ~1 GB | Personal Knowledge Wiki projects                      |

**Custom editions:** add your own stage to `dist/Dockerfile.custom` (template
included) — it then appears automatically in the TUI, `build-container.sh`,
and `start.sh`. The file is never overwritten on updates.

## HIL Mode

For HIL tests with USB oscilloscope and microcontrollers, install the udev
rules from `udev/99-hil.rules` (PicoScope 2000 → `/dev/scope0`; serial MCU
devices → stable symlinks in `/dev/hil/`).

Find your device's IDs, edit the rules, and install:

```bash
lsusb
$EDITOR udev/99-hil.rules    # adjust vendor/product IDs
sudo cp udev/99-hil.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger
ls -l /dev/scope0 /dev/hil/  # verify
```

`start.sh --hil_mode` (or the TUI's HIL mode) mounts `/dev/hil` live into the
container. Note: the whole USB bus of the scope is mounted, not just the one
device (libusb limitation); serial devices may appear as `nobody:nogroup`
— see [troubleshooting](#troubleshooting).

## Legacy

The TUI covers everything. If you prefer calling the scripts directly
(`build-container.sh`, `init-project.sh`, `start.sh`, `uninstall.sh`), see
**[docs/legacy-script-usage.md](docs/legacy-script-usage.md)** — the complete
flag reference, project-root structure, and credential setup for direct usage.

## Troubleshooting

### Container won't start (`crun`/`runc` gid_map error)

```
Error: crun: writing file `/proc/<pid>/gid_map`: Operation not permitted: OCI permission denied
```

`--userns=keep-id` needs `/etc/subuid`/`/etc/subgid` entries covering your own
UID/GID. Ubuntu 24.04 only creates the 100000+ range; add the self-entry
manually, then log out and back in:

```bash
echo "$(id -un):$(id -u):1" | sudo tee -a /etc/subuid
echo "$(id -un):$(id -g):1" | sudo tee -a /etc/subgid
```

### HIL: USB device has no permissions in container (nobody:nogroup)

The device appears as `nobody:nogroup` (UID/GID 65534); `picoscope`/`pyusb`
fail with `PermissionError` or `LIBUSB_ERROR_ACCESS`. System groups like
`dialout` (GID 20) are not mapped into the rootless user namespace. Options:

```bash
# a) one session (reset on next plug): 
sudo chmod a+rw /dev/bus/usb/XXX/YYY
# b) persistent: add dialout-GID to /etc/subgid, re-login
echo "$(id -un):20:1" | sudo tee -a /etc/subgid
```

## Security principles

- Rootless Podman (no Docker daemon as root)
- `--cap-drop=ALL` + `no-new-privileges`
- Project-root structure strictly separates code (`project/`) from secrets
  (`.ssh_local/`, `.opencode_data/`, `.git_local/`)
- Per-project git keys, git identity, and OpenCode login without script edits
- SSH-key and git-credential permissions are checked at start
- Optional network egress via proxy allowlist; `--offline` for no-network runs
- HIL mode uses bus-level passthrough (only the scope's USB bus, never `/dev/bus/usb` entirely)
- Config backups contain no VCS or AI credentials; restores validate JSON and replace one file atomically

## Releases

GitHub releases are created automatically from the `dist/` folder with
`scripts/create-release.sh`, e.g. `./scripts/create-release.sh --version
v1.0.0`. See `scripts/RELEASE_README.md` for details.

## Credits

Thanks to these open-source projects and tools:

- **Matt Pocock: Skills For Real Engineers** (https://github.com/mattpocock/skills) — engineering skills for OpenCode with proven workflows for code review, TDD, architecture design, and more.
- **codebase-memory-mcp** (https://github.com/DeusData/codebase-memory-mcp) — knowledge-graph indexing and auto-configured code intelligence for OpenCode with web UI and cross-repo links.
- **opencode-models-discovery** (https://github.com/yuhp/opencode-models-discovery) — model discovery for OpenCode: finds available models and providers dynamically instead of maintaining them by hand.
- **SearXNG** (https://github.com/searxng/searxng) — privacy-respecting metasearch engine, used as the shared `oc-searxng` container for keyless web search.
- **mcp-searxng** (https://github.com/ihor-sokoliuk/mcp-searxng) — MCP server for SearXNG, pre-installed in the sandbox images.
- **Bubble Tea** (https://github.com/charmbracelet/bubbletea) — Go framework for terminal UIs, together with **Bubbles** (https://github.com/charmbracelet/bubbles) and **Lip Gloss** (https://github.com/charmbracelet/lipgloss) the foundation of the TUI.

## Disclaimer

**BETA version — extensive tests are still needed.**

This project is not built by the OpenCode team and is not affiliated with OpenCode in any way.
