# Dockerfile Stage Names as the Container-Edition Registry

Supersedes the edition half of ADR 0008 (mode discovery stays help-text-based). Editions were previously hardwired in three places — Dockerfile stages, `build-container.sh` case branches, and the `start.sh` whitelist — with the TUI discovering them by parsing `build-container.sh --help`. Now the canonical registry is Dockerfile stage naming: every stage named `opencode-sandbox-<name>` (via `FROM ... AS`) in `dist/Dockerfile` or `dist/Dockerfile.custom` is a user-selectable edition; discovery greps those files live at each menu render. This lets users add their own editions by editing `Dockerfile.custom` without touching any script.

## Considered Options

- **Stage-name registry (chosen)**: the Dockerfile already *is* the build definition; one source of truth, no drift.
- **Separate manifest file** (`editions.json`): duplicates stage names and drifts from the build recipes.
- **Keep help-text parsing** (ADR 0008): editions can't be user-extended without editing script help text, which updates would overwrite.

## Consequences

- **Custom edition Dockerfile**: a separate `dist/Dockerfile.custom`, never overwritten on update, holds user editions as standalone recipes (typically `FROM opencode-sandbox-base`, ending `USER dev` + `ENTRYPOINT ["/bin/bash","-l"]`). A commented example template ships on install; if absent at update time it is created fresh.
- **`Dockerfile` itself is still overwritten on update** — user stages there do not survive; the custom file is the sanctioned home for them.
- **`build-container.sh` is generic**: any edition is one `podman build --target <stage>`; `all` builds every discovered stage; no explicit argument defaults to `all`. The `full` edition is no longer a special recipe or default.
- **`start.sh` requires an explicit `--edition`** and validates against discovered stage names (replacing the hardcoded whitelist); missing parent images for custom editions trigger an automatic `base` build, other missing parents fail loudly.
- **Collisions and invalid names**: a stage name appearing in both files is skipped with a warning; names not matching lowercase `[a-z0-9_-]` are skipped from menus and rejected by `start.sh`.
- **Disappearing editions**: `start.sh` fails listing available editions; the TUI change-settings wizard routes into re-selection.

## Amendment (2026-10-19): `base` is not project-selectable

The stage `opencode-sandbox-base` is the parent image every shipped edition builds `FROM` — infrastructure, not a user-facing edition. The Go TUI excludes it from every menu that selects a *project's* edition (New Project wizard, change-settings, re-selection) via a separate selectable-edition list (`SelectableEditions`, dropping the literal name `base`); raw stage discovery is unchanged everywhere else: the Container center Build Images tab must still offer `base` (custom editions need its image built first), and `build-container.sh`/`start.sh` keep accepting it. Projects already stored with `container_edition: "base"` remain valid — the filter only removes `base` from selectable alternatives, never invalidates a stored value. If `base` is the only discovered stage, the project menus show the existing no-editions error/state; the legacy bash `start-tui.sh` kept listing `base` (out of scope until the parity gate flipped the default, ADR 0020 — the bash TUI has since been deleted, #99, taking that exception with it).
