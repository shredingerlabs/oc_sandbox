# WebUI Serving Architecture (Single-Binary Go)

**Issue:** #132  
**Date:** 2026-11-09  
**Status:** Complete

## Executive Summary

For oc-sandbox's WebUI-first pivot (single Go binary, CGO off, dist/-only production installs, cross-compiled in `scripts/create-release.sh` from the repo checkout), the best fit is: **embed a repo-root `web/` asset tree at compile time with `//go:embed` and serve it via `net/http` `FS`/`FileServer`** (no release-script layout change beyond building the binary as today, because embedding happens at build time); **no Node toolchain in production and no Node required at build time** for a small local admin UI — use plain HTML/JS/CSS plus HTMX and optionally Alpine.js loaded as static assets; **bind to `127.0.0.1` only, validate the `Host` header, and put a high-entropy per-launch token in the URL path prefix** (not query, not cookie-auth), with the first authenticated hit optionally setting a short-lived, `HttpOnly`/`SameSite=Strict` session cookie only to avoid repeating the token in every resource request — but the canonical design is token-in-path verified per request and no ambient-cookie authority for state changes; **use Server-Sent Events (SSE)** for one-way server→browser log streaming (fits `text/event-stream`, built-in `EventSource` reconnect, and taskmgr's line-oriented producer) — token must be in path/query/cookie because `EventSource` cannot set custom headers; **open a host terminal window best-effort per platform with a single decisive fallback: do not block — run headless and expose the console/command in the WebUI while printing the exact command to the user**; **open the browser tab with `xdg-open`/macOS `open`/Windows `cmd /c start` (or `rundll32 url.dll,FileProtocolHandler`) via `os/exec`, with a small wrapper like `github.com/pkg/browser` acceptable, and always print the full URL as the fallback**.

Key security posture (local tool, no login UI): loopback bind + per-launch unguessable token + `Host` header allowlist (localhost/127.0.0.1/[::1] on the bound port) to mitigate DNS rebinding/cross-origin abuse; treat CSRF defenses as defense-in-depth (Fetch Metadata/Origin checks) because browsers can be induced to make requests to localhost in some threat models. This matches how established local-server tools gate access (Jupyter token; Vite/webpack host checks) while keeping the UX to "open URL with token."

---

## 1. Static Assets Embedded in the Single Go Binary

### 1.1 `go:embed` mechanics (primary: `embed` package docs)

Source: Go standard library `embed` package documentation, <https://pkg.go.dev/embed>.

- **Directives and targets.** A `//go:embed` directive immediately precedes a single package-scope variable declaration (blank lines/`//` comments allowed between). The variable type must be `string`, `[]byte`, or `embed.FS` (or alias). Patterns use `path.Match` syntax, space-separated on one directive or repeated across directives.
- **Path rules.** Patterns are interpreted relative to the package directory containing the source file. Path separator is forward slash even on Windows. Patterns **may not contain `.` or `..` or empty path elements, nor may they begin or end with a slash**. `*` matches everything in the current directory (not `.`). Patterns can be Go double/back-quoted literals to allow spaces. "Patterns must not match files outside the package's module, such as `.git/*`, symbolic links, `vendor/`, or any directories containing `go.mod` (these are separate modules)." Matches for empty directories are ignored. Each pattern must match at least one file or non-empty directory or the build fails.
- **Directory embedding and hidden/underscore files.** If a pattern names a directory, all files in the subtree are embedded recursively, **except files with names beginning with `.` or `_` are excluded**. The `all:` prefix changes directory walking to **include files beginning with `.` or `_`** (e.g., `//go:embed all:image`). The docs note `image/*` embeds `image/.tempfile` while `image` does not; neither embeds `image/dir/.tempfile` unless using `all:`/appropriate behavior per rule.
- **Scope/usage.** Variables may be exported or unexported; only package scope, not local variables. Importing `embed` is required even for `string`/`[]byte` embeds (blank import if `embed.FS` not referenced).
- **Tooling.** Patterns are visible to `go list` (`EmbedPatterns`, `TestEmbedPatterns`, `XTestEmbedPatterns`).

### 1.2 Serving an embedded FS via `net/http` (primary: `net/http` + `io/fs`)

Sources: Go `net/http` docs, <https://pkg.go.dev/net/http#FileServer> and <https://pkg.go.dev/net/http#FS>; `embed.FS` implements `io/fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS` behavior as described in `embed` docs (<https://pkg.go.dev/embed>).

