#!/usr/bin/env bash
# tests/test-verify-set-image.sh — Tier-1 tests for the Tier-2 verification
# tooling (spec toolchain-sets §12): the spot-check table's integrity against
# the catalog (base + every catalog id has at least one entry; no stale ids;
# every snippet parses), and the verify-set-image.sh contract — argument
# validation refusals before any podman call, expectation derivation from the
# components' own dumps (no second expectations file), the sanity gates
# (dpkg --audit, full dpkg -l, pip RECORD, spot-check invocations, dpkg -L,
# full-vs-incremental equivalence) and the unasserted per-phase timings —
# against a PATH-mocked podman with fixture component dumps. Runs anywhere,
# no podman needed.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verify="$PROJECT_ROOT/tests/verify-set-image.sh"
table="$PROJECT_ROOT/dist/toolchains/spot-check.tsv"
toolchains_sh="$PROJECT_ROOT/dist/scripts/toolchains.sh"

test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

fail() { printf 'test-verify-set-image: %s\n' "$1" >&2; exit 1; }

# --- shared fixture: the real toolchains tree, self-derived tags ------------------
# The real recipes pin the ids (expectations derive from the fixture dumps, not
# from a second expectations file); the set/component tags come from the same
# identity lib the builder uses (pinned to the Go side by the golden vectors).
fixture_root="$test_home/dist"
mkdir -p "$fixture_root"
cp -r "$PROJECT_ROOT/dist/toolchains" "$fixture_root/toolchains"

# shellcheck source=../dist/scripts/lib-identity.sh
source "$PROJECT_ROOT/dist/scripts/lib-identity.sh"
base_dockerfile="$fixture_root/toolchains/base.Dockerfile"
recipe_of() { printf '%s/toolchains/recipes/%s.Dockerfile\n' "$fixture_root" "$1"; }

# The fixture members: dpkg-only (cpp), dpkg+pip (go), pip-only (micropython).
fixture_members=(cpp go micropython)
set_tag_args=()
for id in "${fixture_members[@]}"; do set_tag_args+=("$id" "$(recipe_of "$id")"); done
set_tag="$(tc_set_tag "$base_dockerfile" "${set_tag_args[@]}")"
component_tag() { tc_component_tag "$base_dockerfile" "$1" "$(recipe_of "$1")"; }
base_tag="$(tc_base_tag "$base_dockerfile")"

# Fixture component dumps — each component's own PM state, the expectations the
# verify script must derive. cpp: one new dpkg package; go: one new dpkg
# package + one pip package; micropython: two pip packages, no dpkg packages.
# Every dump's status carries base-core (a component is FROM base).
make_dump() { # <dir> <tc.pkgs lines…> -- <status Package lines…>
  local dir=$1
  shift
  local -a pkgs=()
  while [[ "$1" != "--" ]]; do pkgs+=("$1"); shift; done
  shift
  mkdir -p "$dir/oc-dump/pm"
  printf '%s\n' "${pkgs[@]}" > "$dir/oc-dump/tc.pkgs"
  { printf 'Package: base-core\nStatus: install ok installed\n\n'
    for p in "$@"; do printf 'Package: %s\nStatus: install ok installed\n\n' "$p"; done; } \
    > "$dir/oc-dump/pm/status"
}
base_dump="$test_home/dumps/base"
for id in "${fixture_members[@]}"; do
  case "$id" in
    cpp) make_dump "$test_home/dumps/$id" g++-one -- base-core g++-one ;;
    go) make_dump "$test_home/dumps/$id" golang-go-fake go-pip-fake -- base-core golang-go-fake ;;
    micropython) make_dump "$test_home/dumps/$id" mpremote esptool -- base-core ;;
  esac
done
mkdir -p "$base_dump/var/lib/dpkg"
{ printf 'Package: base-core\nStatus: install ok installed\n\n'
  printf 'Package: dash\nStatus: install ok installed\n\n'; } \
  > "$base_dump/var/lib/dpkg/status"

# The expected full dpkg -l: base's status ∪ every member's status, self-derived.
dpkg_l_fixture="$test_home/dpkg-l.txt"
{
  sed -n 's/^Package: /ii  /p' "$base_dump/var/lib/dpkg/status"
  for id in "${fixture_members[@]}"; do
    sed -n 's/^Package: /ii  /p' "$test_home/dumps/$id/oc-dump/pm/status"
  done
} | LC_ALL=C sort -u | sed 's/$/  1.0  all  fixture/' > "$dpkg_l_fixture"
: > "$test_home/empty-dpkg-l.txt"

