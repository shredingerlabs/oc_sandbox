# OpenCode Sandbox

A toolchain-set development sandbox running OpenCode in rootless Podman containers
(Ubuntu 24.04), managed through its browser WebUI.

**⚠ BETA version — extensive tests are still needed.**

## Features

- **Sandbox** — rootless Podman containers with hardened settings
  (`--cap-drop=ALL`, `no-new-privileges`), one container per workspace, multiple
  sandboxes in parallel
- **Toolchain sets** — fine-grained toolchain recipes (`dist/toolchains/`) composed
  into a per-workspace set image: named bundles (software dev, web, embedded, ROS 2,
  LaTeX, …) plus any custom mix; every workspace's image is assembled
  content-addressed at build time — adding an already-built toolchain costs
  seconds, a never-built one costs one build reused by every workspace thereafter
- **Search tools** — per-workspace web search: OpenCode `websearch` (Exa/Parallel,
  no API key), a per-workspace SearXNG pod member, or none; Context7-MCP for docs
  search in every mode
- **Hardware in Loop (HIL) mode** — USB oscilloscope (PicoScope) and
  microcontroller device passthrough for HIL tests
- **Dictation** — speak a prompt into the OpenCode TUI: per-workspace local
  whisper.cpp server (`oc-stt`), transcript review before anything is sent,
  browser sidecar page for web sessions
- **Workspace & git handling** — workspace wizard with VCS setup (GitHub, GitLab,
  own GitLab), hidden token storage, git identity, in-container cloning
- **Prepared skills** — first-run setup installs an engineering skill set for
  OpenCode automatically
- **All manageable from the WebUI** — `oc-sandbox` serves the browser interface:
  workspaces, images, containers, settings, update, and uninstall; a minimal
  terminal launcher face carries the tokenized URL and the quit flow

For details on direct script usage, see the
[legacy script guide](docs/legacy-script-usage.md).

## Starting Guide

