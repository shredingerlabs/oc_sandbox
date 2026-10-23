# Legacy: Direct Script Usage

All steps of the TUI workflow can also be run directly via the scripts in
`dist/scripts/` (or `~/.oc-sandbox/scripts/` after installation). This is the
legacy path — the Go TUI (`oc-sandbox`) is the recommended entry point.

## 1. Build an image

Build the desired sandbox edition once:

```bash
cd opencode-sandbox
./dist/scripts/build-container.sh web     # or: base, embedded, swdev, matlab, ros2, writing, latex, all
```

The script discovers editions live from the
`FROM ... AS opencode-sandbox-<name>` stages of `dist/Dockerfile` and
`dist/Dockerfile.custom`; `all` builds all discovered editions, including your
own custom ones.

This builds:

- `opencode-sandbox-base` — Python + core system packages (parent of all editions)
- `opencode-sandbox-web` — base + Node/TypeScript/Playwright
- `opencode-sandbox-embedded` — base + ARM toolchains/Arduino/MicroPython
- `opencode-sandbox-swdev` — base + C/C++, Java, Rust, Go + test frameworks
- `opencode-sandbox-matlab` — base + GNU Octave + Python scientific stack
- `opencode-sandbox-ros2` — base + ROS 2 Jazzy + colcon/rosdep/vcstool
- `opencode-sandbox-writing` — base + LibreOffice + pandoc
- `opencode-sandbox-latex` — base + latexmk + TeX Live collections
- `opencode-sandbox-pkm` — base only (Personal Knowledge Wiki edition)
- plus all custom editions from `dist/Dockerfile.custom`
- `oc-proxy` — Squid egress proxy image (started as a per-project pod member
  by `start.sh --use_proxy`; the image itself is only needed then)
- `oc-searxng` — SearXNG image for web search (started as a per-project pod
  member by `start.sh --search_mode searxng`; configuration in
  `dist/searxng/settings.yml`, survives updates)

## 2. Set up a project root

The start script takes exactly **one** path: a **project root** with the
following subfolders.

```
<PROJECT_ROOT>/
  project/             <- your actual repo (git clone/init goes here)
  .opencode_config/    <- OpenCode config, per-project, persistent
  .opencode_data/      <- OpenCode data incl. sessions & auth/credentials
  .ssh_local/          <- SSH keys + custom ssh config for this project
  .git_local/          <- git identity/settings + optional HTTPS credentials
    gitconfig             <- user.name/user.email, safe.directory, credential-store
    credentials           <- git credential-store (optional)
    gh-cli/hosts.yml      <- GitHub CLI token (alternative to SSH deploy keys)
    glab-cli/config.yml   <- GitLab CLI token (hosts: block)
  .cbm_cache/          <- CBM knowledge-graph database (persistent)
  .bash_local/         <- bash_profile for the container (persistent prompt/env tweaks)
```

The sensitive folders are **siblings** of `project/`, so the git repo never
sees them — not even accidentally via `git add .`.

Create a new project root with correct structure/permissions:

```bash
dist/scripts/init-project.sh ~/projects/customer-x
# ... clone the repo into ~/projects/customer-x/project, adjust git config ...
```

Note: re-running `init-project.sh` overwrites the project-specific templates
(incl. `.opencode_config/opencode.json`) — your own customizations there are
lost; only `proxy/allowlist.txt` is preserved on updates.
`dist/Dockerfile.custom` is also preserved on updates.

### `.git_local/` adjustments

```bash
$EDITOR ~/projects/customer-x/.git_local/gitconfig   # set user.name / user.email
```

### Fill `.ssh_local/`

```bash
ssh-keygen -t ed25519 -f ~/projects/customer-x/.ssh_local/id_ed25519_github -N ""
ssh-keygen -t ed25519 -f ~/projects/customer-x/.ssh_local/id_ed25519_gitlab -N ""
chmod 600 ~/projects/customer-x/.ssh_local/id_ed25519_*
```

Add the public keys as **deploy keys** (GitHub: repo → Settings → Deploy keys;
own GitLab: project → Deploy Keys). Adjust `.ssh_local/config`.

### Alternative to SSH: HTTPS + token (gh / glab)

Instead of SSH deploy keys you can use **HTTPS with a personal access token**.
The token is stored once and covers git authentication + the CLI tools
(issues, PRs etc.):

**GitHub (`gh`):**

```bash
cp templates/git_local/gh-cli/hosts.yml ~/projects/customer-x/.git_local/gh-cli/hosts.yml
$EDITOR ~/projects/customer-x/.git_local/gh-cli/hosts.yml   # enter oauth_token
chmod 600 ~/projects/customer-x/.git_local/gh-cli/hosts.yml
```

