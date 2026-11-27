#!/usr/bin/env bash
# tests/test-build-images.sh — end-to-end tests for build-images.sh with a
# PATH-mocked podman (and df) that log their arguments: the successor pattern
# of tests/test-build-container.sh. Scope here is the --base flag (ticket
# #198): flag contract, generation determinism, podman-arg pins, preflight
# refusals and lint-refusal propagation. --tc/--set/--all arrive with tickets
# #199/#201; the old build-container tests die only at cutover (#215).
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

# Fixture-dist directory: the builder plus its two helpers and the real
# toolchains tree (base.Dockerfile is exercised as shipped).
fixture_dist="$test_home/dist"
mkdir -p "$fixture_dist/scripts"
cp "$PROJECT_ROOT/dist/scripts/build-images.sh" "$fixture_dist/scripts/"
cp "$PROJECT_ROOT/dist/scripts/lib-identity.sh" "$fixture_dist/scripts/"
cp "$PROJECT_ROOT/dist/scripts/toolchains.sh" "$fixture_dist/scripts/"
cp "$PROJECT_ROOT/dist/scripts/dump.sh" "$fixture_dist/scripts/"
cp -r "$PROJECT_ROOT/dist/toolchains" "$fixture_dist/toolchains"

# Generated-Dockerfile directory determinism: the builder writes per-component
# generation dirs under TMPDIR keyed by id + component hash — pin TMPDIR into
# the test home so runs are isolated and paths assertable.
export TMPDIR="$test_home"

base_dockerfile="$fixture_dist/toolchains/base.Dockerfile"
expected_tag="opencode-sandbox-base:$(sha256sum "$base_dockerfile" | cut -c1-12)"

# Component tags via the same identity lib the builder uses (the golden-vector
# fixture pins it to the Go side; here it is the independent expectation).
# shellcheck source=../dist/scripts/lib-identity.sh
source "$PROJECT_ROOT/dist/scripts/lib-identity.sh"
component_tag() { tc_component_tag "$base_dockerfile" "$1" "$fixture_dist/toolchains/recipes/$1.Dockerfile"; }

# --- mocks -----------------------------------------------------------------------

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"

# podman mock: logs every invocation's args (one line each) into $PODMAN_LOG.
# PODMAN_VERSION overrides the --version answer; the tags named in
# PODMAN_MISSING_IMAGES fail `image exists` (all others exist). `build -q`
# answers PODMAN_STAGE_ID; `images --filter` answers PODMAN_IMAGES (tags, one
# per line); `image inspect` answers PODMAN_IMAGE_SIZE; `image mount` answers
# PODMAN_MOUNT_DIR (the test places the dump's tc.pkgs there).
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
case "$1" in
  --version)
    printf '%s\n' "${PODMAN_VERSION:-podman version 4.9.3}"
    exit 0
    ;;
  build)
    case " $* " in
      *" -q "*) printf '%s\n' "${PODMAN_STAGE_ID:-sha256:mockstage0}" ;;
    esac
    exit 0
    ;;
  images)
    [[ -n "${PODMAN_IMAGES:-}" ]] && printf '%s\n' "$PODMAN_IMAGES"
    exit 0
    ;;
esac
case "$1 $2" in
  "image exists")
    case " ${PODMAN_MISSING_IMAGES:-} " in
      *" $3 "*) exit 1 ;;
    esac
    exit 0
    ;;
  "image inspect")
    printf '%s\n' "${PODMAN_IMAGE_SIZE:-1073741824}"
    exit 0
    ;;
  "image mount")
    printf '%s\n' "${PODMAN_MOUNT_DIR:-}"
    exit 0
    ;;
esac
exit 0
EOF
chmod +x "$fake_bin/podman"

# df mock: mirrors GNU df's header + value output, answering
# $DF_AVAIL_BYTES (defaults to 100 GiB — plenty above the base budget; the
# refusal tests shrink it).
cat > "$fake_bin/df" <<'EOF'
#!/usr/bin/env bash
printf 'Avail\n%s\n' "${DF_AVAIL_BYTES:-107374182400}"
exit 0
EOF
chmod +x "$fake_bin/df"

export PATH="$fake_bin:/usr/bin:/bin"
export PODMAN_LOG="$test_home/podman.log"
export TOOLCHAINS_ROOT="$fixture_dist/toolchains"

script="$fixture_dist/scripts/build-images.sh"

run_script() {
  ( cd "$test_home" && bash "$script" "$@" > "$test_home/out.log" 2> "$test_home/err.log" )
}

