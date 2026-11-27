# Cutover acceptance — toolchain sets (#215)

The single-cutover ticket of the toolchain-sets spec (§10): the stage world
dies in one change set and the toolchains world ships as one update. This
note records what was verified at the cutover implement-acceptance and what
remains for the tier-2 rig.

## Cutover state (this change set)

Deleted: `dist/scripts/build-container.sh`, `tests/test-build-container.sh`,
the stage `Dockerfile` (repo copy + install.sh stale-file removal — root and
`dist/` variants), `dist/Dockerfile.custom` shipping (the file on user hosts
is never touched — the §10 porting warning reads it), the per-edition SVG
assets and the `editions` hue/label map (earlier tickets; absence pinned),
`applyEditionPreset`, and the old discovery seam functions (`Editions`,
`SelectableEditions`, the Dockerfile stage grep, `EditionFromImage`,
`ImageExists`, `ImageInUse`). CACHEBUST handling died with the stage
Dockerfile and the builder shim — pins + content addressing replaced busting,
no shim; the lint refusal of `ARG` (the "CACHEBUST lesson") stays as the
guard. The shared infra images (`oc-proxy`, `oc-searxng`, `oc-stt`) now build
directly via podman — the exact vectors `start.sh`'s lazy member-start builds
run — from the Images screen rows, the proxy screen's rebuild, and start.sh.

Shipped: `dist/toolchains/` + `dist/scripts/build-images.sh` + the WebUI/Go
changes as one update; no transition period carries both worlds. English-only
completed for the remaining legacy strings (install.sh prompts + help,
uninstall.sh prompts + help, keepNote).

## Repo suite at cutover

- `go test ./...` — green (discovery stage-grep tests removed with their
  functions; the infra-build pins re-keyed to the direct podman vectors;
  smoke harness re-keyed off the deleted dist files).
- Full bash suite green, with `tests/test-install.sh` re-keyed (stale-file
  sweep pins `scripts/build-container.sh` + `Dockerfile` + `dist/Dockerfile`
  removal and Dockerfile.custom never-touched; the install fixture validates
  the new required-file list incl. `toolchains/base.Dockerfile` +
  `toolchains/manifest.json`) and `tests/test-uninstall.sh` re-keyed to the
  English uninstall copy.
- `tests/test-build-images.sh` + `tests/test-start.sh` replace the dead
  `tests/test-build-container.sh` as the builder's tier-1 coverage.

## Tier-2 rig run — pending (no podman in the implement environment)

The implement environment for #215 has no podman (like #202's). The runbook
(`docs/verification/README.md`, tier-2 `tests/verify-set-image.sh`) stands;
record the first rig run there — build the representative family
(`--base`, `--tc cpp,rust,java,go`, `--set`), run
`tests/verify-set-image.sh <set-tag> cpp,rust,java,go --full-vs <second
build of the same members>` (full-canonical vs incremental equivalence), and
note the per-phase timings. Per the spec §12, no thresholds: timings print
unasserted.
