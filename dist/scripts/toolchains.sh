#!/usr/bin/env bash
# Toolchain recipe grammar, lint, and discovery (spec toolchain-sets §1/§2).
#
# One Dockerfile-text file per toolchain; allowed: RUN, ENV, USER, comments and
# '# extra: <absolute path>' lines. Everything else is a lint error: ARG
# (bakes out-of-band state invisible to the content hash), COPY, WORKDIR,
# ENTRYPOINT (the composition footer owns it), unknown directives, USER
# switch-backs (switching is monotonic root→dev), ENV PATH (PATH additions ride
# base's ~/.profile.d/<id>.sh hook), direct ~/.profile//~/.bashrc writes, and
# floating installs (pip ==x.y.z, npm @x.y.z, version-embedded direct URLs —
# no latest segments, no release-API queries — rustup --default-toolchain <ver>;
# apt is exempt — its pin is the suite). 'base' is a reserved id.
#
# Usage:
#   toolchains.sh discover [ROOT]        TAB records on stdout (see below)
#   toolchains.sh resolve [ROOT] <id>    print a valid id's recipe path; refuse otherwise
#   toolchains.sh lint FILE              exit 0 when valid; reasons on stderr
#   toolchains.sh parse FILE             TAB records: directive lines + '# extra:' paths
#   toolchains.sh extras FILE            collected '# extra:' absolute paths, one per line
#
# ROOT is the toolchains dir (…/dist/toolchains): first arg, else $TOOLCHAINS_ROOT,
# else <script_dir>/../toolchains. Discovery (list + parse + lint of recipes/ and
# custom/ + manifest read) runs live at each call. All strings are English (binding).
#
# discover records (TAB-separated):
#   unit<TAB><id><TAB>valid<TAB><recipe path>
#   unit<TAB><id><TAB>invalid<TAB><reason>
#   bundle<TAB><name><TAB>valid<TAB><space-separated sorted member ids>
#   bundle<TAB><name><TAB>invalid<TAB><reason>
#   warning<TAB><text>
# Unit records render sorted by id and bundle records sorted by name (the
# manifest is semantically unordered — the canonical order is derived here).
# Invalid recipes render visibly-with-reason; a member id without a (valid)
# recipe invalidates the bundle with a warning; a manifest read/parse failure
# invalidates all bundles with a warning; a user/shipped id collision skips the
# user recipe with a warning — shipped wins.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: toolchains.sh discover [ROOT]
       toolchains.sh resolve [ROOT] <id>
       toolchains.sh lint FILE
       toolchains.sh parse FILE
       toolchains.sh extras FILE
EOF
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

default_root() {
  if [[ -n "${TOOLCHAINS_ROOT:-}" ]]; then
    printf '%s\n' "$TOOLCHAINS_ROOT"
  else
    printf '%s\n' "$script_dir/../toolchains"
  fi
}

# --- lint core ----------------------------------------------------------------

LINT_ERRORS=()
EXTRAS=()
PARSE_RECORDS=()
USER_STATE="none"

