# Release notes — toolchain sets (the cutover release)

The cutover release's notes, ready to paste into `scripts/create-release.sh`
(interactive mode) when the release is cut. Every block is the spec's
(toolchain-sets §11) release-notes list, written for the shipped release —
the WebUI gist rows and the porting doc link stay in sync with what the
update actually does. All English (the §11 binding switch — every shipped
user-facing string is English from this release).

---

## Toolchain sets replace editions

Editions are gone; toolchain sets replace them end to end. A workspace no
longer picks a monolithic edition stage — it picks **toolchains**: fine-grained,
individually built units (cpp, rust, go, node-dev, playwright, latex, ros2,
embedded hardware units, and more) that compose into the workspace's image at
build time. The old edition names stay as ready-made bundles (web, embedded,
swdev, matlab, ros2, writing, latex — chat and pkm are base-only), and the New
Workspace wizard's toolchain multi-select takes any combination; picking
hardware toolchains flips on the hardware-in-the-loop preset automatically.
The caching target: **adding an already-built toolchain costs seconds** — a
never-built toolchain costs one build, reused by every workspace on this
machine thereafter.

## What the update does — and does not do

The update **builds nothing and rewrites nothing**: no auto-build at update,
ever, and your stored configuration is untouched. Migration is lazy — the
next Workspace Settings save stores your selection as toolchains (that write
is the whole migration), and nothing is rebuilt until the next start or
build. Your old edition images and containers are untouched: superseded
images render as **Legacy rows** on the Images screen (delete-only, never
auto-deleted), and at the end of the update a garbage-collection pass
reclaims the images nothing derives anymore — the update log pane carries
the summary line ("reclaimed ~X GB"); anything a container still references
stays, and the first start of a stopped workspace reuses or rebuilds
honestly.

## Dockerfile.custom keeps working — port it in one minute

Your `Dockerfile.custom` is never touched and stops shipping. Port it when
you like: each stage's `RUN`/`ENV`/`USER` lines become one recipe file under
`toolchains/custom/<id>.Dockerfile` — one toolchain per file, adapted to the
recipe grammar (pins everywhere, no ARG/COPY/WORKDIR/ENTRYPOINT). The
warning rows on Images and Workspace Settings show the inline porting gist
per stage, and the full recipe is in the porting doc:
[`docs/porting-dockerfile-custom.md`](porting-dockerfile-custom.md).

## First build after the update is long — then it never is again

The first start or build after the update builds the base image and the
toolchain components your selection needs — expect a long one (a minute or
two per component, the full assembly on top). Every later build reuses the
cached components: adding an already-built toolchain to a workspace costs
seconds, and the same component is never built twice on the machine.

## Floating installs are dead — pins arrive via updates

Every toolchain recipe pins its versions exactly (pip `==x.y.z`, npm
`@x.y.z`, versioned download URLs, rustup `--default-toolchain`); the old
floating installs (latest channels, release-API lookups, unpinned pip) are
gone, and the lint refuses any recipe that un-pins. New versions arrive
through updates, which rewrite the shipped recipes wholesale — your
`custom/` recipes are kept across updates, untouched.

## Workspaces that stored an unknown edition must pick toolchains

A workspace whose stored edition maps to no bundle (a removed edition, a
custom stage that was never ported, a corrupt config) refuses to start with
a visible reason — "stored edition 'X' maps to no bundle — choose toolchains
under Workspace Settings". Nothing is destroyed: pick toolchains in
Workspace Settings and the workspace is back; the old image and container
stay untouched until then.

## Restart after the update

The updater never restarts the running binary — restart the WebUI after
updating. Until you do, the old binary cannot see the new toolchain layout
and start/settings refuse with a visible reason; running containers are
unaffected.
