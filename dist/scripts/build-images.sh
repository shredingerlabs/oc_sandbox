#!/usr/bin/env bash
# build-images.sh — the toolchain-sets builder (spec "toolchain sets" §7).
#
# Born with --base (ticket #198): builds the content-addressed base image
# opencode-sandbox-base:<hash12> from the pinned base recipe
# toolchains/base.Dockerfile (FROM ubuntu:24.04 inline, no ARG — the same pin
# lint as the unit recipes applies to its RUN/ENV/USER lines). The base recipe
# hash feeds every component and set identity (lib-identity.sh), so any base
# edit re-addresses the whole family at the next build. An existing tag is
# never rebuilt — cached-skip; delete the image to force.
#
# Preflight, before any build: podman >= 4.0 floor (RUN cache mounts) and a
# disk estimate + 20 % headroom refuse-with-reason.
#
# Later tickets: --tc <ids> (#199, generated component Dockerfiles), --set and
# --all (#201, assembly), and the pre-build network probe (#212, §7 item 3 —
# the seam is _network_probe below).
#
# Usage:
#   build-images.sh --base     build the base image (skip when the tag exists)
#   build-images.sh -h|--help  show the help
#
# All user-facing strings are English (binding).

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib-identity.sh
source "$script_dir/lib-identity.sh"

usage() {
  cat <<'EOF'
Usage: build-images.sh --base

Builds the content-addressed base image opencode-sandbox-base:<hash12> from
the pinned base recipe (toolchains/base.Dockerfile). An existing tag is
skipped — delete the image to force a rebuild.

  --base        build the base image
  -h, --help    show this help

(--tc / --set / --all arrive with later tickets.)
EOF
}

die() {
  printf 'build-images: %s\n' "$*" >&2
  exit 1
}

toolchains_root() {
  if [[ -n "${TOOLCHAINS_ROOT:-}" ]]; then
    printf '%s\n' "$TOOLCHAINS_ROOT"
  else
    printf '%s\n' "$script_dir/../toolchains"
  fi
}

# --- base recipe lint -------------------------------------------------------------

# The base is fixed, not a unit (§1), but carries the same pin lint as the unit
# recipes: pinned FROM, no ARG, and the shared RUN/ENV/USER rules (pins,
# ENV PATH, profile writes) via toolchains.sh. Its FROM/WORKDIR/ENTRYPOINT
# lines are base-only and are filtered out before the shared lint runs.
lint_base() {
  local file=$1 arg from errs tmp
  arg="$(grep -nE '^[[:space:]]*ARG([[:space:]]|$)' "$file" || true)"
  [[ -z "$arg" ]] || die "base recipe uses ARG (line ${arg%%:*}) — the base pins everything inline; build args bake out-of-band state invisible to the content hash"
  from="$(grep -m1 -E '^[[:space:]]*FROM([[:space:]]|$)' "$file" | xargs)"
  [[ "$from" == "FROM ubuntu:24.04" ]] || die "base recipe must pin its source image inline as 'FROM ubuntu:24.04' — found: '${from:-none}'"
  tmp="$(mktemp)"
  grep -vE '^[[:space:]]*(FROM|WORKDIR|ENTRYPOINT)([[:space:]]|$)' "$file" > "$tmp"
  if ! errs="$("$script_dir/toolchains.sh" lint "$tmp" 2>&1)"; then
    rm -f "$tmp"
    die "base recipe failed the recipe lint:
$errs"
  fi
  rm -f "$tmp"
}

# --- preflight (§7; runs before any build) -----------------------------------------

# Rootless podman stores images under $HOME/.local/share/containers, so $HOME's
# filesystem carries the physical free-space number. The Go-side preflight
# (ticket #212) refines the storage-dir resolution.
free_space_bytes() {
  df -B1 --output=avail "$HOME" | awk 'NF { v = $1 } END { print v }'
}

podman_floor() {
  local ver major
  ver="$(podman --version 2>/dev/null)" \
    || die "podman is not available — install podman >= 4.0 and retry"
  ver="${ver#podman version }"
  major="${ver%%.*}"
  [[ "$major" =~ ^[0-9]+$ ]] \
    || die "unparsable podman version '$ver' — podman >= 4.0 is required"
  (( major >= 4 )) \
    || die "podman >= 4.0 is required (found ${ver}) — toolchain builds use RUN cache mounts; update podman and retry"
}

# Base-build budget (§7 item 2): the known base size ballpark. The component
# and set tickets refine the estimate with per-image sizes and the payload
# formula; the 20 % headroom refusal already keeps honest here.
BASE_DISK_BUDGET_BYTES=$((2 * 1024 * 1024 * 1024))

disk_preflight() {
  local free need free_gib need_gib
  need=$((BASE_DISK_BUDGET_BYTES + BASE_DISK_BUDGET_BYTES / 5)) # + 20 % headroom
  free="$(free_space_bytes "$HOME")"
  [[ "$free" =~ ^[0-9]+$ ]] || die "could not read free disk space — refusing to build"
  if ((free < need)); then
    free_gib="$(awk -v b="$free" 'BEGIN { printf "%.1f", b / 1073741824 }')"
    need_gib="$(awk -v b="$need" 'BEGIN { printf "%.1f", b / 1073741824 }')"
    die "refusing to build: ${free_gib} GiB free is below the ${need_gib} GiB needed (2.0 GiB estimate + 20% headroom) — free space or run Clean intermediates (podman image prune), then retry"
  fi
}

# _network_probe — seam for §7 item 3, ticket #212: one HEAD per distinct
# source host of the plan's download needs (host-proxy-env aware, short
# timeout), honored before any build; offline hosts get a refusal naming the
# unreachable hosts. Until #212 this is a no-op and an offline host fails at
# the build itself.
_network_probe() { :; }

# --- build -------------------------------------------------------------------------

build_base() {
  local root file tag
  root="$(toolchains_root)"
  file="$root/base.Dockerfile"
  [[ -f "$file" ]] || die "base recipe not found: $file — is the toolchains tree installed?"
  lint_base "$file"
  podman_floor
  tag="$(tc_base_tag "$file")"
  if podman image exists "$tag"; then
    printf 'build-images: base image %s already exists — skipping (delete the image to force a rebuild)\n' "$tag"
    return 0
  fi
  disk_preflight
  _network_probe
  printf 'build-images: building %s\n' "$tag"
  podman build --network host -t "$tag" -f "$file" "$root"
  printf 'build-images: built %s\n' "$tag"
}

main() {
  if (($# == 0)); then
    usage >&2
    exit 2
  fi
  case "$1" in
    --base)
      shift
      (($# == 0)) || {
        usage >&2
        exit 2
      }
      build_base
      ;;
    -h | --help)
      usage
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
}

main "$@"
