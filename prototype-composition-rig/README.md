# PROTOTYPE — composition rig: prefix-chain vs component-merge, measured

**Throwaway prototype for wayfinder ticket ["Prototype: prefix-chain vs component-merge composition, measured"](https://github.com/shredingerlabs/oc_sandbox/issues/179). Not production code. Never merged.**

## Question

*Which composition strategy actually meets the measurable target on real podman — measured, not argued?*

Target (from the map): **adding an already-built toolchain to a workspace's set costs seconds; a never-built toolchain costs one build, reused by every workspace thereafter.**

Two candidate strategies:

- **(a) prefix-chain** — a generated per-set Dockerfile, one global canonical toolchain order, one layer per toolchain, plain `podman build`.
- **(b) component-merge** — each toolchain built once as a tagged component image, filesystem-merged into the set image with `COPY --from=<image>` (the mechanism the podman/buildah research validated as available since podman ≥ 4.0).

## The toy rig

Base: `ubuntu:24.04` + small apt set (`ca-certificates`, `curl`) + one pinned curl'd binary (`yq` v4.44.3).

Three toy toolchains, one per install style, mirroring the ticket:

| Toy name | Stands in for | Install style | Content |
|---|---|---|---|
| `tc-apt` | cpp | apt-only | `gcc`, `make`, `file` (pinned by archive state) |
| `tc-curl` | playwright | curl'd binary | `hadolint` v2.12.0 → `/usr/local/bin` |
| `tc-pip` | go | apt+pip mix | `python3`, `python3-pip` + `requests==2.31.0` |

Canonical order = `tc-apt, tc-curl, tc-pip` (the union order, like the ticket's
`cpp, go, playwright` example). Two sets:

- set1 = `{tc-curl, tc-pip}` (canonical order restricted to members)
- set2 = `{tc-apt, tc-curl, tc-pip}` (full canonical order)

set1 → set2 **adds a toolchain at the front** of the canonical order — the
realistic "user adds cpp to a workspace that already had go+playwright" case,
and the worst case for prefix chains. That is measured on purpose.

## Measurements

| Phase | Strategy (a) prefix-chain | Strategy (b) component-merge |
|---|---|---|
| M1 cold set2 | build set2 Dockerfile, nothing cached | build 3 components + merge |
| M2 warm rebuild set2 | rebuild, expect full cache hits | components cached + merge re-run |
| M3 **headline** add-one: set1 → set2 | build set1 (cold), then build set2 | set1 built its components; add = build `tc-apt` + merge |
| M3i bonus: add-one with *incremental* chain | set2's chain extends set1's order (`tc-curl, tc-pip, tc-apt`) instead of the global canonical order — tests whether dropping the global order rescues strategy (a) | n/a |
| M4 sizes | `podman image inspect` sizes + `podman system df -v` | same |
| M5 sanity | dpkg status coherent (`dpkg --audit` empty, `gcc` known to dpkg), pip metadata intact (`import requests`), `/usr/local` not clobbered (`yq`, `hadolint` run) | same |

All timings are wall-clock seconds from `bench.sh`; the raw log is
`results.log`, the table in `results.tsv`.

## How to run

```sh
cd prototype-composition-rig
./bench.sh          # runs the whole matrix, prints the table
./sanity.sh         # runs M5 checks, prints pass/fail per image
```

## Environment caveats (fidelity notes)

- The rig was authored inside an agent sandbox where container execution is
  seccomp-blocked (no chroot / pivot_root / writable cgroupfs), so **it must be
  run on a real podman host** — ideally a rootless-podman Linux desktop, which
  is exactly the product's target environment (podman ≥ 4.3: Ubuntu 24.04
  pocket / Debian 12 baseline, per the cache-semantics research).
- `RUN --mount=type=cache` mounts are used for apt archives and pip cache —
  this is the caching concept being validated for strategy (a).
- `--network=host` is used for all builds: network mode does not affect layer
  caching, only download speed, and is identical for both strategies.
