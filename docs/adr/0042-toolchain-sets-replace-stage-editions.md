# Toolchain Sets Replace Stage-Named Editions

The stage registry of ADR 0019 — an edition is a `FROM … AS opencode-sandbox-<name>` Dockerfile stage — is replaced by fine-grained toolchains: a **toolchain** is one installable unit (language/ecosystem/toolset boundary) with its own Dockerfile-fragment recipe; an **edition** becomes a named bundle of toolchain ids declared in `dist/toolchains/manifest.json` (bundles only — no version fields, no toolchain list; the recipe files themselves are the registry); and a workspace stores its own **toolchain set** as `toolchains: []` in sandbox_config.json — the only stored truth, semantically unordered (canonical order = alphabetical by id, derived at every use, never stored). Discovery greps nothing anymore: list + parse + lint of the recipe dirs + a manifest read, live at each render, with invalid recipes surfaced visibly-with-reason (ADR 0019's silent skip rejected). Stages cannot compose — a workspace wanting web+cpp had to pick a fat stage or hand-edit a Dockerfile; per-workspace sets over fine-grained units make the image match the workspace, and content-addressed composition (ADR 0043) keeps that cheap.

## Considered Options

- **Stage registry kept, more stages added** (chosen in ADR 0019): a stage is a build target, not a composable unit; per-combination stages are combinatorial, and the measured add-one costs (ADR 0043) ruled out generated per-set chains.
- **Manifest with per-recipe version fields**: a second, drift-prone identity — the recipe *content* hash is the identity; an edit re-addresses automatically.
- **`editions.json` registry file** (already rejected in ADR 0019): duplicates the build recipes and drifts; the same argument now applies to any toolchain list in the manifest.

## Consequences

- `container_edition` is never written again. Reads keep expanding legacy values via the bundle map forever; the first settings write materializes `toolchains` — that write is the whole migration (ADR 0044).
- `--edition` survives on start.sh as a bundle-expanding alias for the required `--toolchains`; passing both refuses (explicit conflict beats silent precedence).
- Versions are pinned everywhere, enforced by the builder's lint (pip `==`, npm `@`, versioned URLs with no `latest` segments, rustup `--default-toolchain`; apt exempt via the suite pin). The floating-latest installs die — opencode, glab, CBM, pip bases are pinned in `base.Dockerfile` — and new versions arrive only via update-flow file rewrites. CACHEBUST dies with them: content addressing replaces busting.
- Recipe grammar: exactly `RUN`/`ENV`/`USER` plus comments and `# extra: <absolute path>` lines (ARG/COPY/WORKDIR/ENTRYPOINT are lint errors; `ARG` would bake out-of-band state invisible to the content hash — the CACHEBUST lesson). User switching is monotonic root→dev; ENV values are literal-only and `ENV PATH` is forbidden — PATH rides base's `~/.profile.d/*.sh` hook, and direct `~/.profile`/`~/.bashrc` writes are lint errors.
- User units live in `dist/toolchains/custom/` (preserved on update, recreated from a commented template when absent); a user/shipped id collision → the user recipe is skipped with a warning, shipped wins. `base` is a reserved id. `toolchains/snapshots/` is never-discovered (ADR 0044 / recipe editor).
- Base follows the essentiality rule: compilers moved to `cpp`, usb tools to `usb`, gnupg to transient recipe deps — only sets containing `cpp` keep native compilers (accepted: shipped units install wheels/prebuilts; a runtime build needing a compiler fails on non-cpp sets until `cpp` is added).
- Mode discovery (the other half of ADR 0008) is untouched: modes still parse from start.sh help text.

*Rationale, catalog, and bundle member lists: wayfinder map — "toolchain catalog and edition bundles" (#181), "toolchain manifest and recipe format rules" (#182), "WebUI and CLI surface for toolchain sets" (#183).*