# lint_core FILE — walks the file's logical lines (backslash continuations
# joined), filling LINT_ERRORS / EXTRAS / PARSE_RECORDS / USER_STATE.
lint_core() {
  local file=$1
  LINT_ERRORS=()
  EXTRAS=()
  PARSE_RECORDS=()
  USER_STATE="none"
  local raw logical="" cont=0 ln=0 start=0
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    raw="${raw%$'\r'}"
    ln=$((ln + 1))
    if (( cont )); then
      raw="${raw#"${raw%%[![:space:]]*}"}"
      logical+=" $raw"
      logical="${logical%"${logical##*[![:space:]]}"}"
    else
      logical="$raw"
      start=$ln
    fi
    if [[ "$logical" == *\\ ]]; then
      logical="${logical%\\}"
      logical="${logical%"${logical##*[![:space:]]}"}"
      cont=1
      continue
    fi
    cont=0
    _lint_line "$logical" "$start"
  done < "$file"
  if (( cont )); then
    _lint_line "$logical" "$start"
  fi
}

_lint_line() {
  local line=$1 ln=$2
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [[ -z "$line" ]] && return 0
  if [[ "$line" == \#* ]]; then
    if [[ "$line" =~ ^#[[:space:]]extra:[[:space:]]*(.+)$ ]]; then
      local p="${BASH_REMATCH[1]}"
      p="${p%"${p##*[![:space:]]}"}"
      if [[ "$p" != /* ]]; then
        LINT_ERRORS+=("line $ln: '# extra: $p' is not an absolute path")
      else
        EXTRAS+=("$p")
        PARSE_RECORDS+=("extra"$'\t'"$p")
      fi
    fi
    return 0
  fi
  local dir rest upper
  dir="${line%%[[:space:]]*}"
  rest="${line#"$dir"}"
  rest="${rest#"${rest%%[![:space:]]*}"}"
  upper="${dir^^}"
  case "$upper" in
    RUN)
      PARSE_RECORDS+=("run"$'\t'"$rest")
      _lint_run "$rest" "$ln"
      ;;
    ENV)
      PARSE_RECORDS+=("env"$'\t'"$rest")
      _lint_env "$rest" "$ln"
      ;;
    USER)
      PARSE_RECORDS+=("user"$'\t'"$rest")
      _lint_user "$rest" "$ln"
      ;;
    ARG)
      LINT_ERRORS+=("line $ln: ARG is refused — build args bake out-of-band state invisible to the content hash (the CACHEBUST lesson)")
      ;;
    COPY)
      LINT_ERRORS+=("line $ln: COPY is refused — recipes carry no file content; declare author-declared files with '# extra: <absolute path>'")
      ;;
    WORKDIR)
      LINT_ERRORS+=("line $ln: WORKDIR is refused — base owns the workdir")
      ;;
    ENTRYPOINT)
      LINT_ERRORS+=("line $ln: ENTRYPOINT is refused — the composition footer owns it")
      ;;
    *)
      LINT_ERRORS+=("line $ln: unknown directive '$dir' — allowed: RUN, ENV, USER, comments and '# extra: <absolute path>'")
      ;;
  esac
}

_lint_user() {
  local spec=$1 ln=$2
  local name="${spec%%[:[:space:]]*}"
  case "$name" in
    root)
      if [[ "$USER_STATE" == "dev" ]]; then
        LINT_ERRORS+=("line $ln: USER switch-back to root after dev — user switching is monotonic root→dev")
      else
        USER_STATE="root"
      fi
      ;;
    dev)
      USER_STATE="dev"
      ;;
    *)
      LINT_ERRORS+=("line $ln: USER '$spec' is refused — only root and dev are allowed (monotonic root→dev)")
      ;;
  esac
}

_lint_env() {
  local rest=$1 ln=$2
  local -a toks keys=()
  read -r -a toks <<< "$rest"
  if [[ "${toks[0]}" != *=* ]]; then
    # legacy 'ENV KEY value' form — the first token is the only key
    keys=("${toks[0]}")
  else
    local tok
    for tok in "${toks[@]}"; do
      [[ "$tok" == *=* ]] && keys+=("${tok%%=*}")
    done
  fi
  local key
  for key in "${keys[@]}"; do
    if [[ "${key^^}" == "PATH" ]]; then
      LINT_ERRORS+=("line $ln: ENV PATH is refused — PATH additions ride base's ~/.profile.d/<id>.sh hook")
    fi
  done
}

_lint_run() {
  local rest=$1 ln=$2
  # profile/bashrc direct writes
  local word touched=""
  for word in $rest; do
    case "$word" in
      "~/.profile" | "~/.bashrc" | '$HOME/.profile' | '$HOME/.bashrc' | "/home/dev/.profile" | "/home/dev/.bashrc")
        touched="$word"
        ;;
    esac
  done
  if [[ -n "$touched" ]]; then
    case "$rest" in
      *'>>'* | *'>'* | *tee* | *'sed -i'*)
        LINT_ERRORS+=("line $ln: direct write to $touched is refused — write a ~/.profile.d/<id>.sh hook instead")
        ;;
    esac
  fi
  # direct URLs must embed the version
  local url
  while IFS= read -r url; do
    [[ -z "$url" ]] && continue
    if [[ "$url" =~ /latest(/|$) || "$url" == */releases/latest* ]]; then
      LINT_ERRORS+=("line $ln: URL '$url' floats on 'latest' — embed the exact version (no latest segments, no release-API queries)")
    fi
  done < <(grep -oE "https?://[^\"' ]+" <<< "$rest" || true)
  # rustup pins (whole RUN so the curl-piped install.sh form is covered)
  if [[ "$rest" == *rustup* ]]; then
    if [[ "$rest" == *rustup-init* || "$rest" == *sh.rustup.rs* ]]; then
      _lint_rustup_init "$rest" "$ln"
    else
      _lint_rustup_cmd "$rest" "$ln"
    fi
  fi
  # pip / npm pins, per command segment
  local seg
  while IFS= read -r seg; do
    seg="${seg#"${seg%%[![:space:]]*}"}"
    [[ -z "$seg" ]] && continue
    _lint_run_seg "$seg" "$ln"
  done < <(sed -E $'s/&&/\\n/g; s/\\|\\|/\\n/g; s/;/\\n/g; s/\\|/\\n/g' <<< "$rest")
}

_lint_run_seg() {
  local seg=$1 ln=$2
  local -a w args=()
  read -r -a w <<< "$seg"
  local cmd="${w[0]##*/}"
  local is_pip=0 is_npm=0
  case "$cmd" in
    pip | pip3)
      is_pip=1
      args=("${w[@]:1}")
      ;;
    python3 | python)
      if [[ "${w[1]:-}" == "-m" && "${w[2]:-}" =~ ^pip3?$ ]]; then
        is_pip=1
        args=("${w[@]:3}")
      fi
      ;;
    npm)
      is_npm=1
      args=("${w[@]:1}")
      ;;
    *)
      return 0
      ;;
  esac
  local tok skip_next=0
  if (( is_pip )) && [[ "${args[0]:-}" == "install" ]]; then
    for tok in "${args[@]:1}"; do
      if (( skip_next )); then
        skip_next=0
        continue
      fi
      case "$tok" in
        -r | --requirement | -e | --editable | --target | -t | --prefix) skip_next=1 ;;
        -*) : ;;
        *)
          if [[ "$tok" != *==* && "$tok" != *@* && "$tok" != *://* ]]; then
            LINT_ERRORS+=("line $ln: pip package '$tok' is not pinned — use '$tok==x.y.z'")
          fi
          ;;
      esac
    done
  fi
  if (( is_npm )) && [[ "${args[0]:-}" == "install" || "${args[0]:-}" == "i" ]]; then
    skip_next=0
    for tok in "${args[@]:1}"; do
      case "$tok" in
        --*=*) continue ;;
        --*)
          skip_next=1
          continue
          ;;
        -*) : ;;
        *)
          if (( skip_next )); then
            skip_next=0
            continue
          fi
          if [[ ! "$tok" =~ @[0-9] ]]; then
            LINT_ERRORS+=("line $ln: npm package '$tok' is not pinned — use '$tok@x.y.z'")
          fi
          ;;
      esac
    done
  fi
}

