# Pod-per-project networking: companions share the pod's pasta namespace

## Status

Accepted (2026-10-23) — supersedes ADR 0028

## Context

ADR 0028 routed container→companion traffic through the host's
default-route gateway (`--add-host host.oc-sandbox.internal:<gw>`).
Production disproved it: on a VirtualBox guest, the default-route
gateway `10.0.2.2` is the *physical host's* localhost — a probe answered
with the physical host's OpenWebUI, not the sandbox's SearXNG. The
underlying pasta constraints remain unchanged (noble's passt snapshot
lacks the map options, podman 4.9 forces `--no-map-gw`, and podman 4.9
rejects multi-network setups: `cannot set multiple networks without
bridge network mode, selected mode pasta`).

The only topology verified to work on podman 4.9.3 is a **podman pod**
(`pod create --network pasta:--ipv4-only` + member runs, POD-PASTA-OK
probe; pod-level `-p` publishes reach pasta's bind).

## Decision

One pod per project start: `opencode-sandbox-pod-<container-id>`,
created with network mode `pasta:--ipv4-only`. The pod holds:

- the **project container**, joining via `--pod` — it loses its own
  `--network` and its own publishes, and keeps everything else of its
  run shape (userns, caps, mounts, env, detach/foreground);
- a **per-pod SearXNG member** when `search_mode: searxng` (detached,
  `oc-searxng` image — still built on demand from the searxng
  Dockerfile —, settings.yml bind mount, generated secret, no
  publish, per-pod unique name `oc-searxng-<container-id>`);
- a **per-pod oc-proxy member** when `use_proxy` is set (#127);
- the **pod-level publishes** for host access: web UI (4096 scan) and
  CBM UI (9749 scan). The free-port scans run before pod creation;
  port scans for SearXNG/proxy service ports are gone.

Because pod members share one network namespace, companions are
addressed through in-namespace **constants**:
`SEARXNG_URL=http://127.0.0.1:8080/search`,
`HTTP(S)_PROXY=http://127.0.0.1:3128`. No hosts-entry injection, no
gateway detection; ADR 0028's `HOST_GW` / `--add-host` /
`host.oc-sandbox.internal` machinery is fully reverted and the
host-side shared `oc-searxng` run is gone. ADR 0026's combination
validation (searxng+proxy, searxng+offline) stays ahead of any podman
interaction.

**Pod lifecycle — decided: deterministic recreate, not
`--replace`.** `podman pod create --replace` could not be verified on
podman 4.9.3 in this environment (no podman available for the probe),
so start.sh takes the safe path: a best-effort, guarded
`podman pod rm -f` followed by a fresh `podman pod create` on every
start. Recreate also self-heals stale members. The Go layer's
"already-running containers are reused, not restarted" rule is
untouched — start.sh only runs when a start is actually requested.

Offline starts keep the pre-upgrade shape: no pod, no members,
project container runs with `--network=none` and carries the UI
publishes itself.

## Consequences

- Companions can no longer be reached via anything host-scoped; their
  addresses never parameterize and can never go stale.
- Host-side leftovers (`oc-searxng` containers started before the
  upgrade) are left alone — no auto-cleanup.
- Stop/delete flows must target the pod for pod-based runs with a
  container-only fallback for pre-upgrade containers (#127 tickets).
- Podman 5.x may allow the multi-network/pasta-map redesign noted in
  the spec's out-of-scope section: the pod is the simplest verified
  mechanism today, not the long-term shape.
