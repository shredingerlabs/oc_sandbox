# OpenCode v2: Latest-Channel Install, Shared Service, Pair-Link Web Access

OpenCode v2 breaks the sandbox's invocation surface (no `opencode web`
subcommand, no `-m` on the interactive TUI, GitHub releases stays
v1-only) and turns the CLI into a client-server system. Decided to move
the sandbox to v2 in one flip:

- **Install**: the Dockerfile pulls the v2 tarball from the opencode.ai
  update API's `latest` channel
  (`https://opencode.ai/update/api/latest/cli/opencode/opencode-linux-x64.tar.gz`).
  GitHub `releases/latest` would silently keep v1 forever; the channel
  URL floats on the current v2 so container rebuilds pick up v2 fixes
  without repo changes — version drift between builds is accepted.
- **Runtime model**: OpenCode runs as the shared background service per
  container — never `--standalone` — so console, TUI, mini and web
  clients share one server against the mounted session DB. The service
  auto-starts on first `opencode` use; the image must never ship
  `opencode service set disabled true`.
- **Web start option**: the service is pinned to container port 4096 —
  v2 listens on localhost by default, which a published port can never
  reach — via the mounted service config: start.sh pre-seeds
  `.opencode_config/service.json` (hostname `0.0.0.0`, port 4096) before
  the first start, so the container boot is a single `service start` (an
  idempotent ensure against the shared service) plus a keep-alive as PID
  1. Re-running `service set hostname/port` inside the container on
  every boot was dropped: each `service set` stops a running service
  and can abort the boot chain ("Server process ... is still running").
  The host side keeps the dynamic 4096–4196 port
  scan. Access runs `opencode pair --url http://<published-host>` and
  announces the printed one-time sign-in link; pair output is
  ephemeral, so every web access pairs afresh. The direct-CLI
  foreground path (`--start_web` without `--detach`) runs
  `opencode serve --hostname 0.0.0.0 --port 4096` instead: Ctrl+C must
  keep stopping and removing the container, which a daemonized service
  cannot honor.
- **Skills setup**: the first-run skills stage calls
  `opencode --prompt "run the skill /setup-matt-pocock-skills using the skill tool"` —
  v2's regular TUI carries `--prompt` as a root flag, so the `mini`
  drop-in is not needed and there is no model flag: the model comes
  from the config default. This supersedes the invocation and
  model halves of ADR-0014 (the `opencode/big-pickle` pin is dropped;
  the interactive-stage and setup-completion semantics of ADR-0014
  stand) as well as this ADR's earlier `mini` invocation for this
  stage.
- **Plugin**: the models-discovery plugin pins
  `opencode-models-discovery@2.0.0` (the v2 line).

## Considered Options

- **Pinning an exact opencode version** (`ARG OPENCODE_VERSION`, the
  versioned `files/bin/<version>` tarball URL): rejected for now —
  every bump would need a repo commit; revisit if a channel update
  ever breaks a build.
- **`ghcr.io/anomalyco/opencode` as the image base**: rejected — it
  would restructure the edition Dockerfile around a foreign base image.
- **`--standalone` everywhere**: rejected — every client would boot a
  private server against the same mounted DB (lock/divergence risk,
  exactly the scenario sandboxes create), and web pairing requires the
  shared service.
- **`opencode serve --port 4096` as the detached web main process**
  (fixed-port drop-in): rejected — serve replaces the shared service
  with a dedicated server, re-introducing the two-servers-one-DB drift;
  the service's port is configurable (`service set port`), so the
  fixed-port model survives without it.
- **Publishing a container port range** so dynamic service ports stay
  reachable: rejected — needless host exposure when the port is
  configurable.

## Consequences

- Existing containers keep v1 until their edition image is rebuilt.
  After updating the install, the first start of a v1 image with the
  new scripts fails visibly (v1 has no `service`, `mini` or `serve` web
  subcommands) — rebuild the edition image, then start.
- v2 writes `service.json` into `~/.config/opencode` (the mounted
  `.opencode_config/`); the dynamic host port stays unrecorded in
  sandbox_config.json/projects.json (smoke check intact). The service
  registration in `~/.local/state/opencode/` is container-ephemeral —
  harmless.
- The web toast signs the browser in via one-time pair links (sessions
  then last 30 days); the foreground `serve` path shows the server
  password in its banner for direct URL sign-in.
- Verify on the first v2 build: pair output format, `auth.json`
  compatibility (`.opencode_data/auth.json` mount), and `mini` inside
  the ephemeral `podman exec -it` skills session (fallback:
  `--standalone` for that stage only).