# run_script_failing CMD… — asserts the builder refuses (exit non-zero)
run_script_failing() {
  if run_script "$@"; then
    printf 'build-images unexpectedly succeeded: %s\n' "$*" >&2
    exit 1
  fi
}

# --- 1: flag contract -------------------------------------------------------------

: > "$PODMAN_LOG"
if run_script; then
  printf 'no-argument invocation unexpectedly succeeded\n' >&2
  exit 1
fi
grep -q 'Usage: build-images.sh' "$test_home/err.log"
if run_script --nonsense; then
  printf 'unknown flag unexpectedly succeeded\n' >&2
  exit 1
fi
grep -q 'Usage: build-images.sh' "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
printf 'flag contract test passed\n'

# --- 2: --base builds the content-addressed tag with the pinned podman args -------

: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="$expected_tag" run_script --base
grep -q -- "image exists $expected_tag" "$PODMAN_LOG"
grep -q -- "build --network host -t $expected_tag -f $fixture_dist/toolchains/base.Dockerfile $fixture_dist/toolchains" "$PODMAN_LOG"
if grep -q -- 'CACHEBUST' "$PODMAN_LOG"; then
  printf 'base build still passes CACHEBUST\n' >&2
  exit 1
fi
printf 'base build tag and podman-arg pins test passed\n'

# --- 3: generation determinism — two runs log byte-identical podman calls ---------

: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="$expected_tag" run_script --base
cp "$PODMAN_LOG" "$test_home/log1"
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="$expected_tag" run_script --base
cmp "$test_home/log1" "$PODMAN_LOG"
printf 'determinism test passed\n'

# --- 4: cached-skip when the tag exists --------------------------------------------

: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="" run_script --base
grep -q -- "image exists $expected_tag" "$PODMAN_LOG"
if grep -q '^build ' "$PODMAN_LOG"; then
  printf 'existing base image was rebuilt\n' >&2
  exit 1
fi
grep -q 'already exists — skipping' "$test_home/out.log"
printf 'cached-skip test passed\n'

# --- 5: podman floor preflight -----------------------------------------------------

: > "$PODMAN_LOG"
PODMAN_VERSION="podman version 3.4.2" \
  PODMAN_MISSING_IMAGES="$expected_tag" run_script_failing --base
grep -q 'podman >= 4.0 is required (found 3.4.2)' "$test_home/err.log"
grep -q 'update podman' "$test_home/err.log"
# Refusal happens before anything but the version probe.
[[ "$(wc -l < "$PODMAN_LOG")" -eq 1 ]]
grep -q '^--version$' "$PODMAN_LOG"
printf 'podman floor preflight test passed\n'

# --- 6: disk preflight refuses below estimate + 20 % headroom ----------------------

: > "$PODMAN_LOG"
DF_AVAIL_BYTES=1000000000 PODMAN_MISSING_IMAGES="$expected_tag" \
  run_script_failing --base
grep -q 'refusing to build' "$test_home/err.log"
grep -q '0.9 GiB free' "$test_home/err.log"
grep -q '2.4 GiB needed' "$test_home/err.log"
grep -q 'podman image prune' "$test_home/err.log"
# The refusal happens after the cached-skip probe, but before any build.
if grep -q '^build ' "$PODMAN_LOG"; then
  printf 'disk-refused build reached podman\n' >&2
  exit 1
fi
# Exactly estimate + 20 % headroom free passes the gate and builds.
: > "$PODMAN_LOG"
DF_AVAIL_BYTES=2576980378 PODMAN_MISSING_IMAGES="$expected_tag" run_script --base
grep -q -- "build --network host -t $expected_tag" "$PODMAN_LOG"
printf 'disk preflight refusal and headroom test passed\n'

# --- 7: lint-refusal propagation (no podman call on an invalid base recipe) --------

# 7a: ARG is refused
cp "$base_dockerfile" "$test_home/base.bak"
printf 'ARG FOO=1\n' >> "$base_dockerfile"
: > "$PODMAN_LOG"
if run_script --base; then
  printf 'base recipe with ARG unexpectedly built\n' >&2
  exit 1
fi
grep -q 'ARG' "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
cp "$test_home/base.bak" "$base_dockerfile"

# 7b: a floating pip install rides the same pin lint
sed -i 's/pytest==9.2.0 pytest-cov==7.1.0/pytest/' "$base_dockerfile"
: > "$PODMAN_LOG"
if run_script --base; then
  printf 'base recipe with floating pip install unexpectedly built\n' >&2
  exit 1