[https://shredingerlabs.github.io/oc_sandbox/](https://shredingerlabs.github.io/oc_sandbox/)
— step-by-step walkthrough of installation, first workspace, and daily WebUI workflow.

## Installation

Prerequisites (Ubuntu 24.04 or similar):

```bash
sudo apt-get update
sudo apt-get install -y git curl podman passt fuse-overlayfs jq slirp4netns
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

By default the installer creates the symlink `oc-sandbox` (→ the binary in
`~/.local/bin`) and a desktop shortcut; both are opt-out via `--no_symlinks` /
`--no_shortcut`. The script never uses `sudo` automatically. Further options:
`--version <tag>`, `--yes` (non-interactive update), `--verbose`, `--help`.

## Launch and WebUI

Start with:

```bash
oc-sandbox          # or: ~/.oc-sandbox/bin/oc-sandbox
```

`oc-sandbox` is a standalone Go binary, built at release time (ADR-0020), with
the WebUI embedded in it (`go:embed` — no separate web assets). Starting it
performs a **one-time first-start migration** on pre-rename installs
(`projects.json`/`project/` trees → `workspaces.json`/`workspace/`, with
`backups/` created before anything changes — ADR-0030/0032), then starts the
WebUI server on `127.0.0.1` behind a per-launch token, auto-opens the browser
tab, and shows a minimal launcher face in the terminal: the full tokenized URL,
**Open WebUI** (re-opens the tab after a closed window or a failed auto-open),
and **Quit**. A second launch while an instance runs points at the running one
and exits. Quitting asks "Stop running containers before quitting? [y/N]" —
default is to leave them running; the token dies with the process, so a stale
tab shows the "server stopped — relaunch oc-sandbox" screen.

### The screens

The sidebar tree groups the screens; the header carries the **Standard /
Advanced** display-mode control (re-renders immediately, persisted in
`global_config.json`) and the dark/bright theme toggle. The footer shows the
version and the update banner when an update is available.

| Screen                          | Function                                                                                                                                                                    |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Workspace → Open**            | Registered workspaces as cards (favorite workspaces pinned on top, sorted by last use with relative dates) with start buttons **Console / OpenCode / Web**, gear, restart, stop, and dictation-sidecar access while running |
| **Workspace → New**             | Create a workspace (the wizard below)                                                                                                                                       |
| **Settings → Workspace Settings** | Per-workspace settings (toolchains, modes incl. HIL, start option, VCS/AI provider, proxy toggle) with Standard/Advanced matrix rows, import, remove-from-list          |
| **Settings → VCS Tracker / AI-Provider** | Reusable catalogs feeding the wizard and settings screens (token-free, per ADR-0031)                                                                          |
| **Settings → Images / Containers** | Image build (`● built` / `○ missing` markers) and delete (`● in use` / `○ unused`, with sizes); container start/stop/delete (graceful pod-as-unit stop/delete — companions ride the pod — verified-gone registry flip) |
| **Settings → Export / Restore Config** | Named config backup round trip (advanced-only)                                                                                                                 |
| **Settings → Update / Uninstall** | Update card (§ update below) and the typed-`uninstall` wizard                                                                                                        |
| **Settings → Proxy**            | Per-workspace proxy member: start/stop/rebuild and the inline allowlist editor (advanced-only)                                                                              |

### New Workspace

The wizard asks for the **workspace source** ("Blank Workspace" or "From Repo" — clones
happen inside the container during first-run setup), creates the workspace root,
collects VCS host (`none`, GitHub, public GitLab, own GitLab) and tokens (hidden
input, stored workspace-locally in `.git_local/`; both selects are fed by the
reusable catalogs with an inline "New…" entry so an empty catalog never dead-ends),
configures the git identity, and picks a start option:

Other git hosts are unsupported — pick `none` and configure git inside the
workspace.

- `console` — interactive shell in the container
- `opencode` — OpenCode TUI
- `web` — OpenCode web UI (background service on port 4096, published on
  `127.0.0.1`; **Web access** opens a fresh one-time sign-in link via
  `opencode pair` in a new tab within the click — a plain-URL fallback appears
  when pairing fails)
The **Agent** row picks the workspace kind: `Code` (default) or chat/PKM
variants (PKM pins the toolchain set to base only — un-pinning restores
the prior selection). The **AI provider** selection offers `none` and
`Custom` (name, endpoint (OpenAI-compatible API), token — written to
`opencode.json` v2 and `.opencode_data/credentials/`, see
ADR-0022/0025). Existing credentials are kept unless **Replace** is
chosen.

The **Toolchain set** row offers the manifest bundles as chips (chat/pkm
rendered "base only"); a bundle preselects the toolchain multi-select
under it (Advanced-only: flat alphabetical list with live cached
markers, invalid units disabled with their reason, "n of m selected"
header) — editing the multi-select reads **Custom**. Selecting any
hardware toolchain presets the **HIL** mode toggle (a preset, not a
pin). While the selection's set image is missing, an **Image build** row
offers **Build now** / **Build later** (default: Build now) with the
honest cost line — how many toolchains need building, never time
estimates. The create summary confirm shows the full plan before
anything runs.

First-run setup runs automatically in the background (CBM configuration, then —
for `code` workspaces — interactive skills setup in a host terminal window;
`chat` and `pkm` workspaces skip the skills stage — the window opens
automatically when the skills stage arrives; on a headless/SSH machine the
browser shows the exact copyable command instead). On failure the screen offers
`Retry` and `Cancel setup` (plus `Change URL` when a clone failed); a retry
repeats only the incomplete step, and cancelling preserves partial progress.
**OpenCode must be exited via `/exit`.**

### Open Workspace

Opening a workspace whose container is already running enters it directly:
`opencode`/`console` open a host terminal window (Linux probe list with an
`OC_SANDBOX_TERMINAL` override; Windows Terminal via `cmd.exe` interop on WSL),
`web` via a one-time pair link (the fresh link opens in a new tab within the
click; a plain-URL fallback appears when pairing fails). Each
workspace card has start buttons **Console / OpenCode / Web** (start + access in
one click) and shows the last active date (relative: `Today`, `Yesterday`,
`n d ago`, else the date) instead of the workspace path. If a running container has no published web port, it is replaced
by a verified `--start_web` restart (auto-adopt). A headless/SSH machine gets
the exact copyable command in the browser instead, with the session pending
until it completes in your own terminal or is cancelled.

### Workspace settings

**Settings → Workspace Settings** re-edits the full stored set
(toolchains, modes incl. HIL, start option, VCS, AI provider, proxy toggle) for any
registered workspace — including running containers — with the Standard/Advanced
matrix so every stored option stays visible and editable in either mode.
Changes are persisted to `sandbox_config.json` and apply on next container
start, never live. Replacements of a running container (toolchain-mismatch
switch, re-adopt) ask an extra confirm first — an agent mid-task is never
killed silently.

### VCS login (re-login)

**Workspace Settings → VCS Tracking** re-establishes a workspace's
VCS credentials — expired/invalid token, switched account — without
re-running the wizard: **New token login** pastes a token (a running container
validates it with the in-container `gh`/`glab` CLI first; a rejection
leaves the stored credentials untouched), **Re-login** keeps the stored
credentials while the tracker moves, and **Web login**
runs the browser device flow — the one-time code and verification link
render inline while the container CLI polls until authorized
(github/gitlab only; own-GitLab instances may need an admin OAuth app, in
which case paste the token). Both paths end in the same host-side
credential files (`gh-cli/hosts.yml`, `glab-cli/config.yml` or
`vcs/hosts.yml`, plus the git credential bridge). Login needs a running
container; pasting works while stopped. See ADR-0035.

### Update / Uninstall

**Settings → Update** shows installed vs. latest release version, checks for
updates at every server start (footer banner when available; "Check now" on
demand), and runs the install script non-interactively, fetched fresh from the
repository's `main` branch — `proxy/allowlist.txt` (kept after a prompt,
default keep), `searxng/settings.yml`, `toolchains/custom/`,
`toolchains/snapshots/`, and user configs are preserved; your legacy
`Dockerfile.custom` is never touched (see the release notes for the porting
recipe). Recipe rewrites arrive wholesale — `toolchains/recipes/`,
`manifest.json`, and `base.Dockerfile` are overwritten (that is how pinned
versions bump). While an update runs it is globally exclusive
(no other operations); the live log streams in the browser. When applied, the
banner clears, VERSION is re-read, and a "Update installed — restart
oc-sandbox to apply" note appears — the restart is yours (the per-launch token
never surprises you with reconnects), and restarting soon after an update is
expected: between install and restart the old binary refuses against the new
toolchains world. The update never builds anything; its
completion runs an image GC pass that reclaims the toolchain images nothing
derives from the current recipes anymore (containers keep theirs pinned), and
the log pane carries the "reclaimed ~X GB" summary line.

**Settings → Uninstall** runs a guided wizard (warning → options → typed
`uninstall` confirmation). It removes only the installation, never workspace
roots or user data. Details: `dist/scripts/uninstall.sh --help`.

## Toolchain sets

A workspace's image is composed from its **toolchain set** — the list of
toolchain ids stored per workspace (`toolchains` in `sandbox_config.json`).
Each toolchain is one pinned recipe file in `dist/toolchains/`:

- `base.Dockerfile` — the fixed base recipe (Ubuntu 24.04, Python 3 + pip/venv,
  git/gh/glab, OpenCode + codebase-memory-mcp, node + mcp-searxng, the dev
  user). Its content hash is the family identity: any base edit re-addresses
  every image at the next build.
- `recipes/` — the shipped toolchain recipes (cpp, go, rust, java,
  playwright, ros2, latex, … — see `manifest.json`'s bundles for the curated
  sets). Wholesale-overwritten on updates: that is how version pins bump.
- `custom/` — your own recipes (never overwritten; a commented example
  template is recreated when the dir is empty). One file per toolchain,
  `RUN`/`ENV`/`USER` lines only, everything pinned (no floating `latest`).

The Images screen lists the base image, one row per toolchain component, the
label-enumerated set images, the shared infra images (`oc-searxng`, `oc-stt`)
and a **Legacy** group for old pre-toolchain-sets images (delete-only, never
auto-deleted). Build all rides `build-images.sh --all`; per-row builds ride
`--base` / `--tc <id>`.

**Custom toolchains:** add a recipe file to `toolchains/custom/` on your
install (grammar and pin rules in `docs/porting-dockerfile-custom.md`); it
appears automatically in the WebUI's toolchain multi-select and Images screen.
The file is never overwritten on updates.

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

`start.sh --hil_mode` (or the HIL switch in Workspace Settings) mounts
`/dev/hil` live into the container. Note: the whole USB bus of the scope is
mounted, not just the one device (libusb limitation); serial devices may appear
as `nobody:nogroup` — see [troubleshooting](#troubleshooting).

## Dictation

Dictation lets you speak a prompt instead of typing it. Every dictation-enabled
workspace runs its own local speech-to-text server — the `oc-stt` pod member,
a whisper.cpp server with the small multilingual ggml model baked into the
image (no runtime downloads, no cloud services). Audio is persisted nowhere
beyond the transcription request, and transcription happens entirely on your
own machine. See ADR-0036.

### The dictation toggle

Dictation is governed by a per-workspace toggle (`use_stt` in
`sandbox_config.json`), like the proxy toggle:

- **Preselected ON for new workspaces** — the wizard row is advanced-only
  (WebUI Standard display mode hides it and silently records the preset;
  Advanced shows it like every other row).
- **OFF for pre-feature workspaces** — enable it once in workspace settings;
  this keeps their next start from silently growing RAM usage.
- **Applies on next container start**, never live.
- **Offline runs exclude the member**: `--offline` starts no `oc-stt` member
  and publishes nothing (a silent skip, not an error).

A running `oc-stt` member uses ~≤1.5 GB RAM (the baked model); it starts and
stops with the workspace pod like every other pod member.

### In the OpenCode TUI (`ctrl+alt+s`)

Press `ctrl+alt+s` once to start recording, again to stop (recordings stop and
transcribe automatically after ~5 minutes). The transcript opens in an
editable review dialog:

- **`ctrl+enter` submits** the edited text to your session.
- **`esc` discards** — nothing is ever submitted without that explicit confirm.
- **Empty text closes the dialog silently.**
- **A failed send keeps the dialog open** with your edited text and an error
  toast; submit again to retry (never auto-retried).
- From the **home route**, dictation creates a session first (exactly like
  typing a prompt there); on any other route the transcript is discarded with
  a warning toast.

The command is also reachable as **`/dictate`** from the palette/slash UI.

**Keybind note:** `ctrl+alt+s` is verified free against the shipped
opencode keybind defaults and decodes on terminals that do not report
modifiers (SSH/tmux/legacy encoding) — `ctrl+shift+<letter>` chords collapse
to a single swallowed control byte there. No stock keybind is touched and
the workspace's `cli.json` is left alone. You can rebind the command there
(`"keybinds": {"dictation.toggle": "..."}`); the `/dictate` palette entry
stays.

**Clipboard paste is the failure fallback only:** if the dialog route cannot
deliver (plugin-API drift after an update, or a terminal send failure), the
transcript is copied to your clipboard (OSC52) with a paste toast instead.
Nothing is auto-sent in either route.

### Web sessions (dictation sidecar)

For OpenCode web sessions, the `oc-stt` member serves a small local page
(the dictation sidecar) on its published loopback port — default
`http://127.0.0.1:8091` (the scan starts above the WebUI's own 8090; the
start log names the actual port if it moved, and `podman port
opencode-sandbox-pod-<id>` shows it). Open it with the microphone icon on
the workspace card (Workspace → Open — visible while the workspace runs
dictation-enabled; the click resolves the live port and opens a new tab),
or navigate to the port directly. Record with the
browser microphone and copy the transcript into the web composer. Chrome and
Firefox record webm/opus, Safari falls back to mp4/AAC; recordings stay
in-memory. The sidecar is reachable only from your own machine (loopback
publish).