_lint_rustup_init() {
  local rest=$1 ln=$2
  local tok val="" has=0 pend=0
  for tok in $rest; do
    case "$tok" in
      --default-toolchain)
        has=1
        pend=1
        ;;
      --default-toolchain=*)
        has=1
        val="${tok#*=}"
        pend=0
        ;;
      *)
        if (( pend )); then
          val="$tok"
          pend=0
        fi
        ;;
    esac
  done
  if (( ! has )); then
    LINT_ERRORS+=("line $ln: rustup install is not pinned — pass --default-toolchain <version>")
  elif [[ "$val" =~ ^(stable|nightly|beta)(-[^0-9].*)?$ ]]; then
    LINT_ERRORS+=("line $ln: rustup --default-toolchain '$val' is not a pinned toolchain — use an exact version")
  fi
}

_lint_rustup_cmd() {
  local rest=$1 ln=$2 sub
  for sub in $rest; do :; done # (no-op; subcommand read below for clarity)
  case "$rest" in
    *"rustup default"* | *"rustup toolchain"*)
      LINT_ERRORS+=("line $ln: 'rustup default'/'rustup toolchain' at run time is not pinned — bake --default-toolchain <version> into the rustup install")
      ;;
  esac
}

# --- command wrappers over the lint core ---------------------------------------

