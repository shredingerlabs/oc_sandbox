#!/usr/bin/env bash
# Seam 3 (Spec #47): End-to-end-Tests fuer build-container.sh mit PATH-mock
# podman, das seine Argumente in $PODMAN_LOG protokolliert.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

# Fixture-Dist-Verzeichnis: echtes Dockerfile + Dummy-Proxy + Test-Dockerfile.custom
fixture_dist="$test_home/dist"
mkdir -p "$fixture_dist/scripts" "$fixture_dist/proxy"
cp "$PROJECT_ROOT/dist/Dockerfile" "$fixture_dist/Dockerfile"
cp "$PROJECT_ROOT/dist/scripts/build-container.sh" "$fixture_dist/scripts/"
cat > "$fixture_dist/proxy/Dockerfile" <<'EOF'
FROM alpine
EOF
touch "$fixture_dist/proxy/dummy"

make_custom_dockerfile() {
  cat > "$fixture_dist/Dockerfile.custom"
}

write_podman_mock() {
  # $PODMAN_MISSING_IMAGES: leerzeichengetrennte Images, fuer die `podman image
  # exists` fehlschlaegt (alle anderen existieren).
  cat > "$test_home/bin/podman" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "\${PODMAN_LOG}"
case "\$1 \$2" in
  "image exists")
    case " \${PODMAN_MISSING_IMAGES:-} " in
      *" \$3 "*) exit 1 ;;
    esac
    ;;
esac
exit 0
EOF
  chmod +x "$test_home/bin/podman"
}

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
write_podman_mock
export PATH="$fake_bin:/usr/bin:/bin"
export PODMAN_LOG="$test_home/podman.log"

script="$fixture_dist/scripts/build-container.sh"

targets_from_log() {
  grep -o -- '--target opencode-sandbox-[a-z0-9_-]*' "$PODMAN_LOG" \
    | sed -E 's/--target //' | sort -u
}

run_script() {
  ( cd "$test_home" && bash "$script" "$@" > /dev/null 2> "$test_home/err.log" )
}

script_err() {
  ( cd "$test_home" && bash "$script" "$@" > /dev/null 2> "$test_home/err.log" )
}

make_custom_dockerfile <<'EOF'
FROM opencode-sandbox-base AS opencode-sandbox-rust

USER dev
WORKDIR /home/dev/project

ENTRYPOINT ["/bin/bash", "-l"]
EOF

# --- 1: Custom-Edition fuehrt zu generischem podman build -------------------
rm -f "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_script rust
grep -q -- '--network host' "$PODMAN_LOG"
grep -q -- '-t opencode-sandbox-rust' "$PODMAN_LOG"
grep -q -- '--target opencode-sandbox-rust' "$PODMAN_LOG"
grep -q -- '-f Dockerfile.custom' "$PODMAN_LOG"
printf 'custom edition build test passed\n'

# --- 2: rebuild der Custom-Edition ohne fehlendes Parent --------------------
# (base-Image existiert laut Mock -> kein zusaetzlicher base-Build)
rust_builds=$(grep -c -- '--target opencode-sandbox-rust' "$PODMAN_LOG")
[[ "$rust_builds" -eq 1 ]]

# --- 3: 'all' baut jede entdeckte Stage incl. custom ------------------------
rm -f "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_script all
targets="$(targets_from_log)"
for expected in base web embedded full rust; do
  grep -q "opencode-sandbox-$expected" <<< "$targets"
done
printf 'all editions build test passed\n'

# --- 4: kein Argument == 'all' ----------------------------------------------
all_targets="$(targets_from_log)"
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="" run_script
noarg_targets="$(targets_from_log)"
[[ "$all_targets" == "$noarg_targets" ]]
printf 'no-argument defaults to all test passed\n'

# --- 5: fehlender base-Parent wird automatisch zuerst gebaut ----------------
rm -f "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="opencode-sandbox-base web" run_script embedded
base_line=$(grep -n -- '--target opencode-sandbox-base' "$PODMAN_LOG" | head -1 | cut -d: -f1)
emb_line=$(grep -n -- '--target opencode-sandbox-embedded' "$PODMAN_LOG" | head -1 | cut -d: -f1)
[[ -n "$base_line" && -n "$emb_line" && "$base_line" -lt "$emb_line" ]]
printf 'missing base parent auto-build test passed\n'

# --- 6: fehlender Nicht-base-Parent bricht laut ab --------------------------
rm -f "$PODMAN_LOG"
make_custom_dockerfile <<'EOF'
FROM opencode-sandbox-web AS opencode-sandbox-rust

USER dev
ENTRYPOINT ["/bin/bash", "-l"]
EOF
if PODMAN_MISSING_IMAGES="opencode-sandbox-web" script_err rust; then
  printf 'build with missing non-base parent unexpectedly succeeded\n' >&2
  exit 1
fi
grep -q 'opencode-sandbox-web' "$test_home/err.log"
grep -q 'zuerst' "$test_home/err.log"
if grep -q -- '--target opencode-sandbox-rust' "$PODMAN_LOG"; then
  printf 'edition with missing non-base parent was built anyway\n' >&2
  exit 1
fi
printf 'missing non-base parent failure test passed\n'

# --- 7: unbekannter/ungueltiger Editionsname -------------------------------
make_custom_dockerfile <<'EOF'
FROM opencode-sandbox-base AS opencode-sandbox-rust
EOF
rm -f "$PODMAN_LOG"
for bad in nosuchedition Web; do
  if script_err "$bad"; then
    printf 'invalid edition %s unexpectedly succeeded\n' "$bad" >&2
    exit 1
  fi
  grep -q 'opencode-sandbox-rust' "$test_home/err.log"
done
grep -s -q -- '--target opencode-sandbox-nosuchedition' "$PODMAN_LOG" && {
  printf 'unknown edition was passed to podman\n' >&2
  exit 1
}
printf 'unknown edition rejection test passed\n'

# --- 8: Egress-Proxy-Build unveraendert ------------------------------------
: > "$PODMAN_LOG"
if ! PODMAN_MISSING_IMAGES="" script_err base; then
  printf 'base build failed\n' >&2
  exit 1
fi
grep -q -- 'build -t oc-proxy -f proxy/Dockerfile proxy/' "$PODMAN_LOG"
printf 'egress proxy build untouched test passed\n'

printf 'build-container tests passed\n'
