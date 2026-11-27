#!/usr/bin/env bash
# tests/test-merge.sh — fixture tests for the set assembly's merge core
# (toolchain sets #200, spec §5/§12): merge-pm.py runs directly against a
# fixture staging tree + fixture root (no podman) and must
#   - extract every member's payload archive in canonical (alphabetical by
#     member id) order — a contested path ends up with the alphabetically
#     last member's content, matching the PM merge's last-canonical-writer,
#   - stanza-union the dpkg status files (package stanzas deduped by name,
#     info entries unioned, last canonical writer wins a contested package;
#     the target root's own status is the first writer),
#   - re-emit the fragment ENVs (duplicate keys: alphabetical-last wins,
#     emitted sorted) into /etc/profile.d/oc-toolchains-env.sh.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MERGE="$PROJECT_ROOT/dist/scripts/merge-pm.py"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

# --- fixture tree -----------------------------------------------------------------
# staging/<id>/  — one dir per member id: payload.tar.gz, pm/ (status + info/),
#                  envs (KEY=value lines, builder-rendered from the fragment)
# The target root carries the base image's dpkg state (base.Dockerfile's own
# status + info entries) — the first writer every member may override.

# target root: base state knows hello + base-files
mkdir -p "$root/var/lib/dpkg/info" "$root/etc/profile.d" "$root/usr/bin"
cat > "$root/var/lib/dpkg/status" <<'EOF'
Package: base-files
Status: install ok installed

Package: hello
Status: install ok installed
Version: 1.0-base
EOF
printf 'base\n' > "$root/var/lib/dpkg/info/hello.md5sums"
printf 'base-list\n' > "$root/var/lib/dpkg/info/base-files.list"
printf 'base-binary\n' > "$root/usr/bin/tool"

# member "a": payload installs /usr/bin/tool (contested path) and /usr/bin/a-only,
# its status carries a contested hello stanza (v2) + its own pkg-a,
# its info dir contests hello.md5sums, its envs carry a contested key.
a="$root/staging/a"
mkdir -p "$a/pm/info" "$root/tree-a/usr/bin" "$root/tree-a/usr/bin/a-only-dir"
printf 'content-from-a\n' > "$root/tree-a/usr/bin/tool"
printf 'a\n' > "$root/tree-a/usr/bin/a-only"
tar -C "$root/tree-a" -cf - usr \
  | gzip -n > "$a/payload.tar.gz"
cat > "$a/pm/status" <<'EOF'
Package: base-files
Status: install ok installed

Package: hello
Status: install ok installed
Version: 2.0-a

Package: pkg-a
Status: install ok installed
EOF
printf 'from-a\n' > "$a/pm/info/hello.md5sums"
printf 'a-extra\n' > "$a/pm/info/pkg-a.list"
printf 'COMMON=from-a\nONLY_A=alpha\n' > "$a/envs"

# member "b": contests the same payload path, hello stanza (v3) and env key;
# adds its own pkg + info entry.
b="$root/staging/b"
mkdir -p "$b/pm/info" "$root/tree-b/usr/bin"
printf 'content-from-b\n' > "$root/tree-b/usr/bin/tool"
printf 'b\n' > "$root/tree-b/usr/bin/b-only"
tar -C "$root/tree-b" -cf - usr \
  | gzip -n > "$b/payload.tar.gz"
cat > "$b/pm/status" <<'EOF'
Package: base-files
Status: install ok installed

Package: hello
Status: install ok installed
Version: 3.0-b

Package: pkg-b
Status: install ok installed
EOF
printf 'from-b\n' > "$b/pm/info/pkg-b.md5sums"
printf 'COMMON=from-b\nKEY_B=beta\n' > "$b/envs"

# --- 1: payload extraction in canonical order — contested path: last writer wins --

"$MERGE" "$root/staging" "$root" 2> "$root/.merge-err" \
  || fail "merge-pm.py refused a valid fixture: $(cat "$root/.merge-err")"
grep -q 'content-from-b' "$root/usr/bin/tool" \
  || fail "contested payload path did not resolve to the alphabetically last member (b)"
grep -q 'a' "$root/usr/bin/a-only" || fail "member a's unique payload file is missing"
grep -q 'b' "$root/usr/bin/b-only" || fail "member b's unique payload file is missing"
printf 'canonical-order payload extraction test passed\n'

# --- 2: stanza-union — contested package: last canonical writer wins ---------------

