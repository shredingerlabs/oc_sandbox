#!/usr/bin/env bash
# lib-identity.sh — content-addressed hash12 identity for toolchain sets
# (spec "toolchain sets" §6, ticket #194). Sourced by build-images.sh and
# start.sh; pinned to the Go implementation (internal/identity) by the
# golden vectors in tests/fixtures/identity-golden — both suites
# (tests/test-identity-golden.sh and the Go test) must stay green together.
#
# Serialization (pinned by the golden vectors, not prose):
#   content hash   = sha256 hex of the file's exact bytes
#   base hash12    = first 12 hex of sha256(base.Dockerfile bytes)
#   identity input = "base <base content hash>" line, then one
#                    "tc <id> <recipe content hash>" line per member,
#                    members sorted by id (C byte order), \n-terminated lines
#   hash12         = first 12 lowercase hex of sha256(identity input)
#   component hash12 = the identity input with exactly one member
#                      (recipe content hash + base recipe content hash)
#   tags           = opencode-sandbox-base:<hash12> /
#                    opencode-sandbox-tc-<id>:<hash12> /
#                    opencode-sandbox-set:<hash12>
#
# All functions print to stdout and take the base recipe file as $1.

# tc_content_hash <file> — sha256 hex of the file's exact bytes
tc_content_hash() {
  sha256sum "$1" | cut -d' ' -f1
}

# tc_base_hash12 <base.Dockerfile>
tc_base_hash12() {
  tc_content_hash "$1" | cut -c1-12
}

# tc_base_tag <base.Dockerfile> — prints opencode-sandbox-base:<hash12>
tc_base_tag() {
  printf 'opencode-sandbox-base:%s\n' "$(tc_base_hash12 "$1")"
}

# _tc_identity_input <base.Dockerfile> [<id> <recipe.Dockerfile>]...
# Builds the pinned serialization; prints the input bytes on stdout.
_tc_identity_input() {
  local base_hash
  base_hash=$(tc_content_hash "$1")
  shift
  printf 'base %s\n' "$base_hash"
  if [ "$#" -gt 0 ]; then
    local -a lines=()
    while [ "$#" -ge 2 ]; do
      lines+=("tc $1 $(tc_content_hash "$2")")
      shift 2
    done
    printf '%s\n' "${lines[@]}" | LC_ALL=C sort
  fi
}

# tc_component_hash12 <base.Dockerfile> <id> <recipe.Dockerfile>
tc_component_hash12() {
  _tc_identity_input "$1" "$2" "$3" | sha256sum | cut -c1-12
}

# tc_component_tag <base.Dockerfile> <id> <recipe.Dockerfile>
tc_component_tag() {
  printf 'opencode-sandbox-tc-%s:%s\n' \
    "$2" "$(tc_component_hash12 "$1" "$2" "$3")"
}

# tc_set_hash12 <base.Dockerfile> [<id> <recipe.Dockerfile>]...
# Member order is irrelevant — members are sorted by id before hashing.
tc_set_hash12() {
  _tc_identity_input "$@" | sha256sum | cut -c1-12
}

# tc_set_tag <base.Dockerfile> [<id> <recipe.Dockerfile>]...
tc_set_tag() {
  printf 'opencode-sandbox-set:%s\n' "$(tc_set_hash12 "$@")"
}