# --- mocks ------------------------------------------------------------------------

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
PODMAN_LOG="$test_home/podman.log"
: > "$PODMAN_LOG"
export PODMAN_LOG

# podman mock: logs every invocation, implements exactly the seams the verify
# script uses. PODMAN_MOUNTS maps <tag><TAB><dir> for `image mount`;
# PODMAN_MISSING_IMAGES (space-separated tags) fails `image exists`;
# PODMAN_LABELS_FILE holds <tag><TAB><label><TAB><value> rows;
# PODMAN_DPKG_L is the `dpkg -l` answer file; PODMAN_FINDS maps
# <tag><TAB><file> for the full-vs find listings; PODMAN_DPKG_AUDIT_FAIL /
# PODMAN_RECORD_FAIL / PODMAN_DPKGL_FAIL / PODMAN_SPOTCHECK_FAIL make the
# corresponding in-container command fail.
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$PODMAN_LOG"
tag=""
for a in "$@"; do
  case "$a" in opencode-sandbox-*) tag=$a; break ;; esac
done
missing() { [[ " ${PODMAN_MISSING_IMAGES:-} " == *" $1 "* ]]; }
field_of() { awk -F'\t' -v t="$tag" '$1 == t { print $2 }' "$1"; }
case "$1" in
  --version) printf 'podman version 4.9.3\n' ;;
  image)
    case "$2" in
      exists) ! missing "$3" ;;
      mount)
        missing "$3" && exit 1
        dir="$(field_of "${PODMAN_MOUNTS:?}")"
        [[ -n "$dir" ]] || exit 1
        printf '%s\n' "$dir"
        ;;
      unmount) exit 0 ;;
      inspect)
        [[ " $* " == *" Labels "* ]] || exit 1
        awk -F'\t' -v t="$tag" '$1 == t { print $3 }' "${PODMAN_LABELS_FILE:-/dev/null}"
        ;;
    esac
    ;;
  run)
    args=("$@")
    i=0
    while [[ "${args[$i]}" != "$tag" ]]; do i=$((i + 1)); done
    cmd=("${args[@]:$((i + 1))}")
    case "${cmd[0]}" in
      dpkg)
        case "${cmd[1]}" in
          --audit) [[ -n "${PODMAN_DPKG_AUDIT_FAIL:-}" ]] && { printf 'broken\n'; exit 2; }; exit 0 ;;
          -l) cat "${PODMAN_DPKG_L:?}"; exit 0 ;;
          *) exit 0 ;;
        esac
        ;;
      python3)
        [[ -n "${PODMAN_RECORD_FAIL:-}" ]] && { printf 'RECORD miss\n'; exit 3; }
        exit 0
        ;;
      bash)
        case "${cmd[1]}" in
          -c) [[ -n "${PODMAN_DPKGL_FAIL:-}" ]] && { printf 'dpkg -L failed\n'; exit 5; }; exit 0 ;;
          -lc) [[ -n "${PODMAN_SPOTCHECK_FAIL:-}" ]] && exit 6; exit 0 ;;
          *) exit 0 ;;
        esac
        ;;
      find)
        f="$(field_of "${PODMAN_FINDS:-/dev/null}")"
        [[ -n "$f" ]] || exit 0
        cat "$f"
        ;;
      *) exit 0 ;;
    esac
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$fake_bin/podman"

mounts_file="$test_home/mounts.tsv"
{
  printf '%s\t%s\n' "$base_tag" "$base_dump"
  for id in "${fixture_members[@]}"; do
    printf '%s\t%s\n' "$(component_tag "$id")" "$test_home/dumps/$id"
  done
} > "$mounts_file"

labels_file="$test_home/labels.tsv"
printf '%s\tmembers\t%s\n' "$set_tag" \
  "$(printf '%s\n' "${fixture_members[@]}" | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')" \
  > "$labels_file"

run_verify() { ( export PATH="$fake_bin:$PATH"; "$@" ); }
invocations() { wc -l < "$PODMAN_LOG" | tr -d ' '; }

# --- 1: the spot-check table covers the catalog -----------------------------------
[[ -f "$table" ]] || fail "dist/toolchains/spot-check.tsv is missing"

spec_units=(cpp rust java go node-dev playwright
  arm-embedded arduino platformio micropython picoscope pyvisa usb
  ros2 octave python-science latex pdf-tools pandoc fonts libreoffice)

