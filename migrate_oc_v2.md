# OpenCode v2 Migration Findings

Status: implemented in two steps — the runtime surface in ADR 0024, the
config surface (template, provider blocks, credentials, one-time v2
migration) in ADR 0025. Evidence: OpenCode v2 docs (opencode.ai/v2/docs), the
GitHub releases page, and the real v2 binary (`opencode v2.0.29` / `v2.1.1`,
downloaded to `/tmp` for `--help` inspection — installed binary in this
container is still v1.18.15).
Reviewed on 2026-10-15.

## Verdict

The **project structure survives v2 unchanged** — no big structural changes needed. The work
is in the sandbox's OpenCode *invocation surface* (install source, `web`, `--prompt`/`-m`),
spread over `dist/` scripts, two Go call sites, and tests.

## What keeps working (unchanged in v2)

| Structure | v2 status |
| --- | --- |
| `AGENTS.md` at project root | ✅ still the rules file. v2 now reads **`AGENTS.md` only** (CLAUDE.md fallback dropped — this repo doesn't rely on it) |
| `.opencode/skills/<id>/SKILL.md` | ✅ same discovery path and precedence. `create-release` frontmatter (name + description) is valid; v2 needs `description` for model-facing discovery |
| Global skills at `~/.config/opencode/skills/` | ✅ same source; Matt-Pocock skill setup keeps working |
| Sandbox mount `.opencode_config/` → `~/.config/opencode` | ✅ v2 still uses `~/.config/opencode/opencode.json(c)` plus `skills/`, `agents/`, `commands/`, `plugins/` subdirs. New in v2: `~/.config/opencode/service.json` (service config) lands in this mount — harmless |
| Sandbox mount `.opencode_data/` → `~/.local/share/opencode` | ✅ v2 still stores db (`opencode.db`) and logs (`log/opencode.log`) there; sessions survive container restarts via this mount |
| Edition discovery (`FROM ... AS opencode-sandbox-*`), container naming, Go TUI, docs/specs layout | ✅ OpenCode-version-agnostic |

## Breaking changes and affected code

### 1. Install channel / version pinning — `dist/Dockerfile:31-33`

- Currently pulls `github.com/anomalyco/opencode/releases/latest/download/opencode-linux-x64.tar.gz`.
  Today that is still v1 (v1.19.0 is marked "latest"; no v2 tag on the releases page).
- v2 ships through different channels: `https://opencode.ai/v2/install`, brew
  `anomalyco/tap/opencode-v2`, npm `@opencode/cli`, AUR `opencode-beta`, docker
  `ghcr.io/anomalyco/opencode:2.0.0`.
- **v1 and v2 both use the `opencode` command and cannot be installed side by side** under
  the default name. If v2 ever becomes the GitHub "latest" release, every rebuilt container
  silently switches major version. Existing containers keep v1 until rebuild.
- v1.19 offers `opencode upgrade --major` as an in-place migration path.

Action: pin the opencode version/URL in the Dockerfile deliberately (v1 or v2), don't float
on `releases/latest`.

### 2. `opencode web` is gone — `dist/scripts/start.sh:428`, `dist/templates/bash/bash_profile:7`

Confirmed by the v2.0.29 binary: no `web` subcommand. v2 replaces it with:

- `opencode serve` — "Start the v2 API and web server"; flags: `--hostname`, `--port`,
  `--cors` (repeatable), `--service`, `--stdio`.
- `opencode pair` — "Print one-time links to connect a browser or app"; prints URL +
  username + password. Pairing **requires the shared background service** (unavailable while
  the service is disabled).
- `opencode service` — manage the background server (`status/start/stop/restart/set/unset`).

Affected: the whole `--start_web` flow (container main process command, `--detach` variant,
port-scan 4096–4196, `Show Web URL` access flow, bash_profile alias) plus the doc text in
`dist/README.md:62,243` and root `README.md` (~55-57).

Candidate replacement, keeps the fixed-port model: `opencode serve --port 4096` as container
main process (drop-in for the detached case); browser connects directly. Auth model differs
from v1 (`OPENCODE_SERVER_PASSWORD` env → v2 server authentication / one-time pair links) —
needs a design decision (ADR) for how the URL + credentials surface in the TUI.

### 3. First-run skills-setup command — `internal/setupflow/stages.go:114`, `dist/scripts/start-tui.sh:1533`

Current: `opencode --prompt "run skill setup-matt-pocock-skills" -m opencode/big-pickle`
(inside interactive `podman exec -it`).

Verified against v2.0.29:

- `opencode run` has `-m` but is **one-shot / non-interactive** ("submit a prompt without
  opening the interactive interface") — cannot answer skill questions. Not suitable.
- v2 full TUI keeps `--prompt string` but **lost `-m`** — current command fails on `-m` in v2.
- **`opencode mini` is the drop-in**: minimal *interactive* interface with both
  `--model, -m` and `--prompt`.

Replacement: `opencode mini --prompt "run skill setup-matt-pocock-skills" -m opencode/big-pickle`
(`SetupModel` at `internal/setupflow/stages.go:17` unchanged).

Tests asserting this exact invocation: `tests/test-start-tui.sh:768-769`.

### 4. Runtime integration model: shared background service vs `--standalone`

**Decision (preliminary): use the shared background service inside the container — do not
standardize on `--standalone`.**

- v2 is client-server: one shared server per user owns sessions, config reload,
  permissions, and tool execution; every local client (TUI, mini, run, pair) connects to it.
- Web pairing requires the shared service — standalone cannot serve the `web` start mode.
  With a shared service, all three start modes are the same shape:
  - `console` → plain bash; service auto-starts on first `opencode` use
  - `opencode` → TUI connects to the service
  - `web` → service (or `serve --port 4096`) + `pair` for links
- Session continuity across attach/detach: start a TUI session, close the terminal,
  reconnect later via console with `opencode --continue` / `--session` and land on the same
  live session. With standalone, each invocation boots a private server; two concurrent
  standalone clients each run their own server against the same mounted DB (lock/divergence
  risk — exactly the scenario sandboxes create).
- One service = one reload point for the mounted `.opencode_config/` (`opencode reload`).

Service files: registration `~/.local/state/opencode/service.json` (ephemeral in container —
fine), private config `~/.config/opencode/service.json`, db `~/.local/share/opencode/opencode.db`.

Implication for the repo: start/verify the service in the container start path (entrypoint or
bash profile — the slot where v1 ran `opencode web`), e.g. `opencode service status ||
opencode service start`, for `opencode`/`web` modes. Never ship `opencode service set
disabled true` in the image.

## Open verification items (do at migration time)

1. `serve --port 4096` vs auto-started service in the same container: port/auth conflict or
   clean takeover? Decides whether `web` mode uses `service start` + `pair` (dynamic port) or
   keeps fixed port 4096 via `serve --port 4096`.
2. `auth.json` path in v2 — v2 docs document `/connect` but not the file path. The sandbox
   writes GWDG tokens to `.opencode_data/auth.json` (mounted at
   `~/.local/share/opencode/auth.json`). Verify v2 reads the same location
   (`internal/smoke/checks_upgrade.go:52`, `internal/smoke/checks_config.go:25`,
   `internal/smoke/checks_atomic.go:64`, `dist/scripts/configure-project.sh:206`).
3. `opencode/big-pickle` availability in the v2 catalog (v2 has the "OpenCode Go" plan;
   model/provider IDs may have changed). Used in `internal/setupflow/stages.go:17` and
   `dist/scripts/start-tui.sh:19`.
4. Whether the shared service works cleanly inside the ephemeral `podman exec -it`
   skills-setup session (it has `--standalone` as fallback if not).

## Command mapping (v1 → v2)

| Purpose | v1 (current) | v2 |
| --- | --- | --- |
| Interactive TUI with initial prompt + model | `opencode --prompt "..." -m model` | `opencode mini --prompt "..." -m model` (or full TUI `--prompt` + model via config default) |
| One-shot prompt | `opencode run "..."` | `opencode run "..."` (unchanged) |
| Web UI on fixed port | `opencode web --port 4096` | `opencode serve --port 4096` (+ browser) or `opencode service start` + `opencode pair` |
| Attach TUI to server | `opencode attach http://localhost:4096` | `opencode pair` / `--server` (verify) |
| Headless server | `opencode serve` | `opencode serve` (kept; now also serves web) |
| Install | `releases/latest` tarball / `curl ... install` | `https://opencode.ai/v2/install` etc.; upgrade path `opencode upgrade --major` from v1.19 |

## Affected files inventory

Runtime:

- `dist/Dockerfile:31-33` — install source/pinning
- `dist/scripts/start.sh:23-24,53-54,60,68-69,94-95,428` — `--start_web` flow, container command
- `dist/templates/bash/bash_profile:7` — `opencode web` alias
- `dist/scripts/start-tui.sh:19` (`OPENCODE_MODEL`), `:1533` (skills-setup invocation)
- `internal/setupflow/stages.go:17` (`SetupModel`), `:114` (skills-setup invocation)
- `internal/discovery/discovery.go:76-77` — `start_web`/`start_opencode` known flags
- `internal/openproject/start.go:20-21` — start-args wiring
- `dist/README.md` (lines 62, 242-243 and usage examples), `dist/scripts/USAGE_EXAMPLES.md` — docs

Tests:

- `tests/test-start.sh:48,54,59,69-70` — asserts `-c 'opencode web --port 4096'` in mocked podman call
- `tests/test-start-tui.sh:432-433` (`--start_web` forwarding), `:768-769` (skills-setup command)

Also worth checking: proxy allowlist already covers `opencode.ai` / `*.opencode.ai` /
`api.opencode.ai` (`dist/proxy/allowlist.txt:39-41`) — verify v2 endpoints (e.g.
`update/api/latest`) resolve under the covered domains or extend the list.

## Suggested migration order

1. Write an ADR for the v2 runtime model (install pinning + web replacement + service
   decision) — the open items above are its grill list.
2. Pin the Dockerfile to a known-good opencode version (v1 today; flip to v2 deliberately).
3. Rework the `--start_web` flow around `serve --port 4096` / `pair` + service lifecycle.
4. Port the skills-setup invocation to `opencode mini --prompt ... -m ...`.
5. Verify the open items (auth.json, big-pickle, serve-vs-service, exec session).
6. Update docs (`README.md`, `dist/README.md`, `USAGE_EXAMPLES.md`,
   `internal/discovery/testdata/start_help.txt`) and the two test files.
