#!/usr/bin/env bash
# Seam 1-5 (Spec #47, Ticket #53): End-to-End-Demo des Custom-Edition-
# Green-Paths. Fixture-Dist-Baum mit den echten Skripten; podman via
# PATH-Mock, der seine Argumente in $PODMAN_LOG protokolliert.
#
# Schritte:
#   1. Custom-Stage in Dockerfile.custom -> live in AVAILABLE_EDITIONS
#   2. build-container.sh <custom-name> und 'all' (gemockter podman)
#   3. start.sh --edition <custom-name>; Unbekannte werden abgelehnt
#   4. sandbox_config.json nimmt die Custom-Edition auf wie eine Built-in
#   5. Namens-Kollision + invalid-char warnen und werden uebersprungen
#   6. Mode-Discovery und Egress-Proxy-Build unveraendert
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

# Fixture-Dist-Verzeichnis: echtes Dockerfile + echte Skripte + Dummy-Proxy
fixture_dist="$test_home/dist"
mkdir -p "$fixture_dist/scripts" "$fixture_dist/proxy"
cp "$PROJECT_ROOT/dist/Dockerfile" "$fixture_dist/Dockerfile"
cp "$PROJECT_ROOT/dist/scripts/start.sh" "$fixture_dist/scripts/"
cp "$PROJECT_ROOT/dist/scripts/build-container.sh" "$fixture_dist/scripts/"
cat > "$fixture_dist/proxy/Dockerfile" <<'EOF'
FROM alpine
EOF

# Custom-Dockerfile: Template als Basis, Ehange-Stages von stdin
make_custom_dockerfile() {
  cp "$PROJECT_ROOT/dist/Dockerfile.custom" "$fixture_dist/Dockerfile.custom"
  cat >> "$fixture_dist/Dockerfile.custom"
}

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/podman" <<EOF
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
chmod +x "$fake_bin/podman"

export PATH="$fake_bin:/usr/bin:/bin"
export PODMAN_LOG="$test_home/podman.log"
PODMAN_MISSING_IMAGES=""

build_script="$fixture_dist/scripts/build-container.sh"
start_script="$fixture_dist/scripts/start.sh"
tui_script="$PROJECT_ROOT/dist/scripts/start-tui.sh"

run_build() {
  ( cd "$test_home" && bash "$build_script" "$@" > /dev/null 2> "$test_home/build-err.log" )
}

run_start() {
  ( cd "$test_home" && bash "$start_script" "$@" > "$test_home/start-out.log" 2> "$test_home/start-err.log" )
}

# TUI-Harness: start-tui.sh sourcen, SCRIPT_DIR aufs Fixture lenken,
# Editions-Discovery ausfuehren; Ausgabe = Editionen, dann "modes", dann Modes
run_tui_editions() {
  bash -e -c '
    source "$1"
    SCRIPT_DIR="$2"
    detect_available_editions
    printf "%s\n" "${AVAILABLE_EDITIONS[@]}"
    printf "%s\n" modes
    printf "%s\n" "${AVAILABLE_MODES[@]}"
  ' _ "$tui_script" "$fixture_dist/scripts" 2> "$test_home/tui-warnings.log"
}

targets_in_log() {
  grep -o -- '--target opencode-sandbox-[a-z0-9_-]*' "$PODMAN_LOG" \
    | sed -E 's/--target //' | sort -u
}

# --- 1: Custom-Stage erscheint live / verschwindet bei Entfernung ------------
run_tui_editions > "$test_home/editions-before.txt"
for ed in base web embedded full; do
  grep -qx "$ed" "$test_home/editions-before.txt"
done
if grep -qx 'custom' "$test_home/editions-before.txt"; then
  printf 'custom stage discovered before being added\n' >&2
  exit 1
fi
run_tui_editions | GREP_PATH=/dev/null grep -qx modes
for mode in offline cbm_ui hil_mode; do
  run_tui_editions | awk '/^modes$/{f=1;next} f' | grep -qx "$mode"
