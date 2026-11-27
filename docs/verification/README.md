# docs/verification/ — manual verification runbooks

Runbooks and sign-off records for the manual (real-hardware / real-podman)
verification tiers that never run in CI.

- `b-list-checklist.md` — WebUI parity checklist (spec #147).
- `dictation.md` — dictation checks.
- **Tier-2 toolchain-set verification** — see the runbook below (`tests/verify-set-image.sh`,
  spec `docs/specs/toolchain-sets.md` §12): real-podman sanity gates against a
  built set image, run at implement-acceptance, after risky changes, on demand.
- `toolchain-sets-cutover.md` — the #215 cutover acceptance note (what the
  single-cutover change set deleted/shipped and the pending rig-run record).

---

## Tier-2 runbook — `tests/verify-set-image.sh` (toolchain sets §12)

**Status: runbook shipped; the acceptance rig run itself is pending** — the
implement environment for ticket #202 had no podman. The run record below is
filled in the first time it executes on a podman machine.

### What it checks

Given `tests/verify-set-image.sh [--full-vs <other-set-tag>] <set-tag> [<tc-ids>…]`:

1. **dpkg `--audit`** is empty inside the set container.
2. **Full `dpkg -l`** keeps the base's and every member's packages — expectations
   self-derived from the components' own PM dumps (host-side `podman image mount`,
   never a container start), no second expectations file. The given tag must be
   the one derived from the members' current recipes (content identity), and the
   image's `members` label must list exactly the given ids.
3. **pip RECORD intact** — every installed dist-info RECORD's files exist; every
   member's pip packages resolve via `importlib.metadata`.
4. **Binaries run** per the spot-check table `dist/toolchains/spot-check.tsv`
   (id → invocations/imports, maintained alongside the catalog): every row for
   the base and the set's members executes inside the container (`bash -lc`,
   cwd `/home/dev` — the `~/.profile.d` hooks apply, so `cargo`/`ros2` work).
5. **`dpkg -L`** succeeds for every member dpkg package (a package listing no
   existing path warns — the merged info lists are fuller than the
   existence-filtered payload archive).
6. With `--full-vs`, **file-list equivalence** between two images of the same
   members built the other way (full canonical vs incremental): compared
   order-insensitively, volatile paths ignored.

Per-phase timings are printed unasserted — no thresholds anywhere; the seconds
target's formal check is the rig measurement recorded at acceptance.

### Running it

```bash
# 1. Build the family for a representative set (example: swdev members).
dist/scripts/build-images.sh --base
dist/scripts/build-images.sh --tc cpp,rust,java,go
dist/scripts/build-images.sh --set cpp,rust,java,go

# 2. Derive the set tag the same way the builder/start.sh do (first 12 hex of
#    the identity hash); simplest: read it from the builder's log line or:
tag="opencode-sandbox-set:$(bash -c \
  'source dist/scripts/lib-identity.sh
   tc_set_tag dist/toolchains/base.Dockerfile \
     cpp dist/toolchains/recipes/cpp.Dockerfile \
     rust dist/toolchains/recipes/rust.Dockerfile \
     java dist/toolchains/recipes/java.Dockerfile \
     go dist/toolchains/recipes/go.Dockerfile' | cut -d: -f2)"

# 3. Verify.
tests/verify-set-image.sh "$tag" cpp rust java go
```

Expected: `verify-set-image: PASS <tag>` with a `timing` line per phase.

### The full-vs-incremental equivalence run

Both assembly modes share one content tag, so compare by retagging: verify the
image as built (incremental, if a valid parent existed), keep a retagged copy,
remove the image and any candidate parent so the rebuild runs full canonical
under the same tag, then compare:

```bash
podman tag "$tag" verify-incr:tmp       # keep the incremental result
podman rmi "$tag"
# remove the parent set images so no valid parent exists and the rebuild is
# full canonical (content identity — the tag comes out identical)
podman images --filter label=kind=set --format '{{.Repository}}:{{.Tag}}' \
  | xargs -r podman rmi
dist/scripts/build-images.sh --set cpp,rust,java,go   # full canonical
tests/verify-set-image.sh --full-vs verify-incr:tmp "$tag" cpp rust java go
podman rmi verify-incr:tmp
```

The gate passes when the file lists match modulo volatile paths
(`/tmp`, `/run`, `/proc`, `/sys`, `/dev`, `/var/log`, `/var/cache/ldconfig`,
dpkg triggers/locks, `/etc/machine-id`).

### Acceptance run record (fill in on first podman run)

```
Host: <machine / distro / podman version>
Set:  <members>          tag: <tag>
[ ] preflight (identity, images, labels)        timing: ______
[ ] dpkg --audit empty                          timing: ______
[ ] dpkg -l keeps base + member packages        timing: ______
[ ] pip RECORD intact                           timing: ______
[ ] spot-check table rows ran (n = ____ )       timing: ______
[ ] dpkg -L per member package                  timing: ______
[ ] full-vs-incremental file-list equivalence   timing: ______
Result: PASS / FAIL (detail below)
```
