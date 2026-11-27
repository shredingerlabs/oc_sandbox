# Toolchain sets — the implementable spec

**Status: decided design, not code.** Every rule below is closed; the rationale, measurements, and rejected alternatives live in the wayfinder tickets cited per section — zoom them on demand, never re-litigate here. If anything looks unresolved during implementation, it goes back to its owning ticket, never silently assumed. Decisions are recorded in ADR 0042 (the model), ADR 0043 (composition and caching), ADR 0044 (migration); the working vocabulary is `GLOSSARY.md` → "Containers, images & toolchain sets".

**Destination** (map "Wayfinder map — toolchain sets", #176): replace the stage-registry edition system end to end — editions become named bundles over fine-grained, user-extensible toolchain recipes; each workspace's image is composed content-addressed from its set at build time; the caching target is *adding an already-built toolchain costs seconds; a never-built toolchain costs one build, reused by every workspace thereafter*. Chat/pkm/workspace-kind machinery is untouched. Done = this spec implemented via `/implement`.

**Out of scope** (map): per-workspace toolchain *version* pinning (recipes carry one pinned version; bumps are global via the update flow); remote/multi-machine image or cache sharing incl. CI pre-built distribution; project-local dependency management (venvs/node_modules).

---

## 1. Filesystem artifacts

```
dist/toolchains/
├── base.Dockerfile        # full Dockerfile: pinned FROM ubuntu:24.04 inline (no ARG), same pin lint,
│                          # ends with dev-user creation + USER dev + WORKDIR, no ENTRYPOINT.
│                          # Fixed, not a unit; its content hash feeds every identity below.
├── manifest.json          # { "bundles": { "<name>": [ids…] } } — bundles only. No version fields,
│                          # no toolchain list (recipe files are the registry). Unordered member lists.
├── recipes/               # shipped recipes, <id>.Dockerfile — wholesale-overwritten on update
├── custom/               # user recipes, <id>.Dockerfile — always-keep on update; recreated from a
│                          # commented English example template when absent
└── snapshots/             # created lazily by the recipe editor, never shipped, never-discovered, always-keep
dist/scripts/build-images.sh   # NEW — the builder (§7). build-container.sh is DELETED at cutover.
dist/scripts/start.sh          # flags re-keyed (§8); still never builds
dist/Dockerfile                # DELETED at cutover (stages die); install.sh removes the stale root file
dist/Dockerfile.custom         # stops shipping; the file on user hosts is never touched (§10)
```

Rationale: #182 (layout, naming, collisions), #184 (disposition, preserve list), #181 (base composition).

## 2. Recipe grammar and lint

One file per toolchain; parseable as Dockerfile text. Allowed: `RUN`, `ENV`, `USER`, comments, `# extra: <absolute path>` lines (one per line, collected into the dump's extras input). Everything else is a **lint error**: `ARG` (bakes out-of-band state invisible to the content hash — the CACHEBUST lesson), `COPY`, `WORKDIR`, `ENTRYPOINT` (the composition footer owns it), unknown directives.

- **User switching**: monotonic root→dev; a switch-back is a lint error. The generator injects `USER root` before the fragment and again before the dump step, so dev-ending fragments are safe.
- **ENV**: literal values only; `ENV PATH` is a lint error; direct writes to `~/.profile`/`~/.bashrc` are lint errors. PATH additions ride base's `~/.profile.d/*.sh` hook — fragments write per-id files with unique names (rust: `rustup --no-modify-path` + `~/.profile.d/rust.sh`; ros2's source line → `~/.profile.d/ros2.sh`). Base's dev skeleton gains the sourcing hook.
- **Pins, enforced by the same lint** (run at discovery render, at build, and at editor save): pip `==x.y.z`; npm `@x.y.z`; direct URLs embed the version (no `latest` segments, no release-API queries); rustup `--default-toolchain <ver>`. apt is exempt — its pin is the suite. Today's floating installs (opencode latest-channel, glab release-API query, CBM `releases/latest`, unpinned pip) become pinned; new versions arrive only via update-flow rewrites.
- **`base` is a reserved id**; duplicate ids are lint errors.
- **Discovery** (the stage-grep successor): list + parse + lint of `recipes/` + `custom/` + manifest read, live at each render. Invalid recipes render visibly-with-reason (silent skip rejected); a member id without a recipe invalidates the bundle with a warning; a manifest parse failure invalidates all bundles with a warning; a user/shipped id collision skips the user recipe with a warning — shipped wins.
- Root-side manual writes under `/etc` stay author-declared extras (ros2 declares its `sources.list.d` entry + keyring); `/home/dev` is auto-captured by the dump, no declaration needed.

Rationale: #182 (grammar, pins, discovery), #189 (lint-refusal cases as tests).

## 3. The catalog and the bundles

21 shipped units (ids `[a-z0-9-]`); runtime content = unit, build-only deps = transient recipe steps.

| unit | content boundary |
|---|---|
| `cpp` | build-essential, cmake, ninja-build, pkg-config, python3-dev, autoconf automake libtool, clang clangd lldb gdb, libgtest-dev catch2 |
| `rust` | rustup stable, minimal profile, installed as dev (`~/.cargo`) |
| `java` | openjdk-21-jdk, maven |
| `go` | apt golang-go — shared: swdev + embedded |
| `node-dev` | npm globals: typescript, jest, pnpm, corepack, better-sqlite3 (node runtime stays in base) |
| `playwright` | npm playwright, `playwright install-deps`, chromium + firefox, `PLAYWRIGHT_*` ENVs |
| `arm-embedded` | gcc-arm-none-eabi, gdb-multiarch |
| `arduino` | arduino-cli, cores avr + esp32, simavr |
| `platformio` | pip platformio |
| `micropython` | pip mpremote, esptool |
| `picoscope` | PicoTech apt repo + libps2000/libps2000a/libpicoipp debs, pip picoscope picosdk (transient gnupg) |
| `pyvisa` | pip pyvisa, pyvisa-py, pyusb |
| `usb` | apt usbutils, libusb-1.0-0 — shared: hardware units |
| `ros2` | ROS apt repo + ros-jazzy-ros-base, rviz2, rmw-cyclonedds-cpp, rosdep, colcon, vcstool, bashrc source line (transient gnupg) |
| `octave` | apt octave |
| `python-science` | pip numpy, scipy, matplotlib, pandas, openpyxl, pillow — shared: matlab + latex |
| `latex` | latexmk, texlive collections (recommended, extra, science, bibtex-extra, publishers, xetex, luatex, pictures, plain-generic, extra-utils, lang-european, fonts-recommended/extra), biber, chktex, latexdiff, dvipng, python3-pygments (minted), tex-gyre, lmodern |
| `pdf-tools` | ghostscript, poppler-utils |
| `pandoc` | apt pandoc — shared: writing + latex |
| `fonts` | fonts-liberation, fonts-noto-core — shared: writing + latex |
| `libreoffice` | apt libreoffice |

**Base keeps (fixed, not a unit)**: ca-certificates, curl, wget, git, python3, python3-pip, python3-venv, jq, unzip, openssh-client, pulseaudio-utils, gh; **pinned** glab / opencode / codebase-memory-mcp; pip pytest pytest-cov; node runtime + npm + mcp-searxng; dev user + safe.directory; the `GLAB_CONFIG_DIR`/`GH_CONFIG_DIR` ENVs (moved web → base). Base purge: compilers → `cpp`; usbutils/libusb → `usb`; gnupg → transient recipe deps. Accepted consequence: only sets containing `cpp` have native compilers — shipped units install wheels/prebuilts, and a *runtime* pip/npm build needing a compiler fails on non-cpp sets until `cpp` is added.

**Edition bundles** (also the legacy migration mapping):

| edition → bundle | members |
|---|---|
| web | node-dev, playwright |
| embedded | go, arm-embedded, arduino, platformio, micropython, picoscope, pyvisa, usb |
| swdev | cpp, rust, java, go |
| matlab | octave, python-science |
| ros2 | ros2 |
| writing | libreoffice, pandoc, fonts |
| latex | latex, pdf-tools, pandoc, fonts, python-science |
| chat | ∅ (base only) |
| pkm | ∅ (base only) |

**HIL hardware units** (the multi-select's preset rule, re-evaluated on every pick): `arm-embedded`, `arduino`, `platformio`, `micropython`, `picoscope`, `pyvisa`, `usb` (the embedded bundle minus `go`).

Rationale: #181 (catalog, base purge, bundles), #183 pt 11 (HIL rule).

## 4. Component build and the dump step

The generated component Dockerfile: `FROM opencode-sandbox-base:<hash12>` → `USER root` → the recipe fragment → re-injected `USER root` → the **dump step** (a shared helper script, injected — never fragment boilerplate):

- **Payload archive** = `dpkg -L` of packages newly installed by the fragment (status diff against a pre-fragment snapshot) ∪ pip RECORD paths ∪ the fragment's declared `# extra:` paths ∪ the whole of `/home/dev`. Tar requirements surfaced by the rig: `--no-recursion` (dpkg -L lists dirs; base-files owns `/`) and an existence filter (dpkg -L lists path-excluded docs/man/locale files).
- **PM-state dump** = the dpkg status file + `/var/lib/dpkg/info` entries, for the stanza-union merge.
- **Label `tc.pkgs`** = that member's normalized dpkg + pip package names (the contest-check input, §5).
- **Tag** `opencode-sandbox-tc-<id>:<hash12>`, hash over the recipe content + the base recipe hash (a base bump re-addresses every component).

Build invocation: `--network host` (status quo; user-authored recipes already run on the user's own host — no new trust boundary), `RUN --mount=type=cache` allowed. Component image size ≈ base + extracted payload + compressed archive (the archive stays inside: assemblies COPY it back out; budget ≈ base + 2× payload). Component builds never traverse squid — no allowlist entries ever.

## 5. Set assembly

**Full canonical** (the sole semantics): the generated set Dockerfile `FROM opencode-sandbox-base:<hash12>`, `COPY --from=opencode-sandbox-tc-<id>:<hash12>` each member's payload archive and PM dump in **alphabetical order by id**, then **one RUN**: extract in canonical order, merge PM state (dpkg stanza-union, package stanzas deduped by name, info entries unioned, last canonical writer wins a contested package — status matches disk because extraction runs in the same order), re-emit fragment ENVs (duplicate keys: alphabetical-last wins), run `ldconfig`, remove the staging tree. The stanza-union merge core is one Python file COPY'd into the image (and executed directly by the test suite against fixtures). Footer uniform `USER dev` + `ENTRYPOINT ["/bin/bash","-l"]`. **Always `--network=none`** — structural honesty: no recipe or merge step can phone home; fully-cached plans are offline-capable by construction.

**Incremental delta assembly** (an optimization, never a second semantics — set tags are shared across workspaces, so history-dependence is rejected): build `FROM` the parent set image, COPY only the new members' archives, one RUN extracting them (applied alphabetically — multi-add included) + PM merge (parent status ∪ new dumps). Runs **iff**:

1. a **valid parent** exists — a set image whose members (by component hash) form a strict subset of the new set's members, same base/component versions; and
2. the **contest check** passes — new members' dpkg/pip names ∩ the parent's merged union = ∅, read host-side via `podman image inspect` from the `tc.pkgs` component label and the merged-union set label (ms-scale; never a container start).

Everything else is full canonical: fresh sets, member drops, base bumps/recipe changes, deleted parents, pre-label parents (honest full, self-healing), any contest. **Alphabetical-last-wins stays the only contested resolution.** A sanity-gate failure on an incremental assembly **auto-falls-back to full canonical** with one honest log line — that fallback is the repair path; full's own gate failure is an ordinary build failure. Identity is content alone (no parent hash in it); an optional non-normative provenance label (mode + parent tag) serves diagnostics.

Measured at the real 1.66 GB base (rig `prototype/composition-rig-186`): incremental add-one 5.0 s + 0.5 s component; full canonical assembly 25–45 s (accepted for fresh sets); never-built component one build 73–179 s, reused by every workspace thereafter.

## 6. Identity, tags, labels

- **hash12** = first 12 hex of the hash over sorted (toolchain id, recipe content hash) pairs + the base recipe content hash. Recipe *content*, not version fields — any edit re-addresses automatically. Component hashes additionally include the base recipe hash; base tag = hash of `base.Dockerfile` content. Derived in bash (`build-images.sh`, `start.sh`) and Go (WebUI) — **one checked-in golden-vector fixture (fragment set + expected hash12 + expected tag) is consumed by BOTH the bash test and a Go test**, pinning the implementations to each other; unordered member input must yield the identical hash; a base-recipe content change must re-address. The exact serialization is pinned by the golden vectors, not prose.
- **Tags**: `opencode-sandbox-base:<hash12>`, `opencode-sandbox-tc-<id>:<hash12>`, `opencode-sandbox-set:<hash12>`.
- **Labels**: `kind`; the sorted member list and member recipe hashes (the WebUI enumerates set rows by label, never tag parsing); `tc.pkgs` on components, the merged union on sets; optional non-normative provenance.
- Stored config keeps plain ids; the hash is always re-derived. Same members + changed recipe content → new tag → the mismatch prompt fires (honest: the image really differs).

## 7. `build-images.sh` — the builder's contract

Flags: `--base`; `--tc <id>[,<id>…]`; `--set <ids>` (assembly — the path every start flow calls); `--all` (base + every discovered component — what Build all invokes). Ids validated against live discovery; unknown/invalid id → refuse with visible reason; an invalid fragment refuses the build. All user-facing strings English (help, comments, messages — binding).

**Preflight, before any podman call, every invocation that would build**:

1. **Podman floor**: require podman ≥ 4.0 (cache mounts; effectively Ubuntu 24.04 / Debian 12) — below, refuse with an update hint; no silent slow path.
2. **Disk**: estimate what the op will build (missing components only — lazy build; same-id existing image size where one exists, else the budget formula with the largest known payload on host, floored at 1 GB; sets via the formula) and **refuse-with-reason when free < estimate + 20 % headroom**, the reason naming both numbers and pointing at clean intermediates. Physical numbers only (footer free space, reclaimable) — row sizes are logical and never summed.
3. **Network** (skipped for assembly-only plans): native-Go-side probe honored by the script seam — one HEAD per distinct source host of the plan's download needs, host-proxy-env aware, short timeout; offline Build-now for a download-needing plan (any uncached component or a missing base image) refuses naming the uncached units, the unreachable hosts, and the escape ("connect and retry, or Build later"). Fully-cached plans never refuse.

**Determinism**: two renders from identical inputs must be byte-identical (self-comparison; no byte-golden files — the golden vectors pin content identities). Podman-arg pins (mock-tested): `--network=none` on every assembly invocation (full and incremental), `--network host` on component/base builds, incremental FROM-parent-set generation, cached-component skip when the tag exists.

**Egress list** (build-time only; squid never sees builds; snapshot for restricted-host admins): apt mirrors (archive.ubuntu.com, security.ubuntu.com), gitlab.com, opencode.ai, github.com, objects.githubusercontent.com, pypi.org, files.pythonhosted.org, deb.nodesource.com, registry.npmjs.org, the playwright browser CDN, labs.picotech.com, downloads.arduino.cc, sh.rustup.rs, static.rust-lang.org, raw.githubusercontent.com, packages.ros.org, docker.io (base image pull); infra-only (outside build-images.sh): dl-cdn.alpinelinux.org, huggingface.co. Built-in construct→host mapping: apt → distro mirror, pip → PyPI, npm → registry.npmjs.org, rustup → static.rust-lang.org, versioned URL → its host, missing base image → docker.io. Exotic implicit sources take the false-green fallback (a false green is recoverable; a false red blocks work). No new fragment syntax.

## 8. `start.sh`

- `--toolchains "go,playwright"` — comma-separated, order-free, ids validated against live discovery; unknown id → refuse with visible reason. `--toolchains ""` = base-only set (how chat/pkm start).
- `--edition <name>` kept as an alias that expands via the manifest bundle at arg parsing; `--edition` + `--toolchains` together → refuse (explicit conflict beats silent precedence). One of the two is required.
- The container runs the derived `opencode-sandbox-set:<hash12>` image (hash derivation shared/pinned with the builder — golden vectors).
- **Still no build inside start.sh** — builds stay op/CLI-side. A missing set image refuses with a visible reason naming the build command (`build-images.sh --set <ids>`); the WebUI paths intercept earlier with the start-time build prompt (§9, WP-6).
- `StartArgs` emits sorted ids. Modes, proxy, STT, search-mode, start-option flags unchanged.

## 9. Go / WebUI work packages

Task-manager keys unchanged throughout (workspace container-name key; create-chain name key; shared `imagesKey`; exclusive keys); refusals per key unchanged; SSE re-render points unchanged (`line` → card/busy/create panes, `workspace` re-render, images fragment re-render, draft-replay extended to the new rows/chips).

**WP-1 Discovery seam** — `internal/discovery/discovery.go` (+ wiring in `cmd/oc-sandbox/main.go`): manifest/recipe discovery (list + parse + lint, §2) replaces stage grep; `SelectableEditions` → discovered units + bundles; `EditionFromImage` → set-tag/ref parsing (`opencode-sandbox-set:` / `tc-` / `base:` prefixes); `ImageExists` → per-tag existence; `ParentStages` deleted; `BaseImageRef` stays (base row); deps re-keyed from `dist/Dockerfile`/`Dockerfile.custom` to the toolchains dir.

**WP-2 Config seam** — `internal/sandboxcfg/sandboxcfg.go`: `Config.ContainerEdition` → `Toolchains []string`. Read path: absent `toolchains` = legacy → expand `container_edition` via the bundle map (`base`→∅; unknown → unresolved state §10); the legacy value is never written back. First settings write materializes `toolchains` and omits `container_edition` — that write is the migration. `Create`'s first entry and `Settings`/`Rewrite` payloads re-keyed.

**WP-3 New Workspace wizard** — `internal/webserver/create.go` + `web/js/main.js`: bundle row (Standard-visible, today's Edition slot, chip control) listing the 9 bundles, chat/pkm rendered "base only"; a bundle with a missing member recipe renders its reason and is not selectable; selecting a bundle overwrites the multi-select, editing the multi-select flips the row to Custom (never auto-back). New Advanced-only toolchain multi-select under it: flat alphabetical checkbox list over discovered units, each entry carrying the live cached marker, header "n of m selected"; invalid units render disabled with their reason. Default preselect = the swdev bundle; leaving pkm kind restores the *prior* selection (today's hard-code-back dies). pkm pin: rows hidden + `toolchains: []` forced; un-pin restores. HIL preset: ON iff the selection contains any hardware unit (§3) — a preset, never a pin. Build-plan row re-keyed to set-tag existence with the honest cost line (counts, no time estimates); create-confirm popup lists "image build: <members summary>"; the create chain's build step calls `build-images.sh` (missing components + assembly) under the workspace-name op key. `applyEditionPreset` deleted with its edition rule.

**WP-4 Workspace Settings / revisit** — `internal/webserver/wssettings.go`: bundle row + multi-select prefilled from the stored set (derived bundle shown; Custom read-only in Standard with an Advanced hint); edition validation → toolchain-id validation ("unknown toolchain — choose a listed one"); the `Rewrite` payload materializes `toolchains`; image-missing hint unchanged; pkm still not pinned here (both-direction switches stand); unresolved state renders the warning with nothing preselected.

**WP-5 Identity across surfaces** — `internal/webserver/workspaces.go`, `containers.go`, `proxyscreen.go`, `counts.go`, `import.go` + `internal/importcheck`: label = derived bundle name on exact member-match, else "Custom"; hue from the matching bundle, else the neutral accent; one generic glyph for all sets — `web/img/editions/*.svg` and the `editions` hue/label map deleted; member list as subtitle/tooltip ("n toolchains: …"). Sidebar count pill re-keys to the sandbox-family image count (base/components/sets; infra and Legacy never counted — mirrors the Images screen's family rows). Import: validation accepts legacy `container_edition` and new `toolchains`; summary shows the derived bundle/set.

**WP-6 Start paths** — `internal/webserver/wsops.go`, `internal/openworkspace/start.go`, `internal/setupflow/stages.go`: reuse-running and mismatch detection re-keyed to the derived set tag vs the running container's image ref; the three-way prompt body shows the member delta (added X / removed Y; same members new hash → "toolchain recipes updated"; old-world ref → "this container predates toolchain sets" + bundle-map-expanded delta, unknown name → "members unknown — Switch rebuilds from your set"). The start-time **Build now / Build later / Go back** sidecar returns on every start path when the set image is missing (Open Workspace card buttons, Containers-screen start, setup retry) — Build now is one queued op (missing components + assembly) streamed into the card/busy pane, then the start continues; Build later stays stopped with the Images hint; offline hosts get the preflight refusal (§7) instead. The mismatch **Switch** and **restart preflight** use the same inline Build now — "never stop without a replacement" becomes "build the replacement, then stop"; a failed switch leaves the workspace stopped with the error in the busy pane; unresolved workspaces refuse before any prompt; registry flips verified-only as today.

**WP-7 Images screen** — `internal/webserver/images.go`: rows = base + components (union of discovered recipes and tagged component images: invalid recipe → row with reason, Build/Rebuild disabled, Delete enabled if an image exists; orphan image → "recipe missing"; collision → warning line on the shipped row) + set rows (label-enumerated, title = sorted member list, Delete only) + infra rows (oc-searxng/oc-stt on-screen; oc-proxy off-screen as today; lazy auto-build at member start keeps them out of Build all) + the Legacy group (§10). Cached markers on base/components, fresh per render. Build all → `build-images.sh --all`; row builds → `--tc`/`--base`. Deletes: base/components never in-use-pinned — the danger confirm carries the honest cost ("one rebuild when next needed"; base: "needed for every component/set build"); set/Legacy rows pin by container refs; **refusal-first for parent-having rows** — surface podman rmi's exit-2 with the honest reason ("build parent of newer images — shares their layers, holds no space of its own; remove the newer ones first"), no children preflight, no `--force`. Footer: host free space; **Clean intermediates** button with the reclaimable estimate ("~X GB reclaimable — removes dangling (untagged, unused) images from all projects on this machine"), danger-confirmed, host-wide dangling-only, one op on the images key.

**WP-8 Build preflight + network probe (Go side)** — the op flows (row build, Build all, create chain, mismatch/restart preflight) run the same preflight family as §7 before the op starts: free-space refuse-with-reason (estimate + 20 % headroom, reason names both numbers, points at clean intermediates) and the native-Go HTTP HEAD probe (per-plan source hosts via the construct→host mapping, `http.ProxyFromEnvironment`, short timeout, sync pre-op; skipped for assembly-only plans). Builds need no squid entries, ever.

**WP-9 Version editor** — `internal/webserver/images.go`, the proxy-editor pattern (`internal/webserver/proxyscreen.go:478-486` precedent): per-row **Edit recipe** on component and base rows, **AdvancedOnly** in the presentation matrix (`internal/webserver/matrix.go`); set rows get nothing. Save = the builder's same loud lint (pin enforcement rides it — an edit that un-pins is refused; pure-file parse, always available) → **atomic write, synchronous, no task key**; a **follow-up image GC pass** launches as its own op on the shared images key (skip-if-busy, never queued). Honest durability copy on the editor: shipped/base edits are ephemeral by design (update wholesale-overwrites — "re-applied your bump may be lost at the next update; fork to `custom/` for a durable copy"); custom edits durable ("kept across updates"); base rows add "a base bump re-addresses every image at next build". **Reset from shipped** per row, confirm-gated, enabled only when a snapshot exists and differs; the odd post-update case gets truthful copy ("restores the version from before your last edit — may be older than the latest update"); custom rows: no snapshot, no reset. First save snapshots the pristine file into `toolchains/snapshots/` (write-once; browsing creates nothing).

**WP-10 Migration read path + unresolved state** — everywhere config is read (start flows, settings render, import): legacy expansion (§10), unknown edition → the unresolved refusal before any prompt machinery, settings warning + pick-to-materialize.

## 10. Migration and cutover

- **Lazy next-touch, permanent read support**: no startup migration pass (the ADR-0032 precedent rejected — it would touch every file to fix zero behaviors and read support is needed regardless); the version marker stays `"2.0"`; the registry is untouched (stores no edition today). The first settings write materializes `toolchains` (whatever the writer — settings save, import). Both fields absent (corrupt/foreign) → unresolved handling.
- **Unresolved state** (unknown stored editions — Dockerfile.custom stages, catalog-removed, corruption): start refuses with a visible reason + settings pointer ("stored edition 'X' maps to no bundle — choose toolchains"); settings renders a warning, nothing preselected; picking a set materializes `toolchains`; nothing destroyed, the old image/container never touched. One refusal everywhere, before any prompt.
- **Old images → Legacy rows**: `opencode-sandbox-*` refs that are no current base/tc/set tag (old base included) render delete-only (danger confirm: "superseded by toolchain sets — delete freely"), in-use pinned, group hides when empty; unreferenced rows also sweep away with clean intermediates; never auto-deleted, never rendered as set rows.
- **Mismatch prompt survives the re-key** (WP-6); old-ref containers prompt on every touch (reuse-running matches set-tag refs only).
- **`Dockerfile.custom`**: never touched (stops shipping, so update leaves it by construction); when it exists with an uncommented stage, a warning row (Images + settings) renders the inline porting gist — each stage's `RUN`/`ENV`/`USER` lines → `toolchains/custom/<id>.Dockerfile`, one toolchain per file — self-contained (production serves dist/ only), with the porting doc in `docs/` linked from release notes. The old root `Dockerfile` joins install.sh's stale-file removal.
- **Mid-migration chain**: update lands → nothing auto-builds, nothing rewrites → touch → legacy read expands (unknown → refusal) → start-time Build prompt (first component build 73–179 s each; counts, no estimates) → old-ref prompt → the settings write materializes. The update-restart window is accepted: the updater never restarts the binary, so between install and manual restart the old binary's stage grep errors on the missing `dist/Dockerfile` and starts/settings refuse; running containers unaffected; no shim possible. Release notes carry "restart after update".
- **Single cutover**: one update ships `dist/toolchains/` + `build-images.sh` + the WebUI/Go changes and deletes `build-container.sh`, `tests/test-build-container.sh`, the edition stages, and the per-edition SVG assets — no transition period carrying both. CACHEBUST dies clean in the same cutover: pins + content addressing replace busting, no shim.

## 11. Update flow, GC, release notes

- **Preserve list rewritten to root-level flattened runtime paths** (`$target_dir/toolchains/custom/`, `$target_dir/toolchains/snapshots/` always-keep; `$target_dir/proxy/allowlist.txt` prompted default-keep; `$target_dir/searxng/settings.yml` preserved): today's `dist/`-prefixed checks miss real flattened installs and clobber user-edited searxng settings — a live bug fixed here; the install tests follow (`tests/test-install.sh` currently fakes a non-flattened source to pass). `recipes/`, `manifest.json`, `base.Dockerfile` wholesale-overwritten (pins arrive via these rewrites). keepNote updated to name what's kept.
- **No auto-build at update**; the dual runs: an **image GC pass at update completion** (inside the update op chain, "reclaimed ~X GB" summary line in the update log pane) — removes family-tagged images whose hash is no longer derivable from current recipes and have zero container refs (exit-2 child targets skipped: shared layers, zero reclaim lost), plus the host-wide dangling sweep. Abandoned sets (no stored workspace derives them, no container refs) die at the next pass; a workspace returning to a swept set pays one rebuild; a stopped-at-update workspace pays one full assembly at next start (no incremental parent). No grace period, no timestamps, no reaper. The recipe-editor save is the second trigger (WP-9); skip-if-busy, never queued. `toolchains/snapshots/` is a host dir — image GC never touches it by construction.
- **Every successful `podman rmi` auto-prunes the target's dangling parents** — row deletes and GC removals self-clean their chains.
- **Release-notes blocks** (all English): editions→sets overview; automatic migration on next settings save, nothing rebuilt until start/build, old images/containers untouched (Legacy rows); Dockerfile.custom porting recipe + doc link; first build after update is long, thereafter cached adds seconds; floating-latest death (pins arrive via updates); unknown-edition workspaces must pick toolchains; restart after update.
- **English-only switch** (binding, user directive): all shipped user-facing strings English from this release — install.sh prompts, keepNote, `build-images.sh` help/comments, `start.sh` messages, the `custom/` example template, UI hints, release notes. Preserved legacy user files stay untouched as-is (the old Dockerfile.custom keeps its German template comments). WebUI copy and docs are already English.

## 12. Tests

**Tier 1 — repo suite (bash + `go test`, podman mocked/faked, runs anywhere):**

- `tests/test-build-images.sh` (successor of `tests/test-build-container.sh`, same PATH-mock pattern — a podman that logs its args): flag contract (`--base`/`--tc`/`--set`/`--all`, comma lists, unknown/invalid id → refuse with reason); generation determinism (byte-identical re-render); canonical order (member COPY order + ENV re-emission alphabetical; unordered input renders identically to sorted); podman-arg pins (`--network=none` on every assembly, `--network host` on component/base, tag formats, enumeration labels, cached-component skip, incremental FROM-parent generation); preflight refusals (free-space, offline-for-download-plans-only, podman floor); lint-refusal propagation.
- **Merge-core fixture tests**: `dump.sh`/`assemble.sh` take a root (env var or arg, default `/`); the Python stanza-union core is its own file, COPY'd into images by the generated Dockerfiles and executed directly by the suite against fixture trees — dpkg status fixtures with contested stanzas (non-contested incremental, contest-forced-full, multi-add alphabetical, gate-failure fallback), pip RECORD fixtures, tarballs; the base-files `--no-recursion` trap and the existence filter as cases; extras union; home-capture + `~/.profile.d/` hook cases; ENV re-emission cases; every lint refusal case.
- **Golden vectors**: one checked-in fixture (fragment set + expected hash12 + expected tag) consumed by BOTH a bash test and a Go test; unordered-input and base-bump cases.
- **Go checks** (existing smoke/fake patterns, no new machinery): bundle derivation (exact member-match, Custom, chat/pkm ∅ mapping), migration read-path expansion + first-write materialization, mismatch member-delta body, Images row enumeration via fake-podman labels, cached-marker rendering, unresolved-state refusal. `start.sh` `--toolchains`/`--edition` aliasing and conflict refusal in the `tests/test-start.sh` pattern. Per-check detail is specced at implement time (per the test-strategy ticket).
- `tests/test-build-container.sh` dies in the same cutover commit as `build-container.sh`.

**Tier 2 — real-podman verification, manual, never a build flag:**

- `tests/verify-set-image.sh <set-tag> <tc-ids…>`: sanity gates parameterized by image + members — dpkg `--audit` empty; full `dpkg -l` keeps base's and every member's packages (expectations self-derived from the components' own dumps, no second expectations file); pip RECORD intact; binaries run from a repo-side spot-check table (id → invocations/imports) maintained alongside the catalog; `dpkg -L` per member package; full-vs-incremental file-list equivalence.
- Run at implement-acceptance (documented rig run under `docs/verification/`), after risky changes, on demand. Timing: no thresholds — per-phase timings printed unasserted; the seconds target's formal check is the rig measurement recorded in the runbook at acceptance (as `prototype/composition-rig-186` already did).

## 13. Provenance

| Spec section | Owning ticket(s) |
|---|---|
| Model, recipes, manifest, discovery, English-only | #182 (format rules), #181 (catalog), #184 pt 10 |
| Composition mechanism, PM merge, identity, floor, budget | #180, validated by #186; contested/incremental rule #190; cache semantics research #177 |
| Strategy choice (prefix-chain rejected, naive-merge rejected) | measured in #179; prior art #178 |
| Catalog + bundles + base purge | #181 |
| Wizard/settings/CLI/Images surface, start-time prompt, card identity | #183 |
| Migration, unresolved state, Legacy rows, cutover, preserve-list fix, release notes | #184 |
| GC policy, clean intermediates, disk honesty, refusal-first | #187 |
| Offline behavior, probe, egress list, `--network=none` | #188 |
| Version editor, snapshots, Reset from shipped | #191 |
| Test strategy, golden vectors, sanity gates | #189 |