- `http.FileServer(root FileSystem) http.Handler` serves files from `root`. `http.FS(fsys fs.FS) http.FileSystem` adapts an `fs.FS` (including `embed.FS`) for `FileServer`. Typical pattern: `http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(content))))` (example shown in `embed` docs). `FileServerFS(root fs.FS)` (Go 1.22+) serves directly from `fs.FS`.
- `embed.FS.Open`/`ReadFile`/`ReadDir` make it usable anywhere `io/fs` is accepted. Because embedded paths are rooted at the embed point, serving a subdirectory usually uses `fs.Sub` (`io/fs`) to re-root (standard practice consistent with `io/fs.FS` semantics; cite `io/fs` package when implementing). `FileServer` handles index/content-type/redirect semantics; care is needed so API routes are not shadowed by the static handler.
- **Dotfiles/underscore:** default `//go:embed dir` excludes `.`/`_`-prefixed names; add `all:` if such assets are intentionally shipped (usually not needed for a bundled WebUI). Gitignored dirs: `go:embed` embeds from source files present at build time per pattern/match rules; patterns cannot reach outside module or into separate modules. A gitignored `dist/bin` or other ignored outputs are irrelevant if not matched and not under the embedding package in the intended pattern — but **do not place embeddable web source under a path excluded from the build context you compile from**.

### 1.3 Where web assets should live in this repo

Constraints from context: production installs ship **only `dist/`** contents; Go source lives at repo root; `dist/bin/` gitignored; `scripts/create-release.sh` cross-compiles the Go TUI from the repo checkout (CGO off, linux/darwin × amd64/arm64) and places binaries into `dist/bin/` before cutting an orphan release branch from `dist/` contents.

**Decision:** put WebUI source assets in a normal repo-root package/directory, e.g. **`web/` at repo root** (or `internal/webui/assets/`), owned by a Go package that embeds them (e.g. `//go:embed all:static` or `//go:embed static/*` depending on naming needs). Compile-time embedding runs during `go build` in `create-release.sh` (and local builds), so the embedded bytes are inside `dist/bin/oc-sandbox_*` binaries — **no runtime dependency on `dist/` web files and no change to the dist/-only production model**.

Release-script impact (minimal, decisive):
- **No new asset copy step** required if assets live in-repo and are compiled in; `create-release.sh` already runs `go build` per target from repo root. Ensure `build_go_tui` (and any future build helper) builds the package that references the embed (currently `./cmd/oc-sandbox` is fine if it imports the web package, or if embed lives in that package path).
- **Optional hygiene only:** if generated frontend artifacts are introduced later, decide whether they are committed (simplest, reproducible, no Node in release script) or generated in `create-release.sh` before `go build` (then add Node/tooling checks). For the chosen no-node stack, no generation step is needed.
- Do **not** generate web files into `dist/` and embed from there unless the embed source file lives there too (embed paths are relative to the embedding package and cannot use `..`; they also must be in module context). Embedding from `dist/` would couple source layout to release staging awkwardly; repo-root `web/` is cleaner.

### 1.4 Frontend stack tradeoff: no-node vs node-at-build-only

