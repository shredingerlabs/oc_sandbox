# Legacy: Direct Script Usage

All steps of the WebUI workflow can also be run directly via the scripts in
`dist/scripts/` (or `~/.oc-sandbox/scripts/` after installation). This is the
legacy path — the WebUI (`oc-sandbox`) is the recommended entry point.

## 1. Build images

The builder is `build-images.sh` (toolchain sets). A workspace's image is a
**set image** assembled content-addressed from its toolchain list; the set's
components build on the base image.

```bash
cd opencode-sandbox
dist/scripts/build-images.sh --base                          # the base image
dist/scripts/build-images.sh --tc cpp,rust,java,go           # components
dist/scripts/build-images.sh --set cpp,rust,java,go          # the set image
dist/scripts/build-images.sh --all                           # base + every shipped component
```

- `--tc` takes comma-separated toolchain ids (valid against live discovery of
  `toolchains/recipes/` + `toolchains/custom/`; unknown ids refuse with a
  reason).
- `--set ""` assembles the base-only set (what chat/pkm workspaces run).
- An existing tag is never rebuilt — cached-skip. Deletes are the force:
  remove the image and build again.
- The shared infra images (`oc-proxy`, `oc-searxng`, `oc-stt`) stay outside
  this contract; `start.sh` builds them lazily on first need.

A first build of a never-built toolchain is long (one download-and-build per
toolchain); every workspace afterwards reuses it — adding an already-built
toolchain to a set costs seconds (assembly only).

## 2. Set up a workspace root

The start script takes exactly **one** path: a **workspace root** with the
following subfolders.

```
<WORKSPACE_ROOT>/
  workspace/           <- your actual repo (git clone/init goes here)
  .opencode_config/    <- OpenCode config, per-workspace, persistent
  .opencode_data/      <- OpenCode data incl. sessions & auth/credentials
  .ssh_local/          <- SSH keys + custom ssh config for this workspace
  .git_local/          <- git identity/settings + optional HTTPS credentials
    gitconfig             <- user.name/user.email, safe.directory, credential-store
    credentials           <- git credential-store (optional)
    gh-cli/hosts.yml      <- GitHub CLI token (alternative to SSH deploy keys)
    glab-cli/config.yml   <- GitLab CLI token (hosts: block)
  .cbm_cache/          <- CBM knowledge-graph database (persistent)
  .bash_local/         <- bash_profile for the container (persistent prompt/env tweaks)
```

The sensitive folders are **siblings** of `workspace/`, so the git repo never
sees them — not even accidentally via `git add .`.

Create a new workspace root with correct structure/permissions:

```bash
dist/scripts/init-workspace.sh ~/workspaces/customer-x
# ... clone the repo into ~/workspaces/customer-x/workspace, adjust git config ...
```

The optional `--workspace_kind` flag seeds the OpenCode templates accordingly:
`code` (default), `chat` (chat-style `AGENTS_chat.md`), or `pkm` (full
Personal-Knowledge-Wiki templates, also applied to cloned workspaces).

Note: re-running `init-workspace.sh` overwrites the workspace-specific templates
(incl. `.opencode_config/opencode.json`) — your own customizations there are
lost; only `proxy/allowlist.txt` and the toolchains user dirs
(`toolchains/custom/`, `toolchains/snapshots/`) are preserved on updates.
Your legacy `Dockerfile.custom` (from pre-toolchain-sets installs) is also
never touched — port it to `toolchains/custom/` recipes, see
`docs/porting-dockerfile-custom.md`.

### `.git_local/` adjustments

```bash
$EDITOR ~/workspaces/customer-x/.git_local/gitconfig   # set user.name / user.email
```

### Fill `.ssh_local/`

```bash
ssh-keygen -t ed25519 -f ~/workspaces/customer-x/.ssh_local/id_ed25519_github -N ""
ssh-keygen -t ed25519 -f ~/workspaces/customer-x/.ssh_local/id_ed25519_gitlab -N ""
chmod 600 ~/workspaces/customer-x/.ssh_local/id_ed25519_*
```

Add the public keys as **deploy keys** (GitHub: repo → Settings → Deploy keys;
own GitLab: workspace → Deploy Keys). Adjust `.ssh_local/config`.

### Alternative to SSH: HTTPS + token (gh / glab)

Instead of SSH deploy keys you can use **HTTPS with a personal access token**.
The token is stored once and covers git authentication + the CLI tools
(secretly stored workspace-locally):

