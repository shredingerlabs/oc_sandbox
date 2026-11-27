#!/usr/bin/env bash
# build-images.sh — the toolchain-sets builder (spec "toolchain sets" §4/§7).
#
# --base (#198): builds the content-addressed base image
# opencode-sandbox-base:<hash12> from the pinned base recipe
# toolchains/base.Dockerfile (FROM ubuntu:24.04 inline, no ARG — the same pin
# lint as the unit recipes applies to its RUN/ENV/USER lines). The base recipe
# hash feeds every component and set identity (lib-identity.sh), so any base
# edit re-addresses the whole family at the next build.
#
# --tc <id>[,<id>…] / --all (#199): builds toolchain component images
# opencode-sandbox-tc-<id>:<hash12> (component hash = recipe content + base
# recipe hash). The generated component Dockerfile wraps the recipe fragment
# with the injected dump step (dist/scripts/dump.sh, never fragment
# boilerplate): FROM base:hash12 → USER root → dpkg/pip pre-fragment snapshot
# → the fragment → re-injected USER root → the dump (payload archive = dpkg
# status-diff ∪ pip RECORD paths ∪ '# extra:' paths ∪ /home/dev, tarred
# --no-recursion with an existence filter; PM-state dump for the stanza-union
# merge; the tc.pkgs name list). The tc.pkgs label is attached in a second
# FROM-stage build after the builder reads the dump's name list from the built
# image host-side (podman image mount — no container start). An existing tag
# is never rebuilt — cached-skip; delete the image to force. Ids are validated
# against live discovery (toolchains.sh resolve); an unknown or invalid
# fragment refuses the build.
#
# --set <ids> (#200, full-canonical assembly, spec §5 — the path every start
# flow calls): assembles opencode-sandbox-set:<hash12> (set hash = sorted
# (id, recipe content hash) pairs + base recipe hash, lib-identity.sh). The
# generated set Dockerfile is FROM base:hash12 → USER root → the COPY'd merge
# core (merge-pm.py) → each member's payload archive + PM dump COPY'd
# --from=opencode-sandbox-tc-<id>:<hash12> in alphabetical id order (plus the
# member's builder-rendered ENV pairs file when its fragment declares ENVs) →
# ONE RUN: extract in canonical order, stanza-union the PM state (package
# stanzas deduped by name, info entries unioned, last canonical writer wins a
# contested package), re-emit the fragment ENVs (duplicate keys:
# alphabetical-last wins), then ldconfig and the staging-tree removal → the
# uniform footer USER dev + ENTRYPOINT ["/bin/bash","-l"]. Labels: kind, the
# sorted member list, the member recipe content hashes, the merged tc.pkgs
# union (read host-side from the member images via podman image inspect — no
# container start). Missing base/components are built first (lazy); an
# existing set tag is never rebuilt — cached-skip. Assemblies pin
# --network=none (structural honesty: no recipe or merge step can phone home);
# an assembly-only plan skips the download-needing network probe seam.
#
# Preflight, before any build: podman >= 4.0 floor (RUN cache mounts) and a
# disk estimate + 20 % headroom refuse-with-reason (missing components only,
# lazy; same-id existing image size where one exists, else the budget formula
# floored at 1 GiB).
#
# Later tickets: the pre-build network probe (#212, §7 item 3 — the seam is
# _network_probe below).
#
# Usage:
#   build-images.sh --base              build the base image
#   build-images.sh --tc <id>[,<id>…]   build toolchain components
#   build-images.sh --set <ids>         assemble a set image ("" = base-only)
#   build-images.sh --all               build base + every discovered component
#   build-images.sh -h|--help           show the help
#
# All user-facing strings are English (binding).

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib-identity.sh
source "$script_dir/lib-identity.sh"

