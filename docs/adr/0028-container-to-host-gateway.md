# Container→Host reachability goes through the default-route gateway, not host.containers.internal

## Status

Accepted (2026-10-23)

## Context

start.sh publishes host-side services (oc-proxy on 3128, oc-searxng on a
scanned port) **on 127.0.0.1 only** and hands their address to the project
container as `http://host.containers.internal:<port>`. In production (podman
4.9.3, Ubuntu 24.04 noble, passt 0.0~git20240220) every such connection was
refused while the same port answered on the host loopback. Probes from inside
a project container:

- `host.containers.internal` resolves to `10.0.2.15` — the container's **own**
  pasta address (pasta hands the guest the host's first interface address, so
  the guest's address shadows the host's). Connecting to it is a local
  connection inside the namespace and never reaches the host.
- `curl http://10.0.2.2:8080/` → `200` — the guest's **default-route gateway**
  address reaches host-loopback-published ports (pasta translates guest
  connections to the default gateway into host-loopback connections).
- `ps -ef | grep pasta` shows podman passes `--no-map-gw`, yet the
  gateway→loopback translation is active on this passt snapshot; portability
  of that behavior is documented empirically, not by contract.
- noble ships passt `0.0~git20240220`, which predates the `--map-host-loopback`
  pasta option, so pasta options cannot express "map this address to host
  loopback" portably either.

## Decision

start.sh resolves the host's default-route gateway (`ip -4 route show
default`, fallback `10.0.2.2`) and injects it as `--add-host
host.oc-sandbox.internal:<gw>` into every project container. Proxy env
(`HTTP(S)_PROXY`) and `SEARXNG_URL` use that name instead of
`host.containers.internal`. The loopback-only publish (`-p 127.0.0.1:...`)
stays — the service is not exposed on the host's LAN address.

## Alternatives considered

- **Publish on all host interfaces** (`-p <port>:<port>` without the IP):
  works everywhere, but hands the search/proxy endpoint to the host's LAN —
  exactly the exposure the loopback-only publish was chosen to avoid.
- **Fix host.containers.internal ourselves** (`--add-host
  host.containers.internal:<gw>`): podman writes its own (broken) entry first;
  /etc/hosts resolves on first match, so ours would never win.
- **Pass pasta options** (`--map-host-loopback`): unavailable in noble's passt
  snapshot; would also pin us to a pasta version floor for the whole host fleet.

## Consequences

- Container→host services work on podman 4.9/noble setups (VM and LAN hosts
  alike, because the guest's default-route gateway follows the host's).
- A host without a default route (rare) falls back to `10.0.2.2`; services
  then fail until a route exists — visible as connection refused in the MCP.
- If a future podman makes `host.containers.internal` correct, our own entry
  is unaffected (unique name) and the mechanism keeps working.
