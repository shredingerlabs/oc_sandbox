#!/usr/bin/env bash
# tests/test-dump.sh — fixture tests for the shared dump step (toolchain sets
# #199, spec §4/§12): dump.sh runs against a fixture root (no podman) and must
# produce the payload archive (dpkg -L status-diff ∪ pip RECORD paths ∪ declared
# '# extra:' paths ∪ the whole /home/dev, tarred --no-recursion with an
# existence filter), the PM-state dump (dpkg status + info entries) and the
# tc.pkgs label list (normalized dpkg + pip names).
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DUMP="$PROJECT_ROOT/dist/scripts/dump.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# payload_members ROOT — run dump.sh against a fixture root, print the payload
# archive's member list (sorted) on stdout; stderr carries the run's stderr.
payload_members() {
  local root=$1
  "$DUMP" dump "$root" > /dev/null 2> "$root/.dump-err"
  tar -tf "$root/oc-dump/payload.tar.gz" | sed -e 's#/\+$##' | LC_ALL=C sort
}

# assert_member / assert_absent — check the sorted member list of the last run
member_list=""
check_member() { grep -qxF "$1" <<< "$member_list" || fail "payload archive is missing member '$1'"; }
check_absent() { grep -qxF "$1" <<< "$member_list" && fail "payload archive unexpectedly contains member '$1'"; return 0; }

# --- fixture tree -------------------------------------------------------------

root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

mkdir -p "$root/var/lib/dpkg/info"
# Pre-fragment status knows only base-files; the fragment "installs" hello.
cat > "$root/var/lib/dpkg/status-pre" <<'EOF'
Package: base-files
Status: install ok installed
EOF
cat > "$root/var/lib/dpkg/status" <<'EOF'
Package: base-files
Status: install ok installed

Package: hello
Status: install ok installed
EOF
# hello's .list is the dpkg -L source: a real file, an existing dir, and a
# path-excluded docs file that does not exist on disk (existence filter).
cat > "$root/var/lib/dpkg/info/hello.list" <<'EOF'
/usr/bin/hello
/usr/share/doc/hello/changelog.Debian.gz
/usr/share/hello
EOF
# base-files owns / and top dirs — the --no-recursion trap: tar'ing / with
# recursion would pull the whole tree in.
cat > "$root/var/lib/dpkg/info/base-files.list" <<'EOF'
/
/usr
/bin
EOF
printf 'md5 lines\n' > "$root/var/lib/dpkg/info/hello.md5sums"
mkdir -p "$root/usr/bin" "$root/usr/share/hello" "$root/usr/lib/deep/nested"
printf 'binary\n' > "$root/usr/bin/hello"
# The recursion canary lives under /usr but in no package .list — it may only
# enter the archive if --no-recursion is violated.
printf 'canary\n' > "$root/usr/lib/deep/nested/x.txt"

# pip: oldpkg was already present pre-fragment (snapshot lists it), Ros_Pkg is
# new; its RECORD names a real file and a missing one (existence filter).
mkdir -p "$root/usr/local/lib/python3.12/dist-packages/Ros_Pkg-1.4.dist-info" \
         "$root/usr/local/lib/python3.12/dist-packages/oldpkg-1.0.dist-info" \
         "$root/usr/local/lib/python3.12/dist-packages/ros_pkg"
cat > "$root/usr/local/lib/python3.12/dist-packages/Ros_Pkg-1.4.dist-info/RECORD" <<'EOF'
ros_pkg/__init__.py,,
ros_pkg/data.dat,,
Ros_Pkg-1.4.dist-info/METADATA,,
EOF
printf 'pkg\n' > "$root/usr/local/lib/python3.12/dist-packages/ros_pkg/__init__.py"
printf 'meta\n' > "$root/usr/local/lib/python3.12/dist-packages/Ros_Pkg-1.4.dist-info/METADATA"
cat > "$root/usr/local/lib/python3.12/dist-packages/oldpkg-1.0.dist-info/RECORD" <<'EOF'
oldpkg.py,,
EOF
printf 'old\n' > "$root/usr/local/lib/python3.12/dist-packages/oldpkg.py"
printf '%s\n' "$root/usr/local/lib/python3.12/dist-packages/oldpkg-1.0.dist-info" \
  > "$root/var/lib/dpkg/pip-pre"