usage() {
  cat <<'EOF'
Usage: build-images.sh [--base] | [--tc <id>[,<id>…]] | [--set <ids>] | [--all]

Builds the content-addressed toolchain-sets images. An existing tag is never
rebuilt — the build is skipped; delete the image to force a rebuild.

  --base              build the base image
  --tc <id>[,<id>…]   build toolchain components (comma-separated ids,
                      validated against live discovery; the base image is
                      built first when it is missing)
  --set <ids>         assemble the set image opencode-sandbox-set:<hash12>
                      from comma-separated members (validated against live
                      discovery; missing base/components are built first;
                      "" assembles the base-only set)
  --all               build the base and every discovered toolchain component
  -h, --help          show this help
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

# Base-build budget (§7 item 2): the known base size ballpark.
BASE_DISK_BUDGET_BYTES=$((2 * 1024 * 1024 * 1024))

# Per-component budget when no same-id image exists on host: the §4 budget
# formula (≈ base + 2× payload) floored at 1 GiB.
COMPONENT_DISK_BUDGET_BYTES=$((1024 * 1024 * 1024))

# Set-assembly budget (§7 item 2, "sets via the formula"): the assembly's own
# layer cost — the COPY'd payload archives plus their extracted trees. The
# base and component layers are shared with images already on host (an
# assembly requires them built first, so their cost was budgeted by their own
# builds); floored at 1 GiB.
SET_DISK_BUDGET_BYTES=$((1024 * 1024 * 1024))

# component_estimate_bytes <id> — same-id existing image size where one exists
# (a stale-hash tag still sizes the rebuild honestly), else the floored budget.
component_estimate_bytes() {
  local id=$1 tag size
  tag="$(podman images --filter "reference=opencode-sandbox-tc-$id" \
    --format '{{.Repository}}:{{.Tag}}' | head -n 1)"
  if [[ -n "$tag" ]]; then
    size="$(podman image inspect --format '{{.Size}}' "$tag" 2>/dev/null || true)"
    if [[ "$size" =~ ^[0-9]+$ ]] && ((size > 0)); then
      printf '%s\n' "$size"
      return 0
    fi
  fi
  printf '%s\n' "$COMPONENT_DISK_BUDGET_BYTES"
}

# disk_preflight [estimate-bytes] — refuse-with-reason when free < estimate +
# 20 % headroom; the reason names both numbers and points at clean
# intermediates.
disk_preflight() {
  local est=${1:-$BASE_DISK_BUDGET_BYTES} free need free_gib need_gib est_gib
  need=$((est + est / 5)) # + 20 % headroom
  free="$(free_space_bytes "$HOME")"
  [[ "$free" =~ ^[0-9]+$ ]] || die "could not read free disk space — refusing to build"
  if ((free < need)); then
    free_gib="$(awk -v b="$free" 'BEGIN { printf "%.1f", b / 1073741824 }')"
    need_gib="$(awk -v b="$need" 'BEGIN { printf "%.1f", b / 1073741824 }')"
    est_gib="$(awk -v b="$est" 'BEGIN { printf "%.1f", b / 1073741824 }')"
    die "refusing to build: ${free_gib} GiB free is below the ${need_gib} GiB needed (${est_gib} GiB estimate + 20% headroom) — free space or run Clean intermediates (podman image prune), then retry"
  fi
}

# _network_probe — seam for §7 item 3, ticket #212: one HEAD per distinct
# source host of the plan's download needs (host-proxy-env aware, short
# timeout), honored before any build; offline hosts get a refusal naming the
# unreachable hosts. Until #212 this is a no-op and an offline host fails at
# the build itself.
_network_probe() { :; }

# --- base build --------------------------------------------------------------------

build_base_now() {
  local root=$1 base_file=$2 base_tag=$3
  printf 'build-images: building %s\n' "$base_tag"
  podman build --network host -t "$base_tag" -f "$base_file" "$root"
  printf 'build-images: built %s\n' "$base_tag"
}

# --- component generation and build (§4) ---------------------------------------------

# gen_dir <id> <tag> — the per-component generation directory, keyed by id +
# component hash so identical inputs always render to the same path (a re-render
# is byte-identical and the podman invocation log with it).
gen_dir() {
  local id=$1 tag=$2 hash12=${tag#*:}
  printf '%s/oc-build-gen-%s/tc-%s-%s\n' "${TMPDIR:-/tmp}" "$(id -u)" "$id" "$hash12"
}

# generate_component <dir> <id> <base-hash12> <recipe> — render the component
# Dockerfile (FROM base:hash12 → USER root → snapshot → fragment → re-injected
# USER root → dump step) plus the injected dump helper and the fragment's
# declared '# extra:' paths.
generate_component() {
  local dir=$1 id=$2 base_hash12=$3 recipe=$4
  mkdir -p "$dir"
  cp "$script_dir/dump.sh" "$dir/dump.sh"
  # the declared extras, deduped and canonically sorted
  "$script_dir/toolchains.sh" extras "$recipe" | LC_ALL=C sort -u \
    > "$dir/oc-extras.txt"
  {
    printf '# syntax=docker/dockerfile:1\n'
    printf "# Generated by build-images.sh — component build for toolchain\n"
    printf "# '%s'. Do not edit; the recipe is toolchains/{recipes,custom}/\n" "$id"
    printf '# %s.Dockerfile and is content-hashed into the tag.\n' "$id"
    printf 'FROM opencode-sandbox-base:%s\n' "$base_hash12"
    printf 'USER root\n'
    printf 'COPY dump.sh /tmp/dump.sh\n'
    printf 'COPY oc-extras.txt /tmp/oc-extras.txt\n'
    printf 'RUN /tmp/dump.sh snapshot\n'
    cat "$recipe"
    [[ -n "$(tail -c 1 "$recipe")" ]] && printf '\n'
    printf 'USER root\n'
    printf 'RUN /tmp/dump.sh dump /\n'
  } > "$dir/Dockerfile"
}

build_component() {
  local base_file=$1 id=$2 recipe=$3
  local tag base_hash12 dir stage_id mnt pkgs
  tag="$(tc_component_tag "$base_file" "$id" "$recipe")"
  base_hash12="$(tc_base_hash12 "$base_file")"
  dir="$(gen_dir "$id" "$tag")"
  generate_component "$dir" "$id" "$base_hash12" "$recipe"
  printf 'build-images: building %s\n' "$tag"
  stage_id="$(podman build -q --network host -t "$tag" -f "$dir/Dockerfile" "$dir" | tail -n 1)"
  # read the dump's tc.pkgs name list host-side from the built image — a
  # filesystem mount, never a container start
  pkgs=""
  if mnt="$(podman image mount "$stage_id" 2>/dev/null)" && [[ -n "$mnt" ]]; then
    [[ -f "$mnt/oc-dump/tc.pkgs" ]] && pkgs="$(paste -sd ' ' "$mnt/oc-dump/tc.pkgs")"
    podman image umount "$stage_id" >/dev/null 2>&1 || true
  fi
  {
    printf 'FROM %s\n' "$stage_id"
    printf 'LABEL tc.pkgs="%s"\n' "$pkgs"
  } > "$dir/label.Dockerfile"
  podman build --network host -t "$tag" -f "$dir/label.Dockerfile" "$dir"
  printf 'build-images: built %s\n' "$tag"
}

# --- plan (flag semantics + preflight + builds) ---------------------------------------

# resolve_id <root> <id> — print the recipe path; refuse unknown/invalid ids
# with the live discovery's reason.
resolve_id() {
  local root=$1 id=$2 out
  out="$("$script_dir/toolchains.sh" resolve "$root" "$id" 2>&1)" \
    || die "refusing to build — $out"
  printf '%s\n' "$out"
}

# discovered_valid_ids <root> — every validly-discovered unit id, canonically
# sorted; an invalid recipe refuses (Build all never silently skips one).
discovered_valid_ids() {
  local root=$1 kind id status reason
  while IFS=$'\t' read -r kind id status reason; do
    [[ "$kind" == "unit" ]] || continue
    if [[ "$status" != "valid" ]]; then
      die "refusing to build — toolchain '$id' is invalid: $reason (fix or remove the recipe, then retry)"
    fi
    printf '%s\n' "$id"
  done < <("$script_dir/toolchains.sh" discover "$root")
}

build_plan() {
  local want_base=$1 want_all=$2
  shift 2
  local -a ids=()
  local root base_file base_tag id recipe tag est
  root="$(toolchains_root)"
  base_file="$root/base.Dockerfile"
  [[ -f "$base_file" ]] || die "base recipe not found: $base_file — is the toolchains tree installed?"

  if ((want_all)); then
    # discovered through a file so a refusal inside the enumeration kills the
    # build, not just a subshell
    local all_ids
    all_ids="$(mktemp)"
    discovered_valid_ids "$root" > "$all_ids"
    mapfile -t ids < "$all_ids"
    rm -f "$all_ids"
  else
    if (($# > 0)); then
      # canonical order is derived at every use: dedupe and sort by id
      mapfile -t ids < <(printf '%s\n' "$@" | LC_ALL=C sort -u)
    fi
  fi

  # id validation against live discovery, before any podman call
  local -A recipe_of=()
  for id in "${ids[@]}"; do
    recipe_of["$id"]="$(resolve_id "$root" "$id")"
  done

  lint_base "$base_file"
  podman_floor

  base_tag="$(tc_base_tag "$base_file")"
  local base_exists=0
  if podman image exists "$base_tag"; then
    base_exists=1
    if ((want_base)); then
      printf 'build-images: base image %s already exists — skipping (delete the image to force a rebuild)\n' "$base_tag"
    fi
  fi

  # enumerate: cached-skip per id, estimate the missing ones (lazy build)
  local -a build_ids=()
  local -A tag_of=()
  for id in "${ids[@]}"; do
    recipe="${recipe_of["$id"]}"
    tag="$(tc_component_tag "$base_file" "$id" "$recipe")"
    tag_of["$id"]="$tag"
    recipe_of["$id"]="$recipe"
    if podman image exists "$tag"; then
      printf 'build-images: component image %s already exists — skipping (delete the image to force a rebuild)\n' "$tag"
    else
      build_ids+=("$id")
    fi
  done

  # disk preflight over what this invocation will actually build
  if ((base_exists == 0)) || ((${#build_ids[@]} > 0)); then
    est=0
    ((base_exists == 0)) && est=$((est + BASE_DISK_BUDGET_BYTES))
    for id in "${build_ids[@]}"; do
      est=$((est + $(component_estimate_bytes "$id")))
    done
    disk_preflight "$est"
    _network_probe
  fi

  # build: base first (a component's FROM needs it), then the missing
  # components in canonical (sorted) order
  if ((base_exists == 0)); then
    build_base_now "$root" "$base_file" "$base_tag"
  fi
  for id in "${build_ids[@]}"; do
    build_component "$base_file" "$id" "${recipe_of[$id]}"
  done
}

# --- set generation and assembly (§5/§6) -----------------------------------------------

# set_gen_dir <set-tag> — the content-keyed generation directory of a set
# assembly (identical inputs render to the same path, byte-identically).
set_gen_dir() {
  local hash12=${1##*:}
  printf '%s/oc-build-gen-%s/set-%s\n' "${TMPDIR:-/tmp}" "$(id -u)" "$hash12"
}

# recipe_envs <recipe> — the fragment's ENV pairs as KEY=value lines, keys
# unique (the last directive wins, Dockerfile semantics), lines sorted (C
# order). Parsed from toolchains.sh parse records ('env' lines with
# continuations already joined); the token grammar is the lint's own.
recipe_envs() {
  local recipe=$1
  local -A pairs=()
  local -a toks
  local rest tok key val
  while IFS= read -r rest; do
    [[ -z "$rest" ]] && continue
    read -r -a toks <<< "$rest"
    if [[ "${toks[0]}" == *=* ]]; then
      for tok in "${toks[@]}"; do
        [[ "$tok" == *=* ]] || continue
        pairs["${tok%%=*}"]="${tok#*=}"
      done
    else
      # legacy 'ENV KEY value' form — the first token is the only key
      key="${toks[0]}"
      val="${rest#"$key"}"
      val="${val#"${val%%[![:space:]]*}"}"
      pairs["$key"]="$val"
    fi
  done < <("$script_dir/toolchains.sh" parse "$recipe" 2>/dev/null |
    awk -F'\t' '$1 == "env" { $1 = ""; sub(/^\t/, ""); print }')
  local k
  for k in "${!pairs[@]}"; do
    printf '%s=%s\n' "$k" "${pairs[$k]}"
  done | LC_ALL=C sort
}

# member_tc_pkgs <tag> — a component image's tc.pkgs label, read host-side via
# podman image inspect (a filesystem/JSON read, never a container start).
member_tc_pkgs() {
  podman image inspect --format '{{ index .Labels "tc.pkgs" }}' "$1" 2>/dev/null || true
}

# generate_set <dir> <base-file> [<id> <recipe>]… — render the set Dockerfile
# plus the merge core and the members' ENV staging files. Members arrive
# canonically sorted by id (the caller's argument order is the render order).
generate_set() {
  local dir=$1 base_file=$2
  shift 2
  mkdir -p "$dir/oc-envs"
  cp "$script_dir/merge-pm.py" "$dir/merge-pm.py"
  local id_list="" hash_list="" tag pkgs
  local -a ids=() recipes=() tags=() envs=()
  local -a pkgs_raw=()
  while (($#)); do
    id=$1
    recipe=$2
    shift 2
    tag="$(tc_component_tag "$base_file" "$id" "$recipe")"
    ids+=("$id")
    recipes+=("$recipe")
    tags+=("$tag")
    id_list+="${id} "
    hash_list+="${id}:$(tc_content_hash "$recipe") "
    # the fragment's ENV pairs, staged for the merge core's re-emission
    recipe_envs "$recipe" > "$dir/oc-envs/$id.env"
    if [[ -s "$dir/oc-envs/$id.env" ]]; then
      envs+=("$id")
    else
      rm -f "$dir/oc-envs/$id.env"
    fi
    pkgs_raw+=("$(member_tc_pkgs "$tag")")
  done
  id_list="${id_list% }"
  hash_list="${hash_list% }"
  # the merged union label (§6): every member's tc.pkgs names, sorted, deduped
  pkgs="$(printf '%s\n' "${pkgs_raw[@]:-}" | tr ' ' '\n' | sed '/^$/d' |
    LC_ALL=C sort -u | paste -sd' ' -)"
  {
    printf '# syntax=docker/dockerfile:1\n'
    printf "# Generated by build-images.sh — full-canonical set assembly for\n"
    printf "# toolchains '%s'. Do not edit; the members' recipe contents are\n" "$id_list"
    printf '# content-hashed into the tag and the members are merged in\n'
    printf '# canonical (alphabetical id) order — spec toolchain-sets §5.\n'
    printf 'FROM opencode-sandbox-base:%s\n' "$(tc_base_hash12 "$base_file")"
    printf 'LABEL kind="set"\n'
    printf 'LABEL members="%s"\n' "$id_list"
    printf 'LABEL member.hashes="%s"\n' "$hash_list"
    printf 'LABEL tc.pkgs="%s"\n' "$pkgs"
    printf 'USER root\n'
    printf 'COPY merge-pm.py /tmp/merge-pm.py\n'
    for i in "${!ids[@]}"; do
      id="${ids[$i]}"
      tag="${tags[$i]}"
      printf 'COPY --from=%s /oc-dump/payload.tar.gz /tmp/oc-merge/%s/payload.tar.gz\n' "$tag" "$id"
      printf 'COPY --from=%s /oc-dump/pm /tmp/oc-merge/%s/pm\n' "$tag" "$id"
      if [[ " ${envs[*]:-} " == *" $id "* ]]; then
        printf 'COPY oc-envs/%s.env /tmp/oc-merge/%s/envs\n' "$id" "$id"
      fi
    done
    printf 'RUN python3 /tmp/merge-pm.py /tmp/oc-merge / && ldconfig && rm -rf /tmp/oc-merge /tmp/merge-pm.py\n'
    printf 'USER dev\n'
    printf 'ENTRYPOINT ["/bin/bash","-l"]\n'
  } > "$dir/Dockerfile"
}

build_set() {
  local base_file=$1
  shift
  local set_tag dir i
  local -a pairs=("$@") tag_args=("$base_file")
  for ((i = 0; i < ${#pairs[@]}; i += 2)); do
    tag_args+=("${pairs[$i]}" "${pairs[$((i + 1))]}")
  done
  set_tag="$(tc_set_tag "${tag_args[@]}")"
  dir="$(set_gen_dir "$set_tag")"
  generate_set "$dir" "$base_file" "$@"
  printf 'build-images: assembling %s\n' "$set_tag"
  # structural honesty (§5): an assembly never phones home — always offline
  podman build --network=none -t "$set_tag" -f "$dir/Dockerfile" "$dir"
  printf 'build-images: built %s\n' "$set_tag"
}

# set_plan [<id> <recipe>]… — the --set plan: validate ids against live
# discovery, build the missing base/components first (lazy), then assemble
# full-canonical. A missing recipe/refusal refuses the whole plan; an existing
# set tag is never rebuilt.
set_plan() {
  local root base_file base_tag id recipe tag est
  root="$(toolchains_root)"
  base_file="$root/base.Dockerfile"
  [[ -f "$base_file" ]] || die "base recipe not found: $base_file — is the toolchains tree installed?"

  # id validation against live discovery, before any podman call (members
  # arrive canonically sorted by id — the canonical order is derived here)
  local -a ids=()
  if (($# > 0)); then
    mapfile -t ids < <(printf '%s\n' "$@" | LC_ALL=C sort -u)
  fi
  local -A recipe_of=()
  for id in "${ids[@]}"; do
    recipe_of["$id"]="$(resolve_id "$root" "$id")"
  done

  lint_base "$base_file"
  podman_floor

  base_tag="$(tc_base_tag "$base_file")"
  local base_exists=0
  podman image exists "$base_tag" && base_exists=1

  # cached-skip: an existing set tag never rebuilds (the set image is
  # self-contained; a member image deleted afterwards changes nothing)
  local -a tag_args=("$base_file")
  for id in "${ids[@]}"; do
    tag_args+=("$id" "${recipe_of[$id]}")
  done
  local set_tag
  set_tag="$(tc_set_tag "${tag_args[@]}")"
  if podman image exists "$set_tag"; then
    printf 'build-images: set image %s already exists — skipping (delete the image to force a rebuild)\n' "$set_tag"
    return 0
  fi

  local -a build_ids=()
  local -A tag_of=()
  for id in "${ids[@]}"; do
    recipe="${recipe_of[$id]}"
    tag="$(tc_component_tag "$base_file" "$id" "$recipe")"
    tag_of["$id"]="$tag"
    if podman image exists "$tag"; then
      printf 'build-images: component image %s already exists — skipping (delete the image to force a rebuild)\n' "$tag"
    else
      build_ids+=("$id")
    fi
  done

  # disk preflight over what this invocation will actually build: the set's
  # own layer budget plus the missing base/components
  est=$SET_DISK_BUDGET_BYTES
  ((base_exists == 0)) && est=$((est + BASE_DISK_BUDGET_BYTES))
  for id in "${build_ids[@]}"; do
    est=$((est + $(component_estimate_bytes "$id")))
  done
  disk_preflight "$est"
  # the download-needing probe (§7 item 3) skips assembly-only plans: only a
  # missing base/component needs the network; the assembly never does
  if ((base_exists == 0)) || ((${#build_ids[@]} > 0)); then
    _network_probe
  fi

  # build: base first (every component's FROM needs it), then the missing
  # components in canonical (sorted) order, then the assembly
  if ((base_exists == 0)); then
    build_base_now "$root" "$base_file" "$base_tag"
  fi
  for id in "${build_ids[@]}"; do
    build_component "$base_file" "$id" "${recipe_of[$id]}"
  done
  local -a set_args=()
  for id in "${ids[@]}"; do
    set_args+=("$id" "${recipe_of[$id]}")
  done
  build_set "$base_file" "${set_args[@]}"
}

main() {
  if (($# == 0)); then
    usage >&2
    exit 2
  fi
  local want_base=0 want_all=0 saw_tc=0 saw_set=0
  local -a tc_ids=() set_ids=() parts=()
  local id
  while (($#)); do
    case "$1" in
      --base)
        want_base=1
        ;;
      --all)
        want_all=1
        ;;
      --tc)
        saw_tc=1
        shift
        (($# > 0)) || {
          usage >&2
          exit 2
        }
        IFS=',' read -r -a parts <<< "$1"
        for id in "${parts[@]}"; do
          [[ -n "$id" ]] || continue
          tc_ids+=("$id")
        done
        ;;
      --set)
        saw_set=1
        shift
        (($# > 0)) || {
          usage >&2
          exit 2
        }
        # an empty value is the base-only set — only missing commas are dropped
        IFS=',' read -r -a parts <<< "$1"
        for id in "${parts[@]}"; do
          [[ -n "$id" ]] || continue
          set_ids+=("$id")
        done
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        usage >&2
        exit 2
        ;;
    esac
    shift
  done
  if ((want_all)); then
    if ((want_base)) || ((saw_tc)) || ((saw_set)); then
      printf 'build-images: --all takes no other flags\n' >&2
      exit 2
    fi
  fi
  if ((saw_set)); then
    if ((want_base)) || ((saw_tc)) || ((want_all)); then
      printf 'build-images: --set takes no other flags\n' >&2
      exit 2
    fi
  fi
  if ((saw_tc && ${#tc_ids[@]} == 0)); then
    usage >&2
    exit 2
  fi
  if ((saw_set)); then
    set_plan "${set_ids[@]}"
  else
    build_plan "$want_base" "$want_all" "${tc_ids[@]}"
  fi
}

main "$@"