fi
grep -q 'not pinned' "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
cp "$test_home/base.bak" "$base_dockerfile"

# 7c: a floating FROM is refused
sed -i 's/^FROM ubuntu:24.04$/FROM ubuntu:latest/' "$base_dockerfile"
: > "$PODMAN_LOG"
if run_script --base; then
  printf 'base recipe with floating FROM unexpectedly built\n' >&2
  exit 1
fi
grep -q "FROM ubuntu:24.04" "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
cp "$test_home/base.bak" "$base_dockerfile"
printf 'lint-refusal propagation tests passed\n'

# --- 8: the shipped base recipe carries the §3 keeps and the §1 shape --------------

dockerfile="$PROJECT_ROOT/dist/toolchains/base.Dockerfile"
# Content checks scan non-comment lines only (comments may name purged packages).
code="$(grep -vE '^[[:space:]]*#' "$dockerfile")"
[[ "$(sed -n '1p' "$dockerfile")" == '# syntax=docker/dockerfile:1' ]]
grep -q '^FROM ubuntu:24.04$' <<< "$code"
if grep -qE '^[[:space:]]*ARG([[:space:]]|$)' <<< "$code"; then
  printf 'base recipe ships an ARG\n' >&2
  exit 1
fi
if grep -qE '^[[:space:]]*ENTRYPOINT' <<< "$code"; then
  printf 'base recipe ships an ENTRYPOINT — the composition footer owns it\n' >&2
  exit 1
fi
user_line=$(grep -n '^USER dev$' "$dockerfile" | cut -d: -f1)
workdir_line=$(grep -n '^WORKDIR ' "$dockerfile" | cut -d: -f1)
create_line=$(grep -n '&& useradd' "$dockerfile" | cut -d: -f1)
[[ -n "$user_line" && -n "$workdir_line" && "$create_line" -lt "$user_line" && "$user_line" -lt "$workdir_line" ]]
for keep in 'GLAB_CONFIG_DIR=' 'GH_CONFIG_DIR=' 'safe.directory' 'profile.d' \
            'ca-certificates curl wget git' 'python3-pip python3-venv' \
            'jq unzip openssh-client' 'gh pulseaudio-utils' \
            'pytest==9.2.0 pytest-cov==7.1.0' 'mcp-searxng@2.12.0' \
            'setup_22.x'; do
  grep -q "$keep" <<< "$code" || {
    printf 'base recipe lost its keep: %s\n' "$keep" >&2
    exit 1
  }
done
# Purged to units (§3): compilers → cpp, usbutils/libusb → usb, gnupg → transient.
for purged in 'build-essential' 'usbutils' 'libusb' 'gnupg' 'cmake'; do
  if grep -q "$purged" <<< "$code"; then
    printf 'base recipe still installs purged package: %s\n' "$purged" >&2
    exit 1
  fi
done
# Pins: no floating URLs, no release-API queries.
if grep -qE '/latest(/|$)|releases/latest|api/v4/projects/gitlab-org' <<< "$code"; then
  printf 'base recipe still floats a version on latest\n' >&2
  exit 1
fi
for pin in 'glab_1.133.0_linux_amd64' 'v1.133.0' \
           'releases/download/v2.4.2/opencode-linux-x64.tar.gz' \
           'releases/download/v0.12.1/codebase-memory-mcp-ui-linux-amd64.tar.gz'; do
  grep -q "$pin" <<< "$code" || {
    printf 'base recipe lost a pin: %s\n' "$pin" >&2
    exit 1
  }
done
printf 'base keeps and shape tests passed\n'

# --- 9: --tc component builds (ticket #199) ------------------------------------------

go_tag="$(component_tag go)"
go_hash12="${go_tag#*:}"
go_gen="$test_home/oc-build-gen-$(id -u)/tc-go-$go_hash12"

# 9a: unknown / empty ids refuse before any podman call
: > "$PODMAN_LOG"
run_script_failing --tc nosuch-toolchain
grep -q "unknown toolchain id 'nosuch-toolchain'" "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
if run_script --tc; then
  printf -- '--tc without a value unexpectedly succeeded\n' >&2
  exit 1
fi
grep -q 'Usage: build-images.sh' "$test_home/err.log"
if run_script --tc ""; then
  printf -- '--tc with an empty list unexpectedly succeeded\n' >&2
  exit 1
fi
printf -- '--tc flag contract (unknown/empty ids) test passed\n'

