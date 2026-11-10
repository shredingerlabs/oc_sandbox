# WebUI-First oc-sandbox Specification

Workspace rename with one-time migration · minimal TUI launcher with embedded WebUI · always-on OpenCode web access.

Assembled 2026-11-10 from the closed wayfinder tickets on
[Map: WebUI-first oc-sandbox — Workspace rename, minimal TUI + WebUI, always-on OpenCode web](https://github.com/shredingerlabs/oc_sandbox/issues/131) (+ its
pre-map decisions, recorded in the map Notes). Wish-list numbering (§1, §2.1, §2.2,
§3, §4, §4.1) follows the map Notes' record of the user request of 2026-11-09; the
checklist at the end ties every numbered item to a section. Implementation planning
happens outside the map.

## Decision Record

| Area | Decided on | Recorded in |
|---|---|---|
| Workspace rename | [Grilling: Workspace terminology mapping](https://github.com/shredingerlabs/oc_sandbox/issues/134) | below §1; ADR-0030; CONTEXT.md terms |
| Migration + compatibility | [Grilling: Migration and compatibility plan](https://github.com/shredingerlabs/oc_sandbox/issues/140) | below §2 |
| Serving architecture | [Research: WebUI serving architecture for the single Go binary](https://github.com/shredingerlabs/oc_sandbox/issues/132) | below §3; findings on `research/webui-serving-architecture` |
| Minimal TUI launcher | [Grilling: Minimal TUI launcher behavior](https://github.com/shredingerlabs/oc_sandbox/issues/138) | below §4 |
| Design language + icons | [Prototype: WebUI design mockup](https://github.com/shredingerlabs/oc_sandbox/issues/136), [Research: Edition icon set for workspace cards](https://github.com/shredingerlabs/oc_sandbox/issues/143) | below §5; prototype asset; icon findings on `research/edition-icon-set` |
| Standard/Advanced matrix | [Grilling: Standard/Advanced presentation matrix](https://github.com/shredingerlabs/oc_sandbox/issues/137) | below §6 |
| Screens + menus | #136 (signed off), #137 amendments folded | below §7 |
| Global catalogs | [Grilling: Global VCS-tracker and AI-provider catalogs](https://github.com/shredingerlabs/oc_sandbox/issues/135) | below §8; ADR-0031 |
| API surface | [Grilling: WebUI API surface](https://github.com/shredingerlabs/oc_sandbox/issues/139) | below §9 |
| Always-on web access | [Research: Always-on OpenCode web access in every start mode](https://github.com/shredingerlabs/oc_sandbox/issues/133), docs-drift verdict in [Research: opencode web/attach docs drift vs shipped build](https://github.com/shredingerlabs/oc_sandbox/issues/142) | below §10; findings on `research/always-on-opencode-web`, `research/opencode-web-attach` |
| First-run setup | [Grilling: First-run setup interaction in the WebUI](https://github.com/shredingerlabs/oc_sandbox/issues/144) | below §11 |
| Self-update behavior | [Grilling: WebUI behavior during self-update](https://github.com/shredingerlabs/oc_sandbox/issues/146) | below §12 |
| V2 design-for notes | [Grilling: V2 design-for constraints on V1 data shapes](https://github.com/shredingerlabs/oc_sandbox/issues/145) | below §13 |

## Problem Statement

oc-sandbox's primary UI is a full-screen BubbleTea TUI, built on the pre-rename noun
**project** that has spread across user-facing words, data files, scripts, and the
on-disk layout for years. Daily use needs the full option surface for simple tasks,
and OpenCode's web access is only available when the configured start option is
`web`. The effort replaces the terminal surface with a browser UI served by the
single Go binary, renames the central noun everywhere it can be seen, and keeps
OpenCode web access alive in every start mode — released as one safely-migrating
update that retires the full TUI through a parity gate.

## Solution

- One Go binary (`oc-sandbox`) hosts an **embedded local WebUI**: repo-root `web/`
  via `go:embed`, no-node frontend (HTML/CSS/JS + HTMX), bound to 127.0.0.1 with a
  per-launch CSPRNG token carried and validated in the URL path (`/t/<token>/…`).
- The terminal face shrinks to the **minimal TUI launcher** (Open WebUI / Quit +
  URL/status line). Webserver runs launch→quit; second launch points at the running
  instance; quit pushes `server-shutdown` to open tabs.
- The Rename plan (below §1) renames project→workspace across words, data files,
  scripts, Go identifiers, and disk layout; a **one-time first-start migration**
  (§2) converts existing installs, seeds the catalogs, and bumps registry version
  to 2.0.
- **Standard is the default display mode** (§6): presets not pinning; Advanced
  reveals everything.
- **Always-on OpenCode web access** (§10): port 4096 published (auto-scan up) and
  the v2 background service running in every detached start (except `--offline`).
- Global **token-free catalogs** for VCS trackers and AI providers (§8) using the
  template materialization model.
- **Parity gate** (§14): the full BubbleTea TUI stays reachable behind a hidden
  `--full-tui` launch flag until the §4.1-derived checklist passes, then flip and
  delete (#98→#99 precedent). One release carries rename + migration + WebUI +
  minimal TUI.

## User Stories

1. As an existing-install user, I want first start of the new release to migrate everything automatically, so that my workspaces keep working without manual file edits.
2. As a user, I want one vocabulary — workspace — in every screen, script, document, and file name, so that no pre-rename noun survives where I can see it.
3. As a user whose migration hits an error, I want an abort with `backups/` as the recovery path, so that damage is capped and re-runnability is guaranteed.
4. As a Standard user, I want workspace creation to need only name, source, agent, edition, and credentials, so that everyday creation avoids the full option surface.
5. As a Standard user, I want the Open action to always lead into OpenCode's web UI, so that I never confront terminal concepts I do not use.
6. As an Advanced user, I want every stored option visible and editable, so that nothing is configuration-file-only.
7. As a cross-mode user, I want Standard↔Advanced re-editing to just work in either order, so that the modes never fight over stored truth.
8. As a user, I want the WebUI on 127.0.0.1 behind a per-launch tokenized URL, so that only my browser can reach it and nothing re-enterable is persisted.
9. As a launcher user, I want the browser tab to auto-open at launch and the Open WebUI button to cover every failure, so that I never hunt for the URL.
10. As a second-launch user, I want the already-running instance pointed out and exit 0, so that concurrent launches cannot corrupt shared state.
11. As a quitting user, I want the containers question to default to leaving them running, so that agent sessions inside them are never killed accidentally.
12. As a WebUI user when the server goes away, I want a persistent "server stopped — relaunch oc-sandbox" screen, so that a dead tab is explained instead of dangling.
13. As a user of any start option, I want OpenCode's published web port to exist in every start mode, so that web access never depends on the configured start option.
14. As a WebUI user, I want Web access to fetch a fresh one-time sign-in link and open it in a new tab within my click, so that pairing needs no copy-paste.
15. As a desktop user, I want console/OpenCode access and first-run skills setup to open host terminal windows, so that interactive sessions get real terminal input.
16. As a headless/SSH user, I want the exact copyable command shown in the browser instead, so that I can run the session in my own terminal.
17. As a user whose running container is about to be replaced (edition mismatch switch, web re-adopt), I want an extra confirmation, so that an agent mid-task is never killed silently.
18. As a creation-flow user, I want to pick VCS tracker and AI provider from reusable catalogs and type the token once, so that I stop retyping endpoints and git identities.
19. As a catalog editor, I want catalog edits never to cascade into existing workspaces, so that my materialized instances keep their configuration.
20. As a first-run user, I want inline "New…" in both catalog selects, so that an empty catalog never dead-ends creation.
21. As a user whose skills-setup terminal window got closed hard, I want Cancel setup with partial progress preserved, so that a stale pending state never blocks the workspace forever.
22. As an updating user, I want the version check at server start with a footer banner, "Check now" on demand, and one globally exclusive update op with a live log, so that updating is calm and serialized.
23. As a post-install user, I want a "Update installed — restart oc-sandbox to apply" note rather than automatic restart, so that the per-launch token never surprises me with reconnects.
24. As an importer of a legacy-layout folder, I want an explicit "Convert to workspace layout" action behind an informational prompt, so that nothing transforms my folders silently.
25. As a user importing an already-canonical folder, I want the ordinary import summary flow, so that matching roots register without surprises.
26. As a direct CLI user, I want the foreground `start.sh --start_web` contract unchanged, so that scripts and outside tooling do not break.
27. As a maintainer, I want the full TUI reachable behind a hidden flag until the parity checklist passes, so that the flip-then-delete sequence stays precedented and verifiable.

## Implementation Decisions

### 1. Workspace rename (§1) — decided in [Grilling: Workspace terminology mapping](https://github.com/shredingerlabs/oc_sandbox/issues/134); ADR-0030

**workspace** is the domain noun on every surface: UI strings, glossary keys,
file names, data keys, scripts, docs, and Go identifiers.

| Surface | Old → New |
|---|---|
| Registry file | `projects.json` → **`workspaces.json`** — top key `projects` → `workspaces`; entry fields `name` (slug `^[a-zA-Z0-9_-]+$`), `path`, `container_id`, `last_used`, `container_status`, `git_tracking`, `repo_url` unchanged |
| global_config.json | `default_project_path` → **`default_workspace_path`** |
| sandbox_config.json | `project_source` → **`workspace_source`** (values `empty`/`cloned`), `project_kind` → **`workspace_kind`** (values `code`/`chat`/`pkm`) |
| Wizard | Blank Project → **Blank Workspace**; From URL → **From Repo**; "Creating project X" / "Project created successfully." and "Repo URL is required for a From Repo workspace." → workspace wording |
| Menus/labels | Open Project → **Open Workspace**; New Project → **New Workspace**; Create Project → **Create Workspace**; "Project(settings) — X" → **Workspace Settings — X**; Change Project Settings → **Change Workspace Settings**; Import project (tab/label) → **Import workspace** / **Import Workspace** (button); Remove project from list → **Remove workspace from list**; "No projects registered" → **No workspaces registered**; Project name/path → **Workspace name/path**; footer/help → workspace |
| Scripts | `init-project.sh` → **`init-workspace.sh`**; `configure-project.sh` → **`configure-workspace.sh`** (refs in install.sh, uninstall.sh, Go callers, docs); `--project_kind` → **`--workspace_kind`** (no alias); positional arg name → **`<workspace-root>`**; internal vars `PROJECT_*` → **`WORKSPACE_*`** (`WORKSPACE_ROOT` etc.); script output English, needed minimum — German echoes disappear, keep precise errors + `--help` + exactly one final success line per script (e.g. `Workspace root created: <path>`); German next-step tutorial echoes dropped |
| Go identifiers | `internal/newproject` → **`newworkspace`**; `internal/openproject` → **`openworkspace`**; `internal/projectsettings` → **`workspacesettings`**; `registry.Project` → **`registry.Workspace`**; `DefaultProjectPath` → `DefaultWorkspacePath`; JSON struct tags follow the data renames; user-visible errors ("project not found", "projects.json not found") → workspace; fixtures/goldens/smoke testdata ride along |
| On-disk layout | content dir `project/` → **`workspace/`** (init skeleton, `--repo_url` clone target, import layout check, pkm vault `notes/`/`raw/`/`wiki/`, `.gitignore`, testdata Dockerfiles); container mount `-v "…/workspace:/home/dev/workspace:Z"`; container templates' persistent-dir lines → `/home/dev/workspace`; **sibling infra dirs unchanged** (`.opencode_config/`, `.opencode_data/`, `.ssh_local/`, `.git_local/`, `.cbm_cache/`) |
| CBM wiring | container templates' session line: `codebase-memory-mcp (project=home-dev-workspace)` |
| Docs | README.md, dist/README.md, docs/index.html (German prose stays German, "Workspace" loanword, quoted script output updated), docs/legacy-script-usage.md, scripts/RELEASE_README.md, scripts/USAGE_EXAMPLES.md — full swap per table at the release sweep |

**Banned synonyms for the noun** (recorded on the `workspace` glossary entry):
project, project folder, project root (→ workspace root), app, instance, sandbox
(oc-sandbox is the whole tool), container (a workspace is not its container), repo
(a repo is the git source/folder inside a workspace).

**Legitimate survivors of "project"**: the PKM vault's own knowledge-management
sense; `scripts/create-release.sh` internal `PROJECT_ROOT` (the release repo's own
root); "the oc-sandbox project" repo-metadata and open-source credits; historical
`docs/adr/` + `specs/` (archival record, untouched); facts about the CBM tool's own
concepts in `docs/research/`.

**Legacy policy**: full migration, one canonical name — after migration layout
checks accept only `workspace/`; legacy unregistered folders are non-matching for
workspace import. Rejected alternatives: dual-name legacy support forever,
"Arbeitsbereich" as German rendering (loanword "Workspace" instead), keeping Go
identifiers as `project`.

### 2. Migration and compatibility (§1/§2.2 interplay) — decided in [Grilling: Migration and compatibility plan](https://github.com/shredingerlabs/oc_sandbox/issues/140)

**Trigger & guard** — one-time at first start of the new binary, **blocking before
anything reads config** (before `app.Compose`; startup sequence
`cmd/oc-sandbox/main.go:62–80` — migration precedes compose, unlike the
silent-init precedent). Guard: `workspaces.json` absent + `projects.json` present
→ migrate; neither → fresh create; any of the three config files still
`version` < 2.0 → rerun. Every stage idempotent so rerun skips applied work. After
migration no legacy read path stays installed.

**Backup first**: `global_config.json`, the registry file, and every
`sandbox_config.json` go through the existing `backups/` rotation before staging.
`IsGeneralConfigFile` gains `workspaces.json` + `catalogs.json`; the Export/Restore
fixed list follows the new filenames.

**Ordered idempotent stages** — abort on first error with an error screen, **no
auto-restore** (backups + idempotent rerun is the recovery path):

1. per root: `os.Rename` content dir `project/` → `workspace/` (single atomic call)
2. per root: `sandbox_config.json` key rewrite `project_kind`→`workspace_kind`, `project_source`→`workspace_source`
3. registry: file rename + top key `projects`→`workspaces`; **path fields untouched**
4. `global_config.json`: `default_project_path`→`default_workspace_path`
5. seed `catalogs.json` (per §8 seeding rules)
6. `version` → **`2.0`** in all three files **last** — its presence is the migration-complete marker

**Runtime safety**: containers run through — a running bind mount survives the
host-side rename (same inode), the in-container `/home/dev/project` stays
self-consistent until that container's next start; workspace identity
(`container_id` = sha256-12 of the root) is never touched; images rebuild lazily
(ADR-0024 rebuild-after-update precedent). `start.sh`'s git-init heal keys on the
content dir, so no legacy `/project/` git-init can resurrect post-migration.

**Invariants inherited from §13** — the migration never: repoints registry `path`
at the content dir; re-derives container identities; pre-creates per-repo nesting;
renames user-chosen workspace roots; bends the mount beyond `project/` →
`workspace/`.

**Unregistered legacy folders** — import keeps the compat posture: legacy
`project/`-layout folders are non-matching → informational prompt offering the New
Workspace wizard, plus a third action **"Convert to workspace layout"** that runs
the same per-root converter (stages 1–2) then imports normally. No auto-convert
anywhere else.

**Update interplay** — `install.sh` removes stale old-name scripts
(`init-project.sh`, `configure-project.sh`); the update only installs the new tree;
**first start afterwards migrates**. The one-instance-across-update caveat is a
documented release-docs item (§12), not a lock.

### 3. WebUI serving architecture — decided in [Research: WebUI serving architecture for the single Go binary](https://github.com/shredingerlabs/oc_sandbox/issues/132)

- **Assets**: repo-root `web/` tree (committed source), embedded via `//go:embed`
  into an `embed.FS`, served with `net/http` (`http.FileServerFS` re-rooted via
  `fs.Sub`). Embedding happens at `go build` time from repo sources, so
  `create-release.sh` needs **zero changes** and produced `dist/bin` binaries carry
  the UI. Repo pins Go 1.24.
- **No-node frontend**: plain HTML/CSS/JS + HTMX (+ optional Alpine) vendored as
  static assets. Node-at-build tooling judged overkill for a local CRUD+stream
  control plane; revisit only if the UI outgrows it.
- **Auth**: bind 127.0.0.1; per-launch CSPRNG token (≥128-bit) carried **in the URL
  path prefix** `/t/<token>/…`, validated on **every** request including SSE and all
  unsafe verbs. No ambient cookie authority (CSRF-safe by construction); `Host`-header
  allowlist against DNS rebinding; `Referrer-Policy: no-referrer`; the tokenized URL
  is never logged. `http.CrossOriginProtection` (Go ≥ 1.25) noted as optional extra
  if the repo ever floats past Go 1.24.
- **Streaming: SSE** — one-way line streaming fits `text/event-stream` with built-in
  `EventSource` reconnect; maps onto taskmgr's `emit`→`LineEvent` transport (history
  buffer + live flush; SSE `id:` = line serial for reconnect replay). `EventSource`
  cannot send custom headers → the token-in-path choice holds.
- **Host terminal window**: a real terminal window on the host is spawned for the
  console/OpenCode start variants and first-run skills setup. Linux: configurable
  override, else probe `x-terminal-emulator`, `kitty`, `gnome-terminal`, `konsole`,
  `xfce4-terminal`, `foot`, `xterm` via `exec.LookPath`, launch detached. macOS:
  `osascript` `do script` on Terminal.app (iTerm2 optional). Windows/WSL: `wt.exe`
  preferred via `cmd.exe /c wt.exe` interop with path translation. **Single
  decisive fallback when no window can open** (SSH, headless, none offered): the
  WebUI renders the exact copyable command and the session stays pending — no
  in-browser terminal.
- **Browser opener**: platform opener (`xdg-open` / macOS `open` / Windows
  `cmd /c start`); on failure the full tokenized URL is always visible in the
  launcher's URL/status line for manual copy.

### 4. Minimal TUI launcher (§2.1) — decided in [Grilling: Minimal TUI launcher behavior](https://github.com/shredingerlabs/oc_sandbox/issues/138)

- **Face**: exactly two buttons — **Open WebUI** and **Quit** — plus the URL/status
  line showing the full tokenized URL while the server runs. The update check and
  its badge/menu live in the WebUI entirely; version stays reachable via
  `--version` and WebUI Settings.
- **Lifecycle**: webserver starts at binary launch, stops on quit. Browser tab
  auto-opens at launch; the Open WebUI button (re)opens the tab (closed tab,
  failed auto-open, headless recovery).
- **Port**: base **8090**, scans upward on conflict (4096/CBM-UI precedent),
  never persisted — re-derived per launch.
- **Single instance**: before binding, probe the base port for an unauthenticated,
  reveals-nothing `GET /healthz`; if it answers, print "WebUI already running —
  copy the full tokenized URL from that session's window" and **exit 0**. A foreign
  app on the port scans upward past it.
- **Quit flow**: guarded exit first (background work running → press Quit again);
  then the containers prompt **"Stop running containers before quitting? [y/N]"** —
  scope: **all running oc-sandbox containers** via name-convention discovery;
  default **leave running**; esc cancels; "stopped" = the existing graceful
  stop flow (pod stop, verified gone, registry flip). Then a best-effort SSE
  `server-shutdown` push over the live stream; open tabs swap to the
  server-stopped screen, no reconnect. The token dies with the process.
- **No TUI part in host-terminal opens**: the webserver executes the per-platform
  spawn directly (§3 machinery).

### 5. WebUI design language and navigation (§4) — decided in [Prototype: WebUI design mockup](https://github.com/shredingerlabs/oc_sandbox/issues/136) (user-signed-off Variant C — Signal) + [Research: Edition icon set for workspace cards](https://github.com/shredingerlabs/oc_sandbox/issues/143)

- **Signal look** (locked; variants A Atelier / B Mono remain in the mockup file
  for reference): per-edition hue via left accent bar + tinted tile on a cool
  slate/navy palette, bold weights, count pills in the sidebar tree. **Dark AND
  bright** color modes with an in-page theme toggle. Objects as cards in grid
  views. Reference asset: `web/prototype-webui-design.html` on `dev_WebUI`
  @ a934154 (self-contained, hash router `#<variant>/<theme>/<display>/<screen>`).
- **Navigation frame** (pre-decided): left sidebar tree — Workspace → Open / New;
  Settings → sub-menus per §7 — plus the always-present footer update banner and
  the Standard/Advanced segmented control in the header of every screen (§6).
- **Edition icons — Lucide 1.70.0** (ISC + MIT on the Feather-derived subset;
  license texts and per-glyph verification on `research/edition-icon-set` @
  92475b4), one verified 24×24 stroke glyph per edition shipped as inline SVG at
  `web/img/editions/{edition}.svg`, embedded via `go:embed`, `stroke:currentColor`
  so CSS `--ed` hue tints the glyph exactly like the Signal tiles — no icon fonts;
  the ISC/MIT notice file ships next to the assets. Glyphs:
  swdev→`code` · web→`globe` · embedded→`cpu` · matlab→`square-function` ·
  ros2→`bot` · writing→`feather` · latex→`file-text` · pkm→`brain` ·
  base→`container`. Tabler/Phosphor are verified drop-in alternates (Simple Icons
  only for supplemental brand marks; Remix Icon avoided).
- **Look & feel only**: the mockup locks appearance + navigation, not the API;
  data shapes stay with the tickets (§8, §9). Prototype-era presentation details
  superseded by §6 are recorded there ("start triad", "HIL row for every edition",
  search default).

### 6. Standard/Advanced display modes (§2.2) — decided in [Grilling: Standard/Advanced presentation matrix](https://github.com/shredingerlabs/oc_sandbox/issues/137)

**Model: presets, not pinning.** Standard only preselects at creation; stored
`sandbox_config.json` is the single truth; Standard views hide rows/chips; Advanced
always reveals stored values; Advanced edits of Standard-hidden options are sticky
(Standard never re-imposes them). Cross-editing the same workspace Standard↔Advanced
in either order just works.

**Standard (default):**
- Creation rows visible: Workspace name · source radio pair (Blank / From Repo) ·
  workspace-kind Agent row (default `code`; picking `pkm` hides and pins the
  Edition row — existing rule, both modes) · Edition row preselected **`swdev`** ·
  VCS-credential select · AI-provider select + token input (catalog-fed, §8).
- Preset + hidden: start_option **`web`** · proxy **off** · search_mode
  **`searxng`** · offline **off** · CBM-UI **off** (today's `--cbm_ui` default;
  flag is opt-in and `--offline`-incompatible) · HIL **preset on** for embedded
  creations (`hil_mode` recorded in modes).
- Usage/settings screens hide: Edition (incl. switching), Start option, Proxy,
  Search mode, Offline, CBM-UI, HIL-config. Helper rows point to Advanced —
  wording **"presets, see Advanced"**, never "fixed"/"only embedded".
- Cards: edition tile + HIL chip (whenever on, **any** edition) only. The mock's
  "SearXNG (fixed)" and start-role chips are dropped — chips that can misstate
  stored state are worse than none.
- Open action: always web — start container → announce fresh pair link (§10);
  **no terminal buttons in Standard**.

**Advanced:**
- Full rows everywhere: Edition (switchable; next-start apply + image/mismatch
  rules unchanged) · **Start option triad console / OpenCode TUI / Web** (console
  stays a recorded option — zero data migration; the mock's pair omitted it) ·
  Proxy (default off) · Search mode triad none/websearch/searxng with
  **`searxng` preselected** (user amendment 2026-11-09; back-compat intact —
  pre-existing workspaces with an absent `search_mode` still *read* as
  `websearch`) · Offline · CBM-UI (default off) · **HIL row for every edition**
  (free toggle — `--hil_mode` is edition-agnostic USB-oscilloscope/MCU passthrough
  in start.sh; supersedes the prototype's "HIL only on embedded").
- Cards keep the signal mockup's single Open button honoring the configured start
  option; **no per-start variant picker in the WebUI** (the glossary's per-start
  override stays a TUI-era concept).

**Toggle**: Standard/Advanced segmented control in the **header of every screen**;
persisted as `display_mode` (`standard`/`advanced`, default `standard`) in
`global_config.json`; applies **immediately** via reactive re-render (no reload,
no per-workspace state). The ever-visible switch is how Standard users discover
Advanced.

**Ripple**: no workspace-data migration — only the new global `display_mode` key.

### 7. WebUI screens and menus (§4.1 / §2.2)

Sidebar tree per the signed-off mockup; every screen carries the display-mode
control (§6), theme toggle (dark/bright), and the footer update banner (§12).

- **Workspace → Open** — workspace cards in a grid: edition icon tile (left,
  tinted, §5 glyph), name, settings row with icons, last-used relative date
  (registry `last_used`), and action rows: Open (Standard: web always; Advanced:
  the Open button honors the configured start option), Web access (fresh pair
  link → new tab, §10/§9), Stop/Delete confirm rows.
- **Workspace → New** — form: source radio pair (Blank / From Repo), Workspace
  name (derived live from the repo URL in From Repo), Agent row (`code` default;
  `pkm` pins edition), Edition (preselected `swdev`; inline "Image build" row when
  the image is missing, folded into the create confirm + chain per existing TUI
  contract), VCS-credential select + AI-provider select fed from the catalogs
  (each with inline "New…"), token input for the selected provider, "Add
  provider…" slot. Create runs the one create-chain task (§9) with every step
  streamed into the log pane under the form.
- **Workspace Settings** (per workspace, from card settings icon or via menu —
  menu entry carries the workspace selector dropdown, letter-tinted, running
  state; Open cards jump in preselected) — Standard rows (AI-Provider, VCS
  Tracking, remove — with catalog prefill, replace-credentials confirm) +
  Advanced rows (Edition, Modes incl. HIL, Start option triad, Proxy, Search) per
  the §6 matrix. Login WEB-style pair-link handling belongs to the Open screen's
  Web action.
- **Settings → VCS Tracker / AI-Provider** — global catalog lists as cards (§8).
- **Settings → Import Workspace** — path input (server-side validation) +
  legacy-folder convert action (§2).
- **Settings → Update** — phases, "Check now", embedded log pane, restart note (§12).
- **Settings → Export Config / Restore Config** — named server-side backup with
  overwrite confirm; restore behind the confirm popup (§9).
- **Settings → Image: Build / Delete** — edition rows with built/missing markers
  and delete-as-danger per existing Container-center semantics (§9).
- **Settings → Container: Start / Stop / Delete** — one list, three actions,
  popup on stop and delete (§9).
- **Settings → Uninstall** — wizard toggles + typed `uninstall` confirmation +
  confirm popup (§9).
- **Workspace detail screen** — task/stream rows on the open workspace (start,
  build/stop ops, first-run setup stages incl. the skills-setup slot, §11).
- **Server-stopped screen** — terminal state of any tab after `server-shutdown`
  or reconnect exhaustion (§12's rendering rules).

### 8. Global catalogs (§2.2 — VCS Tracker / AI-Provider menus) — decided in [Grilling: Global VCS-tracker and AI-provider catalogs](https://github.com/shredingerlabs/oc_sandbox/issues/135); ADR-0031 (supersedes ADR-0023's global-registry rejection; its runtime model stands)

- **Entry shapes** — never a secret: **tracker entry** = display name, git
  name/mail, tracker kind (GitHub / public GitLab / own GitLab / others), bare
  **Host** (required for own GitLab/others; the field is Host, not URL — "URL"
  collides with repo URL). **Provider entry** = display name + OpenAI-compatible
  endpoint.
- **Storage**: new `catalogs.json` in `~/.config/oc-sandbox/` — the **third general
  config file** (atomic writes, backups, restore); entries unique by slugified
  name, insertion order, `version` key.
- **Template model**: picking an entry at creation materializes today's ordinary
  per-workspace configuration (`opencode.json` provider section + credential file;
  `configure-workspace.sh` vcs-credentials + gitconfig identity) and is never
  referenced again — no cascade on catalog edits, no active-provider record, no
  start-time catalog coupling. Workspace edit flows keep their free-form add and
  gain pick-from-catalog prefill; several provider entries per workspace remain
  possible.
- **Creation**: one tracker pick + one provider pick; each select also offers an
  inline **"New…"** that saves the entry to the catalog before applying it (an
  empty catalog never dead-ends first-run). The token is typed at creation and
  lands in the existing flows untouched — `.git_local/` credential bridge + gh/glab/vcs
  host files; `.opencode_data/credentials/<id>` via the v2 `{file:…}` reference
  (ADR-0025).
- **"None"**: stays the empty default (`git_tracking: "none"`, zero provider
  entries), never a catalog entry.
- **Seeding** (one-time, by the rename migration §2): trackers — one per distinct
  git host (name, git name/mail, kind, bare host) from workspaces' VCS configs;
  providers — one per distinct provider name + endpoint from per-workspace
  provider configs; token-free; dedupe case-insensitive by slugified name,
  first-seen wins; workspaces with "none" or missing roots on disk contribute
  nothing.

### 9. WebUI API surface — decided in [Grilling: WebUI API surface](https://github.com/shredingerlabs/oc_sandbox/issues/139)

- **Contract — HTMX-first hybrid.** Endpoints return HTML fragments for HTMX swap
  targets; small JSON sidecars only where plain `fetch` is simpler (the pair-link
  handoff); SSE events carry JSON-ish line payloads. No JSON API.
- **Streaming — one multiplexed SSE per browser tab** (`GET /events`):
  `line` (taskmgr log lines keyed by Op.ID), `workspace` (card/config re-render on
  state change), `update` (badge/phases, §12), `shutdown` (§4). Cards and busy
  state re-render reactively; no polling.
- **Confirmations — TUI set carried 1:1** (create summary, restore config, build,
  delete-container/delete-image danger, update, remove-from-list,
  replace-credentials) **plus one new web-native confirm: `adopt`** — the edition-
  mismatch switch and web-adopt relaunch both replace a running container (an
  agent may be mid-task), so they confirm first. Uninstall keeps its typed
  `uninstall` text confirmation. Confirms refuse while an op is queued/running
  (unchanged).
- **Layout — one package**: `internal/webserver` (handler files per screen-group,
  `embed.FS` wiring, auth middleware, SSE hub). Business logic stays in
  config/registry/taskmgr/discovery/sandboxcfg; scripts are invoked exactly as the
  TUI invokes them (native script enhancement, ADR-0007); TUI packages keep
  compiling through the parity gate (§14).
- **Routes** — resource + POST action verbs, no version prefix, all under
  `/t/<token>/`; registry workspace **name** is the URL identity
  (percent-encoded). From prior decisions: `GET /healthz` is token-less and
  reveals nothing (§4); `POST .../pair-link` → `{pair_link|plain_url}` sidecar;
  `POST .../access {variant: console|opencode|web}` opens a host terminal window
  or renders the copyable command on the headless fallback.
- **Conflict semantics — mirror taskmgr exactly**: queue per workspace key,
  one-at-a-time per workspace, parallel across workspaces; no 409-refusal path.
- **Inventory (ratified)**:
  - Shell: `GET /` (index; Standard/Advanced in-page), `GET /events`, `GET /healthz`
  - Workspaces: `GET /workspaces` (+`?q=` server-side); `POST /workspaces` (wizard
    payload → one create-chain task: image build → `init-workspace.sh` →
    `configure-workspace.sh` in credential order); `POST
    /workspaces/{name}/remove-from-list`
  - Start/access: `POST /workspaces/{name}/start` (Container-center reuse
    semantics); `POST …/access`; `POST …/pair-link`; `POST …/adopt` (behind the
    new confirm)
  - Workspace ops: `POST …/stop`; `POST …/settings` (edition, modes, start
    option, proxy, search — next-start apply); `POST …/providers
    {add|edit|remove}` (token-optional edit, replace-credentials confirm);
    `POST …/vcs-credentials {kind, host, token, replace}`; `POST
    …/setup/retry` (first-run setup recovery)
  - Container center: `GET /containers` (+stop/delete mirrors); `GET /images`
    (existence markers + sizes via discovery); `POST /images/build`
    (build-container.sh, scaled to `all`); `POST /images/delete` (danger)
  - Global: `GET/POST /settings` (default workspace path, display mode →
    global_config.json); `GET /catalogs` + `POST
    /catalogs/{trackers,providers} {add|edit|remove}`; `GET /import` + `POST
    /import-workspace {path}`; `POST /export-config {label}` + `POST
    /restore-config`; `POST /update/check` + `POST /update/run` (§12); `POST
    /uninstall` (typed confirm + wizard toggles → `uninstall.sh --force` mapping)
- The update check runs at server start and pushes the badge via `/events`;
  Settings "Check now" re-runs (§12 owns the phases/exclusivity).

### 10. Always-on OpenCode web access (§3) — decided in [Research: Always-on OpenCode web access in every start mode](https://github.com/shredingerlabs/oc_sandbox/issues/133); docs-drift verdict in [Research: opencode web/attach docs drift vs shipped build](https://github.com/shredingerlabs/oc_sandbox/issues/142)

- **Port publishing**: `start.sh` publishes 4096 unconditionally (auto-scan
  upward from 4096, unchanged scan range 4096–4196) — pod-level `-p` on pasta is
  the production mechanism; the pod recreate-at-start absorbs the "cannot modify
  after create" constraint. Exception: `--offline` (structurally no publish on
  `--network=none`).
- **Service always-on**: the detached container command becomes
  `opencode service start && exec sleep infinity` in **every** start mode, and the
  `service.json` ensure (hostname `0.0.0.0`, port 4096) runs unconditionally —
  same flow today's `--start_web` exercises (ADR-0024 pin stands; v2's default
  service port 49374 is why the pin is load-bearing). Cost: the shared service
  boots earlier than first use — same process either way.
- **Port authority**: never persisted; live `podman port <ctr> 4096/tcp`
  (`discovery.WebPort`, works against pod members) stays the truth. The
  simultaneous-start TOCTOU is documented, not engineered.
- **WebUI login flow**: reuse `discovery.PairLink` verbatim, **fresh per request**
  (one-time links, 5-min expiry, 30-day session — nothing persisted, every access
  pairs afresh); the API returns both `pair_link` and `plain_url` (fallback when
  pairing fails / service mid-boot); the frontend opens the link in a new tab
  under the click gesture (§9).
- **Direct-CLI parity**: foreground `--start_web` stays untouched (ADR-0024
  Ctrl+C contract); the detached path ignores the flag; the detached start echo
  always announces URL + pair hint.
- **Docs-drift verdict** (discharges #133's "re-verify at implementation"):
  `opencode web`/`attach` do **not** exist in the shipped build (v2.2.6); the
  opencode.ai `/docs/web/` page is a stale v1 leftover. **No revision to
  ADR-0024's invocation flow.** The v2-native client attach is the root flag
  `--server <url>`. Optional release-docs/ADR note: `/docs/web/` is a v1 leftover.
  Footgun for testing: once repo-root `web/` exists, typing `opencode web` at the
  repo root silently opens the TUI *inside* `web/`.

### 11. First-run setup interaction in the WebUI — decided in [Grilling: First-run setup interaction in the WebUI](https://github.com/shredingerlabs/oc_sandbox/issues/144)

- **Vehicle unchanged in kind**: the skills stage keeps its interactive shape —
  the WebUI auto-opens a **host terminal window** (§3 machinery) running the exact
  podman-exec prompt used today. The interactive requirement stands (ADR-0014:
  the setup skill asks the user questions; a browser transcript view and a
  non-interactive variant were rejected; ADR-0014's accepted trade-off stands —
  exit 0 marks `setup_skills_complete` even on quit-early).
- **Auto-open**: the window opens automatically when the skills stage arrives
  (after clone/CBM have streamed in).
- **WebUI surface**: the skills stage is one slot of the setup task lines on the
  workspace detail screen; while it runs: status line ("Skills setup — interactive
  session open in a terminal window") + persistent chip + two actions —
  **Copy manual command** and **Cancel setup**; re-rendered via the existing
  `line`/`workspace` SSE events.
- **Completion/abort relay**: the launched window runs a tiny wrapper
  (`podman exec … opencode --prompt …; printf '%s' $? > <status-file>`); the
  webserver polls for the status file and feeds the exit code into the
  **unchanged** setup machine — 0 → flag patch, 2 → user abort (no flags), else →
  granular recovery — then the SSE push.
- **Hard-closed window**: no status file → pending-stale until the user acts;
  **Cancel setup** marks it abandoned (no flags, partial progress preserved,
  recovery actions reappear); a status file arriving for an already-cancelled
  session is ignored (session guard).
- **Headless fallback**: no window → the slot shows the exact copyable manual
  command and stays pending; completion flows through the same relay. No
  non-interactive variant is offered.
- **Blocking**: while a setup session is pending it behaves like a busy op for
  the one-at-a-time-per-workspace rule — the workspace's other actions
  (console/OpenCode/Web access, start/stop) are disabled with a disabled-reason.
  The pending session is **not** a task-manager task.

### 12. WebUI behavior during self-update — decided in [Grilling: WebUI behavior during self-update](https://github.com/shredingerlabs/oc_sandbox/issues/146)

- **Run from the WebUI**: an ordinary background op behind the shared confirmation
  popup (popup copy carries the keep-note) through the task queue; live log
  streams into the embedded log pane under the Settings Update card. While it
  runs it is **globally exclusive**: the queue accepts nothing else across
  workspaces, queued ops wait behind it, the Update button disables itself;
  read-only actions and Check now stay usable.
- **One SSE `update` event with phases** — `up-to-date` / `available` /
  `installing` / `applied` / `failed`; footer banner and Update card render purely
  from the phase. On `applied` the server re-reads the rewritten `VERSION` and
  the banner clears; the persistent **"Update installed — restart oc-sandbox to
  apply"** note lives on the Update card, plus a completion toast.
- **No in-WebUI restart** — restart is hint-only: quit oc-sandbox in the terminal
  and relaunch. The per-launch token dies with the process, so a browser tab can
  never reconnect on its own; the relaunched instance opens its own fresh tab;
  spawn-and-exit restarts would add dual-instance races for no reconnect benefit.
- **Server-down tab — one screen for both causes**: the best-effort SSE
  `server-shutdown` (quit) shows the "server stopped — relaunch oc-sandbox"
  screen; a hard crash (no push possible) lands on the same screen via
  client-side reconnect exhaustion. No auto-redirect, no token guessing.
- **Migration overlap**: no lock — the one-instance-at-a-time caveat ("quit
  running instances before relaunching across an update") rides the release docs
  sweep and the new migration ADR (§2/§14). Migration stays first-start,
  blocking, staged (stage progress in the minimal TUI status line); the
  transition-release overlap is the documented preexisting multi-instance
  fragility, not a new guard target.

### 13. V2 multi-repo design-for notes (wish-list V2 item) — decided in [Grilling: V2 design-for constraints on V1 data shapes](https://github.com/shredingerlabs/oc_sandbox/issues/145)

*Design-for only — V2 multi-repo is never implemented in this effort (Out of
Scope here and on the map). The constraints below bind the V1 shapes now so the
later per-repo features arrive without a break.*

1. **Repo placement** — V2 repos are subfolders of the content dir:
   `<root>/workspace/repo-a`, `repo-b`, … V1 keeps the single repo at the
   content-dir root; one mount (content dir → `/home/dev/workspace`) and a
   root-anchored registry cover both shapes. Per-repo Open buttons later run
   OpenCode **in the same container** at a repo cwd
   (`/home/dev/workspace/repo-a`), whole workspace visible as the parent.
2. **Version** — `workspaces.json` `"version": "2.0"` is locked by the rename;
   later V2 shapes ride inside 2.0 as absent-optional keys (absent-field read
   rule). No 3.0 planned (not forbidden).
3. **Identity/scope freeze** — no added entry fields: `name` (slug
   `^[a-zA-Z0-9_-]+$`), `path` (canonical workspace root, never the content dir),
   `container_id` (sha256-12 of root; one container + pod per workspace, ADR-0029)
   keep their meanings forever; `repo_url` and `git_tracking` stay
   workspace-scoped. No per-repo container ids, names, or ports.
4. **Git access** — one fine-grained token **per tracker**, typed at workspace
   creation, materialized per-workspace exactly as today (credential bridge
   unchanged), scoped to all repos of that tracker. No per-repo credential shapes.
5. **WebUI API** (§9) — routes, SSE streams, and task queues stay
   workspace-scoped; a V2 per-repo open button arrives additively as a sub-route
   under a workspace resource + a card sub-view. Nothing repo-level is invented
   now.
6. **Catalogs** (§8) — schema stays template-level; per-repo git identity in V2
   is repo-local git config, never a catalog field.

### 14. Parity gate and release plan — decided in [Grilling: Migration and compatibility plan](https://github.com/shredingerlabs/oc_sandbox/issues/140); hidden-flag mechanics per [Grilling: Minimal TUI launcher behavior](https://github.com/shredingerlabs/oc_sandbox/issues/138)

- **Transitional reachability**: the full BubbleTea TUI stays reachable behind a
  **hidden launch flag `--full-tui`** (same binary, no menu entry, no separate
  binary; release README documents it only for the transitional release). WebUI
  code arrived assembled here; the TUI packages keep compiling until the flip.
- **Parity checklist = the wish-list §4.1 screen/function list** (§7 above),
  split into:
  - **A-list — scriptable via `--smoke`** (flows asserted against a fixture
    install + the real scripts, no human judgement): auth gating on every route
    incl. SSE and `healthz` shape; registry list/create/remove/import +
    create-chain invocation order; config + catalog reads/writes (incl.
    `display_mode` persistence and default); migration guard/stage order/abort-on-
    error/idempotent rerun/`version: 2.0`/backup creation on fixture layouts;
    SSE event distribution (`line`, `workspace`, `update`, `shutdown`) and replay
    ids; taskmgr conflict semantics (one-at-a-time per workspace); update check
    comparison + phase logic (mocked GitHub); always-on `start.sh` assertions
    (publish 4096, service command in every mode, `--offline` exempt, foreground
    contract untouched).
  - **B-list — manual boxes on the real machine** (visual/platform judgement):
    Signal look on all screens (dark + bright); display-mode segmented control
    immediate re-render; cards (tile hue, HIL chip, relative last-used); creation
    form incl. catalogs, inline "New…", masked token; pair-link new-tab handoff
    in a real browser; host-terminal opens per platform (Linux/macOS/Windows/WSL)
    and the copyable-command fallback; browser auto-open / Open WebUI button
    recovery; confirm popup set incl. `adopt` and typed uninstall; log-pane
    streaming during build/create/install; update phases end-to-end incl.
    `applied` banner clearing + VERSION re-read; server-stopped screen via quit
    and via crash-path reconnect exhaustion; quit containers prompt; second-
    launch `healthz` point-exit-0; first-run setup window auto-open, status slot,
    Copy manual command, Cancel setup, headless path; legacy-folder import
    prompt + "Convert to workspace layout"; export/restore round trip.
  Every box passes → **flip authorized**: remove the flag and delete full-TUI
  code — the #98→#99 flip-then-delete sequence (checks on ADR precedent #0020).
- **Release contents**: one release carries rename + migration + WebUI + minimal
  TUI; WebUI assets ride inside the binary (`go:embed`); desktop shortcut
  untouched (launches the same binary, which now auto-opens the browser).
- **Update ordering**: `install.sh` overwrite flow installs the new tree,
  removes stale old-name scripts; first start afterwards migrates (§2).
- **Docs/ADR**: README + dist README rewritten in the same release (WebUI-first
  quick start, workspace words); **one new ADR records the migration mechanics +
  hidden parity flag**; optional one-line addendum on the `/docs/web/` v1
  leftover (§10). Repo-level README pointer updates before the release are not
  needed — production behavior is unchanged until implementation, so the full
  sweep rides the release.

## Testing Decisions

- **Smoke (A-list) first**: extend the existing `--smoke` harness (#83/#96
  precedent) with WebUI flows against a fixture install and the real scripts;
  assert externally visible outcomes — registry/config/catalog JSON shapes,
  invoked script argument vectors, HTTP status/auth grammar, SSE payloads — never
  internals.
- **Migration tests** run on committed fixture layouts (pre-rename
  `projects.json`/`project/` trees): stage order, idempotent rerun, abort-on-first-
  error, `backups/` creation, `version: 2.0` written last, paths untouched,
  containers-untouched posture asserted as design constants.
- **Rename rides existing tests**: Go package/command rename pulls fixture,
  golden, and smoke testdata along; JSON struct-tag assertions follow the data
  renames; script/output assertions switch to the English minimal lines.
- **Auth tests**: missing/wrong token refused on every route + SSE; `healthz`
  returns nothing; Host allowlist rejects rebound hosts; tokenized URL never in
  log output.
- **Webview-matrix tests**: the Standard/Advanced matrix as data-driven
  presentation tests (hidden/shown rows per mode); `display_mode` default +
  persistence; absent-`search_mode` legacy read stays `websearch`.
- **Update-phase tests** with mocked GitHub API per the existing update-check
  test patterns; `applied` re-read asserted.
- **Real-podman manual boxes** are the B-list only — nothing in the suites
  requires live podman beyond today's script tests.
- **TUI keeps compiling until the flip**: registry/config/sandboxcfg/taskmgr
  tests updated for renamed symbols stay green; the full-TUI smoke set runs
  behind `--full-tui` for the A-list of the §4.1 TUI-era functions it already
  covers.

## Out of Scope

- **V2 multi-repo implementation** (multiple repos per workspace, per-repo open
  buttons) — design-for constraints only (§13).
- **Mobile-first / responsive phone UI.**
- **Live-apply of workspace settings** — next-start apply stays the contract.
- **Rewriting the bash scripts in Go** — the WebUI calls the scripts (native
  script enhancement, ADR-0007).
- **In-browser terminal (xterm.js)** as primary console delivery — host terminal
  window is the chosen direction; copyable command is the fallback.
- **README rewrite at spec stage** — production behavior is unchanged until
  implementation; the WebUI-first README/dist README/docs sweep rides the same
  release (§14).

## Wish-list Completeness

Source: user request 2026-11-09, as recorded in the map Notes
([Map: WebUI-first oc-sandbox](https://github.com/shredingerlabs/oc_sandbox/issues/131)).

| Wish-list item | Where decided / covered |
|---|---|
| §1 rename | §1 (terms/tables), §2 (migration), §14 (release sweep) |
| §2.1 minimal Go TUI (structure + functionality) | §4 (launcher face/lifecycle/quit), §14 (parity gate, `--full-tui`) |
| §2.2 WebUI as main UI — Standard/Advanced, Open/New Workspace, Settings sub-menus | §3 (serving), §5 (design), §6 (matrix), §7 (screens), §9 (API) |
| §3 OpenCode startmodes + always-open web port | §10 (always-on), §9 (access routes + host-terminal launches) |
| §4 design + menus | §5 (Signal, icons, navigation), §7 (menu tree + screens) |
| §4.1 the menu/screen function list | §7 (all 11 Settings/Open/New screens), §14 (it *is* the parity checklist source) |
| V2 multi-repo (multiple repos as subfolders, per-repo open buttons, OpenCode per repo, access to whole workspace) | §13 design-for constraints only — implementation out of scope |

Every numbered wish-list item has a decided home; nothing on the wish-list is
left undecidable. V2 items appear only as design-for notes.

## Further Notes

- **Fog sweep**: the map's "Not yet specified" section is empty; all fog
  graduated during the map's life (first-run setup → §11; V2 shapes → §13;
  migration overlap → §12; update phases → §12). Nothing defers with a caveat.
- **Research artifacts** live on their `research/*` branches
  (`docs/research/webui-serving-architecture.md`,
  `docs/research/always-on-opencode-web.md`,
  `docs/research/opencode-web-attach.md`,
  `docs/research/edition-icon-set.md`); the signed-off design prototype is
  `web/prototype-webui-design.html` @ a934154 on `dev_WebUI`.
- **Glossary disposition**: CONTEXT.md gained the rename/catalog/launcher/setup/
  update terms during the ticket sessions; the remaining gap — **display mode**
  (§6) — lands with this spec. Implementation-phase glossary switches (TUI →
  WebUI-primary wording on entries like BubbleTea TUI / update menu) ride the
  flip, not this document.
- **Delegations recorded during assembly** (ticket #141's own fog): none — the
  A/B attribution above is the only new derivation created by this assembly.
- **Way clear**: with the sign-off of this spec all decisions on the map are
  locked; no open decision precedes execution. Implementation planning happens
  outside this map.
