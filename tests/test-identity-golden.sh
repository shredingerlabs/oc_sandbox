#!/usr/bin/env bash
set -euo pipefail

# Golden-vector test for the hash12 identity derivation (ticket #194, spec
# §6/§12): pins dist/scripts/lib-identity.sh (sourced by build-images.sh and
# start.sh) and internal/identity (Go) to each other via one checked-in
# fixture — tests/fixtures/identity-golden.

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE="$PROJECT_ROOT/tests/fixtures/identity-golden"
LIB="$PROJECT_ROOT/dist/scripts/lib-identity.sh"

# shellcheck source=dist/scripts/lib-identity.sh
source "$LIB"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

expect() { # expect <name> <want> <got>
  if [ "$2" != "$3" ]; then
    fail "$1: want '$2', got '$3'"
  fi
}

# --- golden vectors: base --------------------------------------------------
want_base_h12="$(awk '$1 == "base_hash12" { print $2 }' "$FIXTURE/expected")"
want_base_tag="$(awk '$1 == "base_tag" { print $2 }' "$FIXTURE/expected")"
expect "base hash12" "$want_base_h12" "$(tc_base_hash12 "$FIXTURE/base.Dockerfile")"
expect "base tag" "$want_base_tag" "$(tc_base_tag "$FIXTURE/base.Dockerfile")"

# --- golden vectors: components (sorted-by-id set serialization, singleton) -
for id in go playwright rust; do
  want_h12="$(awk -v k="component_${id}_hash12" '$1 == k { print $2 }' "$FIXTURE/expected")"
  want_tag="$(awk -v k="component_${id}_tag" '$1 == k { print $2 }' "$FIXTURE/expected")"
  expect "component $id hash12" "$want_h12" \
    "$(tc_component_hash12 "$FIXTURE/base.Dockerfile" "$id" "$FIXTURE/recipes/$id.Dockerfile")"
  expect "component $id tag" "$want_tag" \
    "$(tc_component_tag "$FIXTURE/base.Dockerfile" "$id" "$FIXTURE/recipes/$id.Dockerfile")"
done

# --- golden vectors: set ----------------------------------------------------
want_set_h12="$(awk '$1 == "set_hash12" { print $2 }' "$FIXTURE/expected")"
want_set_tag="$(awk '$1 == "set_tag" { print $2 }' "$FIXTURE/expected")"
sorted_h12="$(tc_set_hash12 "$FIXTURE/base.Dockerfile" \
  go "$FIXTURE/recipes/go.Dockerfile" \
  playwright "$FIXTURE/recipes/playwright.Dockerfile" \
  rust "$FIXTURE/recipes/rust.Dockerfile")"
expect "set hash12" "$want_set_h12" "$sorted_h12"
expect "set tag" "$want_set_tag" \
  "$(tc_set_tag "$FIXTURE/base.Dockerfile" \
    go "$FIXTURE/recipes/go.Dockerfile" \
    playwright "$FIXTURE/recipes/playwright.Dockerfile" \
    rust "$FIXTURE/recipes/rust.Dockerfile")"

# --- unordered member input yields the identical hash ------------------------
expect "unordered members hash" "$want_set_h12" \
  "$(tc_set_hash12 "$FIXTURE/base.Dockerfile" \
    rust "$FIXTURE/recipes/rust.Dockerfile" \
    go "$FIXTURE/recipes/go.Dockerfile" \
    playwright "$FIXTURE/recipes/playwright.Dockerfile")"

# --- a base-recipe content change re-addresses base, components and sets -----
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
cp "$FIXTURE/base.Dockerfile" "$tmpdir/base.Dockerfile"
printf '\n# bumped\n' >> "$tmpdir/base.Dockerfile"

bumped_base="$(tc_base_hash12 "$tmpdir/base.Dockerfile")"
if [ "$bumped_base" = "$want_base_h12" ]; then
  fail "base bump did not re-address the base hash"
fi
bumped_comp="$(tc_component_hash12 "$tmpdir/base.Dockerfile" go "$FIXTURE/recipes/go.Dockerfile")"
if [ "$bumped_comp" = "$want_base_h12" ] || [ "$bumped_comp" = "$(awk '$1 == "component_go_hash12" { print $2 }' "$FIXTURE/expected")" ]; then
  fail "base bump did not re-address the component hash"
fi
bumped_set="$(tc_set_hash12 "$tmpdir/base.Dockerfile" \
  go "$FIXTURE/recipes/go.Dockerfile" \
  playwright "$FIXTURE/recipes/playwright.Dockerfile" \
  rust "$FIXTURE/recipes/rust.Dockerfile")"
if [ "$bumped_set" = "$want_set_h12" ]; then
  fail "base bump did not re-address the set hash"
fi

# --- empty member set (base-only sets) still re-addresses on a base bump -----
empty_h12="$(tc_set_hash12 "$FIXTURE/base.Dockerfile")"
bumped_empty_h12="$(tc_set_hash12 "$tmpdir/base.Dockerfile")"
if [ "$empty_h12" = "$bumped_empty_h12" ]; then
  fail "base bump did not re-address the base-only set hash"
fi
if [ "$empty_h12" = "$want_set_h12" ]; then
  fail "base-only set hash equals the full set hash"
fi

printf 'test-identity-golden: all checks passed\n'