done

make_custom_dockerfile <<'EOF'
FROM opencode-sandbox-base AS opencode-sandbox-custom
USER dev
WORKDIR /home/dev/project
ENTRYPOINT ["/bin/bash", "-l"]
EOF

run_tui_editions > "$test_home/editions-with-custom.txt"
grep -qx 'custom' "$test_home/editions-with-custom.txt"
printf 'live discovery of custom stage test passed\n'

make_custom_dockerfile
run_tui_editions > "$test_home/editions-removed.txt"
if grep -qx 'custom' "$test_home/editions-removed.txt"; then
  printf 'removed custom stage still discovered\n' >&2
  exit 1
fi
printf 'removed custom stage disappears test passed\n'

cat >> "$fixture_dist/Dockerfile.custom" <<'EOF'
FROM opencode-sandbox-base AS opencode-sandbox-custom
USER dev
WORKDIR /home/dev/project
ENTRYPOINT ["/bin/bash", "-l"]
EOF

# --- 2: build-container.sh <custom-name>, 'all', no-Arg-Default --------------
: > "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_build custom
grep -q -- '--network host' "$PODMAN_LOG"
grep -q -- '-t opencode-sandbox-custom' "$PODMAN_LOG"
grep -q -- '--target opencode-sandbox-custom' "$PODMAN_LOG"
grep -q -- '-f Dockerfile.custom' "$PODMAN_LOG"
printf 'custom build by name test passed\n'

: > "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_build all
targets_in_log > "$test_home/all-targets.txt"
for ed in base web embedded full custom; do
  grep -qx "opencode-sandbox-$ed" "$test_home/all-targets.txt"
done
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="" run_build
targets_in_log > "$test_home/noarg-targets.txt"
cmp -s "$test_home/all-targets.txt" "$test_home/noarg-targets.txt"
printf 'all and no-argument default test passed\n'

# --- 3: start.sh akzeptiert Custom-Edition; Unbekannte mit Liste abgelehnt ---
project_root="$test_home/projects/demo"
mkdir -p "$project_root"

: > "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_start "$project_root" --edition custom --detach
grep -q -- 'opencode-sandbox-custom' "$PODMAN_LOG"
printf 'start.sh accepts custom edition test passed\n'

if run_start "$project_root" --edition doesnotexist; then
  printf 'start.sh accepted unknown edition\n' >&2
  exit 1
fi
for known in base web embedded full custom; do
  grep -q "$known" "$test_home/start-err.log"
done
printf 'start.sh unknown edition error lists discovered editions test passed\n'

# --- 4: Custom-Edition landet in sandbox_config.json und wird angewandt ------
export HOME="$test_home"
mkdir -p "$HOME/.config/oc-sandbox"
printf '%s\n' '{"projects": [], "version": "1.0"}' > "$HOME/.config/oc-sandbox/projects.json"

registry_project="$test_home/projects/registered"
mkdir -p "$registry_project"

export TEST_NATIVE_START_ARGS="$test_home/native-start.args"

bash -e -c '
  source "$1"
  SCRIPT_DIR="$2"
  TUI_MODE=bash
  project="$3"
  native_dir="$4"
  apply_project="$5"

  add_project_to_registry "custom-edition-project" "$project" none
  create_sandbox_config "$project" custom offline console none

  mkdir -p "$native_dir"
  printf "%s\n" "#!/usr/bin/env bash" "printf \"%s\n\" \"\$*\" >> \"\${TEST_NATIVE_START_ARGS}\"" > "$native_dir/start.sh"
  chmod +x "$native_dir/start.sh"
  SCRIPT_DIR="$native_dir"

  start_container "$project" "$project/.opencode_config/sandbox_config.json" true
' _ "$tui_script" "$fixture_dist/scripts" "$registry_project" "$test_home/native-dir" "unused" \
  || { cat "$test_home/tui-start.log" 2>/dev/null || true; exit 1; }