**GitHub (`gh`):**
```bash
cp templates/git_local/gh-cli/hosts.yml ~/workspaces/customer-x/.git_local/gh-cli/hosts.yml
$EDITOR ~/workspaces/customer-x/.git_local/gh-cli/hosts.yml   # enter oauth_token
chmod 600 ~/workspaces/customer-x/.git_local/gh-cli/hosts.yml
```

Token: GitHub → Settings → Developer settings → Personal access tokens →
Tokens (classic). Required scopes: `repo`, `read:org`, `workflow`.

**GitLab (`glab`):**
```bash
cp templates/git_local/glab-cli/config.yml ~/workspaces/customer-x/.git_local/glab-cli/config.yml
$EDITOR ~/workspaces/customer-x/.git_local/glab-cli/config.yml   # uncomment hosts: block, enter token
chmod 600 ~/workspaces/customer-x/.git_local/glab-cli/config.yml
```

Token: GitLab → Preferences → Access Tokens. Required scopes: `api`,
`read_repository`, `write_repository`.

The **credential helper for git push/pull via HTTPS** is already active in the
gitconfig template (`[credential] helper = store --file=
/home/dev/.git_local/credentials`). Just add a host entry to
`.git_local/credentials`:

```
https://<user>:ghp_xxxxx@github.com
```

The WebUI VCS flow writes this entry automatically whenever a VCS token is
captured (GitHub: `https://oauth2:<token>@github.com`).

## 3. Start the sandbox

```bash
dist/scripts/start.sh ~/workspaces/customer-x --toolchains cpp,rust,java,go
```

`--toolchains` takes a comma-separated, order-free list of toolchain ids
(validated against live discovery; `""` selects the base-only set). The old
`--edition <name>` flag still works as an alias — it expands through the
manifest bundle map; `--edition` together with `--toolchains` refuses
(explicit conflict beats silent precedence). One of the two is required.

The container runs the derived `opencode-sandbox-set:<hash12>` image (the
hash derives from the sorted member list + recipe content hashes + the base
recipe — same derivation as the builder). A missing set image refuses with
the exact build command (`build-images.sh --set <ids>`) — the WebUI start
paths offer the Build now / Build later prompt instead.

```bash
dist/scripts/start.sh ~/workspaces/customer-x --toolchains ""       # base-only set
dist/scripts/start.sh ~/workspaces/customer-x --toolchains playwright,node-dev
dist/scripts/start.sh ~/workspaces/customer-x --edition swdev       # bundle alias
```

| Flag combination         | Network | Proxy           | Devices                    | Use case                           |
| ------------------------ | ------- | --------------- | -------------------------- | ---------------------------------- |
| *(none)*                 | pasta   | no              | –                          | Coding, full network               |
| `--use_proxy`            | pasta   | Squid allowlist | –                          | Restrictive network access         |
| `--offline`              | none    | no              | –                          | Air-gapped, local models only      |
| `--hil_mode`             | pasta   | no              | Scope + MCU (ttyUSB* etc.) | HIL tests                          |
| `--use_proxy --hil_mode` | pasta   | Squid allowlist | Scope + MCU                | HIL with restricted network        |
| `--cbm_ui`               | pasta   | no              | –                          | CBM knowledge-graph UI (port 9749) |
| `--use_proxy --cbm_ui`   | pasta   | Squid allowlist | –                          | Proxy + graph UI                   |
| `--search_mode searxng`  | pasta   | no              | –                          | SearXNG web search (per-workspace pod member, no proxy) |
| `--start_web --detach`   | pasta   | no              | –                          | OpenCode web UI (port 4096)        |

`pasta` is the primary rootless network mode; when the host has no usable pasta
interface (VPN/tunnel default route, offline, or IPv4-less) the start retries
dual-stack pasta and then `slirp4netns` (see the README troubleshooting entry).

Further single flags (not in the table): `--start_opencode` opens an OpenCode
TUI session in the container directly; `--detach` starts the container in the
background — every detached start runs the OpenCode v2 background service as
main process (`opencode service start && exec sleep infinity`) and publishes
port 4096 (except `--offline`);
`--container-id <id>` sets the persisted container identity from the workspace
registry (otherwise: basename of the workspace root).

Examples:

