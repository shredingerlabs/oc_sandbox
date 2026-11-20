# PROTOTYPE — delta assembly at a realistic base size: payload-archive mechanism, measured

**Throwaway prototype for wayfinder ticket ["Prototype: re-measure delta assembly at a realistic base size"](https://github.com/shredingerlabs/oc_sandbox/issues/186). Not production code. Never merged.**

## Question

The mechanism decision (S2b payload-archive assembly, ticket #180) rests on
three *derived* claims the toy rig never measured at real scale:

1. **Seconds at real scale.** With a ~1 GB base (the real `opencode-sandbox-base`
   size), does add-one to a real-sized set stay seconds?
2. **PM-merge coherence.** Do the canonical stanza-union sanity checks pass on
   assembled sets (the gcc case naive merging lost)?
3. **Sizes vs the budget formula.** Set ≈ base + Σ payloads; component ≈ base + 2× payload.

## What the rig implements

The **decided mechanism**, end to end, as pure Dockerfile/buildah primitives:

- **Component** = base + recipe fragment + injected dump step
  (`scripts/dump.sh`): a payload archive = `dpkg -L` of newly installed
  packages ∪ pip RECORD paths ∪ fragment-declared extras, plus a PM-state dump
  (full dpkg status + `/var/lib/dpkg/info` entries). "Newly installed" =
  status diff against a pre-fragment snapshot the generated Dockerfile takes.
- **Set assembly** = COPY each member's dump output (payload bytes only, never
  `/`) + **one RUN** (`scripts/assemble.sh`): extract in canonical order, merge
  dpkg status as canonical stanza-union (last canonical writer wins), overlay
  PM info entries the same way, `ldconfig`, `rm -rf /rig`.
- Fragment `ENV` lines are re-emitted in the set Dockerfile (archives carry
  files only).

Canonical order = alphabetical by toolchain id: `tc-cpp, tc-go,
tc-playwright, tc-pysci`.

| Member | Stands in for | Install style | Content |
|---|---|---|---|
| `tc-cpp` | `cpp` | apt | clang, clangd, lldb, gdb, libgtest-dev, catch2 |
| `tc-go` | `go` | apt | golang-go |
| `tc-playwright` | `playwright` | npm + apt | playwright@1.49.1 + chromium (+ install-deps apt set) — needs declared extras |
| `tc-pysci` | python-scientific | pip | numpy/scipy/matplotlib/pandas/openpyxl/pillow, pinned |

Two sets: `set1 = {tc-go, tc-playwright, tc-pysci}`; `set2 = set1 + tc-cpp`
(**added at the front** of the canonical order — the worst case the toy rig
measured for prefix chains, kept on purpose).

## Measurements

| Phase | What it answers |
|---|---|
| M3 (runs **first** so cold numbers stay uncontaminated) | set1 components cold + set1 assembly cold; then headline add-one: build `tc-cpp` (never built) + assemble set2 |
| M2 warm | re-assemble set2 + one component rebuild with everything cached |
| M4 sizes | image sizes + payload archive sizes → budget-formula check |
| M5 sanity (`sanity.sh`) | dpkg --audit, full dpkg -l (members AND base gcc), pip imports, binaries run |

## How to run

```sh
cd prototype-composition-rig/real
./bench.sh     # whole matrix, prints the table; full log in results.log
./sanity.sh    # M5 pass/fail per set image
```

Base selection: `BASE_IMG` env wins. If unset, the bench **reuses the runner's
real `opencode-sandbox-base` image when present** (max fidelity), else builds
`real/base/Dockerfile` (a mirror of the production base stage). Optional extras
file per toolchain: `toolchains/<tc>.extras` (absolute paths per line).

## Environment caveats (fidelity notes)

- Authored inside an agent sandbox (no podman); must be run on a real
  rootless-podman desktop, podman ≥ 4.3 — same as the toy rig (`../`).
- `RUN --mount=type=cache` for apt/pip; `--network=host` for all builds —
  same conventions as the toy rig.
- Deviations from the production base stage: CBM-mcp dropped (flake risk,
  ~30 MB), dev-user/ENTRYPOINT stage omitted (rig runs as root; the toy rig
  did the same), glab/opencode URLs float like production (no CACHEBUST — the
  rig has no cache-bust story).
- Mechanism scope notes: the dump spec covers *newly installed* packages — a
  fragment that **upgrades** a base package's files is not covered (the
  decided mechanism's open edge; fragments here avoid upgrades — if
  `playwright install-deps` accidentally upgrades base libs, that will show in
  sanity). `dpkg` diversions / update-alternatives are not exercised.
- Payload archives are gzip: conservative for both targets (slower extract,
  smaller COPY); compression choice is still open in the spec.