status="$root/var/lib/dpkg/status"
grep -q '^Package: base-files$' "$status" || fail "merged status lost base-files"
grep -q '^Package: pkg-a$' "$status" || fail "merged status lost pkg-a (non-contested union)"
grep -q '^Package: pkg-b$' "$status" || fail "merged status lost pkg-b (non-contested union)"
grep -q '^Version: 3.0-b$' "$status" || fail "contested package 'hello' did not resolve to the alphabetically last member (b)"
if grep -q 'Version: 2.0-a' "$status"; then
  fail "merged status kept member a's contested hello stanza beside b's"
fi
grep -q '^Version: 1.0-base$' "$status" \
  && fail "member b's hello stanza did not override the base seed"
# deterministic rendering: stanzas sorted by package name
names="$(sed -n 's/^Package: //p' "$status")"
[[ "$names" == "$(LC_ALL=C sort -u <<< "$names")" ]] \
  || fail "merged status stanzas are not rendered sorted by package name"
printf 'dpkg stanza-union test passed\n'

# --- 3: info entries unioned, contested entry: last canonical writer wins ----------

grep -q 'from-b' "$root/var/lib/dpkg/info/pkg-b.md5sums" || fail "member b's info entry is missing"
grep -q 'a-extra' "$root/var/lib/dpkg/info/pkg-a.list" || fail "member a's info entry is missing"
grep -q 'base-list' "$root/var/lib/dpkg/info/base-files.list" || fail "the root's own info entry was clobbered"
grep -q 'from-a' "$root/var/lib/dpkg/info/hello.md5sums" || fail "member a's contested info entry was dropped (a is its only writer)"
printf 'info-entry union test passed\n'

# --- 4: ENV re-emission — alphabetical-last wins, sorted output ---------------------

envfile="$root/etc/profile.d/oc-toolchains-env.sh"
[[ -f "$envfile" ]] || fail "the ENV re-emission file is missing"
grep -q "^export COMMON='from-b'$" "$envfile" \
  || fail "contested ENV key COMMON did not resolve to the alphabetically last member (b)"
grep -q "^export ONLY_A='alpha'$" "$envfile" || fail "member a's unique ENV key is missing"
grep -q "^export KEY_B='beta'$" "$envfile" || fail "member b's unique ENV key is missing"
lines="$(sed -n 's/^export \([^=]*\)=.*/\1/p' "$envfile")"
[[ "$lines" == "$(LC_ALL=C sort <<< "$lines")" ]] \
  || fail "re-emitted ENV lines are not sorted alphabetically"
perms="$(stat -c '%a' "$envfile")"
[[ "$perms" == "644" ]] || fail "the ENV re-emission file has mode $perms (expected 644)"
printf 'ENV re-emission test passed\n'

# --- 5: single-quote escaping in ENV values -----------------------------------------

esc="$(mktemp -d)"
mkdir -p "$esc/staging/m/pm"
printf "QUOTE=it's\n" > "$esc/staging/m/envs"
tar -C "$root/tree-a" -cf - usr | gzip -n > "$esc/staging/m/payload.tar.gz"
cat > "$esc/staging/m/pm/status" <<'EOF'
Package: base-files
Status: install ok installed
EOF
"$MERGE" "$esc/staging" "$esc" 2> "$esc/.merge-err" \
  || fail "merge-pm.py refused the quoting fixture: $(cat "$esc/.merge-err")"
grep -qF "export QUOTE='it'\''s'" "$esc/etc/profile.d/oc-toolchains-env.sh" \
  || fail "a single quote in an ENV value was not shell-escaped"
# the emitted file must round-trip through a shell
value="$(bash -c "source '$esc/etc/profile.d/oc-toolchains-env.sh'; printf '%s' \"\$QUOTE\"")"
[[ "$value" == "it's" ]] || fail "the escaped ENV value does not round-trip (got '$value')"
rm -rf "$esc"
printf 'ENV single-quote escaping test passed\n'

# --- 6: empty staging is a no-op ------------------------------------------------------

noop="$(mktemp -d)"
mkdir -p "$noop/var/lib/dpkg" "$noop/staging"
printf 'Package: base-files\nStatus: install ok installed\n' > "$noop/var/lib/dpkg/status"
cp "$noop/var/lib/dpkg/status" "$noop/status-before"
"$MERGE" "$noop/staging" "$noop" 2> "$noop/.merge-err" \
  || fail "merge-pm.py refused an empty staging tree: $(cat "$noop/.merge-err")"
cmp "$noop/status-before" "$noop/var/lib/dpkg/status" \
  || fail "an empty assembly touched the target's dpkg status"
[[ ! -e "$noop/etc/profile.d/oc-toolchains-env.sh" ]] \
  || fail "an assembly with no ENVs wrote an ENV re-emission file"
rm -rf "$noop"
printf 'empty-staging no-op test passed\n'

printf 'test-merge: all checks passed\n'
