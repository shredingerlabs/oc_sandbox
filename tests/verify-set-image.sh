#!/usr/bin/env bash
# verify-set-image.sh — Tier-2 real-podman set verification (spec toolchain-sets
# §12). Manual, never a build flag, never wired into CI: run at
# implement-acceptance (a documented rig run lives under docs/verification/),
# after risky changes, and on demand against any built set image.
#
#   tests/verify-set-image.sh [--full-vs <other-set-tag>] <set-tag> [<tc-ids>…]
#
# Sanity gates, parameterized by image + members:
#   1. dpkg --audit is empty inside the set container.
#   2. The full `dpkg -l` keeps the base's and every member's packages — the
#      expectations are self-derived from the components' own PM dumps (read
#      host-side via podman image mounts, never a container start), no second
#      expectations file. The given set tag must equal the tag derived from the
#      members' current recipes (content identity), and the set image's
#      `members` label must list exactly the given ids.
#   3. pip RECORD intact: every installed dist-info RECORD's files exist, and
#      every member's pip packages (its tc.pkgs names minus its dpkg names)
#      resolve through importlib.metadata.
#   4. Binaries run per the repo-side spot-check table (id → invocations /
#      imports, dist/toolchains/spot-check.tsv, maintained alongside the
#      catalog): every row for the base and the set's members runs inside the
#      container.
#   5. dpkg -L succeeds for every member dpkg package (with a warning when a
#      package lists no existing path — dpkg -L reads the merged info lists,
#      which are fuller than the existence-filtered payload archive).
#   6. With --full-vs, file-list equivalence between two images of the same
#      members built the other way (full canonical vs incremental): compared
#      order-insensitively, volatile paths ignored.
#
# Per-phase timings are printed unasserted — no thresholds; the seconds
# target's formal check is the rig measurement recorded in the runbook at
# acceptance. All strings are English (binding).

set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: tests/verify-set-image.sh [--full-vs <other-set-tag>] <set-tag> [<tc-ids>…]

Runs the Tier-2 sanity gates against a built set image (real podman; manual —
never a build flag, never wired into CI):

  --full-vs <other-set-tag>  also compare file-list equivalence against a
                             second image of the same members built the other
                             way (full canonical vs incremental)
  <set-tag>                  the opencode-sandbox-set:<hash12> image to verify
  <tc-ids>…                  the set's toolchain members (empty = base-only)

The given tag must be the tag derived from the members' current recipes
(content identity); expectations are self-derived from the components' own PM
dumps — no second expectations file.
EOF
}

fail() { printf 'verify-set-image: %s\n' "$*" >&2; exit 1; }