# Declared extras: one existing, one nonexistent (existence filter).
printf '%s\n' "$root/etc/apt/sources.list.d/ros2.list" "$root/etc/apt/keyrings/missing.gpg" \
  > "$root/var/lib/dpkg/extras"
mkdir -p "$root/etc/apt/sources.list.d"
printf 'deb line\n' > "$root/etc/apt/sources.list.d/ros2.list"

# Home capture: a .profile.d hook and a nested config file.
mkdir -p "$root/home/dev/.profile.d" "$root/home/dev/.config/tool"
printf 'export PATH\n' > "$root/home/dev/.profile.d/ros2.sh"
printf 'cfg\n' > "$root/home/dev/.config/tool/t.toml"

# --- 1: the dpkg status diff feeds the payload, --no-recursion holds ----------

member_list="$(payload_members "$root")"
check_member "usr/bin/hello"
check_member "usr/share/hello"
# base-files' / and top-dir entries stay dir-entries-only: no recursion into
# /usr or /bin pulls the canary (or hello's binary again) in.
check_absent "usr/lib/deep/nested/x.txt"
check_absent "usr/lib/deep"
printf 'dpkg status-diff + --no-recursion trap test passed\n'

# --- 2: existence filter -------------------------------------------------------

check_absent "usr/share/doc/hello/changelog.Debian.gz"
# pip RECORD path without a backing file is dropped the same way
check_absent "usr/local/lib/python3.12/dist-packages/ros_pkg/data.dat"
printf 'existence filter test passed\n'

# --- 3: pip RECORD paths of newly installed pips only --------------------------

check_member "usr/local/lib/python3.12/dist-packages/ros_pkg/__init__.py"
check_absent "usr/local/lib/python3.12/dist-packages/oldpkg.py"
printf 'pip RECORD paths test passed\n'

# --- 4: declared extras join the union -----------------------------------------

check_member "etc/apt/sources.list.d/ros2.list"
check_absent "etc/apt/keyrings/missing.gpg"
printf 'extras union test passed\n'

# --- 5: home capture incl. the ~/.profile.d hook --------------------------------

check_member "home/dev/.profile.d/ros2.sh"
check_member "home/dev/.config/tool/t.toml"
printf 'home capture test passed\n'

# --- 6: PM-state dump = dpkg status + info entries -------------------------------

grep -q '^Package: hello$' "$root/oc-dump/pm/status" || fail "pm dump lost the dpkg status"
for info in hello.list hello.md5sums base-files.list; do
  [[ -f "$root/oc-dump/pm/info/$info" ]] || fail "pm dump lost info entry $info"
done
printf 'pm-state dump test passed\n'

# --- 7: tc.pkgs = normalized dpkg + pip names, new only ---------------------------

names="$(cat "$root/oc-dump/tc.pkgs")"
grep -qxF 'hello' <<< "$names" || fail "tc.pkgs is missing dpkg name 'hello'"
grep -qxF 'ros-pkg' <<< "$names" || fail "tc.pkgs is missing normalized pip name 'ros-pkg'"
if grep -qx 'base-files' <<< "$names"; then
  fail "tc.pkgs counts pre-existing base-files as newly installed"
fi
if grep -qx 'oldpkg' <<< "$names"; then
  fail "tc.pkgs counts pre-fragment pip oldpkg as newly installed"
fi
printf 'tc.pkgs label list test passed\n'

# --- 8: a fixture with nothing newly installed still yields a valid archive -------

empty="$(mktemp -d)"
mkdir -p "$empty/var/lib/dpkg/info"
cat > "$empty/var/lib/dpkg/status" <<'EOF'
Package: base-files
Status: install ok installed
EOF
cp "$root/var/lib/dpkg/status-pre" "$empty/var/lib/dpkg/status-pre"
printf '%s\n' "$empty/usr/local/lib/python3.12/dist-packages/oldpkg-1.0.dist-info" \
  > "$empty/var/lib/dpkg/pip-pre"
payload_members "$empty"
tar -tf "$empty/oc-dump/payload.tar.gz" > /dev/null
grep -q '^Package: base-files$' "$empty/oc-dump/pm/status" || fail "empty fixture lost its pm status"
printf 'empty-diff edge test passed\n'

rm -rf "$empty"
printf 'test-dump: all checks passed\n'
