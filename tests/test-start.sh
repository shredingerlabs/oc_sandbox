#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_home=$(mktemp -d)
trap 'rm -rf "$test_home"' EXIT

mkdir -p "$test_home/project" "$test_home/.opencode_config" "$test_home/.opencode_data" \
  "$test_home/.ssh_local" "$test_home/.git_local" "$test_home/.cbm_cache" "$test_home/.bash_local"

fake_bin="$test_home/bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${PODMAN_LOG}"
exit 0
EOF
chmod +x "$fake_bin/podman"
cat > "$fake_bin/ss" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == *":9749"* ]]; then
  printf 'LISTEN 0 128 127.0.0.1:9749 0.0.0.0:*\n'
fi
EOF
chmod +x "$fake_bin/ss"

export PATH="$fake_bin:/usr/bin:/bin"
export PODMAN_LOG="$test_home/podman.log"

if ! bash "$PROJECT_ROOT/dist/scripts/start.sh" "$test_home" --cbm_ui --detach; then
  printf 'start script failed with mocked podman\n' >&2
  exit 1
fi

grep -q -- '--network=pasta:--ipv4-only' "$PODMAN_LOG"
grep -q -- '-p 127.0.0.1:9750:9749' "$PODMAN_LOG"
printf 'start networking and CBM port tests passed\n'