| Option | Build/runtime deps | Fit for small local admin UI (forms, buttons, live log pane) | Sources |
|---|---|---|---|
| **No-node (recommended): plain HTML/CSS/JS + HTMX (+ optional Alpine.js / small Web Components)** | No Node at build or runtime; static files committed/embedded as-is. HTMX is loaded via `<script>` (CDN or vendored file) — htmx install docs show script-tag usage and npm option; core idea is HTML attributes driving HTTP requests and swapping HTML (<https://htmx.org/docs/>). Alpine.js is a lightweight script-tag-driven UI layer for local state/interaction (<https://alpinejs.dev/start-here>). | **Best fit.** oc-sandbox needs a small control plane: forms/buttons/actions + live log pane (SSE). HTMX covers partial updates/forms without SPA plumbing; Alpine covers small client-state bits; SSE/EventSource is native browser API for the log pane. Lowest supply-chain/build complexity; aligns with single-binary, CGO-off, multi-OS release script. | htmx docs/install <https://htmx.org/docs/>; Alpine start <https://alpinejs.dev/start-here> |
| Node-at-build-only (React/Svelte/Vue/Vite/etc., compiled to static assets) | Node required in dev/CI/release to build; output is static files embedded in binary. | Viable for larger apps, but overkill here. Adds toolchain/version churn, larger JS payload/routing/state complexity, and release-script/build prerequisites for a UI that is mostly CRUD + stream. Only choose if the WebUI scope grows beyond a small admin surface. | Vite server/build docs context <https://vite.dev/config/server-options.html> (dev server features not needed in production embed; build output would be static) |

**Recommendation (area 1):** no-node static UI, vendored `htmx.min.js`/`alpine*.js` (or equivalent pinned assets) under `web/static/`, embedded from a repo-root `web` Go package via `embed.FS` + `fs.Sub` + `http.FileServerFS`/`StripPrefix`. Keep API under `/api/...` and SSE under a dedicated path; serve SPA-ish fallback only if needed.

---

## 2. Local Auth for 127.0.0.1 + Per-Launch Token in URL

### 2.1 How established local-server tools do it

**Jupyter Server (primary):** token-based auth is **on by default** when no password is configured. The token is generated at startup, logged, and can be supplied in the `Authorization: token ...` header, **as a URL parameter `?token=...`**, or via a login/password form field; visiting the tokenized URL sets a cookie so the token is not needed again until cookies/browser context change. Jupyter also supports disabling auth (`token`/`password` empty) but explicitly warns **NOT RECOMMENDED** unless protected elsewhere. <https://jupyter-server.readthedocs.io/en/latest/operators/security.html>

**Vite dev server (primary):** binds/hosts are configurable; `server.allowedHosts` (default allows localhost/`.localhost`/IPs; skipped for HTTPS per docs page context) exists because setting allowed hosts too permissively or exposing the server allows requests — Vite explicitly warns DNS rebinding can let other sites send requests and download source/content, recommends explicit allowed-host lists, and references GHSA-vg6x-rcgg-rjx6. `server.cors: true` is also dangerous (any origin can request/download). Vite 6.0.9+ behavior tightened `Host` checks. <https://vite.dev/config/server-options.html>

**webpack-dev-server (primary):** `devServer.allowedHosts` defaults to allowing `localhost`, host, and websocket hostname in `auto` mode; `all` bypasses host checking and is **NOT RECOMMENDED** due to DNS rebinding vulnerability. Wildcard subdomain forms (`.host.com`) are supported. <https://webpack.js.org/configuration/dev-server/>

Pattern across tools: **local servers treat the network boundary as insufficient** — they use a secret (token) and/or Host/origin restrictions and warn against permissive exposure.

### 2.2 Token placement: path prefix vs query vs first-hit-then-cookie

| Placement | Pros | Cons / security notes | Verdict |
|---|---|---|---|
| **URL path prefix** (e.g. `http://127.0.0.1:PORT/t/<token>/...`, or redirect from that base) | Token not a normal "query param" convention; easy to mount static/API/SSE under a token-scoped mux; can be stripped by middleware before routing. Avoids some query-string logging defaults in app code (you still must not log full URLs). | Still appears in browser history/address bar, can leak via Referer to external origins if you ever link out (don't), and via proxies if misused (no proxy here). Bookmark/share risk is acceptable/intentional for a per-launch local secret URL. | **Recommended** for the user-facing launch URL and internal routing scope. |
| Query string (`?token=`) | Simple; matches Jupyter's familiar pattern; works naturally with `EventSource` URL and links. | Query strings are commonly logged, appear in Referer, history, screenshots. Jupyter accepts this but also sets a cookie afterward; for a tiny local app, extra care needed. | Acceptable fallback; prefer path prefix. |
| First-hit token, then cookie | Cleaner subsequent URLs; matches Jupyter's "visit URL -> cookie set" UX. | **CSRF risk if cookie becomes ambient authority** for state-changing requests without CSRF protections. OWASP: "browser requests automatically include all cookies..." so CSRF works unless proper authorization is used; cookie-only defenses require care (SameSite is defense-in-depth, has limitations). If cookie is used, bind session to token, set `HttpOnly`, `SameSite=Strict` (or `Lax` with CSRF tokens for top-level nav only as appropriate), `Path` scoped, short lifetime, and still require a non-cookie CSRF check for unsafe methods *or* use a custom header/fetch-metadata strategy — but custom headers are hard with plain forms; simplest is avoid cookie authority and keep token in path. <https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html> | **Not recommended as the primary auth.** Optional ergonomic cookie may store nothing authoritative, or app can re-validate token per request. |

### 2.3 CSRF considerations for localhost services

Primary: OWASP CSRF Prevention Cheat Sheet, <https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html>.

- CSRF exploits browsers automatically attaching cookies/session credentials to cross-site requests; defenses include framework CSRF protections, synchronizer/double-submit tokens, Fetch Metadata (`Sec-Fetch-Site`), custom headers for AJAX/API, Origin/Referer verification, and SameSite cookies (defense-in-depth). XSS defeats CSRF mitigations.
- **SameSite:** useful defense-in-depth for session cookies but has limitations (OWASP notes subdomain/domain scoping hazards, compatibility/case nuances; not sufficient alone in general). Do not set cookies broadly for a domain.
- **For this design (no login UI, per-launch URL token, loopback):** safest is **no ambient session cookie carrying authority**. Put the unguessable token in the path (or query) and validate it on every request, including SSE and POST actions. Then a cross-site page cannot forge requests without knowing the token (which is not sent automatically like a cookie). Still add **Fetch Metadata/Origin checks** and **Host validation** as defense-in-depth; for JSON/fetch POSTs, require `Content-Type: application/json` or a custom header (forces preflight in CORS contexts) — but remember CORS/preflight behavior depends on cross-origin context and is not a substitute for server-side token checks. Since the token is in the URL, avoid leaking it in Referer by not linking to external sites; set a strict `Referrer-Policy` (e.g., `no-referrer`) as hardening.

### 2.4 DNS rebinding against 127.0.0.1 and Host-header validation

- **What it is (as reflected by vendor guidance):** an attacker-controlled DNS name can be made to resolve to a local/loopback address, causing a victim browser to send requests to a local service with an attacker-chosen `Host`/origin context under some conditions; services that trust "local" or skip origin/host checks can be abused. Vite warns DNS rebinding can allow websites to send requests to the dev server and download source/content; it recommends explicit `allowedHosts` and notes checks are skipped in some HTTPS cases on that config surface (<https://vite.dev/config/server-options.html>). webpack-dev-server warns `allowedHosts: 'all'` bypasses host checking and is vulnerable to DNS rebinding (<https://webpack.js.org/configuration/dev-server/>).
- **Mitigation:** validate the HTTP `Host` header against an allowlist for the actual listener: `127.0.0.1:<port>`, `localhost:<port>`, `[::1]:<port>` as applicable; reject others with 403/421. Bind only to loopback (per constraint). This is the standard mitigation implemented by Vite/webpack's host checks. (Also note Go 1.25 `http.CrossOriginProtection` implements Fetch-Metadata-based CSRF/origin protection including `Sec-Fetch-Site` checks — <https://pkg.go.dev/net/http#CrossOriginProtection>, <https://pkg.go.dev/net/http@go1.25> context from OWASP cheat sheet mention; useful as an additional layer, but it is not a substitute for the per-launch secret token.)

### 2.5 Recommendation (area 2)

- Bind `net.Listen("tcp", "127.0.0.1:<port>")` (or `[::1]` equivalent if explicitly supported; prefer IPv4 loopback default for simplicity).
- Generate a per-launch cryptographically random token (>= 128 bits entropy, URL-safe base64/hex). **Put it in the URL path prefix** that scopes the UI/API/SSE, e.g. `http://127.0.0.1:<port>/t/<token>/`. Open that URL automatically.
- **Validate token on every request** (including EventSource SSE, static if scoped, and all POST/unsafe actions). Do not rely on cookies for authority.
- **Host-header allowlist:** accept only loopback hostnames for the bound port.
- Add `Referrer-Policy: no-referrer`, `Cache-Control: no-store` on sensitive responses, `Content-Security-Policy` appropriate for a local app, and consider `X-Content-Type-Options: nosniff`. Use Fetch Metadata (`Sec-Fetch-Site`) / Origin checks as defense-in-depth (OWASP; Go `CrossOriginProtection` can help). Disable CORS unless explicitly needed.

---

## 3. Live Log Streaming: SSE vs WebSocket

### 3.1 SSE (primary: MDN)

Source: <https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events>.

- Client uses `EventSource(url[, { withCredentials }])`. Receives unnamed `message` events and named events (`event:` field) via `EventSource.addEventListener`. `EventSource` **cannot set custom request headers** — auth/params must be in URL (query/path) or cookies (with `withCredentials` behavior as applicable).
- Protocol is `text/event-stream` over HTTP, UTF-8. Messages separated by blank line; fields `event`, `data` (multiple `data` lines concatenated with newline), `id` (last-event-id), `retry` (reconnect ms). Comment lines (`:`) can keep connections alive. Browsers **auto-reconnect** by default; connection closes with `.close()`.
- One-way server→client. MDN notes HTTP/1.1 per-browser connection limits (historically ~6 per domain) and recommends HTTP/2 where relevant; for a local single-user tool opening one/few tabs, this is not a practical constraint.

### 3.2 WebSocket (primary: MDN)

Source: <https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API>.

- Full-duplex interactive channel (`ws`/`wss`), `WebSocket` API (and non-standard/less-supported `WebSocketStream` for backpressure attempts; WebTransport is the newer alternative for advanced cases). Requires handshake/upgrade handling, message framing, and explicit reconnect/app-level protocol design. Useful for bidirectional interaction, binary/low-latency messaging.

### 3.3 Fit for oc-sandbox

Requirements: live log streaming to browser (one-way server→browser log lines), server-side producer exists (internal/taskmgr streams op output line-by-line over channels). No browser→server log messages required.

**Decision: SSE.** It matches one-way text line streaming exactly, gives built-in reconnect/`id`/`retry` semantics, is trivial to implement in Go (`http.Flusher`, `text/event-stream`, disable buffering, detect `Request.Context().Done()`), and maps naturally onto taskmgr channel lines. WebSocket adds handshake/state/reconnect complexity with no benefit for log tailing. The main constraint is auth transport: **`EventSource` cannot send custom headers, so the token must be carried in the SSE URL path/query (or cookie)** — which reinforces area 2's recommendation to scope routes by token/path and validate on every request.

Implementation notes (non-normative, consistent with MDN): set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no` (harmless locally), flush after events; send periodic comments as keepalive if needed; close on client disconnect.

---

## 4. Opening a New Terminal Window on the Host from a Go Backend

Constraint: host-terminal-window (NOT in-browser xterm.js) for console start variants, "if possible"; backend is the Go binary (cross-platform).

### 4.1 Linux

Goal: launch a *new terminal emulator window* that runs a specified command, detached from the server process.

| Terminal | Primary invocation pattern (typical; verify installed CLI) | Notes |
|---|---|---|
| **kitty** | `kitty [options] [program-to-run ...]` / remote control via `kitten @` in single-instance setups. Kitty docs: invocation/launch behavior and rich CLI for running programs/windows (<https://sw.kovidgoyal.net/kitty/invocation/>, <https://sw.kovidgoyal.net/kitty/launch/>). | Good when present; command args differ from VTE terminals. Detect by `exec.LookPath("kitty")`. |
| **gnome-terminal** | Historically `gnome-terminal -- <command>` / `--window` (behavior changed across versions; modern GNOME often prefers `kgx` or DBus activation). | Version/desktop fragmentation is real; avoid assuming default on non-GNOME. |
| **konsole** (KDE) | `konsole -e <command>` / `--new-tab`/separate window options vary by version. | Common on KDE. |
| **xfce4-terminal** | `xfce4-terminal -e "<command>"` or `-x <command>` depending on version. | XFCE. |
| **xterm** | `xterm -e <command>`. | Widely available fallback on X11 systems; minimal UX. |
| **foot** | `foot <command>` (Wayland). | Wayland-native; increasingly common on wlroots compositors. |

**Detection/selection issues:** `XDG_CURRENT_DESKTOP` and `$TERM_PROGRAM` (if set) are hints, not reliable proofs of which terminal binary is installed/launchable. A robust strategy is: if user configured a terminal command, use it; else probe an ordered list of known binaries with `exec.LookPath` (prefer the desktop's native terminal when detectable, else common ones: `x-terminal-emulator` on Debian/Ubuntu, `kitty`, `gnome-terminal`, `konsole`, `xfce4-terminal`, `foot`, `xterm`). Always launch detached (`Start` semantics; new session/process-group as appropriate) so terminal lifetime is independent. **Failure modes:** headless/SSH/no `$DISPLAY`/no Wayland socket — GUI terminal launch will fail; handle error and fall back (below). Sandboxed/Flatpak/Snap contexts can complicate launching host terminals from inside sandboxes (not oc-sandbox's normal host-run case, but note).

### 4.2 macOS

- **Terminal.app:** open a new terminal window running a command via AppleScript/`osascript`: `osascript -e 'tell application "Terminal" to do script "<shell-escaped command>"'` (optionally `activate`). `open -a Terminal` opens Terminal but does not reliably pass an arbitrary command to execute in a new shell session the same way; `osascript` is the standard programmable path. (AppleScript Terminal dictionary behavior is the primary mechanism; iTerm2 provides analogous scripting — see below.)
- **iTerm2:** has extensive AppleScript/scripting support and Python API; scripts can create windows/tabs/sessions and run commands (primary: <https://iterm2.com/documentation-scripting.html>). Example pattern: tell iTerm2 to create window/tab with profile and command/text. Detection by presence of `/Applications/iTerm.app` or bundle id/open behavior; prefer iTerm2 only when configured/requested or clearly active.

**Recommendation:** on macOS, prefer `osascript` targeting Terminal.app for broad availability unless iTerm2 integration is explicitly selected; escape command strings carefully.

### 4.3 Windows / WSL

- **Windows Terminal (`wt.exe`)** (primary: <https://learn.microsoft.com/en-us/windows/terminal/command-line-arguments>): `wt [options] [command ;]`, e.g. `wt new-tab -- cmd /k ...`, `wt -w 0 nt`, `wt --`/profile/starting dir options; supports `split-pane`, `new-tab`, targeting windows. Execution alias `wt`; `wtd` for dev builds. From WSL, aliases may not work directly — Microsoft docs show `cmd.exe /c "wt.exe" ...` from WSL command lines. This is the best modern Windows terminal target.
- **`cmd /c start`**: opens a new console window/process; useful fallback (`start` is a `cmd` internal command). Syntax/quoting is finicky; `cmd /c start "" <command> ...`.
- **WSL specifics (primary: <https://learn.microsoft.com/en-us/windows/wsl/filesystems>):** Windows tools can be run from Linux/WSL as `[tool].exe` (e.g. `cmd.exe`, `powershell.exe`, `wt.exe`), and Linux binaries/commands can be invoked via `wsl.exe`. A Linux Go binary running inside WSL can spawn a *Windows* terminal window via `cmd.exe /c start ...` or `cmd.exe /c wt.exe ...`, subject to interop being enabled. Path translation matters for working directories/commands (`wslpath`, `/mnt/c/...` vs Windows paths). If oc-sandbox runs as a native Windows binary, call `wt.exe`/`cmd` directly; if as a Linux binary under WSL targeting a Windows-side terminal, use `*.exe` interop and translate paths.

### 4.4 Decisive fallback (one)

**Fallback when no terminal can be opened (headless, SSH, no GUI, no supported terminal, launch error): do not fail the overall action; run/start the operation headless and make the console accessible in the WebUI's log/console view, AND print/copyably display the exact command the user can run manually in their own terminal.** Rationale: fits "host-terminal-window ... if possible", preserves live log streaming (already required), works over SSH/headless servers, and gives power users a manual escape hatch. Do not open xterm.js in browser (constraint); do not silently drop the command.

---

## 5. Auto-Opening a Browser Tab from Go at Startup

### 5.1 Per-platform opener (primary commands)

| Platform | Command | Notes |
|---|---|---|
| Linux/*BSD freedesktop | `xdg-open <url>` | Uses the user's configured desktop handler; standard on desktop environments (freedesktop/Portland tooling ecosystem). Requires a desktop session/handler; may fail in headless/SSH/minimal containers. |
| macOS | `open <url>` (or `open -a <browser> <url>`) | Opens URL with default handler/application. |
| Windows | `cmd /c start "" <url>` or `rundll32 url.dll,FileProtocolHandler <url>` | `start`/ShellExecute path opens default browser. `rundll32` is common in older snippets but has quoting/behavior caveats; prefer `cmd /c start` or `ShellExecute` via syscall in native Windows builds — from a cross-platform Go binary using `os/exec`, `cmd /c start` is practical. |

Implementation: `os/exec.Command(...)` with platform `GOOS` switch; do not wait/block UI startup on browser process beyond launch errors. Capture stderr for diagnostics.

### 5.2 `github.com/pkg/browser` vs hand-rolled

Primary: <https://github.com/pkg/browser> (README via pkg source mirror: <https://raw.githubusercontent.com/pkg/browser/master/README.md>; pkg docs <https://pkg.go.dev/github.com/pkg/browser>).

- Provides `OpenURL(url)`, `OpenFile`, `OpenReader`; uses platform mechanisms under the hood and exposes stdout/stderr writers. Small, focused dependency.
- **Tradeoff:** hand-rolled is ~20 lines and avoids an external dependency (fits "pure Go deps" history, though adding a small dep is not CGO). `pkg/browser` has maintenance/history/edge-case handling but is not security-critical; verify maintenance status before adopting. **Decision:** either is acceptable; if minimizing deps, hand-roll `xdg-open`/`open`/`cmd /c start` with careful arg handling and clear errors. If preferring battle-tested behavior, use `pkg/browser` after a quick dependency review. Both must treat failure as non-fatal.

### 5.3 Failure fallback

**Always print the full authenticated URL to stdout/TUI/log (and make it copyable) regardless of opener success.** If opening fails (headless, no browser, SSH, `xdg-open` missing, remote X/Wayland issues), do not treat as fatal: the server is already listening on `127.0.0.1`; the user can open the URL manually or via SSH port forwarding where appropriate. Optionally attempt clipboard copy only if an existing, safe mechanism is present — do not add new platform-specific clipboard deps just for this.

---

## Final Recommendation (Decisive)

### (a) Asset location + embed mechanism + `scripts/create-release.sh` changes

- **Location:** `web/` at repo root (or `internal/webui/web/`), versioned in git, containing only static distributable UI assets for the no-node stack (`index.html`, JS/CSS, vendored HTMX/Alpine if used, icons). No Node build output required.
- **Embed:** in a Go package compiled into `cmd/oc-sandbox` (or imported by it), declare `//go:embed all:static` (or specific patterns) into an `embed.FS`; re-root with `io/fs.Sub` if needed; serve with `http.FileServerFS`/`http.FileServer(http.FS(...))` mounted below the token-scoped path, taking care not to shadow `/api`/SSE routes. Follow `embed` rules: package-scope var, no `..`, module-contained files, `all:` only if dot/underscore assets are intentional.
- **`scripts/create-release.sh`:** **no required functional change to artifact staging**. The script already cross-compiles from the repo checkout into `dist/bin/` (`CGO_ENABLED=0`, `-trimpath`, ldflags version). Because web assets are embedded at build time, binaries contain the UI and dist/-only installs remain valid. Optional: add a brief comment or a sanity check that `web/` exists when WebUI is enabled; if a future Node build is ever adopted, run it *before* `build_go_tui` and fail fast if Node/tooling missing — but do not adopt Node now.

### (b) Auth pattern (where token goes, Host-header check)

- Generate a per-launch CSPRNG token (>=128-bit). Expose the app under `http://127.0.0.1:<port>/t/<token>/...` and auto-open that exact URL.
- **Token goes in the URL path prefix**; validate on every HTTP request and SSE stream. Do not use ambient cookie auth. (Query token only as a secondary implementation detail if unavoidable; path is canonical.)
- **Host-header check:** allow only `127.0.0.1:<port>`, `localhost:<port>`, and `[::1]:<port>` forms actually used; reject others (DNS rebinding mitigation, aligned with Vite/webpack guidance).
- Bind loopback only. Add `Referrer-Policy: no-referrer`, `Cache-Control: no-store` for app/API/SSE responses as appropriate, and defense-in-depth `Sec-Fetch-Site`/Origin validation (OWASP; consider Go `http.CrossOriginProtection` where it fits). Never log the tokenized URL.

### (c) SSE vs WebSocket

- **Choose SSE** for live log streaming (`text/event-stream`, `EventSource`, one-way). Fits taskmgr's line-by-line channel output, gives reconnect semantics, minimal server/client complexity. Pass token in the SSE URL path. Revisit WebSocket only if future features require browser→server interactive streaming.

### (d) Terminal-open strategy per platform + fallback

- **Linux:** configurable override first; else probe `exec.LookPath` in a sensible order (`x-terminal-emulator`, desktop-native candidates like `kitty`, `gnome-terminal`, `konsole`, `xfce4-terminal`, `foot`, `xterm`). Launch detached, run command via each terminal's documented `-e`/argv form (kitty accepts program args directly; others vary). Handle no-display/Wayland/X11/headless failures.
- **macOS:** `osascript` to tell Terminal.app to `do script` (and activate) for the default path; optional iTerm2 AppleScript path if configured/detected (primary: iTerm2 scripting docs). Escape carefully.
- **Windows:** native Windows binary — `wt.exe` (preferred, <https://learn.microsoft.com/en-us/windows/terminal/command-line-arguments>) else `cmd /c start`. WSL-hosted Linux binary needing a Windows terminal — use Windows interop (`cmd.exe /c wt.exe ...` / `cmd.exe /c start ...` per Microsoft WSL docs; execution-alias caveats apply) and translate paths.
- **Single fallback (decisive):** if terminal open is unsupported or fails, **run headless, stream in the WebUI, and display the exact manual command**. Never require xterm.js.

### (e) Browser-open strategy + fallback

- **Strategy:** platform `os/exec` opener — Linux `xdg-open <url>`, macOS `open <url>`, Windows `cmd /c start "" <url>` (or `ShellExecute` in a Windows-native path). Hand-rolled is fine; `github.com/pkg/browser`'s `OpenURL` is an acceptable small abstraction if dependency review passes (<https://raw.githubusercontent.com/pkg/browser/master/README.md>, <https://pkg.go.dev/github.com/pkg/browser>). Launch non-blocking; log opener errors.
- **Fallback:** **always print the tokenized URL** (and keep it visible/copyable in TUI output). Browser-open failure is non-fatal.

---

## Source URLs (primary)

- Go `embed`: <https://pkg.go.dev/embed>
- Go `net/http` (`FileServer`, `FS`, `CrossOriginProtection`): <https://pkg.go.dev/net/http>
- MDN Server-Sent Events / `EventSource`: <https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events>
- MDN WebSocket API: <https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API>
- OWASP CSRF Prevention Cheat Sheet: <https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html>
- Jupyter Server security (token auth): <https://jupyter-server.readthedocs.io/en/latest/operators/security.html>
- Vite server options (`allowedHosts`/DNS rebinding, CORS, host): <https://vite.dev/config/server-options.html>
- webpack-dev-server `allowedHosts` / DNS rebinding warning: <https://webpack.js.org/configuration/dev-server/>
- htmx docs (no-build/script-tag, HTML-over-the-wire model): <https://htmx.org/docs/>
- Alpine.js start (lightweight script-tag UI layer): <https://alpinejs.dev/start-here>
- pkg/browser README: <https://raw.githubusercontent.com/pkg/browser/master/README.md>
- pkg/browser package: <https://pkg.go.dev/github.com/pkg/browser>
- kitty invocation/CLI docs: <https://sw.kovidgoyal.net/kitty/invocation/>
- Windows Terminal command-line arguments (`wt.exe`, WSL invocation notes): <https://learn.microsoft.com/en-us/windows/terminal/command-line-arguments>
- WSL filesystem/interop (running Windows `*.exe` from WSL): <https://learn.microsoft.com/en-us/windows/wsl/filesystems>
- iTerm2 scripting docs: <https://iterm2.com/documentation-scripting.html>
- Freedesktop desktop entry `Exec`/launching context (reference for desktop integration semantics): <https://specifications.freedesktop.org/desktop-entry-spec/latest/exec-variables.html>

Notes: platform "man page" details for `gnome-terminal(1)`, `xterm(1)`, `konsole(1)`, `foot(1)`, `xfce4-terminal(1)`, macOS `open(1)`/`osascript(1)`, and `xdg-open(1)` vary by distribution/version; this report cites primary vendor/project docs where fetched (kitty, wt.exe, iTerm2, WSL) and gives conservative CLI guidance — implementations must probe `--help`/man on target systems and treat terminal launch as best-effort with the mandated fallback.

---

## Verification appendix (second pass, 2026-11-09)

Repo facts re-verified directly against the working tree (`dev_WebUI` @ 696cbc6):

- **Release mechanics confirmed** (`scripts/create-release.sh`): `build_go_tui` cross-compiles `./cmd/oc-sandbox` from the repo checkout (`CGO_ENABLED=0`, 4-target matrix, `-trimpath`, `-X main.Version`), then an orphan `release-dist-<version>` branch strips everything but `dist/` and commits it — including `dist/bin/` (only ignored in the dev tree). Because web assets are embedded at `go build` time from repo-root sources, the released binaries carry the WebUI with **zero changes** to the staging model. The draft's section 1.3 stands as written.
- **Toolchain nuance the draft missed**: `go.mod` declares `go 1.24.0` (the release help text's "Go 1.22+" floor is stale vs the module). `net/http.CrossOriginProtection` exists only from Go 1.25 — adopt it as **optional hardening gated on the installed toolchain**, not as a design dependency; the token-per-request + Host/Origin checks are the load-bearing layer and are all Go 1.22+.
- **taskmgr mechanics confirmed** (`internal/taskmgr/taskmgr.go`): ops run with an `emit func(string)` callback; emit → `LineEvent` structs on an internal `chan tea.Msg`, one queue slot at a time per project, completion as `DoneMsg` carrying the full `Lines` slice. This is a line-oriented, one-producer transport that SSE can pump out verbatim (Go-side: tee the emit stream into the SSE handler's history buffer + live flush; SSE `id:` = line serial → reconnect replays from history).
- **Synergy note**: ticket #136's design mockup is plain static HTML — it can seed the repo-root `web/` tree of this recommendation directly (Standard/Advanced renders included), so no frontend rewrite between prototype and product under the no-node stack.