Workspace cards on that screen also carry a star icon: clicking it favorites
the workspace, moving its card into the **Favorite workspaces** section at the
top of Workspace → Open (clicking again moves it back into Registered
workspaces). Favorites keep their section regardless of the last-used order,
persist in the registry across reloads and oc-sandbox restarts, and change
nothing else — the flag is purely organizational.

### Degradation matrix

| Situation | Behavior |
| --------- | -------- |
| No microphone / headless machine | The `oc-stt` member still runs (the sidecar path works); recording in the OpenCode TUI toasts "no audio input". |
| WSL2 (WSLg) | The WSLg PulseAudio server is auto-detected (`/mnt/wslg/PulseServer`) — no manual setup. |
| Audio session died since boot | The first transcription attempt doubles as a health check and toasts "no audio input" instead of yielding an empty transcript. |
| `--offline` run | The STT member is excluded entirely (silent skip). |
| STT member down / unreachable | A clear "STT member unreachable" toast; transcription is skipped. |
| SELinux distro | The PulseAudio socket mount is deliberately plain — on SELinux hosts append `label=disable` to the socket bind mount (Ubuntu/AppArmor unaffected). |

### Operations notes

- The `oc-stt` image joins **Images** (build and delete) like
  `oc-searxng` (it is built lazily on the first dictation-enabled start too).
