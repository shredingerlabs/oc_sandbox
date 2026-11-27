# Porting `Dockerfile.custom` stages to toolchain sets

Legacy installs carry a `~/.oc-sandbox/Dockerfile.custom` — one build
stage per custom edition, discovered by the old stage grep (ADR 0019).
Toolchain sets replace that world (spec `toolchain-sets`, ADR 0044): a
toolchain is one recipe file under `~/.oc-sandbox/toolchains/custom/`,
one toolchain per file, lint-checked at discovery, build and editor
save.

**Your file is never touched.** `Dockerfile.custom` stops shipping and
is left exactly as it is; the WebUI shows a warning row (Images and
Workspace Settings) with the inline porting gist for every stage that
still holds an uncommented `FROM … AS opencode-sandbox-<id>` header.
Nothing is destroyed on the way: your old edition image keeps existing
(a delete-only Legacy row on Images), and a workspace that stored a
custom edition name refuses to start with the "maps to no bundle"
refusal until you pick toolchains in Workspace Settings.

## The recipe in one minute

For each stage in `Dockerfile.custom`:

1. Create `~/.oc-sandbox/toolchains/custom/<id>.Dockerfile`
   (`<id>`: lowercase `[a-z0-9-]`, `base` is reserved, one toolchain
   per file).
2. Copy the stage's `RUN` / `ENV` / `USER` lines across — those are the
   lines the warning gist shows you.
3. Adapt them to the recipe grammar (below).
4. Pick the new toolchain in Workspace Settings (or add it to a bundle)
   and rebuild — the set image re-addresses content-addressed, so only
   what changed rebuilds.

A commented, lint-clean example lives in
`~/.oc-sandbox/toolchains/custom/example.Dockerfile.template`.

## The grammar your stage must survive

The old stage format accepted full Dockerfile syntax; a recipe file is
deliberately narrower. What changes:

- **`ARG`, `COPY`, `WORKDIR`, `ENTRYPOINT` are refused.** `ARG` bakes
  state the content hash cannot see (the CACHEBUST lesson), `COPY` has
  no build context, and the composition footer owns the rest. Inline
  the values (`ARG X` → literal `X` in the following lines); put files
  on the image via a download URL in `RUN`.
- **Only `RUN`, `ENV`, `USER` and comments are allowed**, plus
  `# extra: <absolute path>` lines declaring files the payload dump must
  capture because dpkg/pip do not know them (npm globals, tar-extracted
  binaries, manual `/etc` writes).
- **Every install is pinned.** pip needs `==x.y.z`, npm needs
  `@x.y.z`, direct download URLs must embed the exact version (no
  `latest` segments, no release-API queries), rustup needs
  `--default-toolchain <version>`. apt is exempt — its pin is the
  suite. Your stage's floating installs must pick exact versions; new
  versions arrive via updates, not rebuilds.
- **`ENV PATH=…` is refused**, as are direct writes to `~/.profile` /
  `~/.bashrc`. PATH additions go into a per-toolchain
  `~/.profile.d/<id>.sh` hook the base skeleton sources — e.g.
  `RUN mkdir -p ~/.profile.d && printf 'export PATH="$HOME/.local/bin:$PATH"\n' > ~/.profile.d/<id>.sh`.
- **`USER` switching is monotonic root → dev.** The fragment runs as
  root unless you switch; end at `USER dev`. Install as root, run as
  dev (rust: `rustup --no-modify-path` as dev).
- **The `FROM` line disappears.** Every component builds `FROM` the
  base image automatically; your recipe is a fragment composed into the
  set image, and the dev user + bash-login entrypoint are the footer's
  job.

## What you get back

The recipe joins the derived world: it shows as a component row on
Images (build/rebuild/delete/edit like every toolchain), feeds the
content-addressed set tags of every set containing it, and is kept
across updates (only `recipes/`, the manifest and the base recipe are
wholesale-overwritten — `custom/` is yours).

See `docs/specs/toolchain-sets.md` (§2 grammar, §10 migration) and
ADR 0044 for the full picture.
