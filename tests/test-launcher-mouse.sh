#!/usr/bin/env bash
set -euo pipefail

# Regression: the minimal TUI launcher's buttons are mouse-clickable.
#
# The terminal reports SGR mouse clicks in *screen* coordinates, so the
# TUI must run on the alt screen (view anchored at the screen origin) for
# the launcher's view-relative hit rects to match. On the inline face the
# rows sit below the banner/migration output and every click missed —
# this test clicks the rendered Open WebUI and Quit buttons and requires
# both the browser-open and a clean exit, which fails (timeout) without
# the alt screen.

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

mkdir -p "$test_home/bin"
fake_bin="$test_home/bin"

cat > "$fake_bin/xdg-open" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$test_home/xdg-open.log"
exit 0
EOF
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin/xdg-open" "$fake_bin/podman"
export PATH="$fake_bin:/usr/bin:/bin"

bin="$test_home/oc-sandbox"
(cd "$PROJECT_ROOT" && go build -o "$bin" ./cmd/oc-sandbox)

# SGR mouse presses at the rendered buttons: Open WebUI at screen row 3
# (view row 2, 1-based), column 4; Quit at the same row, column 21. The
# press/release pair is sent twice, spaced by run-pty's chunk timing.
click_open=$'\x1b[<0;4;3M\x1b[<0;4;3m'
click_quit=$'\x1b[<0;21;3M\x1b[<0;21;3m'

out="$test_home/pty-output"
if ! python3 "$PROJECT_ROOT/tests/run-pty.py" --timeout 8 \
    --input "$click_open|$click_quit" \
    -- "$bin" >"$out"; then
  printf 'launcher pty run did not exit cleanly (mouse clicks ignored?)\n' >&2
  exit 1
fi

# Alt screen + mouse capture active: clicks are screen-relative and can
# only match with the face anchored at the screen origin.
grep -qF -- $'\x1b[?1049h' "$out"
grep -qF -- $'\x1b[?1002h' "$out"
grep -qF -- $'\x1b[?1006h' "$out"

# The Open WebUI click reached the platform opener with the tokenized URL.
grep -q '/t/' "$test_home/xdg-open.log"

# Post-exit transcript: the URL is re-printed to scrollback after the
# alt screen restores (launch print + exit summary ⇒ at least twice).
[[ "$(grep -c 'WebUI: .*http://127.0.0.1:[0-9]*/t/' "$out")" -ge 2 ]]

printf 'launcher mouse click tests passed\n'