has_row() { awk -F'\t' -v id="$1" '$1 == id { f = 1 } END { exit !f }' "$table"; }
table_ids="$(awk -F'\t' '$1 !~ /^#/ && NF { print $1 }' "$table")"
[[ -n "$table_ids" ]] || fail "spot-check table has no entries"

# 1a: base plus every catalog id has at least one entry.
for id in base "${spec_units[@]}"; do
  has_row "$id" || fail "spot-check table has no entry for '$id'"
done
printf 'spot-check table covers base + all 21 catalog units\n'

# 1b: no stale rows (a renamed/dropped unit would leave one behind).
discovered="$(TOOLCHAINS_ROOT="$fixture_root/toolchains" bash "$toolchains_sh" discover \
  | awk -F'\t' '$1 == "unit" && $3 == "valid" { print $2 }')" \
  || fail "discovery failed"
stale="$(comm -23 \
  <(printf '%s\n' "$table_ids" | grep -vx base | LC_ALL=C sort -u) \
  <(printf '%s\n' "$discovered" | LC_ALL=C sort -u))"
[[ -z "$stale" ]] || fail "spot-check table rows for unknown ids: $stale"
printf 'spot-check table has no stale ids\n'

# 1c: every snippet is non-empty and parses as bash.
while IFS=$'\t' read -r id snippet; do
  [[ "$id" == \#* || -z "$id" ]] && continue
  [[ -n "$snippet" ]] || fail "empty snippet for '$id'"
  if ! bash -n <<< "$snippet" 2>/dev/null; then
    fail "snippet for '$id' does not parse: $snippet"
  fi
done < "$table"
printf 'every spot-check snippet parses\n'

# --- 2: argument contract — refusals before any podman call -----------------------
: > "$PODMAN_LOG"
out="$(run_verify "$verify" 2>&1)" && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "no-args invocation must fail"
[[ "$out" == *"Usage:"* ]] || fail "no-args refusal prints usage, got: $out"
[[ "$(invocations)" -eq 0 ]] || fail "no-args refusal must not call podman"
printf 'no-args refusal prints usage and never calls podman\n'

: > "$PODMAN_LOG"
out="$(run_verify "$verify" "$set_tag" nope 2>&1)" && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "unknown member id must fail"
[[ "$out" == *"nope"* ]] || fail "unknown-id refusal names the id, got: $out"
[[ "$(invocations)" -eq 0 ]] || fail "unknown-id refusal must not call podman"
printf 'unknown member id refuses before any podman call\n'

: > "$PODMAN_LOG"
out="$(run_verify "$verify" "opencode-sandbox-set:000000000000" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "a tag mismatched with the members' identity must fail"
[[ "$out" == *"$set_tag"* ]] || fail "mismatch refusal names the derived tag, got: $out"
printf 'content-identity mismatch refuses naming the derived tag\n'

: > "$PODMAN_LOG"
run_verify "$verify" --full-vs "$set_tag" "${fixture_members[@]}" 2>/dev/null \
  && fail "--full-vs without its other-tag argument must fail"
printf '%s\n' '--full-vs without a value refuses'

# --- 3: missing images refuse -----------------------------------------------------
: > "$PODMAN_LOG"
out="$(PODMAN_MISSING_IMAGES="$set_tag" run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "a missing set image must fail"
[[ "$out" == *"$set_tag"* ]] || fail "missing-set refusal names the tag, got: $out"
printf 'missing set image refuses naming the tag\n'

: > "$PODMAN_LOG"
out="$(PODMAN_MISSING_IMAGES="$(component_tag go)" run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "a missing component image must fail"
[[ "$out" == *"$(component_tag go)"* ]] \
  || fail "missing-component refusal names the tag, got: $out"
printf 'missing component image refuses naming its tag\n'

# --- 4: the gates run green against a healthy set ---------------------------------
: > "$PODMAN_LOG"
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$dpkg_l_fixture" run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -eq 0 ]] || fail "healthy set must verify green:
$out"
for phase in "dpkg --audit" "dpkg -l" "RECORD" "spot-check" "dpkg -L"; do
  grep -q "$phase" <<< "$out" || fail "phase '$phase' not reported:
$out"
done
grep -qi "timing" <<< "$out" || fail "per-phase timings are not printed:
$out"
# Every relevant spot-check snippet actually ran (each is logged by the mock).
for id in base "${fixture_members[@]}"; do
  n_snippets="$(awk -F'\t' -v id="$id" '$1 == id' "$table" | wc -l)"
  [[ "$n_snippets" -gt 0 ]] || fail "no table row for fixture member '$id'"
  while IFS=$'\t' read -r _ snippet; do
    grep -qF -- "$snippet" "$PODMAN_LOG" \
      || fail "spot-check snippet for '$id' never ran: $snippet"
  done < <(awk -F'\t' -v id="$id" '$1 == id { print $2 }' "$table")
done
# The expectations came from the components' own dumps: every member dump was
# mounted host-side.
log="$(cat "$PODMAN_LOG")"
for id in "${fixture_members[@]}"; do
  grep -q "image mount opencode-sandbox-tc-$id:" <<< "$log" \
    || fail "member $id's dump was never mounted"
done
printf 'all gates green on a healthy set; timings printed\n'

# --- 5: gate failures refuse loudly -----------------------------------------------
: > "$PODMAN_LOG"
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$test_home/empty-dpkg-l.txt" run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "an empty dpkg -l (every package lost) must fail"
for p in g++-one golang-go-fake; do
  grep -q "$p" <<< "$out" || fail "dpkg -l failure must name the missing package $p:
$out"
done
printf 'dropped member packages fail the dpkg -l gate by name\n'

: > "$PODMAN_LOG"
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$dpkg_l_fixture" PODMAN_DPKG_AUDIT_FAIL=1 \
  run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "a non-empty dpkg --audit must fail"
grep -q "dpkg --audit" <<< "$out" || fail "audit failure reports the phase:
$out"
printf 'a non-empty dpkg --audit fails the gate\n'

: > "$PODMAN_LOG"
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$dpkg_l_fixture" PODMAN_SPOTCHECK_FAIL=1 \
  run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "a failing spot-check invocation must fail"
grep -q "spot-check" <<< "$out" || fail "spot-check failure reports the phase:
$out"
printf 'a failing spot-check invocation fails the gate\n'

: > "$PODMAN_LOG"
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$dpkg_l_fixture" PODMAN_RECORD_FAIL=1 \
  run_verify "$verify" "$set_tag" "${fixture_members[@]}" 2>&1)" && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "a broken pip RECORD must fail"
grep -q "RECORD" <<< "$out" || fail "RECORD failure reports the phase:
$out"
printf 'a broken pip RECORD fails the gate\n'

# --- 6: full-vs-incremental file-list equivalence ---------------------------------
find_root="$test_home/find"
mkdir -p "$find_root"
# The same tree modulo volatile paths and ordering — equivalent.
printf '/home/dev/.profile\n/var/cache/ldconfig/aux-cache\n/usr/bin/g++\n/usr/lib/node_modules\n' \
  > "$find_root/a.txt"
printf '/usr/lib/node_modules\n/usr/bin/g++\n/var/cache/ldconfig/aux-cache\n/home/dev/.profile\n' \
  > "$find_root/b.txt"
other_tag="opencode-sandbox-set:aaaaaaaaaaaa"
finds_file="$test_home/finds.tsv"
finds() {
  { printf '%s\t%s\n' "$set_tag" "$find_root/a.txt"
    printf '%s\t%s\n' "$other_tag" "$find_root/b.txt"; } > "$finds_file"
}
: > "$PODMAN_LOG"
finds
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$dpkg_l_fixture" PODMAN_FINDS="$finds_file" \
  run_verify "$verify" --full-vs "$other_tag" "$set_tag" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -eq 0 ]] || fail "equivalent file lists (order-insensitive, volatile paths ignored) must pass:
$out"
grep -q "full-vs" <<< "$out" || fail "equivalence phase is reported:
$out"
printf 'full-vs equivalence passes modulo volatile paths, order-insensitively\n'

: > "$PODMAN_LOG"
printf '/usr/bin/g++\n/etc/oc-extra\n' > "$find_root/b.txt"
out="$(PODMAN_MOUNTS="$mounts_file" PODMAN_LABELS_FILE="$labels_file" \
  PODMAN_DPKG_L="$dpkg_l_fixture" PODMAN_FINDS="$finds_file" \
  run_verify "$verify" --full-vs "$other_tag" "$set_tag" "${fixture_members[@]}" 2>&1)" \
  && rc=0 || rc=$?
[[ "$rc" -ne 0 ]] || fail "divergent file lists must fail the equivalence gate"
grep -q "/etc/oc-extra" <<< "$out" \
  || fail "equivalence failure names the differing path:
$out"
printf 'divergent file lists fail the equivalence gate naming the difference\n'

printf 'verify-set-image tests passed\n'
