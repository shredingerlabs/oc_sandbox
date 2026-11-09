# `opencode web` / `opencode attach` — Docs Drift vs. Shipped Build (#142)

Part of #131 (map) · verifies the docs-drift flag from #133's resolution · 2026-11-09

## Verdict

**False alarm.** The `opencode web` / `opencode attach` subcommands do **not** exist in the
shipped build (`opencode v2.2.6`, which *is* the current latest channel — upstream's newest v2
release, 2026-11-06). The docs page that documents them (`opencode.ai/docs/web/`, footer
"Last updated: Nov 9, 2026") is a **stale v1-era page upstream never deleted**: its content was
last touched 2026-02-03 (v1), and the footer date is a site-rebuild stamp, not a content update.
The *current* v2 docs (`opencode.ai/docs/cli/web/`) and the shipped binary match ADR-0024's
invocation flow exactly. **ADR-0024 needs no revision.**

## Evidence

### Shipped build (this container = `dist/Dockerfile` latest-channel install)

```
$ opencode --version
opencode v2.2.6
```

`opencode --help` subcommand list (complete, no `web`, no `attach`):

> upgrade · uninstall · acp · api · debug · auth · mcp · plugin · models · stats · mini · run ·
> session · service · reload · pair · serve

`opencode web --help` and `opencode attach --help` **exit 0 and print the root help** — the CLI
silently swallows unknown subcommands because the root usage takes `[<directory>]`. An isolated
live probe (`HOME=/tmp/opencode/oc-probe-home`, fresh empty dir) shows what actually happens:

```
$ opencode web          # outside any TTY, nonexistent dir 'web'
Error: ENOENT: no such file or directory, chdir '.../oc-probe-home' -> 'web'
```

So `opencode web` on v2 is parsed as "start OpenCode in the directory `web`". Footgun note: once
the WebUI repo-root `web/` tree (#132 decision) exists, typing `opencode web` at repo root would
silently launch the TUI inside `web/` instead of erroring. Harmless to the product (users never
run opencode from the repo root), worth remembering during manual testing.

### Upstream release surface

`gh release list --repo anomalyco/opencode`: newest v2 tag **v2.2.6 (2026-11-06)** — identical to
the shipped build; nothing newer exists that could add the subcommands. The v2 `dev` branch's CLI
handler tree (`packages/cli/src/commands/handlers/`) contains no `web.ts` / `attach.ts` either —
even unreleased upstream does not implement them.

### Where the "docs updated 2026-11-09" claim comes from

- `https://opencode.ai/docs/web/` — reachable, linked in the docs nav ("Usage → Web"), documents
  `opencode web --port/--hostname/--mdns/--cors`, `OPENCODE_SERVER_PASSWORD`,
  `opencode attach <url>`, and a `"server"` key in `opencode.json`. Source file
  `packages/web/src/content/docs/web.mdx`: last real commits **2026-02-03** / 2026-02-01 /
  2026-01-14 — all v1-era. This page describes v1, where `opencode web` really existed.
- Site-wide footer "Last updated: Nov 9, 2026" = rebuild timestamp; page content is unchanged.
- The **current v2 page** is `https://opencode.ai/docs/cli/web/` ("Web" under CLI): `opencode
  pair`, `opencode service status/set/start`, `opencode serve`, `opencode --server <url>` — no
  `web`/`attach` subcommand anywhere.

## Per-question answers

1. **Do `web`/`attach` exist and behave as documented in the shipped build?** No. Absent from the
   v2.2.6 subcommand list; `opencode web` resolves as a directory argument (chdir), `attach`
   likewise. The documented behavior lives only in the stale v1 page.
2. **Shared service or private server?** Moot — the subcommand doesn't exist on v2. The v2
   analogues are explicit: `opencode serve` is documented as "keep a dedicated server instead of
   the shared background service" (exactly the two-servers-one-DB drift ADR-0024 rejected), while
   any TUI client joins the shared service with `opencode --server <url>`. No interaction with
   the `service.json` pin is possible. Related discovery: v2's **default service port is 49374**
   (macOS/Linux/WSL; 28894 on Windows), not 4096 — confirming the `service.json` pin
   (`0.0.0.0`/4096, start.sh:591-597) remains load-bearing; without it the published port could
   never reach the service.
3. **Could `web`/`attach` simplify always-on (#133) or terminal/handoff (#126/#132)?** No —
   nothing to simplify with, in the shipped build. The v2-native "attach" already exists as the
   root flag `--server` (visible in `opencode --help`): a TUI started with
   `opencode --server http://127.0.0.1:4096` joins the always-on service. That's consistent with
   the settled design (host-terminal launch via the Go binary, console via `podman exec`,
   #132's headless fallback) and changes nothing. If a future ticket wants a second TUI client
   or in-browser TUI, `--server` is the supported mechanism — no `exec`-in-TUI workaround needed.
4. **Pairing per glossary?** Confirmed. Current v2 docs (`docs/cli/web/`): "Links work once and
   expire in 5 minutes" and "Sessions last 30 days" — exactly CONTEXT.md's "pair link" record
   (one-time link, 5-min expiry, 30-day session). `opencode pair --help` in the shipped build
   confirms the `--url` flag ("Use an external HTTP(S) server URL in pairing links") that
   `discovery.PairLink` relies on. Two new v2 details worth knowing: a QR code pairs the OpenCode
   app the same way, and changing the server password signs out every session. `--remote`
   (OpenTunnel, exposes the server to the internet) requires `--yes` without a TTY — start.sh
   never passes it, so no prompt risk in the non-interactive flow.

## Recommendation

**ADR-0024's invocation flow (service.json pin → `service start` → `pair --url`) stands as-is —
no revision.** The #133 docs-drift flag resolves as "stale upstream docs, not missing v2
subcommands"; #132's terminal research is unaffected. Optional one-line addition to ADR-0024's
Consequences if desired: `/docs/web/` is stale v1 documentation; on v2 `opencode web` is a
directory argument and client attach is `--server <url>`. Not required for correctness.

## Sources

- Shipped binary probes: `opencode --version` / `--help` / `web|attach|pair|serve|service --help`;
  isolated live probe with `HOME=/tmp/opencode/oc-probe-home` (process killed, no real state touched)
- Upstream releases: `gh release list --repo anomalyco/opencode` (v2.2.6, 2026-11-06; v1.19.5 tagged "Latest")
- Stale page: https://opencode.ai/docs/web/ + source history of
  `packages/web/src/content/docs/web.mdx` (commits 2026-01-14 → 2026-02-03)
- Current v2 page: https://opencode.ai/docs/cli/web/ and https://opencode.ai/docs/cli/
- Repo: `dist/Dockerfile:40` (floating latest URL), `dist/scripts/start.sh:591-611,652`
  (service.json pin, service start, pair echo), `internal/discovery/discovery.go:403-422`
  (`WebPort`, `PairLink`), `docs/adr/0024-opencode-v2-runtime.md`, `CONTEXT.md` ("web start
  option", "pair link", "shared background service")