- OpenCode and every other toolchain install is **pinned** in the recipes;
  new versions arrive via the update flow's recipe rewrites — rebuild after
  an update to pick them up, and **re-verify dictation after image
  rebuilds** (ADR-0036).
- **Language pinning**: edit the workspace's `.opencode_config/stt.json`
  (`{"language": "auto"}` by default; e.g. `"de"`), then restart the
  container — the value applies on next start only.
- The `stt.json` file and the dictation plugin are re-seeded on every start
  (update-safe; a pinned language is never overwritten).

## Legacy

The WebUI covers everything. If you prefer calling the scripts directly
(`build-images.sh`, `init-workspace.sh`, `start.sh`, `uninstall.sh`), see
**[docs/legacy-script-usage.md](docs/legacy-script-usage.md)** — the complete
flag reference, workspace-root structure, and credential setup for direct usage.

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

### Start fails with `pasta failed ... External interface not usable`

```
Error: starting some containers: internal libpod error
time="..." level=error msg="\"pasta failed with exit code 1:\nExternal interface not usable\n\""
```

Pasta needs a host interface it can copy an address and gateway from. A VPN
or other tunnel (WireGuard, OpenVPN, Tailscale) that owns the default route
without a MAC, an offline laptop, or a host with only an IPv6 route can make
every start fail this way. The start now retries dual-stack pasta and then
`slirp4netns` automatically (`slirp4netns` must be installed; Podman 6 removed
it). If both fail: reconnect the host network, install `slirp4netns` (Podman
< 6), or use the workspace's offline mode. A plain
`podman run --rm alpine true` failing with the same message confirms the host,
not the sandbox, is the problem.

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
- Workspace-root structure strictly separates code (`workspace/`) from secrets
  (`.ssh_local/`, `.opencode_data/`, `.git_local/`)