# 9b: a component build with a missing base builds the base first, then the
# component in two stages (recipe build → label build), --network host pinned
: > "$PODMAN_LOG"
mnt="$test_home/mnt"
mkdir -p "$mnt/oc-dump"
printf 'golang-go\n' > "$mnt/oc-dump/tc.pkgs"
PODMAN_MISSING_IMAGES="$expected_tag $go_tag" PODMAN_MOUNT_DIR="$mnt" \
  run_script --tc go
grep -q -- "build --network host -t $expected_tag -f $fixture_dist/toolchains/base.Dockerfile" "$PODMAN_LOG"
grep -q -- "build -q --network host -t $go_tag -f $go_gen/Dockerfile $go_gen" "$PODMAN_LOG"
grep -q -- "image mount sha256:mockstage0" "$PODMAN_LOG"
grep -q -- "image umount sha256:mockstage0" "$PODMAN_LOG"
grep -q -- "build --network host -t $go_tag -f $go_gen/label.Dockerfile $go_gen" "$PODMAN_LOG"
# the label carries the tc.pkgs names read from the built image's dump
grep -q '^FROM sha256:mockstage0$' "$go_gen/label.Dockerfile"
grep -q '^LABEL tc.pkgs="golang-go"$' "$go_gen/label.Dockerfile"
printf 'component build stages, tag and podman-arg pins test passed\n'

# 9c: the generated component Dockerfile wraps the fragment with the injected
# dump step: FROM base:hash12 → USER root → snapshot → fragment → USER root → dump
[[ "$(sed -n '1p' "$go_gen/Dockerfile")" == '# syntax=docker/dockerfile:1' ]]
grep -q -- "FROM opencode-sandbox-base:$(sha256sum "$base_dockerfile" | cut -c1-12)" "$go_gen/Dockerfile"
grep -q '^COPY dump.sh /tmp/dump.sh$' "$go_gen/Dockerfile"
grep -q '^COPY oc-extras.txt /tmp/oc-extras.txt$' "$go_gen/Dockerfile"
grep -q '^RUN /tmp/dump.sh snapshot$' "$go_gen/Dockerfile"
grep -q '^RUN apt-get update && apt-get install -y --no-install-recommends golang-go \\' "$go_gen/Dockerfile"
grep -q '^RUN /tmp/dump.sh dump /$' "$go_gen/Dockerfile"
# the injected footer re-roots after the fragment (the fragment may switch to
# dev and may carry its own USER root lines)
[[ "$(tail -n 2 "$go_gen/Dockerfile" | head -n 1)" == 'USER root' ]]
[[ "$(tail -n 1 "$go_gen/Dockerfile")" == 'RUN /tmp/dump.sh dump /' ]]
printf 'generated component Dockerfile shape test passed\n'

# 9d: determinism — two renders invoke podman byte-identically and re-render
# the generated Dockerfile byte-identically
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="$expected_tag $go_tag" PODMAN_MOUNT_DIR="$mnt" \
  run_script --tc go
cp "$PODMAN_LOG" "$test_home/log-tc1"
cp "$go_gen/Dockerfile" "$test_home/gen-tc1"
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="$expected_tag $go_tag" PODMAN_MOUNT_DIR="$mnt" \
  run_script --tc go
cmp "$test_home/log-tc1" "$PODMAN_LOG"
cmp "$test_home/gen-tc1" "$go_gen/Dockerfile"
printf 'component determinism test passed\n'

# 9e: cached-skip when the component tag exists
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="" run_script --tc go
grep -q -- "image exists $go_tag" "$PODMAN_LOG"
grep -q "$go_tag already exists — skipping" "$test_home/out.log"
if grep -q '^build' "$PODMAN_LOG"; then
  printf 'existing component image was rebuilt\n' >&2
  exit 1
fi
printf 'component cached-skip test passed\n'

# 9f: comma lists build several components in canonical (sorted) order and
# deduplicate
: > "$PODMAN_LOG"
usb_tag="$(component_tag usb)"
PODMAN_MISSING_IMAGES="$go_tag $usb_tag" run_script --tc usb,go,go
go_line=$(grep -n -- "build -q --network host -t $go_tag " "$PODMAN_LOG" | cut -d: -f1)
usb_line=$(grep -n -- "build -q --network host -t $usb_tag " "$PODMAN_LOG" | cut -d: -f1)
[[ -n "$go_line" && -n "$usb_line" && "$go_line" -lt "$usb_line" ]]
[[ "$(grep -c -- "build -q --network host -t $go_tag " "$PODMAN_LOG")" -eq 1 ]]
printf 'comma list + canonical order + dedupe test passed\n'