Token: GitHub → Settings → Developer settings → Personal access tokens →
Tokens (classic). Required scopes: `repo`, `read:org`, `workflow`.

**GitLab (`glab`):**

```bash
cp templates/git_local/glab-cli/config.yml ~/projects/customer-x/.git_local/glab-cli/config.yml
$EDITOR ~/projects/customer-x/.git_local/glab-cli/config.yml   # uncomment hosts: block, enter token
chmod 600 ~/projects/customer-x/.git_local/glab-cli/config.yml
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

The TUI VCS flow writes this entry automatically whenever a VCS token is
captured (GitHub: `https://oauth2:<token>@github.com`).

## 3. Start the sandbox

```bash
dist/scripts/start.sh ~/projects/customer-x --edition web            # full network
```

The `--edition` flag accepts any edition discovered from
`Dockerfile`/`Dockerfile.custom` (no fixed default).

```bash
dist/scripts/start.sh ~/projects/customer-x --edition web       # Web only
dist/scripts/start.sh ~/projects/customer-x --edition embedded  # Embedded only
dist/scripts/start.sh ~/projects/customer-x --edition base      # Minimal Python
dist/scripts/start.sh ~/projects/customer-x --edition swdev     # C/C++, Java, Rust, Go
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
| `--search_mode searxng`  | pasta   | no              | –                          | SearXNG web search (per-project pod member, no proxy) |
| `--start_web --detach`   | pasta   | no              | –                          | OpenCode web UI (port 4096)        |

Further single flags (not in the table): `--start_opencode` opens an OpenCode
TUI session in the container directly; `--detach` starts the container in the
background (without `--start_web` with `sleep infinity` as main process);
`--container-id <id>` sets the persisted container identity from the project
registry (otherwise: basename of the project root).

Examples:

```bash
# Coding, no restrictions (web edition)
dist/scripts/start.sh ~/projects/customer-x --edition web

# Embedded edition with HIL
dist/scripts/start.sh ~/projects/hil-tests --edition embedded --hil_mode

# Egress proxy (allowlist)
dist/scripts/start.sh ~/projects/customer-x --use_proxy

# Web search via the project's own SearXNG pod member
dist/scripts/start.sh ~/projects/customer-x --search_mode searxng

# Completely offline
dist/scripts/start.sh ~/projects/customer-x --offline

# HIL tests with scope + microcontrollers
dist/scripts/start.sh ~/projects/hil-tests --hil_mode

# With CBM knowledge-graph UI (default http://localhost:9749)
dist/scripts/start.sh ~/projects/customer-x --cbm_ui

# OpenCode web UI (default http://localhost:4096)
dist/scripts/start.sh ~/projects/customer-x --start_web --detach
```

> **`--cbm_ui`:** `codebase-memory-mcp` starts in the background with
> `autoindex: true` and serves a web UI on port 9749. The graph UI needs
> network access and therefore does not work with `--offline` (network=none);
> combinable with all other modes. If port 9749 is taken, `start.sh` picks the
> next free port up to 9849 and prints the URL.

> **`--start_web`:** starts the OpenCode web service (v2 background service,
> pinned to container port 4096) and publishes it on `127.0.0.1` (port scan
> 4096–4196 if 4096 is taken). With `--detach` the service runs as the
> container's main process; the URL or a one-time pair link is printed on
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

### OpenCode config & data (persistent, per project)

| Purpose                                          | Path in container         | Source in project root |
| ------------------------------------------------ | ------------------------- | ---------------------- |
| Config (`opencode.json`, agents, themes)         | `~/.config/opencode`      | `.opencode_config/`    |
| Data (sessions, history, **auth/credentials**)   | `~/.local/share/opencode` | `.opencode_data/`      |

> **Important:** `.opencode_data/` may contain API keys/auth tokens in plain
> text — restrict access rights, do not include in unattended backups/syncs.

Reset:

```bash
rm -rf ~/projects/customer-x/.opencode_config/* ~/projects/customer-x/.opencode_data/*
```

## 4. Uninstall (script)

The TUI offers uninstall under Settings → Uninstall. Directly via script,
`dist/scripts/uninstall.sh` removes only the installation folder, the symlinks
and optionally the config — never project roots or user data. Running
containers are detected and reported; files without removable permissions are
skipped and listed at the end.

```bash
dist/scripts/uninstall.sh                    # Interactive, confirm via [y/N]
dist/scripts/uninstall.sh --remove-config    # Also removes ~/.config/oc-sandbox/ (with backup)
dist/scripts/uninstall.sh --force            # No prompts (scripts/CI)
dist/scripts/uninstall.sh --dry-run          # Show only, delete nothing
```
