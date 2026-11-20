# Podman/buildah layer-cache semantics for composed images

**Issue:** #177
**Research Date:** 2026-11-20

Facts below are from primary sources fetched 2026-11-20: repo files/man pages/CHANGELOGs (containers/buildah, containers/podman, containers/common, containers/storage, opencontainers/image-spec) and distro package pages (packages.debian.org, launchpad.net, src.fedoraproject.org, download.opensuse.org). Version-dependent claims name an explicit version + citation. Sources were fetched via https://github.com/containers/buildah and https://github.com/containers/podman (both redirect to podman-container-tools/*). Line numbers refer to `main` unless noted.

## Executive summary (headline answer)

Both composition strategies' cache requirements are real, with these semantics:

1. **Store-scan layer reuse across builds** — `podman build` defaults to `--layers=true` (docs/source/markdown/options/layers.md, "Default is `true`"; also podman-build v3.4.4 man page, docs/source/markdown/podman-build.1.md at v3.4.4:379). buildah's default is `--layers=false` (docs/buildah-build.1.md:619-624). When `--layers` is on, each step re-uses cached intermediates found in the **local image store** by matching **parent-layer chain identity + history equality** (imagebuildah/stage_executor.go:2640-2756). Two separately invoked builds therefore share their common prefix: identical `Dockerfile` text, identical base image, identical content digests. A trailing difference misses and disables further cache lookups for the rest of the build (stage_executor.go:1965-1968).
2. **Distributed cache** — buildah has its own registry-based cache: `--cache-from` / `--cache-to` push/pull intermediate **images** tagged with a computed cache key; both are ignored unless `--layers` (docs/buildah-build.1.md:115-159, "Note: `--cache-from` option is ignored unless `--layers` is specified"). Podman exposes the same options (@@option cache-from/cache-to in podman-build.1.md.in; present in podman v3.4.4, docs/source/markdown/podman-build.1.md:90). This is **not** BuildKit-style external cache import/export (see §5).
3. **ARG semantics** — an ARG value change breaks the cache from the first RUN after the arg's declaration onward: `ARG` steps store only the arg **names** (stage_executor.go:2184-2191, getBuildArgsKey 2357-2368: "for key := range s.stage.Builder.Args ... args = append(args, key)"), while `RUN` steps record the **resolved values of ALL args declared so far in the stage**, whether or not that RUN consumes them (stage_executor.go:2192-2194 + getBuildArgsResolvedForRun 2305-2355: env strings `KEY=VALUE` for every `AllowedArgs` key, ENV-override-aware, proxy args excluded unless ARG'd; appended verbatim to the RUN's created-by string at 2277-2280, which is what the last history entry is compared against, historyAndDiffIDsMatch 2155-2159). So changing **any declared** arg value busts every RUN layer after its declaration point, consumed or not — which is exactly how today's `CACHEBUST` arg busts RUN layers that never reference it (dist/Dockerfile's own comment); a passed-but-undeclared arg (no `ARG` line) never busts anything, and an ENV of the same name overrides the arg value in the history string. Caution: the doc comment above `getBuildArgsResolvedForRun` claims it "excludes any build-args that were not used in the build process" — the code includes every declared arg (all `AllowedArgs` keys' values); code wins.
4. **What kills a hit** — any declared build-arg value change (above — consumed or not); COPY/ADD content/context change (content digest via builder.ContentDigester, stage_executor.go:1344-1355, 2286-2295, 1817-1842); base-image history change; text of any instruction (history entries compare CreatedBy/Comment/Author/EmptyLayer/Created, stage_executor.go:2085-2108); arch/OS/manifest-type mismatch (2716-2737); `--cache-ttl` age filter (2662-2673); `--no-cache` (useCache false, executor.go:353); `--no-cache-filter` (stage_executor.go:1392-1396 — unreleased in any podman/buildah as of 2026-11, see §4); build context differences for RUN (no pre-execution check; step re-executes and re-runs network fetches — there is no HTTP-level fetch cache).
5. **RUN --mount=type=cache** — supported by buildah since **v1.24.0 (2022-01-26)** (buildah-CHANGELOG.md:2149 "buildkit: add support for mount=type=cache" — in the v1.24.0 section, headers between v1.24.0 (line 2036) and v1.23.0 (line 2192)). Data is stored on the host in `/var/tmp/buildah-cache-<rootlessUID>/<digest("[id|dest]:uid:gid")[:16]>` and **persists across builds**; locking modes shared/private/locked; prunable via `podman image prune --build-cache`.
6. **COPY --from=<image>** — files only; image config (ENV/ENTRYPOINT/LABELS) is **not** merged into the composed image.
7. **Floors** — Ubuntu 22.04 (podman 3.4.4, bundled buildah v1.23.1) lacks `type=cache`; Ubuntu 24.04 (podman 4.9.3 in the noble release pocket, buildah 1.33.x — 5.4.2 available via noble-backports) and above + Debian bookworm/trixie + Fedora F41+ + openSUSE Leap 15.6/16.0/16.1 have it. BuildKit-style `--cache-from`/`--cache-to`: **not available** in podman/buildah (buildah's own registry-based `--cache-from/--cache-to` exist since buildah v1.27.0 = podman v4.2.0).

## 1. Layer-cache keying & matching

### 1.1 The two mechanisms

**(a) Store-scan reuse of cached intermediates — requires `--layers` (podman default true).**

Gate: `checkForLayers := s.executor.layers && s.executor.useCache && !stageNoCache` (imagebuildah/stage_executor.go:1393); ARG steps are excluded from the check (1817). Per step, `intermediateImageExists` scans `store.Images()` (2649) and returns the most recently created match (2750-2752). Candidate match criteria:

- **Parent chain**: `s.builder.TopLayer == imageParentLayerID` (2699-2702), where `imageParentLayerID` is the candidate's top-layer parent when the step adds a layer, else its top layer (2692-2694).
- **History equality**: `historyAndDiffIDsMatch` (2116-2160): `len(history) == len(baseHistory)+1`; each base history entry equals the candidate's via `historyEntriesEqual` (2085-2108: CreatedBy, Comment, Author, EmptyLayer, and Created); diffIDs of common layers are equal (2150-2154); and the candidate's last history entry equals `getCreatedBy()` of the current step (2155-2159).
- **Platform/manifest**: manifest type equals the build's output format (2716-2719); arch/OS match when set (2722-2737).
- **`--cache-ttl`**: images older than the TTL are ignored (2662-2673).

For **separately invoked builds sharing a prefix** (two generated Dockerfiles differing only in trailing lines): build #2 replays the prefix against build #1's cached intermediates from the store — prefix steps are cache hits, trailing lines miss, and after the first miss no further cache lookups happen for that stage ("No longer searching cache due to miss", stage_executor.go:1965-1968).

**(b) Distributed registry cache (`--cache-from`/`--cache-to`).** Key = `generateCacheKey` (stage_executor.go:2486-2523): SHA-256 over (1) base image diffIDs (each layer digest string, in order), (2) `createdBy` for the current step, (3) `buildAddsLayer`, (4) manifestType, (5) each base history entry (CreatedBy/Author/Comment/Created/EmptyLayer). For COPY/ADD, `addedContentDigest` (digest of the content just copied, via `builder.ContentDigester.Digest()` — `getContentSummaryAfterAddingContent`, 1344-1355; wired into `createdBy` at 2286-2295 and `setLinkedLayerHistory`, 1360-1372) is folded into the history. `pushCache` (2546-2585) pushes intermediate images to the `--cache-to` repos **tagged with the cacheKey** (`cacheImageReferences`, 2527-2541); `pullCache` (2593-2635) pulls them with `PullIfNewer`. Both are ignored without `--layers` (buildah-build.1.md:131, 153; options/cache-from.md: "Note: `--cache-from` option is ignored unless `--layers` is specified"). Buildah's docs stress this differs from BuildKit: "Buildah's distributed cache mechanism pulls intermediate images from the remote registry itself, unlike Docker and BuildKit where the intermediate image is stored in the image itself" (buildah-build.1.md:133-137). Version: buildah **v1.27.0 (2022-08-01)**, CHANGELOG "build: add support for distributing cache to remote sources using `--cache-to` and `--cache-from`" (buildah-CHANGELOG.md:1859, v1.27.0 section between headers at lines 1854/1884); first shipped in podman **v4.2.0** ("Updated Buildah to v1.27.0", podman v4.2.0 release notes). (Remote/multi-machine cache use is out of scope for the map — recorded here only as a fact of what exists.)

### 1.2 What breaks a cache hit

- **ARG value changes** — every subsequent `RUN` layer busts (all declared args' `KEY=VALUE` are in each RUN's created-by string, stage_executor.go:2192-2194, 2277-2280, 2305-2355 — consumed or not); COPY/ADD steps bust only via their content digests (an arg change that alters the copy source paths/text changes the instruction text; one that does not, does not). The `ARG` step itself records only the arg names (stage_executor.go:2184-2191, 2357-2368). Passed-but-undeclared args never bust anything (AllowedArgs check, 2317-2318). Note ENV semantics: "Values from ENV will always override values specified arg" (stage_executor.go:2303) — the effective value in the history string is ENV's, so changing an ARG shadowed by a same-named ENV yields no history change.
- **Build context / content differences** — COPY/ADD digest changes → different `createdBy` + different diffIDs → miss. RUN steps that depend on context re-execute; network fetches re-run (no request-level fetch cache).
- **Base image changes** — the candidate must match `baseHistory`/diffIDs of the current base; a new base image's history breaks every step (2704-2737, 2116-2135).
- **Instruction text changes** — `historyEntriesEqual` compares CreatedBy/Comment/Author/EmptyLayer/Created (2085-2108); any text change of a shared instruction breaks the chain at that step.
- **Platform/manifest/age** — arch/OS/manifest-type mismatches (2716-2737) and `--cache-ttl` expiry (2662-2673).
- Explicitly: `--no-cache` ("Do not use existing cached images for the container build", buildah-build.1.md:733-735; `useCache: !options.NoCache`, executor.go:353), `--no-cache-filter` (buildah-build.1.md:737-745; podman options/no-cache-filter.md), `--layers=false` (skips the store-scan entirely: `checkForLayers` false, stage_executor.go:1393).

### 1.3 Where intermediates live

- They are images in the **local image store** (`store.Images()`, stage_executor.go:2649). The final image gets the output name; intermediate stage images are committed with empty `output` and are **untracked** — visible with `podman image ls -a` (podman-images.1.md.in:18-20: "--all: Show all images (by default filter out the intermediate image layers)").
- Kept after the build when `--layers` (podman default) or `--save-stages` (executor.go:1323-1337: `if !b.layers && !b.saveStages && !r.OnlyBaseImage { cleanupImages = append(...) }`); **deleted** when `--layers` is off (podman default is on, buildah default is off) — executor.go:962-976 `store.DeleteImage`.
- `--save-stages`: "Preserve intermediate stage images instead of removing them after the build completes (Default is `false`)... subsequent builds with `--layers` can use the preserved intermediate layers as cache" (podman options/save-stages.md; buildah-build.1.md:918-926). `--layer-label` adds labels to intermediates (buildah-build.1.md:613-617).
- `--cache-to` pushes intermediate images tagged by cacheKey to remote repos (stage_executor.go:2546-2585; buildah-build.1.md:139-159).

### 1.4 What prunes them

- `podman image prune` — removes dangling images (podman-image-prune.1.md:9-13); "--all: Remove dangling images and images that have no associated containers" (16-18); does not prune layers still needed by other images (13).
- `podman system prune` — removes unused containers, dangling + unreferenced images, and "all dangling build cache" by default, "all build cache" with `--all` (podman-system-prune.1.md:9-17; examples show "all dangling build cache").
- `buildah prune` — "Cleanup intermediate images as well as build and mount cache" (buildah-prune.1.md).
- `podman image prune --build-cache` — "Remove persistent build cache created for `--mount=type=cache`" (podman-image-prune.1.md:20-22; option first present in the man page at podman **v5.3.0** — absent at v5.2.0, present at v5.3.0, bisected over tagged man pages).

## 2. `RUN --mount=type=cache`

### 2.1 Support and versions

- Supported by buildah since **v1.24.0 (2022-01-26)**: CHANGELOG entry "buildkit: add support for mount=type=cache" (buildah-CHANGELOG.md:2149, within the v1.24.0 section, headers at lines 2036/2192). Related: "build: Add support for buildkit like --mount=type=bind" (same version, buildah-CHANGELOG.md:2162).
- Implementation: `run_common.go:1665` (`case "cache":`), `run_linux.go:1463` (`Builder.getCacheMount`), `internal/volumes/volumes.go` (`GetCacheMount`, `CacheParent`, `cacheLockFile`, `lockPrivateCacheDir`).
- Podman inherits: "podman build uses code sourced from the Buildah project" (podman-build.1.md:45-46); `type=cache` therefore first ships in **podman v4.0.0** ("Updated Buildah to v1.24.0", podman v4.0.0 release notes). Containerfile(5) documents the types: "Current supported mount TYPES are bind, cache, secret and tmpfs" (containers/common/docs/Containerfile.5.md:108), with cache options `id`, `mode` (default 0755), `ro/readonly`, `uid`, `gid`, `from` (stage/image root instead of host dir), `rw` (Containerfile.5.md:156-170).
- Also on `podman build --mount` (buildah-build.1.md:671-683).

### 2.2 Storage, persistence, locking (from the implementation)

- Cache parent: `CacheParent() = filepath.Join(tmpdir.GetTempDir(), "buildah-cache-"+strconv.Itoa(unshare.GetRootlessUID()))` (internal/volumes/volumes.go:63-66). `GetTempDir()` = `$TMPDIR` else containers.conf `imageCopyTmpDir` else `/var/tmp` (internal/tmpdir/tmpdir.go:12-26). Comment: "mount=type=cache must create a persistent directory on host so its available for all consecutive builds. Lifecycle of following directory will be inherited from how host machine treats temporary directory" (volumes.go:40-42).
- Directory: `os.MkdirAll(cacheParent, 0o755)` (588); the per-cache dir ID is a digest of `[id|destination]:uid:gid` (593-598: "caches are told apart by their ID, or by their target if they don't have one, and by their ownership"; "hash it, so the user can't inject path components and escape the cache"). CHANGELOG: "Distinguish --mount=type=cache locations by ownership, too" (buildah-CHANGELOG.md:874, v1.40.0 section); "mount=type=cache: seperate cache parent on host for each user" (buildah-CHANGELOG.md:1707, v1.29.0 section).
- Lock modes: default `sharing=shared` (641-645); `private` — "a private cache uses a directory which no other build is using, and keeps it locked until the command we're running has finished" (599-606, `lockPrivateCacheDir` 705-713); `locked` — acquires a lockfile during the RUN (630-639; `cacheLockFile` 690-696; lockfiles under `<cacheParent>/buildah-cache-lockfiles/<dirID>/` — constants at 40-48: "mount=type=cache allows users to lock a cache store while its being used by another build"). CHANGELOG: "buildkit: mount=type=cache support locking external cache store" (buildah-CHANGELOG.md:1976, v1.25.0 section).
- Rootless: the cache parent is per-rootless-UID (volumes.go:64; CHANGELOG v1.29.0 above).
- Mount mechanics: the cache dir is a host-side **bind mount** (`newMount.Type = define.TypeBind`, 386-388; options "bind" at 652); `mode`/`uid`/`gid` default to 0755/0/0 "buildkit parity" (391-396, 616-620).

### 2.3 What it caches; clearing

- Across builds: **yes** — the cache dir is a persistent host directory ("a common cache directory, which persists on hosts within temp lifecycle", volumes.go:583-585); consecutive `podman build`s reuse it. Cleared by: host temp-dir lifecycle (/var/tmp policies); `podman image prune --build-cache` ("Remove persistent build cache created for `--mount=type=cache`", podman-image-prune.1.md:20-22 — option present since podman v5.3.0); `podman system prune` (removes "all dangling build cache" by default — podman-system-prune.1.md:75, 88); `buildah prune` ("build and mount cache", buildah-prune.1.md; command added buildah v1.29.0, CHANGELOG "buildah: add prune command and expose CleanCacheMount API", buildah-CHANGELOG.md:1682).

## 3. Reusing built component images when composing

### 3.1 `COPY --from=<image>`

- **Files only, not metadata**: buildah's `COPY --from` uses the root directory of the image as the copy root (docs/buildah-copy.1.md, --from option); "If `--chown` or `--chmod` are not used, permissions and ownership is preserved." The Containerfile(5) docs: "The optional flag `--from=name` can be used to copy files from a named previous build stage. It changes the context of `<src>` from the build context to the named build stage." Name priority (buildah-build.1.md:109-113): named build context (`--build-context`) > stage `AS` name > image name (local or remote). `RUN --mount=from=<image>` accepts the same (buildah-build.1.md:100-107).
- **Image config is not merged**: the composed image's config comes from the stage builder — `stage_executor.go commit()` sets `config := ib.Config()` (2781) and applies ENV/ENTRYPOINT/LABELS etc. from the Containerfile; nothing copies ENV/ENTRYPOINT/LABELS of the component image through `COPY --from`. For metadata to flow, use `FROM <component-image>` (which sets the base config) plus `--target`/`--save-stages` style reuse of the component.
- **Merge semantics**: `COPY` "copies new files from `<src>` and adds them to the filesystem of the container at path `<dest>`" (Containerfile.5.md:386-394; new files created 0755/uid 0 unless overridden). Directory merge: later `COPY` overwrites same-named paths; directories union; `/usr/local` collisions are last-write-wins per path.
- **dpkg/pip/npm**: `/var/lib/dpkg/status` is one file — when two components ship entries, the merged file content is the result of sequential file copies (later COPY wins); there is no package-manager awareness in the merge. pip/npm metadata lives in ordinary files/venv trees — merged by the same file-level rules.

### 3.2 Size accounting

- `podman images`: `.Size` = "Size of layer on disk (human-friendly string)", `.VirtualSize` (same bytes) — docs/source/markdown/podman-images.1.md.in:68,70.
- `podman system df -v`: per-image columns `SIZE | SHARED SIZE | UNIQUE SIZE | CONTAINERS` (podman-system-df.1.md:53-54). `podman system df` warns: "The RECLAIMABLE size that is reported for images can be incorrect. It might report that it can reclaim more than a prune would actually free. This will happen if you are using different images that share some layers." (12-14). I.e., SIZE counts the image's layer size on disk; shared layers are visible in SHARED/UNIQUE SIZE (dedup semantics via the storage driver — containers/storage/README.md:17-29 describes layers as change-on-write with one parent, images referencing top layers).

## 4. Tagging & keeping intermediates

- `--target`: podman (options/target.md): "Set the target build stage to build. When building a Containerfile with multiple build stages, --target can be used to specify an intermediate build stage by name as the final stage for the resulting image. Commands after the target stage are skipped."; buildah-build.1.md:1230-1234 (same).
- `--layers` defaults: **buildah = false** (buildah-build.1.md:619-621: "Cache intermediate images during the build process (Default is `false`)"), **podman = true** (options/layers.md; podman-build v3.4.4 man:379); `BUILDAH_LAYERS` env override honored by both.
- `--save-stages`: podman (options/save-stages.md: "Preserve intermediate stage images instead of removing them after the build completes (Default is `false`)..."); buildah (buildah-build.1.md:918-926). Combined with `--stage-labels` (podman options/save-stages.md; buildah-build.1.md:928-929) stages get labels for identification. **Version: buildah v1.44.0 (2026-05-27)**, CHANGELOG "feat: add support for saving and labeling intermediate stages" (buildah-CHANGELOG.md:402, v1.44.0 section between headers at lines 297/554); first shipped in **podman v6.0.0** ("Updated Buildah to v1.44.0", podman v6.0.0 release notes). Too new to require on LTS hosts as of 2026-11 (Debian 12 / Ubuntu 24.04 / Leap 15.6 lack it); the per-instruction intermediates kept by `--layers` remain the portable cross-build cache.
- `--no-cache-filter`: podman (options/no-cache-filter.md; "Only has an effect when --layers is used, since caching is not performed on a per-stage basis otherwise"); buildah (buildah-build.1.md:737-745: "Only has an effect when **--layers** is used"). **Unreleased as of 2026-11**: the flag is registered on buildah `main` (pkg/cli/common.go:290) and documented in both `main` man sets, but is absent from every released buildah man page (v1.24.0 through v1.46.0, bisected over tagged docs) and from podman's options dir at tag v6.0.0 — do not rely on it at any current-version floor.
- What happens to the cross-build cache when `--layers=false`: the store-scan never runs (`checkForLayers` false, stage_executor.go:1393), so nothing is reused between separately invoked builds; intermediates are deleted after the build (executor.go:1334-1336). With podman `--layers=false` explicitly: same skip + "Build from the start with a new set of cached layers" per `--no-cache` semantics (options/no-cache.md).

## 5. Safe feature floor for rootless-podman Linux desktops

### 5.1 Per-distro versions (package pages)

| Distro | Podman version | Buildah in the stack | Required set OK? | Sources |
|---|---|---|---|---|
| Ubuntu 22.04 (Jammy) | 3.4.4+ds1-1ubuntu3 (source version page) | bundled buildah **v1.23.1** (podman v3.4.4 go.mod) — **no** `type=cache` (needs ≥1.24.0) | Floor: Ubuntu 24.04 | https://launchpad.net/ubuntu/+source/podman/3.4.4+ds1-1ubuntu3; https://raw.githubusercontent.com/containers/podman/v3.4.4/go.mod; buildah-CHANGELOG.md:2149 |
| Ubuntu 24.04 (Noble) | **4.9.3+ds1-1ubuntu0.1** in the release pocket (packages.ubuntu.com/noble/podman); 5.4.2+ds1-1ubuntu0.24.04.1 via noble-backports (launchpad "Podman Backports from Debian") | pocket: buildah **v1.33.x** (podman 4.8/4.9 line — "Updated Buildah to v1.33.2", podman v4.8.0 release notes); backports: v1.39.4 (podman v5.4.2 go.mod) | Yes (4.9.3 already has `type=cache` ≥1.24) | https://packages.ubuntu.com/noble/podman; https://launchpad.net/ubuntu/+source/podman; podman v4.8.0/v5.4.2 release notes |
| Ubuntu 26.04 (Resolute) | 5.8.6+ds1-3 (launchpad version page; "The Resolute Raccoon" row, https://launchpad.net/ubuntu/+source/podman) | ≥ v5.8 (current) | Yes | https://launchpad.net/ubuntu/+source/podman/5.8.6+ds1-3 |
| Debian stable (bookworm, 12) | 4.3.1+ds1-8+deb12u1 | buildah 1.28.2+ds1-3+deb12u1 (≥1.24.0; `--target` in its man, buildah-build-1.28.2.md:717) | Yes (its `--no-cache-filter`/`--save-stages` are absent from its man page) | https://packages.debian.org/bookworm/podman; https://packages.debian.org/bookworm/buildah |
| Debian stable (trixie, 13) | 5.4.2+ds1-2 | buildah 1.39.3+ds1-1 | Yes | https://packages.debian.org/trixie/podman; https://packages.debian.org/trixie/buildah |
| Fedora F41 | 5.6.2 | current (5.6.x) | Yes | https://src.fedoraproject.org/rpms/podman (raw/f41/f/podman.spec) |
| Fedora F42 / F43 / F44 | 5.8.2 / 5.8.10 / 5.8.10 | current | Yes | https://src.fedoraproject.org/rpms/podman (raw/f42,f43,f44) |
| openSUSE Leap 15.6 | 4.8.3 | v1.33.x (podman 4.8 line) | Yes | https://download.opensuse.org/distribution/leap/15.6/repo/oss (repomd + primary.xml.gz; verified podman 4.8.3 in the OSS repo metadata) |
| openSUSE Leap 15.5 | 4.4.4 | bundled buildah v1.29.0 (podman v4.4.4 go.mod) | Yes | https://download.opensuse.org/distribution/leap/15.5/repo/oss |
| openSUSE Leap 16.0 | 5.4.2 | current | Yes | https://download.opensuse.org/distribution/leap/16.0/repo/oss (primary.xml.zst decoded) |
| openSUSE Leap 16.1 | 5.8.2 | current | Yes | https://download.opensuse.org/distribution/leap/16.1/repo/oss (primary.xml.zst decoded) |

Podman feature availability by version — podman bundles buildah (podman build *is* buildah code); the podman→buildah bundle mapping, from each podman release's notes ("Updated Buildah to vX"):

| Podman | Bundled buildah | First-shipped features this buys |
|---|---|---|
| v3.4.x | v1.23.x | `--layers` default true; COPY --from=<image>; **no** `RUN --mount=type=cache` |
| v4.0.0 (2022-02) | v1.24.0 | `RUN --mount=type=cache` / `type=bind` / `type=tmpfs` |
| v4.2.0 (2022-08) | v1.27.0 | `--cache-from`/`--cache-to` (registry distributed cache); `--build-context` |
| v4.3.x (Debian 12) | v1.28.x | `--skip-unused-stages` (default true) |
| v4.8.0 / v4.9.x (Ubuntu 24.04 pocket) | v1.33.2 | `--layer-label` (v1.32.0) already in |
| v5.3.0 (2025) | v1.38.0 | `podman image prune --build-cache` |
| v5.4.0 / Debian 13 (5.4.2) | v1.39.0 | cache mounts accept stages/images as sources |
| v6.0.0 | v1.44.0 | `--save-stages`, `--stage-labels` |
| v6.2.0 (current, 2026-11) | v1.46.0 | (cache-mount `sharing=private`) |

(Sources: podman release notes per tag — v4.0.0 "Updated Buildah to v1.24.0", v4.2.0 → v1.27.0, v4.3.0 → v1.28.0, v4.8.0 → v1.33.2, v5.3.0 → v1.38.0, v5.4.0 → v1.39.0, v6.0.0 → v1.44.0, v6.1.0 → v1.45.0, v6.2.0 → v1.46.0; buildah CHANGELOG.md for feature versions.)

Flags whose docs exist only on `main` and in **no** release: `--no-cache-filter` (absent from tagged buildah docs v1.24.0–v1.46.0 and from podman v6.0.0's options dir). `--layers` default true, `--cache-from`/`--cache-to`, `--cache-ttl` (default: no TTL — flag default empty, `pkg/cli/common.go:248`, and the age filter only applies when set, stage_executor.go:2662-2673), `--target` are available in every podman ≥ 3.4. `--save-stages`/`--stage-labels` require podman ≥ 6.0.0; `podman image prune --build-cache` requires podman ≥ 5.3.0.

### 5.2 Required feature set & floor summary

Required set for oc-sandbox composition work: `podman build --layers` (store-scan reuse), `--target`, `--cache-from`/`--cache-to` (distributed cache), `RUN --mount=type=cache` (buildah ≥ 1.24.0), plus `--save-stages`/`--no-cache-filter` where available.

- **Ubuntu 22.04 (3.4.4) fails the type=cache set** (bundled buildah 1.23.1; per podman v3.4.4 go.mod; type=cache needs ≥ 1.24.0 = podman ≥ 4.0.0). Floor for Ubuntu LTS = **24.04**: its release pocket ships podman 4.9.3 (buildah 1.33.x, has `type=cache`), noble-backports offers 5.4.2 (buildah 1.39.4).
- Debian stable bookworm/trixie, Fedora F41+, openSUSE Leap 15.6/16.0/16.1 all satisfy the set (Leap 15.5: podman 4.4.4 → bundled buildah 1.29.0 per podman v4.4.4 go.mod).
- **`--cache-from`/`--cache-to` are not BuildKit-style**: the current podman-build man documents only the Buildah distributed-image cache (`@@option cache-from`, `@@option cache-to`, `@@option cache-ttl` in podman-build.1.md.in:82-86; docs/source/markdown/options/cache-from.md "Repository to utilize as a potential list of cache sources... Buildah will try to look for cache images..."). Buildah-build.1.md:133-137 documents the different semantics vs Docker/BuildKit. No BuildKit-style `--cache-from`/`--cache-to` (import/export with `type=` values) is available in podman/buildah as examined; `podman build` has no `--build-cache` import/export option.

## Threats to the measurable target ("builds once per machine, reused by every workspace thereafter")

Anything that turns these caches off or breaks the shared prefix:

1. `--layers=false` (buildah's default) or `--no-cache`: store-scan reuse disabled entirely (stage_executor.go:1393, executor.go:353).
2. Deleting/unused intermediates: `podman image prune` (dangling; `--all`: unused), `podman system prune` (dangling + unreferenced images + "all dangling build cache" by default), `podman system prune --all` (all unused images + "all build cache"), `buildah prune` (intermediate images + build/mount cache).
3. `podman image prune --build-cache` — deletes the persistent `RUN --mount=type=cache` directories (podman-image-prune.1.md:20-22).
4. Host temp-dir lifecycle: the `--mount=type=cache` dir lives in the temp dir ("Lifecycle of following directory will be inherited from how host machine treats temporary directory", volumes.go:40-42).
5. Any **declared** ARG value changing between builds — busts every RUN layer after the arg's declaration, consumed or not (stage_executor.go:2192-2194, 2277-2280; today's CACHEBUST relies on exactly this). Conversely: a generated per-set Dockerfile that declares per-toolchain ARGs must scope each version ARG directly above its own fragment, or one version bump busts every later toolchain's RUNs.
6. COPY/ADD content or context changes: content digest in history + diffIDs change → miss (stage_executor.go:1344-1355, 1817-1842, 2286-2295).
7. Base image change (base history/diffIDs mismatch, 2704-2737).
8. Any text change in a shared instruction (history equality on CreatedBy/Comment/Author/EmptyLayer/Created, 2085-2108).
9. Arch/OS/manifest-type mismatch (2716-2737) or `--cache-ttl` expiry (2662-2673).
10. `--no-cache-filter` targeting a stage (stage_executor.go:1392-1396) — but note it is unreleased in any podman/buildah version as of 2026-11 (docs on `main` only; see §4).
11. Intermediates are untagged; a `podman build` of a *different* set with overlapping prefixes still hits only if instruction text/history/content match (by design).
12. Design-level: with strategy (a), cross-set sharing is prefix-ordered — a toolchain added "in the middle" of the global order re-executes that toolchain's steps on first build (the prefix below it must be cached); strategy (b) shifts this to component builds (§5 facts).

## Facts feed for the composition decision (strategy a vs b — facts only)

**Strategy (a) — generated per-set Dockerfiles, one layer per toolchain, `podman build` (prefix reuse):**
- Relies on: podman default `--layers=true` → store-scan reuse across separately invoked builds (options/layers.md; stage_executor.go:1393, 2649).
- Cost model: first build of toolchain T on a machine executes T's steps; every later build that shares the prefix hits the store cache; "adding an already-built toolchain to a workspace" costs the workspace COPY + commit only; "a never-built toolchain costs exactly one build per machine" then is reused by every workspace thereafter (same machine, same store).
- The shared prefix is matched by: identical instruction text, identical base-image history, identical content digests for COPY/ADD. Generated Dockerfiles must keep the global canonical toolchain order so prefixes line up; and since every RUN's key embeds all declared args' values (§ exec summary 3), a version bump of toolchain k busts the cache from toolchain k's ARG line onward — earlier toolchains stay cached; if per-toolchain ARGs are hoisted to the top instead, one bump busts every toolchain's RUNs. Inline pinned versions (no ARGs at all) keep every other toolchain's layers stable.
- Pruning risks: intermediates are untagged but pruneable (§1.4).

**Strategy (b) — component images (tagged), set images via `COPY --from=<image>`/chained `FROM`:**
- Component image built once, tagged: costs `podman build` + tag; visible in `podman images` (so must be managed; unused ones pruned by `podman image prune --all`).
- Adding to a workspace: per-workspace COPY of the component rootfs + commit — no toolchain step re-execution; `COPY --from` is files-only (§3.1).
- Facts/risks from citations: (i) `COPY --from` does not carry component image metadata — ENV/ENTRYPOINT/LABELS must come from the stage builder (stage_executor.go:2781) or from using `FROM <component>` for metadata-sensitive composition; (ii) dpkg status is a single file — merging is last-write-wins per file (buildah-copy.1.md --from semantics; Containerfile.5.md:386-394); if components share status files, COPY order matters; (iii) same-path files across components: later COPY wins; (iv) image-size duplication: `podman images` SIZE counts per-image layer size (podman-images.1.md.in:68), and `podman system df -v` exposes SHARED/UNIQUE SIZE with a RECLAIMABLE caveat (podman-system-df.1.md:12-14, 53-54); (v) `COPY --from=<image>` external refs supported by buildah since at least v1.7 (CHANGELOG "Handle 'COPY --from' in Dockerfile", buildah-CHANGELOG.md; "imagebuildah: handle ID mappings for COPY --from" v1.8.4; "Fix ownership of content copied using COPY --from" v1.16.0).
- Both strategies are compatible: (b)'s tagged components are also the natural `--cache-to`/`--cache-from` sources, and (a) can copy component rootfs via `COPY --from` when a toolchain was built as a component image.

**Neutral comparison, no recommendation:** (a) has no per-image bookkeeping and no size duplication, but depends on podman's `--layers` default (true on podman, false on buildah — §5.2 floor evidence) and on prefix text stability; (b) gives explicit, tagged, prunable artifacts and per-workspace isolation, at the cost of per-image size duplication and merge-order sensitivity for shared files.
