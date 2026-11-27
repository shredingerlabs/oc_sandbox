#!/usr/bin/env bash
# dump.sh — the shared component dump step (toolchain sets #199, spec §4).
#
# Injected into every generated component Dockerfile (COPY + two RUN calls,
# never fragment boilerplate) and runnable host-side against a fixture root by
# the test suite (spec §12) — no podman needed.
#
# The dump step turns a component build into a composable payload:
#   payload archive = dpkg -L of the packages newly installed by the fragment
#                     (a dpkg status diff against a pre-fragment snapshot;
#                     dpkg -L reads exactly /var/lib/dpkg/info/<pkg>.list)
#                     ∪ pip RECORD paths of pips newly installed by the
#                     fragment (dist-info diff against a pre-fragment snapshot)
#                     ∪ the fragment's declared '# extra:' paths
#                     ∪ the whole of /home/dev
#                     — tarred --no-recursion (dpkg -L lists dirs; base-files
#                     owns /, so recursion would tar the whole filesystem) with
#                     an existence filter (dpkg -L lists path-excluded docs and
#                     RECORD paths whose files were pruned).
#   PM-state dump   = the dpkg status file + the /var/lib/dpkg/info entries,
#                     for the set assembly's stanza-union merge.
#   tc.pkgs         = the fragment's normalized dpkg + pip package names (the
#                     set assembly's contest-check label input).
#
# Usage:
#   dump.sh snapshot       pre-fragment: snapshot dpkg + pip state (in-image)
#   dump.sh dump [ROOT]    write the dumps (in-image: ROOT=/ → /oc-dump)
#
# ROOT (env OC_DUMP_ROOT or arg, default /) selects the filesystem root the
# fixture suite runs the dump against. Environment overrides (in-image defaults
# in parentheses; when the env var is unset the helper falls back to the
# root-relative fixture location, then the in-image path):
#   OC_DUMP_PRE_STATUS  pre-fragment dpkg status snapshot
#                       (<ROOT>/var/lib/dpkg/status-pre, then /tmp/oc-status-pre;
#                       absent everywhere → every current package is new)
#   OC_DUMP_PIP_PRE     pre-fragment dist-info snapshot
#                       (<ROOT>/var/lib/dpkg/pip-pre, then /tmp/oc-pip-pre)
#   OC_DUMP_EXTRAS      declared '# extra:' paths, one per line
#                       (<ROOT>/var/lib/dpkg/extras, then /tmp/oc-extras.txt)
#   OC_DUMP_OUT         output dir (default <ROOT>/oc-dump)
set -euo pipefail
shopt -s nullglob

fail() {
  printf 'dump: %s\n' "$*" >&2
  exit 1
}

# --- shared enumeration ---------------------------------------------------------

# _pip_distinfos ROOT — print the $ROOT-prefixed paths of every installed pip
# dist-info directory (the same globs the in-image snapshot uses).
_pip_distinfos() {
  local root=$1 dir
  for dir in \
    "$root"/usr/lib/python3*/dist-packages \
    "$root"/usr/lib/python3/dist-packages \
    "$root"/usr/lib/python3*/site-packages \
    "$root"/usr/local/lib/python3*/dist-packages \
    "$root"/usr/local/lib/python3*/site-packages; do
    [[ -d "$dir" ]] || continue
    find "$dir" -maxdepth 1 -name '*.dist-info' -print
  done | LC_ALL=C sort -u
}