- Per-workspace git keys, git identity, and OpenCode login without script edits
- SSH-key and git-credential permissions are checked at start
- Optional network egress via proxy allowlist (the WebUI's Settings → Proxy,
  advanced-only, manages the per-workspace proxy member — start/stop/rebuild —
  and an inline allowlist editor; each workspace seeds its own
  `.opencode_config/allowlist.txt` from the shared `proxy/allowlist.txt`, squid
  syntax is validated on save, and the `oc-proxy` image is rebuilt on every
  proxied start); `--offline` for no-network runs
- The WebUI binds `127.0.0.1` behind a per-launch tokenized URL only — nothing
  re-enterable is persisted, and the token dies with the process
- HIL mode uses bus-level passthrough (only the scope's USB bus, never `/dev/bus/usb` entirely)
- Dictation audio never leaves the machine: the per-workspace `oc-stt` member transcribes locally, audio is persisted nowhere beyond the transcription request, and the STT port is published on loopback only
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
- **SearXNG** (https://github.com/searxng/searxng) — privacy-respecting metasearch engine, run as a per-workspace pod member (`oc-searxng`) for keyless web search.
- **mcp-searxng** (https://github.com/ihor-sokoliuk/mcp-searxng) — MCP server for SearXNG, pre-installed in the sandbox images.
- **Bubble Tea** (https://github.com/charmbracelet/bubbletea) — Go framework for terminal UIs, together with **Bubbles** (https://github.com/charmbracelet/bubbles) and **Lip Gloss** (https://github.com/charmbracelet/lipgloss) the foundation of the minimal launcher face.

## Disclaimer

**BETA version — extensive tests are still needed.**

This project is not built by the OpenCode team and is not affiliated with OpenCode in any way.
