#!/usr/bin/env bash
# The shipped toolchain catalog (spec toolchain-sets §3, ticket #197): the 21
# unit recipes, the 9 edition bundles, and the custom/ example template.
# Verifies the shipped tree through the shipped lint/discovery seam
# (dist/scripts/toolchains.sh) against the §3 tables as the source of truth.
# Runs anywhere, no podman needed.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="$PROJECT_ROOT/dist/scripts/toolchains.sh"
root="$PROJECT_ROOT/dist/toolchains"

# The §3 unit table (spec toolchain-sets §3) — the catalog's ids, as literals.
spec_units=(
  cpp rust java go node-dev playwright
  arm-embedded arduino platformio micropython picoscope pyvisa usb
  ros2 octave python-science latex pdf-tools pandoc fonts libreoffice
)

# The §3 edition→bundle table — name<TAB>space-separated members, as literals.
spec_bundles=(
  "web	node-dev playwright"
  "embedded	go arm-embedded arduino platformio micropython picoscope pyvisa usb"
  "swdev	cpp rust java go"
  "matlab	octave python-science"
  "ros2	ros2"
  "writing	libreoffice pandoc fonts"
  "latex	latex pdf-tools pandoc fonts python-science"
  "chat	"
  "pkm	"
)

fail() { printf 'test-catalog: %s\n' "$1" >&2; exit 1; }

discover() { TOOLCHAINS_ROOT="$root" bash "$helper" discover; }
unit_status() { discover | awk -F'\t' -v id="$1" '$1 == "unit" && $2 == id { print $3 }'; }
bundle_field() { discover | awk -F'\t' -v n="$1" '$1 == "bundle" && $2 == n { print $4 }'; }
warnings() { discover | awk -F'\t' '$1 == "warning" { print }'; }

# --- 1: recipes/ holds exactly the 21 §3 ids, lowercase [a-z0-9-] -------------
found="$(cd "$root/recipes" && ls -1 *.Dockerfile 2>/dev/null | sed 's/\.Dockerfile$//' | sort)" \
  || fail "recipes/ is missing or unreadable"
expected="$(printf '%s\n' "${spec_units[@]}" | sort)"
[[ "$found" == "$expected" ]] || fail "recipes/ ids do not match the §3 unit table — got:
$found
expected:
$expected"
bad_id="$(printf '%s\n' "${spec_units[@]}" | grep -vE '^[a-z0-9-]+$' || true)"
[[ -z "$bad_id" ]] || fail "ids outside [a-z0-9-]: $bad_id"
printf 'recipes/ ids match the §3 unit table (21 units)\n'

# --- 2: every shipped recipe lints clean and discovers as a valid unit --------
for id in "${spec_units[@]}"; do
  out="$(bash "$helper" lint "$root/recipes/$id.Dockerfile" 2>&1)" \
    || fail "recipe '$id' fails the shipped lint:
$out"
  [[ -z "$out" ]] || fail "recipe '$id' lints with output (zero-warning rule):
$out"
  [[ "$(unit_status "$id")" == "valid" ]] \
    || fail "recipe '$id' does not discover as a valid unit"
done
printf 'all 21 recipes lint clean and discover valid\n'

# --- 3: discovery is warning-free ---------------------------------------------
w="$(warnings)"
[[ -z "$w" ]] || fail "discovery emits warnings (the shipped catalog must be clean):
$w"
printf 'discovery emits no warnings\n'

# --- 4: manifest bundles match the §3 edition→bundle table exactly -------------
manifest="$root/manifest.json"
[[ -f "$manifest" ]] || fail "manifest.json is missing"
names="$(jq -r '.bundles | keys[]' "$manifest")" \
  || fail "manifest.json is not a bundles-only object"
expected_names="$(printf '%s\n' "${spec_bundles[@]}" | cut -f1)"
diff_out="$(diff <(printf '%s\n' "$names" | sort) <(printf '%s\n' "$expected_names" | sort))" \
  || fail "manifest bundle names do not match the §3 table:
$diff_out"
for row in "${spec_bundles[@]}"; do
  name="${row%%$'\t'*}"
  members="${row#*$'\t'}"
  got="$(bundle_field "$name")"
  # discovery renders sorted members; empty bundles come back empty
  expected_sorted="$(printf '%s\n' $members | sort | paste -sd' ' -)"
  [[ "$got" == "$expected_sorted" ]] \
    || fail "bundle '$name' members do not match the §3 table — got '$got', expected '$expected_sorted'"
done
printf 'manifest bundles match the §3 edition→bundle table (9 bundles, chat/pkm empty)\n'

# --- 5: every bundle member resolves to a valid recipe -------------------------
for row in "${spec_bundles[@]}"; do
  name="${row%%$'\t'*}"
  for m in ${row#*$'\t'}; do
    TOOLCHAINS_ROOT="$root" bash "$helper" resolve "$m" >/dev/null \
      || fail "bundle '$name' member '$m' does not resolve to a valid recipe"
  done
done
printf 'every bundle member resolves\n'

# --- 6: bundle names and member ids are lowercase [a-z0-9-] --------------------
bad="$(printf '%s\n' "$names" | grep -vE '^[a-z0-9-]+$' || true)"
[[ -z "$bad" ]] || fail "bundle names outside [a-z0-9-]: $bad"
jq -r '.bundles | to_entries[] | .value[]' "$manifest" | grep -vE '^[a-z0-9-]+$' \
  && fail "bundle member ids outside [a-z0-9-]" || true
printf 'bundle names and member ids are [a-z0-9-]\n'

# --- 7: the custom/ example template ships inert and outside discovery --------
template="$root/custom/example.Dockerfile.template"
[[ -f "$template" ]] || fail "the custom/ example template is missing ($template)"
tmpl_lines="$(grep -cvE '^[[:space:]]*(#|$)' "$template" || true)"
[[ "$tmpl_lines" -eq 0 ]] \
  || fail "the custom/ example template has $tmpl_lines uncommented directive line(s) — a shipped template must be inert (all comments)"
out="$(bash "$helper" lint "$template" 2>&1)" \
  || fail "the custom/ example template fails the shipped lint:
$out"
custom_recipes="$(cd "$root/custom" && ls -1 *.Dockerfile 2>/dev/null || true)"
[[ -z "$custom_recipes" ]] \
  || fail "custom/ ships user recipes — only the inert template belongs there:
$custom_recipes"
printf 'custom/ example template ships inert and lint-clean\n'

# --- 8: discovery lists exactly 21 units + 9 bundles ----------------------------
units="$(discover | awk -F'\t' '$1 == "unit" { print $2 }' | sort)"
[[ "$units" == "$expected" ]] \
  || fail "discovery does not list exactly the 21 §3 units:
$units"
bundles="$(discover | awk -F'\t' '$1 == "bundle" { print $2 }' | sort)"
[[ "$bundles" == "$(printf '%s\n' "${spec_bundles[@]}" | cut -f1 | sort)" ]] \
  || fail "discovery does not list exactly the 9 §3 bundles:
$bundles"
printf 'discovery lists exactly 21 units + 9 bundles\n'

printf 'test-catalog: all checks passed\n'
