# Pod-per-project networking: companions share the pod's pasta namespace

## Status

Accepted (2026-10-23) — supersedes ADR 0028 (the container→host gateway detour, fully reverted).

## Context

Every project container needs companion services: the SearXNG instance for
`search_mode: searxng` and the squid proxy for `--use_proxy` (never both,
ADR 0026). Both used to be **host-side shared containers** published on
`127.0.0.1` only, so the project container had to reach host-loopback ports —
and on the production stack (podman 4.9.3, Ubuntu 24.04 noble, passt
`0.0~git20240220`) that is exactly what pasta cannot do. Probes from inside a
project container established the constraint table:

| Constraint (podman 4.9.3 / noble passt) | Evidence |
| --- | --- |
| podman forces `--no-map-gw` on pasta | `ps -ef \| grep pasta` shows the flag; gateway→loopback behavior is therefore not contractually available |
| noble's passt snapshot (`0.0~git20240220`) predates the pasta map options (`--map-host-loopback` etc.) | option absent from the shipped binary; pasta options cannot express "map this address to host loopback" |
| multi-network is rejected (`cannot set multiple networks without bridge network mode, selected mode pasta`) | pasta + bridge detour for companions cannot be expressed |
| `host.containers.internal` resolves to the container's **own** address (`10.0.2.15`) — pasta hands the guest the host's first interface address, so the guest shadows the host | connecting to it loops locally and is refused |
| the guest's default-route gateway (`10.0.2.2`) does reach host-loopback ports — but is the **physical host's** localhost on a VirtualBox guest | the interim ADR 0028 gateway detour answered with the physical host's OpenWebUI, not the sandbox's SearXNG; aimed at the wrong server |
| slirp4netns is not a direction | deprecated in some distros |

What *did* verify on podman 4.9.3 (POD-PASTA-OK probes):

- `podman pod create --name X --network pasta:--ipv4-only` plus a member run
  with `--pod` works.
- Pod-level `-p` publishes reach pasta's bind (a probe bind on port 4096
  failed only because that port was genuinely occupied on the host).
- `podman pod rm -f` removes a pod with members non-interactively.
- `podman pod create --replace` could not be verified on 4.9.3 — see Decision.
- `--userns` and `--pod` cannot be set together on one `podman run`
  (reproduced on 4.9.3, 2026-10: `Error: --userns and --pod cannot be set
  together`). The keep-id usern therefore moves to the pod:
  `podman pod create --userns=keep-id` + member runs without any per-container
  `--userns` (verified on 4.9.3). Only the offline (non-pod) project run keeps
  its own `--userns=keep-id`.

## Decision

One pod per project start: `opencode-sandbox-pod-<container-id>`, created
with network mode `pasta:--ipv4-only`. The pod holds:

- the **project container**, joining via `--pod` — it loses its own
  `--network`, its own publishes, and its own `--userns` (the pod carries
  `--userns=keep-id`; podman 4.9 rejects `--userns` on pod members), and
  keeps everything else of its run shape (caps, mounts, env, detach/foreground);
- a **per-pod SearXNG member** when `search_mode: searxng` (detached,
  `oc-searxng` image — still built on demand from the searxng Dockerfile —,
  settings.yml bind mount, generated secret, no publish, per-pod unique name
  `oc-searxng-<container-id>`);
- a **per-pod oc-proxy member** when `use_proxy` is set (detached, oc-proxy
  image, no publish, per-pod unique name `oc-proxy-<container-id>`);
- the **pod-level publishes** for host access: web UI (4096 scan) and CBM UI
  (9749 scan). The free-port scans run before pod creation; port scans for
  SearXNG/proxy service ports are gone.

Because pod members share one network namespace, companions are addressed
through in-namespace **constants**:
`SEARXNG_URL=http://127.0.0.1:8080` (base URL only — the
`mcp-searxng` package appends `/search` itself) and
`HTTP(S)_PROXY=http://127.0.0.1:3128`. No hosts-entry injection, no gateway
detection, no `host.oc-sandbox.internal`, no dependence on podman's
`host.containers.internal` — ADR 0028's machinery is fully reverted and the
host-side shared `oc-searxng` run is gone.

**Host exposure stays loopback-only.** The pod-level UI publishes keep the
`127.0.0.1:<port>:<port>` shape the pre-pod container runs used; the
companions publish nothing at all (they are reachable only inside the pod's
namespace). Nothing is exposed on the host's LAN address.

**Pod lifecycle — deterministic recreate, not `--replace`.**
`podman pod create --replace` could not be verified on podman 4.9.3 in this
environment (no podman available for the probe), so start.sh takes the safe
path: a best-effort, guarded `podman pod rm -f` followed by a fresh
`podman pod create` on every start. Recreate also self-heals stale members.
The Go layer's "already-running containers are reused, not restarted" rule is
untouched — start.sh only runs when a start is actually requested.

Offline starts keep the pre-upgrade shape: no pod, no members, project
container runs with `--network=none` and carries the UI publishes itself.
ADR 0026's combination validation (searxng+proxy, searxng+offline) stays
ahead of any podman interaction.

## Alternatives considered

- **ADR 0028's gateway detour** (`--add-host host.oc-sandbox.internal:<gw>`):
  superseded by this ADR — it aims at the wrong server behind a NAT'd guest
  (the default-route gateway is the physical host's localhost there) and
  rests on pasta→loopback translation that podman's own `--no-map-gw` exists
  to suppress.
- **Publish companions on all host interfaces**: works everywhere, but hands
  the search/proxy endpoints to the host's LAN — exactly the exposure the
  loopback-only publish exists to avoid.
- **Fix `host.containers.internal` ourselves** (`--add-host`): podman writes
  its own (broken) entry first; /etc/hosts resolves on first match.
- **Pass pasta map options** (`--map-host-loopback`): unavailable in noble's
  passt snapshot; would pin the whole host fleet to a pasta version floor.
- **slirp4netns networking**: rejected — deprecated in some distros.
- **SearXNG inside the project image**: rejected for image bloat across all
  editions (out of scope in the pod-per-project spec).

## Consequences

- Companions work regardless of podman version or host network topology;
  their addresses are constants and can never go stale. Two searxng projects
  run concurrently with per-pod instances and no port conflicts.
- Stop/delete flows target the pod for pod-based runs (companions go down as
  one unit) with a container-only fallback for pre-upgrade containers.
- Host-side leftovers (`oc-searxng`/`oc-proxy` containers started before the
  upgrade) are left alone — no auto-cleanup.
- Memory cost per running searxng project: one granian worker
  (~60–100 MB RSS); squid adds ~20 MB — accepted.
- **Podman 5.x path (future simplification, not implemented):** a newer
  passt/podman combination may allow multi-network setups or pasta map
  options, which would let companions live in a separate network again. The
  pod is the simplest *verified* mechanism today, not necessarily the
  long-term shape.
