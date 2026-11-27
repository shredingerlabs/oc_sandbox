# Lazy Next-Touch Migration to Toolchain Sets

No migration pass. Reading a legacy `container_edition` expands it via the manifest bundle map forever (`base` → ∅; chat/pkm map to their base-only bundles); the first settings write materializes `toolchains` and omits `container_edition` — that write is the whole migration. Unknown edition names — former `Dockerfile.custom` stages, catalog-removed editions, corruption — enter an **unresolved state**: start refuses with a visible reason and a settings pointer ("stored edition 'X' maps to no bundle — choose toolchains"); picking a set in settings resolves it; nothing is ever destroyed on the way. Old `opencode-sandbox-<edition>` images (the old base included) become delete-only **Legacy rows** on the Images screen (in-use pinned, group hides when empty); running containers keep their images; and the three-way mismatch prompt survives, re-keyed to set identity — the body shows the member delta, and Switch builds the replacement before anything stops, turning "never stop without a replacement" into "build the replacement, then stop".

## Considered Options

- **ADR-0032-style startup migration pass**: touches every workspace file to fix zero behaviors; read-side support is needed regardless (imports, restored backups), so the pass buys nothing. Rejected.
- **Silent ∅ on unknown editions**: surprise tool loss. Rejected; the unresolved state refuses visibly.
- **Fixed base tag under content-addressed components**: a stale old base under the fixed tag would make the cached marker lie (● on old bytes) while components build `FROM` old content — hence `opencode-sandbox-base:<hash12>`.

## Consequences

- **Single cutover**: the update that ships `dist/toolchains/` and `build-images.sh` deletes `build-container.sh`, its test, and the edition stages in the same release — no transition period carrying both.
- **Update-restart window, accepted**: the updater never restarts the binary, so between install and the manual restart the old binary's stage grep errors on the missing `dist/Dockerfile`; starts/settings refuse with running containers unaffected. Release notes carry "restart after update".
- **No auto-build at update**: recipe rewrites re-address hashes, cached markers flip ○, and the next start's build prompt carries the refresh (refresh is explicit). The dual runs instead: an image GC pass at update completion (plus one after recipe-editor saves) removes family-tagged images whose hash is no longer derivable and have zero container refs, plus the same host-wide dangling sweep as the clean-intermediates op.
- **`Dockerfile.custom` is never touched** and stops shipping; when it holds an uncommented stage, Images/settings render a porting hint (each stage's RUN/ENV/USER lines → `toolchains/custom/<id>.Dockerfile`, one toolchain per file) with the porting doc in `docs/` linked from the release notes. The stale root `Dockerfile` joins install.sh's stale-file removal.
- **The update preserve list is rewritten to root-level flattened runtime paths**: `toolchains/custom/` and `toolchains/snapshots/` always-keep (never prompted), `proxy/allowlist.txt` prompted default-keep, `searxng/settings.yml` preserved — fixing a live bug where flattened installs' settings.yml edits were clobbered; the install tests follow the rewrite.
- **All shipped user-facing strings are English from this release** (install prompts, script help/comments, UI hints, release notes); preserved legacy user files stay untouched as-is.

*Rationale: wayfinder map — "migration and update flow for toolchain sets" (#184), "GC policy and the clean-intermediates op" (#187), "version editor for toolchain recipes" (#191).*