# _norm_abs PATH — lexically normalize an absolute path (resolve "." and ".."
# without touching the filesystem; RECORD entries may carry ../ segments).
_norm_abs() {
  local seg
  local -a parts=()
  local IFS='/'
  for seg in $1; do
    [[ -z "$seg" || "$seg" == "." ]] && continue
    if [[ "$seg" == ".." ]]; then
      ((${#parts[@]})) && unset 'parts[${#parts[@]}-1]'
    else
      parts+=("$seg")
    fi
  done
  local res=""
  for seg in "${parts[@]}"; do
    res+="/$seg"
  done
  [[ -n "$res" ]] || res="/"
  printf '%s\n' "$res"
}

# _status_pkgs FILE — print the package names of a dpkg status file, sorted.
_status_pkgs() {
  sed -n 's/^Package: //p' "$1" 2>/dev/null | LC_ALL=C sort -u
}

# _normalize_pip NAME — PEP 503: runs of [-_.] collapse to '-', lowercased.
_normalize_pip() {
  printf '%s\n' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[-_.]+/-/g'
}

# _strip_root ROOT PATH — make a path archive-relative (drop the $ROOT prefix
# and the leading slash; tar -C "$ROOT" restores it at build/extract time).
_strip_root() {
  local root=$1 path=$2
  path="${path#"$root"}"
  path="${path#/}"
  printf '%s\n' "$path"
}

# _first_existing ARG… — print the first argument that exists on disk.
_first_existing() {
  local f
  for f in "$@"; do
    [[ -e "$f" ]] && { printf '%s\n' "$f"; return 0; }
  done
  return 0
}

# --- snapshot (pre-fragment, runs inside the component build) --------------------

cmd_snapshot() {
  cp /var/lib/dpkg/status /tmp/oc-status-pre
  : > /tmp/oc-pip-pre
  _pip_distinfos / >> /tmp/oc-pip-pre
}

# --- dump (the component build's last RUN; fixture-suite entry point) -----------

cmd_dump() {
  local root="${OC_DUMP_ROOT:-${1:-/}}"
  [[ -d "$root" ]] || fail "root '$root' is not a directory"
  local pre_status="${OC_DUMP_PRE_STATUS:-$(_first_existing \
    "$root/var/lib/dpkg/status-pre" /tmp/oc-status-pre /dev/null)}"
  local pre_pip="${OC_DUMP_PIP_PRE:-$(_first_existing \
    "$root/var/lib/dpkg/pip-pre" /tmp/oc-pip-pre /dev/null)}"
  local extras_file="${OC_DUMP_EXTRAS:-$(_first_existing \
    "$root/var/lib/dpkg/extras" /tmp/oc-extras.txt /dev/null)}"
  local out="${OC_DUMP_OUT:-$root/oc-dump}"
  local status="$root/var/lib/dpkg/status"
  [[ -f "$status" ]] || fail "no dpkg status file at $status — is this a dpkg root?"
  mkdir -p "$out"

  # NUL-separated archive-relative path candidates; the existence filter and
  # the dedupe sort run right before the tar.
  local list="$out/.paths"
  : > "$list"

  # dpkg -L of newly installed packages: every package present now but absent
  # from the pre-fragment snapshot; its info/<pkg>.list entries are exactly
  # what dpkg -L prints.
  local -a new_pkgs=()
  mapfile -t new_pkgs < <(comm -13 \
    <(_status_pkgs "$pre_status") <(_status_pkgs "$status"))
  local pkg lst
  for pkg in "${new_pkgs[@]}"; do
    for lst in "$root/var/lib/dpkg/info/$pkg.list" \
               "$root/var/lib/dpkg/info/$pkg:"*.list; do
      [[ -f "$lst" ]] || continue
      sed -e 's#/*$##' -e 's#^/##' -e '/^$/d' "$lst" | tr '\n' '\0' >> "$list"
    done
  done

  # pip RECORD paths of newly installed pips: dist-info dirs present now but
  # absent from the pre-fragment snapshot.
  local -a new_pips=()
  mapfile -t new_pips < <(comm -13 <(_sort_file "$pre_pip") <(_pip_distinfos "$root"))
  local di record entry
  for di in "${new_pips[@]}"; do
    record="$di/RECORD"
    [[ -f "$record" ]] || continue
    # RECORD paths are relative to the dist-info's parent (the site-packages
    # root); resolve them onto the root lexically.
    while IFS= read -r entry; do
      [[ -z "$entry" ]] && continue
      if [[ "$entry" == /* ]]; then
        _strip_root "$root" "$(_norm_abs "$entry")"
      else
        _strip_root "$root" "$(_norm_abs "$(dirname "$di")/$entry")"
      fi
    done < <(cut -d, -f1 "$record" | sed '/^$/d') | tr '\n' '\0' >> "$list"
  done

  # Declared '# extra:' paths (builder-extracted, one per line).
  if [[ -f "$extras_file" ]]; then
    while IFS= read -r entry; do
      [[ -z "$entry" ]] && continue
      _strip_root "$root" "$(_norm_abs "$entry")"
    done < "$extras_file" | tr '\n' '\0' >> "$list"
  fi

  # The whole of /home/dev (the recipe's ~/.profile.d hooks ride along).
  if [[ -d "$root/home/dev" ]]; then
    find "$root/home/dev" -print0 | while IFS= read -r -d '' p; do
      _strip_root "$root" "$p"
    done | tr '\n' '\0' >> "$list"
  fi

  # Existence filter (dpkg -L lists dirs and path-excluded docs; RECORD may
  # list pruned files), dedupe, then the archive — --no-recursion is load-
  # bearing: base-files owns /, recursion would tar the whole filesystem. The
  # path list stays NUL-separated end to end (--null --files-from).
  sort -z -u "$list" > "$list.uniq"
  : > "$list.exists"
  local p
  while IFS= read -r -d '' p; do
    [[ -e "$root/$p" || -L "$root/$p" ]] && printf '%s\0' "$p" >> "$list.exists"
  done < "$list.uniq"

  if [[ -s "$list.exists" ]]; then
    tar -C "$root" --no-recursion --null --files-from="$list.exists" -cf - \
      | gzip -n > "$out/payload.tar.gz"
  else
    # A valid (empty) tar: one zero block, gzipped — tar refuses to create
    # empty archives itself.
    head -c 1024 /dev/zero | gzip -n > "$out/payload.tar.gz"
  fi

  # PM-state dump: the dpkg status file + the info entries, for the set
  # assembly's stanza-union merge.
  rm -rf "$out/pm"
  mkdir -p "$out/pm"
  cp "$status" "$out/pm/status"
  if [[ -d "$root/var/lib/dpkg/info" ]]; then
    cp -a "$root/var/lib/dpkg/info" "$out/pm/info"
  fi

  # tc.pkgs: normalized dpkg + pip package names of this fragment only — the
  # set assembly's contest check intersects exactly these.
  {
    printf '%s\n' "${new_pkgs[@]}" 2>/dev/null || true
    local name
    for di in "${new_pips[@]}"; do
      name="$(basename "$di" .dist-info)"
      name="${name%%-*}"
      _normalize_pip "$name"
    done
  } | LC_ALL=C sort -u > "$out/tc.pkgs"

  rm -f "$list" "$list.uniq" "$list.exists"
}

# _sort_file FILE — sorted unique lines; a missing file sorts as empty.
_sort_file() {
  if [[ -f "$1" ]]; then
    LC_ALL=C sort -u "$1"
  else
    printf ''
  fi
}

main() {
  (($# >= 1)) || {
    printf 'Usage: dump.sh snapshot | dump [ROOT]\n' >&2
    return 2
  }
  local cmd=$1
  shift
  case "$cmd" in
    snapshot)
      (($# == 0)) || fail "snapshot takes no arguments"
      cmd_snapshot
      ;;
    dump)
      (($# <= 1)) || {
        printf 'Usage: dump.sh dump [ROOT]\n' >&2
        return 2
      }
      cmd_dump "$@"
      ;;
    -h | --help)
      grep -E '^#' "$0" | sed -E 's/^# ?//' | head -50
      ;;
    *)
      fail "unknown command '$cmd' — snapshot or dump"
      ;;
  esac
}

main "$@"