config_file="$registry_project/.opencode_config/sandbox_config.json"
[[ "$(jq -r '.container_edition' "$config_file")" == "custom" ]]
grep -Eq -- '(^|.* )--edition custom( .*|$)' "$test_home/native-start.args"
printf 'sandbox_config.json records and applies custom edition test passed\n'

# 'Build now'-Seam fuer die Custom-Edition: fehlendes Image ->
# build_container_for_edition ruft build-container.sh auf, der baut sie.
: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="opencode-sandbox-custom" bash -e -c '
  source "$1"
  SCRIPT_DIR="$2"
  if ! check_container_images_exist custom; then
    build_container_for_edition custom > /dev/null 2>&1
  fi
' _ "$tui_script" "$fixture_dist/scripts"
grep -q -- '--target opencode-sandbox-custom' "$PODMAN_LOG"
grep -q -- '-f Dockerfile.custom' "$PODMAN_LOG"
printf 'custom edition build-now path test passed\n'

# --- 5: Namens-Kollision und invalid-char warnen und werden übersprungen -----
make_custom_dockerfile <<'EOF'
FROM opencode-sandbox-base AS opencode-sandbox-web
FROM opencode-sandbox-base AS opencode-sandbox-B@d
FROM opencode-sandbox-base AS opencode-sandbox-Rust
EOF

run_tui_editions > "$test_home/editions-collide.txt" 2> "$test_home/collide-warnings.txt"
cat "$test_home/tui-warnings.log" >> "$test_home/collide-warnings.txt"
for ed in base web embedded full; do
  grep -qx "$ed" "$test_home/editions-collide.txt"
done
if grep -q 'B@d' "$test_home/editions-collide.txt"; then
  printf 'invalid stage name entered the menu\n' >&2
  exit 1
fi
grep -q 'invalid characters' "$test_home/collide-warnings.txt"
grep -q 'Dockerfile and Dockerfile.custom' "$test_home/collide-warnings.txt"

# Kollidierende web-Edition kommt ins Ziel aus dem shipped Dockerfile (kein
# sideswept override aus Dockerfile.custom):
: > "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_build web
grep -q -- '--target opencode-sandbox-web' "$PODMAN_LOG"
! grep -q -- '-f Dockerfile.custom' "$PODMAN_LOG"

: > "$PODMAN_LOG"; PODMAN_MISSING_IMAGES="" run_build all
if grep -q -- '--target opencode-sandbox-B@d' "$PODMAN_LOG"; then
  printf 'invalid stage name was built\n' >&2
  exit 1
fi
# Regression: invalid-char Stage darf nicht per Lowercasing zur gueltigen
# Edition umgedeutet werden (start.sh akzeptiert --edition rust nicht).
if run_start "$project_root" --edition "rust"; then
  printf 'start.sh accepted edition derived from invalid stage name\n' >&2
  exit 1
fi
if run_start "$project_root" --edition "Rust"; then
  printf 'start.sh accepted invalid edition name\n' >&2
  exit 1
fi
grep -q 'Ung\u00fcltiger\|Editions-Name' "$test_home/start-err.log"
printf 'warn-and-skip collision and invalid-char end to end test passed\n'

# --- 6: Mode-Discovery und Egress-Proxy-Build unveraendert ------------------
run_tui_editions > "$test_home/editions-final.txt"
for mode in offline cbm_ui hil_mode; do
  awk '/^modes$/{f=1;next} f' "$test_home/editions-final.txt" | grep -qx "$mode"
done
printf 'mode discovery untouched test passed\n'

: > "$PODMAN_LOG"
PODMAN_MISSING_IMAGES="" run_build base
grep -q -- 'build -t oc-proxy -f proxy/Dockerfile proxy/' "$PODMAN_LOG"
for ed in base; do
  grep -qx "opencode-sandbox-$ed" < <(targets_in_log)
done
printf 'egress proxy build untouched test passed\n'

printf 'edition e2e tests passed\n'