# run_lint FILE — print error lines; return 1 when the recipe is invalid
run_lint() {
  local file=$1
  lint_core "$file"
  if ((${#LINT_ERRORS[@]})); then
    printf '%s\n' "${LINT_ERRORS[@]}"
    return 1
  fi
  return 0
}

cmd_lint() {
  local file=$1
  [[ -f "$file" ]] || {
    printf 'toolchains: recipe file not found: %s\n' "$file" >&2
    return 1
  }
  local rc=0
  run_lint "$file" >&2 || rc=1
  return "$rc"
}

cmd_parse() {
  local file=$1
  [[ -f "$file" ]] || {
    printf 'toolchains: recipe file not found: %s\n' "$file" >&2
    return 1
  }
  local rc=0
  run_lint "$file" >&2 || rc=1
  ((rc == 0)) || return "$rc"
  if ((${#PARSE_RECORDS[@]})); then
    printf '%s\n' "${PARSE_RECORDS[@]}"
  fi
}

cmd_extras() {
  local file=$1
  [[ -f "$file" ]] || {
    printf 'toolchains: recipe file not found: %s\n' "$file" >&2
    return 1
  }
  lint_core "$file"
  if ((${#LINT_ERRORS[@]})); then
    printf '%s\n' "${LINT_ERRORS[@]}" >&2
    return 1
  fi
  if ((${#EXTRAS[@]})); then
    printf '%s\n' "${EXTRAS[@]}"
  fi
}

# --- discovery -----------------------------------------------------------------

discover_records() {
  local root=$1
  local -a out=()
  local -A u_path=() u_status=() u_reason=()
  local kind dir f id errs status reason
  for kind in recipes custom; do
    dir="$root/$kind"
    [[ -d "$dir" ]] || continue
    for f in "$dir"/*.Dockerfile; do
      [[ -f "$f" ]] || continue
      id="$(basename "$f")"
      id="${id%.Dockerfile}"
      if [[ "$kind" == "custom" && -n "${u_path[$id]:-}" ]]; then
        out+=("warning"$'\t'"user recipe custom/$id.Dockerfile collides with the shipped recipe of the same id — shipped wins, the user recipe is skipped")
        continue
      fi
      status="valid"
      reason=""
      if ! [[ "$id" =~ ^[a-z0-9-]+$ ]]; then
        status="invalid"
        reason="invalid toolchain id '$id' — ids are lowercase [a-z0-9-]"
      elif [[ "$id" == "base" ]]; then
        status="invalid"
        reason="'base' is a reserved id — base is fixed, not a unit"
      else
        if ! errs="$(run_lint "$f" 2>/dev/null)"; then
          status="invalid"
          reason="$(head -n 1 <<< "$errs")"
        fi
      fi
      u_path["$id"]="$f"
      u_status["$id"]="$status"
      u_reason["$id"]="$reason"
    done
  done
  if ((${#u_path[@]})); then
    while IFS= read -r id; do
      if [[ "${u_status[$id]}" == "valid" ]]; then
        out+=("unit"$'\t'"$id"$'\t'"valid"$'\t'"${u_path[$id]}")
      else
        out+=("unit"$'\t'"$id"$'\t'"invalid"$'\t'"${u_reason[$id]}")
        out+=("warning"$'\t'"toolchain '$id' is invalid — ${u_reason[$id]}")
      fi
    done < <(printf '%s\n' "${!u_path[@]}" | sort)
  fi
  # manifest: bundles only — a read/parse failure invalidates all bundles
  local manifest="$root/manifest.json"
  local bundles="" jqrc=0
  if [[ -f "$manifest" ]]; then
    bundles="$(jq -r '.bundles | to_entries[] | "\(.key)\t\(.value | join(" "))"' "$manifest" 2>/dev/null)" || jqrc=1
  fi
  if [[ ! -f "$manifest" || $jqrc -eq 1 ]]; then
    out+=("warning"$'\t'"manifest.json is missing or unreadable — all bundles are invalidated")
  else
    local name members m
    local -a bad=()
    while IFS=$'\t' read -r name members; do
      [[ -n "$name" ]] || continue
      if ! [[ "$name" =~ ^[a-z0-9-]+$ ]]; then
        out+=("bundle"$'\t'"$name"$'\t'"invalid"$'\t'"invalid bundle name '$name' — names are lowercase [a-z0-9-]")
        out+=("warning"$'\t'"bundle '$name' is invalidated — invalid bundle name '$name'")
        continue
      fi
      bad=()
      for m in $members; do
        if [[ -z "${u_path[$m]:-}" ]]; then
          bad+=("member '$m' has no recipe")
        elif [[ "${u_status[$m]}" != "valid" ]]; then
          bad+=("member '$m' is invalid — ${u_reason[$m]}")
        fi
      done
      if ((${#bad[@]})); then
        reason=""
        local b
        for b in "${bad[@]}"; do
          reason+="${reason:+; }$b"
        done
        out+=("bundle"$'\t'"$name"$'\t'"invalid"$'\t'"$reason")
        out+=("warning"$'\t'"bundle '$name' is invalidated — $reason")
      else
        local sorted=""
        if [[ -n "${members//[[:space:]]/}" ]]; then
          sorted="$(printf '%s\n' $members | sort | paste -sd' ' -)"
        fi
        out+=("bundle"$'\t'"$name"$'\t'"valid"$'\t'"$sorted")
      fi
    done < <(printf '%s\n' "$bundles" | sort -t $'\t' -k1,1)
  fi
  if ((${#out[@]})); then
    printf '%s\n' "${out[@]}"
  fi
}

cmd_resolve() {
  local root=$1 id=$2 rec status reason
  rec="$(discover_records "$root" | awk -F'\t' -v id="$id" '$1 == "unit" && $2 == id')"
  if [[ -z "$rec" ]]; then
    printf "toolchains: unknown toolchain id '%s' — see discovery for the valid ids\n" "$id" >&2
    return 1
  fi
  status="$(awk -F'\t' '{print $3}' <<< "$rec")"
  if [[ "$status" == "valid" ]]; then
    awk -F'\t' '{print $4}' <<< "$rec"
    return 0
  fi
  reason="$(awk -F'\t' '{print $4}' <<< "$rec")"
  printf "toolchains: toolchain '%s' is invalid — %s\n" "$id" "$reason" >&2
  return 1
}

# --- dispatch -------------------------------------------------------------------

main() {
  [[ $# -ge 1 ]] || {
    usage
    return 2
  }
  local cmd=$1
  shift
  case "$cmd" in
    discover)
      local root
      if (($# == 1)); then
        root=$1
      elif (($# == 0)); then
        root="$(default_root)"
      else
        usage
        return 2
      fi
      discover_records "$root"
      ;;
    resolve)
      local root id
      if (($# == 2)); then
        root=$1
        id=$2
      elif (($# == 1)); then
        root="$(default_root)"
        id=$1
      else
        usage
        return 2
      fi
      cmd_resolve "$root" "$id"
      ;;
    lint | parse | extras)
      (($# == 1)) || {
        usage
        return 2
      }
      "cmd_$cmd" "$1"
      ;;
    help | -h | --help)
      usage
      ;;
    *)
      printf 'toolchains: unknown command %s\n' "$cmd" >&2
      usage
      return 2
      ;;
  esac
}

main "$@"