```bash
# Coding, no restrictions (swdev bundle alias)
dist/scripts/start.sh ~/workspaces/customer-x --edition swdev

# Embedded toolchains with HIL
dist/scripts/start.sh ~/workspaces/hil-tests --toolchains go,arm-embedded,arduino,platformio,micropython,picoscope,pyvisa,usb --hil_mode

# Egress proxy (allowlist)
dist/scripts/start.sh ~/workspaces/customer-x --use_proxy

# Web search via the workspace's own SearXNG pod member
dist/scripts/start.sh ~/workspaces/customer-x --search_mode searxng

# Completely offline
dist/scripts/start.sh ~/workspaces/customer-x --offline

# HIL tests with scope + microcontrollers
dist/scripts/start.sh ~/workspaces/hil-tests --hil_mode

# With CBM knowledge-graph UI (default http://localhost:9749)
dist/scripts/start.sh ~/workspaces/customer-x --cbm_ui

# OpenCode web UI (default http://localhost:4096)
dist/scripts/start.sh ~/workspaces/customer-x --start_web --detach
```

> **`--cbm_ui`:** `codebase-memory-mcp` starts in the background with
> `autoindex: true` and serves a web UI on port 9749. The graph UI needs
> network access and therefore does not work with `--offline` (network=none);
> combinable with all other modes. If port 9749 is taken, `start.sh` picks the
> next free port up to 9849 and prints the URL.

> **`--start_web`:** starts the OpenCode web service (v2 background service,
> pinned to container port 4096) and publishes it on `127.0.0.1` (port scan
> 4096–4196 if 4096 is taken). Every detached start publishes port 4096 and
> runs the service as the container's main process (`--start_web` only
> matters in the foreground); the URL or a one-time pair link is printed on
> access. Without `--detach`, `opencode serve` runs foreground; Ctrl+C stops
> and removes the container.

> **`--hil_mode` and USB safety:** the script resolves the real path of
> `/dev/scope0` (e.g. `/dev/bus/usb/007/055`) at runtime and mounts the parent
> bus directory (`/dev/bus/usb/007/`) into the container. **All USB devices on
> the same physical USB bus number** are visible in the container — not just
> the scope. A finer scope (single device) is not possible with libusb, since
> `libps2000` itself scans `/dev/bus/usb/*` via `readdir`. The risk stays
> limited: only that one bus is mounted, not `/dev/bus/usb` as a whole.

The container starts an interactive shell. Start OpenCode with:

```bash
opencode
```

### OpenCode config & data (persistent, per workspace)

| Purpose                                          | Path in container         | Source in workspace root |
| ------------------------------------------------ | ------------------------- | ---------------------- |
| Config (`opencode.json`, agents, themes)         | `~/.config/opencode`      | `.opencode_config/`    |
| Data (sessions, history, **auth/credentials**)   | `~/.local/share/opencode` | `.opencode_data/`      |

> **Important:** `.opencode_data/` may contain API keys/auth tokens in plain
> text — restrict access rights, do not include in unattended backups/syncs.

Reset:

```bash
rm -rf ~/workspaces/customer-x/.opencode_config/* ~/workspaces/customer-x/.opencode_data/*
```

## 4. Uninstall (script)

The WebUI offers uninstall under Settings → Uninstall. Directly via script,
`dist/scripts/uninstall.sh` removes only the installation folder, the symlinks
and optionally the config — never workspace roots or user data. Running
containers are detected and reported; files without removable permissions are
skipped and listed at the end.

```bash
dist/scripts/uninstall.sh                    # Interactive, confirm via [y/N]
dist/scripts/uninstall.sh --remove-config    # Also removes ~/.config/oc-sandbox/ (with backup)
dist/scripts/uninstall.sh --force            # No prompts (scripts/CI)
dist/scripts/uninstall.sh --dry-run          # Show only, delete nothing
```

Full flag reference:

| Flag                     | Meaning                                                                    |
| ------------------------ | -------------------------------------------------------------------------- |
| `--install_path <path>`  | Installation path (default: `$HOME/.oc-sandbox`)                            |
| `--remove-config`        | Also removes `~/.config/oc-sandbox/` (with backup)                          |
| `--no-backup`            | No backup (also for `--remove-config`)                                      |
| `--no-symlinks`          | Don't remove symlinks                                                       |
| `--remove-shortcuts`     | Also removes desktop shortcuts (`.desktop`/`.lnk`/`.app`) and installed icons |
| `--force`                | No prompts (scripts/CI)                                                     |
| `--dry-run`              | Show only, delete nothing                                                   |
| `--verbose`              | Detailed output                                                             |