# --- timing (printed unasserted, no thresholds) -----------------------------------
now() { printf '%s' "${EPOCHREALTIME:-$(date +%s.%N)}"; }
elapsed() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.1fs", b - a }'; }
report() { printf '%s\n' "$*"; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_root="$(cd "$script_dir/.." && pwd)"
toolchains_root="$project_root/dist/toolchains"
toolchains_sh="$project_root/dist/scripts/toolchains.sh"

# shellcheck source=../dist/scripts/lib-identity.sh
source "$project_root/dist/scripts/lib-identity.sh"

# --- arguments --------------------------------------------------------------------
full_vs=""
while (($#)); do
  case "$1" in
    --full-vs)
      (($# >= 2)) || { usage; exit 2; }
      full_vs=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    -*)
      usage
      exit 2
      ;;
    *)
      break
      ;;
  esac
done
(($# >= 1)) || { usage; exit 2; }
set_tag=$1
shift
members=("$@")
[[ "$set_tag" == opencode-sandbox-set:* ]] \
  || fail "'$set_tag' is not an opencode-sandbox-set:<hash12> tag"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=()
gate_failed() { failures+=("$1"); }

# --- preflight: ids, content identity, images (refuses before any container) ------
t0=$(now)
declare -A recipe_of=()
tag_args=()
for id in "${members[@]}"; do
  recipe="$(TOOLCHAINS_ROOT="$toolchains_root" bash "$toolchains_sh" resolve "$id" 2>/dev/null)" \
    || fail "unknown or invalid toolchain id '$id' — refused before any podman call"
  recipe_of[$id]=$recipe
  tag_args+=("$id" "$recipe")
done
derived_tag="$(tc_set_tag "$toolchains_root/base.Dockerfile" "${tag_args[@]}")"
[[ "$derived_tag" == "$set_tag" ]] || fail "the given tag does not match its members' identity — derived from the current recipes: $derived_tag"

command -v podman >/dev/null 2>&1 || fail "podman not found — Tier 2 is the real-podman tier"
podman image exists "$set_tag" || fail "set image does not exist: $set_tag (build it with build-images.sh --set)"
base_tag="$(tc_base_tag "$toolchains_root/base.Dockerfile")"
podman image exists "$base_tag" || fail "base image does not exist: $base_tag"
declare -A component_tag_of=()
for id in "${members[@]}"; do
  component_tag_of[$id]="$(tc_component_tag "$toolchains_root/base.Dockerfile" "$id" "${recipe_of[$id]}")"
  podman image exists "${component_tag_of[$id]}" \
    || fail "component image does not exist: ${component_tag_of[$id]} (build it with build-images.sh --tc $id)"
done

# The set image's own members label must list exactly the given ids.
image_members="$(podman image inspect --format '{{ index .Labels "members" }}' "$set_tag" 2>/dev/null || true)"
if [[ -n "$image_members" ]]; then
  sorted_members="$(printf '%s\n' "${members[@]:-}" | LC_ALL=C sort -u | tr '\n' ' ' | sed 's/ $//')"
  [[ "$image_members" == "$sorted_members" ]] \
    || fail "the set image's members label ('$image_members') does not list the given ids ('$sorted_members')"
fi
provenance="$(podman image inspect --format '{{ index .Labels "oc.provenance" }}' "$set_tag" 2>/dev/null || true)"
report "== verify-set-image: $set_tag"
report "   members: ${members[*]:-(base only)}"
[[ -z "$provenance" ]] || report "   provenance: $provenance (non-normative)"
report "timing  preflight: $(elapsed "$t0" "$(now)")"

# --- expectations, self-derived from the components' own dumps --------------------
# Host-side podman image mounts only — never a container start.
t0=$(now)
status_pkgs() { sed -n 's/^Package: //p' "$1" 2>/dev/null | LC_ALL=C sort -u; }

base_mnt="$(podman image mount "$base_tag")" \
  || fail "cannot mount the base image ($base_tag)"
status_pkgs "$base_mnt/var/lib/dpkg/status" > "$work/base.pkgs"
podman image unmount "$base_tag" >/dev/null

: > "$work/expected.pkgs" # base ∪ every member's dump status (gate 2)
cat "$work/base.pkgs" >> "$work/expected.pkgs"
for id in "${members[@]}"; do
  mnt="$(podman image mount "${component_tag_of[$id]}")" \
    || fail "cannot mount the component image for $id (${component_tag_of[$id]})"
  local_dump="$work/dump-$id"
  mkdir -p "$local_dump"
  cp "$mnt/oc-dump/pm/status" "$local_dump/status"
  [[ -f "$mnt/oc-dump/tc.pkgs" ]] || fail "component image for $id carries no tc.pkgs dump (${component_tag_of[$id]} — rebuilt by build-images.sh?)"
  LC_ALL=C sort -u "$mnt/oc-dump/tc.pkgs" -o "$local_dump/tc.pkgs"
  podman image unmount "${component_tag_of[$id]}" >/dev/null
  status_pkgs "$local_dump/status" >> "$work/expected.pkgs"
  # Member dpkg packages = its status names ∩ its tc.pkgs; member pip packages
  # = its tc.pkgs names not in its status (a dump's tc.pkgs is the fragment's
  # normalized dpkg + pip names).
  status_pkgs "$local_dump/status" > "$local_dump/dpkg.pkgs"
  comm -12 "$local_dump/dpkg.pkgs" "$local_dump/tc.pkgs" > "$local_dump/dpkg.new"
  comm -13 "$local_dump/dpkg.pkgs" "$local_dump/tc.pkgs" > "$local_dump/pip.pkgs"
done
LC_ALL=C sort -u "$work/expected.pkgs" -o "$work/expected.pkgs"
report "timing  expectations from component dumps: $(elapsed "$t0" "$(now)")"

# --- the gates --------------------------------------------------------------------
run_in_set() { podman run --rm --network=none -w /home/dev "$set_tag" "$@"; }

# Gate 1: dpkg --audit empty.
t0=$(now)
audit="$(run_in_set dpkg --audit 2>&1)" && rc=0 || rc=$?
if [[ "$rc" -eq 0 && -z "$audit" ]]; then
  report "gate dpkg --audit: ok"
else
  gate_failed "dpkg --audit"
  report "gate dpkg --audit: FAILED (rc=$rc) — audit output:
$audit"
fi
report "timing  dpkg --audit: $(elapsed "$t0" "$(now)")"

# Gate 2: the full dpkg -l keeps base's and every member's packages.
t0=$(now)
run_in_set dpkg -l > "$work/dpkg-l.txt" 2> "$work/dpkg-l.err" && rc=0 || rc=$?
if [[ "$rc" -ne 0 ]]; then
  gate_failed "dpkg -l"
  report "gate dpkg -l: FAILED — dpkg -l itself failed (rc=$rc): $(cat "$work/dpkg-l.err")"
else
  awk '$1 == "ii" { print $2 }' "$work/dpkg-l.txt" | sed 's/:.*//' | LC_ALL=C sort -u > "$work/installed.pkgs"
  missing="$(comm -23 "$work/expected.pkgs" "$work/installed.pkgs")"
  if [[ -z "$missing" ]]; then
    report "gate dpkg -l: ok ($(wc -l < "$work/expected.pkgs" | tr -d ' ') expected packages present)"
  else
    gate_failed "dpkg -l"
    report "gate dpkg -l: FAILED — packages from the base/member dumps missing from the merged dpkg -l:
$missing"
  fi
fi
report "timing  dpkg -l: $(elapsed "$t0" "$(now)")"

# Gate 3: pip RECORD intact — every installed dist-info RECORD's files exist,
# and every member's pip packages resolve. One python script, one container run.
t0=$(now)
cat > "$work/record-check.py" <<'PYEOF'
import sys, glob, os
ok = True
for di in sum([glob.glob(r) for r in (
    "/usr/lib/python3*/dist-packages/*.dist-info",
    "/usr/lib/python3/dist-packages/*.dist-info",
    "/usr/lib/python3*/site-packages/*.dist-info",
    "/usr/local/lib/python3*/dist-packages/*.dist-info",
    "/usr/local/lib/python3*/site-packages/*.dist-info",
)], []):
    record = os.path.join(di, "RECORD")
    if not os.path.isfile(record):
        continue
    root = os.path.dirname(di)
    for line in open(record, encoding="utf-8", errors="replace"):
        path = line.split(",")[0].strip()
        if not path:
            continue
        target = os.path.normpath(os.path.join(root, path))
        if not os.path.exists(target):
            print("missing RECORD path: %s (%s)" % (target, os.path.basename(di)))
            ok = False
pip_names = sys.argv[1:]
if pip_names:
    import importlib.metadata as md
    for name in pip_names:
        try:
            md.version(name)
        except Exception as exc:
            print("pip package not resolvable: %s (%s)" % (name, exc))
            ok = False
sys.exit(0 if ok else 1)
PYEOF
pip_names=()
for id in "${members[@]}"; do
  mapfile -t rows < "$work/dump-$id/pip.pkgs"
  pip_names+=("${rows[@]}")
done
record_out="$(podman run --rm --network=none \
  -v "$work/record-check.py:/tmp/oc-record-check.py:ro" "$set_tag" \
  python3 /tmp/oc-record-check.py ${pip_names[@]+"${pip_names[@]}"} 2>&1)" && rc=0 || rc=$?
if [[ "$rc" -eq 0 ]]; then
  report "gate pip RECORD: ok ($(wc -l < "$work/expected.pkgs" | tr -d ' ') dpkg + ${#pip_names[@]} member pip packages)"
else
  gate_failed "pip RECORD"
  report "gate pip RECORD: FAILED —
$record_out"
fi
report "timing  pip RECORD: $(elapsed "$t0" "$(now)")"

# Gate 4: spot-check invocations/imports per the repo-side table.
t0=$(now)
table="$toolchains_root/spot-check.tsv"
[[ -f "$table" ]] || fail "the spot-check table is missing: $table"
spot_ok=0
spot_rows=()
for id in base "${members[@]}"; do
  while IFS=$'\t' read -r row_id snippet; do
    [[ "$row_id" == "$id" ]] || continue
    spot_rows+=("$id"$'\t'"$snippet")
  done < "$table"
done
if [[ "${#members[@]}" -eq 0 ]]; then
  : # base-only set: base rows only
fi
spot_failed=()
for row in ${spot_rows[@]+"${spot_rows[@]}"}; do
  id="${row%%$'\t'*}"
  snippet="${row#*$'\t'}"
  out="$(run_in_set bash -lc "$snippet" 2>&1)" && rc=0 || rc=$?
  if [[ "$rc" -eq 0 ]]; then
    spot_ok=$((spot_ok + 1))
  else
    spot_failed+=("$id: $snippet (rc=$rc)
$out")
  fi
done
if [[ "${#spot_failed[@]}" -eq 0 ]]; then
  report "gate spot-check: ok ($spot_ok invocations/imports ran)"
else
  gate_failed "spot-check"
  report "gate spot-check: FAILED (${#spot_failed[@]} of $spot_ok failed) —
$(printf '%s\n---\n' "${spot_failed[@]}")"
fi
report "timing  spot-check: $(elapsed "$t0" "$(now)")"

# Gate 5: dpkg -L per member dpkg package (batched per member, one run each).
t0=$(now)
dpkgl_failed=()
dpkgl_warn=()
dpkgl_pkgs=0
for id in "${members[@]}"; do
  mapfile -t pkgs < "$work/dump-$id/dpkg.new"
  ((${#pkgs[@]})) || continue # pip-only member: nothing for dpkg -L
  dpkgl_pkgs=$((dpkgl_pkgs + ${#pkgs[@]}))
  out="$(run_in_set bash -c '
      for p in "$@"; do
        dpkg -L "$p" >/dev/null 2>&1 || { printf "fail %s\n" "$p"; continue; }
        dpkg -L "$p" | { found=0; while IFS= read -r f; do
          [ -n "$f" ] && [ -e "$f" ] && { found=1; break; }
        done; [ "$found" -eq 1 ] || printf "warn %s\n" "$p"; }
      done' bash "${pkgs[@]}")" && rc=0 || rc=$?
  while IFS= read -r line; do
    case "$line" in
      fail\ *) dpkgl_failed+=("$id/${line#fail }") ;;
      warn\ *) dpkgl_warn+=("$id/${line#warn }") ;;
    esac
  done <<< "$out"
  [[ "$rc" -eq 0 ]] || dpkgl_failed+=("$id (dpkg -L batch rc=$rc)")
done
if [[ "${#dpkgl_failed[@]}" -eq 0 ]]; then
  report "gate dpkg -L: ok ($dpkgl_pkgs member packages)"
  for w in "${dpkgl_warn[@]:-}"; do
    [[ -z "$w" ]] && continue
    report "warn dpkg -L: $w lists no existing path (expected for path-excluded docs only)"
  done
else
  gate_failed "dpkg -L"
  report "gate dpkg -L: FAILED —
$(printf '%s\n' "${dpkgl_failed[@]}")"
fi
report "timing  dpkg -L: $(elapsed "$t0" "$(now)")"

# Gate 6: full-vs-incremental file-list equivalence (--full-vs).
if [[ -n "$full_vs" ]]; then
  t0=$(now)
  podman image exists "$full_vs" || fail "comparison image does not exist: $full_vs"
  file_list() {
    podman run --rm --network=none "$1" find / -xdev \
      \( -path /proc -o -path /sys -o -path /dev -o -path /run -o -path /tmp \
         -o -path /var/log -o -path /var/cache/ldconfig -o -path /var/lib/dpkg/triggers \
         -o -path /var/lib/dpkg/updates -o -path /var/lib/dpkg/lock* -o -path /etc/machine-id \) \
      -prune -o -print | LC_ALL=C sort -u
  }
  file_list "$set_tag" > "$work/files-a.txt"
  file_list "$full_vs" > "$work/files-b.txt"
  diff_out="$(diff "$work/files-a.txt" "$work/files-b.txt")" && rc=0 || rc=$?
  if [[ "$rc" -eq 0 ]]; then
    report "gate full-vs: ok (file lists equivalent, volatile paths ignored)"
  else
    gate_failed "full-vs"
    report "gate full-vs: FAILED — $full_vs vs $set_tag (volatile paths ignored):
$diff_out"
  fi
  report "timing  full-vs: $(elapsed "$t0" "$(now)")"
fi

# --- verdict ----------------------------------------------------------------------
if [[ "${#failures[@]}" -eq 0 ]]; then
  report "verify-set-image: PASS $set_tag"
  exit 0
fi
report "verify-set-image: FAILED $set_tag — gates failed: ${failures[*]}"
exit 1