# 9g: disk preflight — a missing component estimates at the 1 GiB floored budget
# with the +20 % headroom refusal
: > "$PODMAN_LOG"
DF_AVAIL_BYTES=1100000000 PODMAN_MISSING_IMAGES="$go_tag" \
  run_script_failing --tc go
grep -q 'refusing to build' "$test_home/err.log"
grep -q '1.0 GiB free' "$test_home/err.log"
grep -q '1.2 GiB needed (1.0 GiB estimate + 20% headroom)' "$test_home/err.log"
grep -q 'podman image prune' "$test_home/err.log"
if grep -q '^build' "$PODMAN_LOG"; then
  printf 'disk-refused component build reached podman\n' >&2
  exit 1
fi
# the exact headroom boundary passes and builds
: > "$PODMAN_LOG"
DF_AVAIL_BYTES=1288490188 PODMAN_MISSING_IMAGES="$go_tag" run_script --tc go
grep -q -- "build -q --network host -t $go_tag" "$PODMAN_LOG"
printf 'component disk preflight test passed\n'

# 9h: a same-id image (stale hash) sizes the estimate instead of the budget
# formula
: > "$PODMAN_LOG"
DF_AVAIL_BYTES=3500000000 PODMAN_MISSING_IMAGES="$go_tag" \
  PODMAN_IMAGES="opencode-sandbox-tc-go:000000000000" PODMAN_IMAGE_SIZE=3221225472 \
  run_script_failing --tc go
grep -q '3.6 GiB needed (3.0 GiB estimate + 20% headroom)' "$test_home/err.log"
grep -q -- 'images --filter reference=opencode-sandbox-tc-go' "$PODMAN_LOG"
grep -q -- 'image inspect --format {{.Size}} opencode-sandbox-tc-go:000000000000' "$PODMAN_LOG"
: > "$PODMAN_LOG"
DF_AVAIL_BYTES=3865470566 PODMAN_MISSING_IMAGES="$go_tag" \
  PODMAN_IMAGES="opencode-sandbox-tc-go:000000000000" PODMAN_IMAGE_SIZE=3221225472 \
  run_script --tc go
grep -q -- "build -q --network host -t $go_tag" "$PODMAN_LOG"
printf 'same-id image size estimate test passed\n'

# --- 10: --all = base + every discovered component, invalid recipes refuse -----------

: > "$PODMAN_LOG"
all_missing="$expected_tag"
for f in "$fixture_dist"/toolchains/recipes/*.Dockerfile; do
  id="$(basename "$f")"; id="${id%.Dockerfile}"
  all_missing="$all_missing $(component_tag "$id")"
done
PODMAN_MISSING_IMAGES="$all_missing" run_script --all
[[ "$(grep -c '^build ' "$PODMAN_LOG")" -eq 43 ]] # base once + 21×(recipe + label)
first_build=$(grep -n -- "build --network host -t $expected_tag" "$PODMAN_LOG" | cut -d: -f1)
[[ -n "$first_build" ]]
prev=0
for f in "$fixture_dist"/toolchains/recipes/*.Dockerfile; do
  id="$(basename "$f")"; id="${id%.Dockerfile}"
  line=$(grep -n -- "build -q --network host -t $(component_tag "$id") " "$PODMAN_LOG" | cut -d: -f1)
  [[ -n "$line" && "$line" -gt "$prev" ]]
  prev=$line
done
printf -- '--all enumeration test passed\n'

# --all with an invalid recipe refuses naming it, before any podman call
printf 'ARG NOPE=1\n' >> "$fixture_dist/toolchains/recipes/go.Dockerfile"
: > "$PODMAN_LOG"
run_script_failing --all
grep -q "toolchain 'go' is invalid" "$test_home/err.log"
grep -q 'ARG' "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
# the same refusal propagates to a direct --tc
run_script_failing --tc go
grep -q "toolchain 'go' is invalid" "$test_home/err.log"
[[ "$(wc -l < "$PODMAN_LOG")" -eq 0 ]]
cp "$PROJECT_ROOT/dist/toolchains/recipes/go.Dockerfile" "$fixture_dist/toolchains/recipes/go.Dockerfile"
printf 'lint-refusal propagation to --tc/--all test passed\n'

# --all / --tc flag conflicts refuse with the usage
if run_script --all --tc go; then
  printf -- '--all --tc unexpectedly succeeded\n' >&2
  exit 1
fi
if run_script --all --base; then
  printf -- '--all --base unexpectedly succeeded\n' >&2
  exit 1
fi
printf -- '--all flag conflict test passed\n'

printf 'build-images tests passed\n'
