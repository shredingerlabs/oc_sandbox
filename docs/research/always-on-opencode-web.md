# Research: Always-on OpenCode web access in every start mode

Resolves wayfinder ticket [#133](https://github.com/shredingerlabs/oc_sandbox/issues/133) (map [#131](https://github.com/shredingerlabs/oc_sandbox/issues/131)). Files/lines refer to `dev_WebUI` @ 696cbc6.

**Question**: what exact changes make OpenCode web access always-on in EVERY start mode — decided direction: publish port 4096 (auto-scan upward) AND start the OpenCode v2 background service, even when the configured start option is console or OpenCode TUI? Plus: port behavior under pods for concurrent workspaces, and how a Go web backend obtains pair links on demand.

## 1. Current state (verified)

**Publishing is gated on `--start_web`** — `dist/scripts/start.sh:431-444`: `WEB_PORT_ARGS` is only populated when `$START_WEB`; else no `-p` at all. Same for the `service.json` pre-seed (`start.sh:590-599`) and the container command (`start.sh:604-615`): detached web runs `opencode service start && exec sleep infinity`, everything else detached runs `sleep infinity` (the v2 service then auto-starts on first `opencode` use — [opencode CLI docs: "OpenCode discovers or starts one shared background server"; `opencode service start` starts it explicitly](https://opencode.ai/docs/cli/)).

**Pod-level publishing already works on pasta** (ADR 0029 flow): publishings hang on the pod (`podman pod create … -p 127.0.0.1:<scanned>:4096`, members join via `--pod`, `start.sh:542-556`). [podman-pod-create](https://docs.podman.io/en/latest/markdown/podman-pod-create.1.html): `--publish` "within this pod"; pasta is an explicitly supported publish network ("Port publishing is only supported for containers utilizing their own network namespace through bridge networks, or the pasta network mode"); you "must not publish ports of containers in the pod individually, but only by the pod itself"; publishing "cannot be modified once the pod is created" — start.sh's deterministic pod recreate-at-start (`podman pod rm -f` + fresh create, tested against podman 4.9.3, `start.sh:535-545`) is exactly the right mechanism for any per-start port decision.

**Port scan**: `select_free_host_port` probes 4096–4196 host-side (`ss -ltn`, fallback `/dev/tcp`; `start.sh:375-410`) and announces a deviation ("Host-Port 4096 ist belegt; … auf http://127.0.0.1:<port>"). Loopback-bounded (`-p 127.0.0.1:<port>:4096`). Not recorded in the registry; `discovery.WebPort` reads the live mapping on demand (`podman port <container> 4096/tcp`, `internal/discovery/discovery.go:405-415`) — works against pod members with pod-level publish because the publish lands in the pod's shared netns.

**Pair link on demand already exists** — `discovery.PairLink` (`internal/discovery/discovery.go:417-434`) execs `opencode pair --url http://<published-host>` inside the container and extracts the printed link; Open Project web access pairs afresh at every access (`internal/openproject/openproject.go:1232-1249`; CONTEXT.md "pair link": one-time, prints fresh at every web access, falls back to the plain URL when pairing fails — 5-min link expiry / 30-day session per repo glossary). [opencode CLI docs](https://opencode.ai/docs/cli/): "Pairing requires the shared service".

**Foreground direct-CLI path** (`--start_web` without `--detach`) is different by design: `START_WEB_ENV=true` is consumed by the image's login shell (`dist/templates/bash/bash_profile:5`) running `opencode serve --hostname 0.0.0.0 --port 4096` foreground, so Ctrl+C keeps stopping/removing the `--rm` container (ADR 0024 deliberated this: a daemonized service can't honor Ctrl+C). The TUI always uses the detached path.

**Hard exception**: `--offline` runs the project container `--network=none` pod-less (`start.sh:334-341`); no publishing is possible — web access is structurally unavailable offline.

**Docs-drift note**: current [opencode.ai/docs/web](https://opencode.ai/docs/web/) (updated 2026-11-09) documents an `opencode web` server command + `server` config block + `opencode attach <url>` (TUI attachment onto a running web server) — none of which ADR 0024 had available ("no `opencode web` subcommand"). The implemented service+pair flow still fits always-on best; re-verify `opencode web`/`attach` against the shipped image at implementation time (they could simplify things — `attach` especially is relevant to ticket #132's terminal work — but they are not required for always-on).

## 2. Answers

### (a) Always publish 4096 in every mode — trivially cheap
Emit `WEB_PORT_ARGS` unconditionally (skip only under `--offline`) and run `opencode service start && exec sleep infinity` as the detached command for every start mode. No new podman capability is needed — pod-level publish on pasta is the mechanism in production today; the pod recreate cycle absorbs the "cannot modify once created" constraint. One process cost: the opencode server runs from boot even in console-mode containers that never open a client. That is the same process the first `opencode` use would auto-start anyway (identical footprint, earlier). Keep the keep-alive as PID 1.

### (b) service.json — un-pin from the web flag
Make the idempotent ensure (`write-if-missing` / jq-normalize to `{"hostname":"0.0.0.0","port":4096}`, `start.sh:591-599`) unconditional. The pin is required for a published host port to ever reach the service (v2 listens on 127.0.0.1 by default, unreachable through a publish — ADR 0024), and its write-on-boot keeps stale user edits healed. It must not block a failed jq (soft-fail warning, as today).

### (c) Concurrency / collisions
- Multiple workspaces each scan 4096–4196 at their own start; sequential starts are safe. Simultaneous starts carry an existing TOCTOU window (both probe free, both claim) — unchanged by always-on, same window size; document rather than engineer (`flock` on a per-port claim file is the minimal later fix if it ever bites).
- Serve nothing persistent differently per workspace: each `pod create` re-scans; a running workspace keeps its captured port until its pod is recreated at next start.
- Registry: **don't persist the port**. Live discovery (`podman port`, returned for ANY member of the pod) is authoritative and self-heals; a stale registry value would be a new lie (the scan only decides at pod-create). Today's `WebPort()` is exactly the API the future WebUI backend calls.
- Host-port actually occupied by a non-podman app on 4096: scan handles it today.

### (d) Pair links from a Go web backend
Reuse the TUI's capability verbatim: an API action runs `discovery.PairLink(runner, container, "http://" + discovery.WebPort(...))` fresh per request (pair output is ephemeral — one-time sign-in links expire; every access pairs afresh, so no expiry bookkeeping, no persistence). Return BOTH:
1. `pair link` (when pairing succeeded — signs the browser in, 30-day session),
2. `plain published URL` as fallback when pairing fails (today's fallback semantics).

The WebUI frontend hands the link to a new tab (`window.open`). Already-paired browsers just get a fresh one-time link — harmless. Pairing fails when the service is down or mid-boot; the plain URL keeps the UX viable, and a "retry pair" action covers it. Never cache pair links (they die by themselves in minutes).

### (e) Direct CLI parity
Leave the foreground `--start_web` path untouched (ADR 0024's Ctrl+C contract). With always-on, the `--start_web` flag becomes a no-op *for the detached path* (publish and service are unconditional); keep the flag parsed for backward compatibility and for the foreground serve behavior. The detached end-of-start echo (`start.sh:650-653`) should always announce the URL + pair-hint, not only when `--start_web`.

## 3. Recommended start.sh change sketch

1. `WEB_PORT_ARGS`: computed unconditionally (exception: `--offline` → empty + explicit note; `CBM_PORT_ARGS` stays `--cbm_ui`-gated).
2. `service.json` ensure: unconditional (same idempotent write).
3. Detached container command: always `opencode service start && exec sleep infinity`.
4. `--start_web`: stays parsed (foreground serve semantics unchanged); detached path ignores it; help text notes always-on web.
5. Start echo: always announce `http://127.0.0.1:<port>` + the pair-hint line.
6. No image/config/registry changes; `test-start.sh` expectations (pod-create args, publish presence in every mode) need updating.

## 4. Risks / edge cases

- `--offline`: web structurally impossible (no publish on `--network=none`) — document as the explicit exception; WebUI must render the Web action disabled for offline workspaces.
- One host port consumed per running workspace even if the user never opens a browser — acceptable for a local single-user tool; the scan range (101 ports) bounds ~101 concurrent workspaces.
- Long-lived WebSocket traffic over pasta publish: already exercised in production by the current web mode; no new exposure.
- WebUI "Open Web" should verify readiness before handoff: port mapping exists (`podman port`) + attempt pair; on failure present the plain URL (step 2d), not an error dead-end.

## Sources

- `dist/scripts/start.sh` (lines cited inline) — publish gating, scan, pod recreate, service.json ensure, container command, foreground path, echoes
- `docs/adr/0024-opencode-v2-runtime.md`, `docs/adr/0029-pod-per-project-networking.md` — service pin, Ctrl+C contract for foreground, pod/pasta topology
- `CONTEXT.md` — glossary "web start option", "pair link", "shared background service"
- `internal/discovery/discovery.go:403-434` — WebPort / PairLink
- `internal/openproject/openproject.go:1232-1249` — web access + fallback announcement
- https://opencode.ai/docs/cli/ — shared background service, `opencode service start`, pairing requires the service
- https://opencode.ai/docs/web/ — current-day `opencode web`/`attach` (docs-drift note), server config block
- https://docs.podman.io/en/latest/markdown/podman-pod-create.1.html — `--publish` under pods, pasta publish support, immutability after create
