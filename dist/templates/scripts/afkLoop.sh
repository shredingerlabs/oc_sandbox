#!/usr/bin/env bash
#
# loop.sh - drive an opencode agent through a queue of ready-for-agent tickets.
#
# Two modes:
#   Numeric mode  — walks a range of issue numbers (--start N --end N).
#   Title mode    — queries all ready-labeled issues, filters by title prefix
#                   range (--title-start T-K02 --title-end T-K05b), sorts by
#                   prefix (text before first ':'), and walks the list.
#   Modes are mutually exclusive.
#
# Runs a fresh opencode session per ticket, and advances when BOTH the session
# exits 0 AND the agent has created the sentinel file .opencode-loop/<N>/done.
# On repeated failure, marks the ticket ready-for-human and moves on.
# Skips tickets whose body declares open blockers ("Blocked by: #N").
# Ctrl-C prompts for confirmation; a second Ctrl-C forces an immediate exit.
#
# Usage:
#   ./scripts/afkLoop.sh --issues-url https://github.com/owner/repo/issues --start 42
#   ./scripts/afkLoop.sh --issues-url https://github.com/owner/repo/issues --title-start T-K02 --title-end T-K05b

set -euo pipefail

PROG=$(basename "$0")

usage() {
  cat <<EOF
Usage: $PROG --issues-url URL --start N [options]

Required:
  --issues-url URL       Issues URL without the number, e.g.
                           https://github.com/owner/repo/issues
                           https://gitlab.com/owner/repo/-/issues
  --start N              First ticket number to try

Options:
  --end N                Last ticket number. If omitted, the script queries
                         the tracker once at startup for the highest-numbered
                         ticket carrying --ready-label and uses that as the end.
  --max-retries N        Max retries per ticket (default 3; one initial + N retries)
  --timeout SECS         Per-attempt timeout (default 1800)
  --ready-label NAME     Label marking a ticket as agent-ready (default ready-for-agent)
  --human-label NAME     Label applied when the agent gives up (default ready-for-human)
  --prompt-file PATH     File holding the predefined prompt (default LoopPrompt.md)
  --require-changes      Also require 'git status --porcelain' to be non-empty
                         before an attempt is counted as a success
  --title-start PREFIX   First title prefix (e.g. T-K02). Activates title mode;
                         mutually exclusive with --start / --end.
  --title-end PREFIX     Last title prefix (e.g. T-K05b). Required in title mode.
  --title-prefix STR     Title prefix guard; only issues whose title starts with
                         this string are considered (default T-K)
  --yes, -y              Skip the confirmation prompt in title mode
  -h, --help             Show this help

An attempt counts as a success only when the opencode session exits 0 AND the
agent has created the sentinel file .opencode-loop/<ticket>/done. The prompt
file (LoopPrompt.md) must instruct the agent to create that file ONLY when
all acceptance criteria are met and tests are green.

Tickets whose body declares open blockers (a "Blocked by: #N" line where any
#N is still open) are skipped and counted separately. Per-attempt logs land
in .opencode-loop/<ticket>/attempt-<n>/{prompt,stdout,stderr}.log.
EOF
}

ISSUES_URL=""
START=""
END=""
MAX_RETRIES=3
TIMEOUT=1800
READY_LABEL="ready-for-agent"
HUMAN_LABEL="ready-for-human"
PROMPT_FILE="LoopPrompt.md"
LOG_DIR=".opencode-loop"
REQUIRE_CHANGES=0
TITLE_START=""
TITLE_END=""
TITLE_PREFIX="T-K"
SKIP_CONFIRM=0
TITLE_MODE=0
ISSUE_LIST=""
ISSUE_ENTRIES=""

COUNT_ATTEMPTED=0
COUNT_SUCCEEDED=0
COUNT_SKIPPED=0
COUNT_BLOCKED=0
COUNT_GAVE_UP=0

INTERRUPTED=

die() { echo "$PROG: $*" >&2; exit 2; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --issues-url) ISSUES_URL=$2; shift 2 ;;
    --start)      START=$2;      shift 2 ;;
    --end)        END=$2;        shift 2 ;;
    --max-retries) MAX_RETRIES=$2; shift 2 ;;
    --timeout)    TIMEOUT=$2;    shift 2 ;;
    --ready-label) READY_LABEL=$2; shift 2 ;;
    --human-label) HUMAN_LABEL=$2; shift 2 ;;
    --prompt-file) PROMPT_FILE=$2; shift 2 ;;
    --require-changes) REQUIRE_CHANGES=1; shift ;;
    --title-start)   TITLE_START=$2; shift 2 ;;
    --title-end)     TITLE_END=$2;   shift 2 ;;
    --title-prefix)  TITLE_PREFIX=$2; shift 2 ;;
    --yes|-y)        SKIP_CONFIRM=1; shift ;;
    -h|--help)    usage; exit 0 ;;
    *)            die "unknown argument: $1" ;;
  esac
done

[[ -n "$ISSUES_URL" ]] || { usage >&2; die "--issues-url is required"; }

if [[ -n "$TITLE_START" || -n "$TITLE_END" ]]; then
  if [[ -n "$START" || -n "$END" ]]; then
    die "--title-start/--title-end and --start/--end are mutually exclusive"
  fi
  [[ -n "$TITLE_START" ]] || die "--title-start is required when --title-end is given"
  [[ -n "$TITLE_END" ]]   || die "--title-end is required when --title-start is given"
  TITLE_MODE=1
else
  [[ -n "$START" ]] || { usage >&2; die "--start is required in numeric mode"; }
  [[ "$START" =~ ^[0-9]+$ ]] || die "--start must be a positive integer"
  if [[ -n "$END" ]]; then
    [[ "$END" =~ ^[0-9]+$ ]] || die "--end must be a positive integer"
  fi
  TITLE_MODE=0
fi

[[ -f "$PROMPT_FILE" ]] || die "prompt file not found: $PROMPT_FILE"

for cmd in jq timeout opencode; do
  command -v "$cmd" >/dev/null 2>&1 || die "missing required command: $cmd"
done

HOST="${ISSUES_URL#*://}"
HOST="${HOST%%/*}"
case "$HOST" in
  *github*) TRACKER=github ;;
  *gitlab*) TRACKER=gitlab ;;
  *)        die "host '$HOST' is neither github nor gitlab; pass a github.com or *.gitlab.com URL" ;;
esac

if [[ "$TRACKER" == github ]]; then
  command -v gh >/dev/null 2>&1 || die "missing required command: gh"
else
  command -v glab >/dev/null 2>&1 || die "missing required command: glab"
fi

ISSUES_URL="${ISSUES_URL%/}"

# GitHub labels are {name: "x"} objects; GitLab labels are bare strings.
LABEL_PRESENT='map(if type == "object" then .name else . end) | index($rl)'

highest_ready() {
  if [[ "$TRACKER" == github ]]; then
    gh issue list --label "$READY_LABEL" --state open --limit 1000 --json number \
      | jq '[.[].number] | max // empty'
  else
    glab issue list -F json --label "$READY_LABEL" 2>/dev/null \
      | jq '[.[] | select(.state == "opened") | .iid] | max // empty'
  fi
}

# Fetch a ticket and emit a normalized JSON object:
#   {state: "open"|"closed", labels: [...], body: "..."}
fetch_ticket() {
  local n=$1
  if [[ "$TRACKER" == github ]]; then
    gh issue view "$n" --json state,labels,body 2>/dev/null \
      | jq -c '{state: (.state | ascii_downcase), labels: (.labels // []), body: (.body // "")}'
  else
    glab issue view "$n" -F json 2>/dev/null \
      | jq -c '{state: (.state | ascii_downcase), labels: (.labels // []), body: (.description // "")}'
  fi
}

# Echo one of: "ready" | "skip" | "blocked: <space-separated Ns>"
inspect_ticket() {
  local n=$1
  local data
  data=$(fetch_ticket "$n") || { echo "skip"; return; }
  [[ -z "$data" ]] && { echo "skip"; return; }

  if ! printf '%s' "$data" | jq -e --arg rl "$READY_LABEL" \
      "(.state == \"open\" or .state == \"opened\") and ((.labels // []) | $LABEL_PRESENT)" >/dev/null; then
    echo "skip"
    return
  fi

  local body blockers open_blockers b
  body=$(printf '%s' "$data" | jq -r '.body // ""')
  blockers=$(printf '%s\n' "$body" \
    | grep -m1 -E '^[[:space:]]*Blocked[[:space:]]+by:' \
    | grep -oE '#[0-9]+' | tr -d '#' | tr '\n' ' ' | sed 's/ $//')
  [[ -z "$blockers" ]] && { echo "ready"; return; }

  open_blockers=""
  for b in $blockers; do
    if is_open "$b"; then
      open_blockers="$open_blockers $b"
    fi
  done
  open_blockers=$(printf '%s' "$open_blockers" | sed 's/^ //;s/ $//')
  if [[ -n "$open_blockers" ]]; then
    echo "blocked:$open_blockers"
  else
    echo "ready"
  fi
}

# True iff ticket N exists and is open.
is_open() {
  local n=$1
  local data
  if [[ "$TRACKER" == github ]]; then
    data=$(gh issue view "$n" --json state 2>/dev/null) || return 1
    [[ -z "$data" ]] && return 1
    printf '%s' "$data" | jq -e '.state == "OPEN"' >/dev/null
  else
    data=$(glab issue view "$n" -F json 2>/dev/null) || return 1
    [[ -z "$data" ]] && return 1
    printf '%s' "$data" | jq -e '.state == "opened"' >/dev/null
  fi
}

post_comment() {
  local n=$1 msg=$2
  if [[ "$TRACKER" == github ]]; then
    gh issue comment "$n" --body "$msg" >/dev/null
  else
    glab issue note "$n" --message "$msg" >/dev/null
  fi
}

relabel_give_up() {
  local n=$1
  if [[ "$TRACKER" == github ]]; then
    gh issue edit "$n" --add-label "$HUMAN_LABEL" --remove-label "$READY_LABEL" >/dev/null
  else
    glab issue update "$n" --label "$HUMAN_LABEL" --unlabel "$READY_LABEL" >/dev/null
  fi
}

# True iff the agent has created the sentinel file for ticket N.
is_solved() {
  [[ -f "$LOG_DIR/$1/done" ]]
}

# True iff 'git status --porcelain' is non-empty inside a git repo.
has_changes() {
  command -v git >/dev/null 2>&1 || return 1
  git rev-parse --git-dir >/dev/null 2>&1 || return 1
  [[ -n "$(git status --porcelain 2>/dev/null)" ]]
}

resolve_title_range() {
  local issues_json ts te

  if [[ "$TRACKER" == github ]]; then
    issues_json=$(gh issue list --label "$READY_LABEL" --state open --limit 1000 --json number,title 2>/dev/null) || true
  else
    issues_json=$(glab issue list -F json --label "$READY_LABEL" 2>/dev/null | \
      jq '[.[] | select(.state == "opened") | {number: .iid, title: .title}]') || true
  fi

  [[ -n "$issues_json" && "$issues_json" != "[]" && "$issues_json" != "null" ]] \
    || die "no issues with label '$READY_LABEL' found in tracker"

  ts="${TITLE_START%:}"
  te="${TITLE_END%:}"

  local no_colon
  no_colon=$(printf '%s' "$issues_json" | jq -r --arg prefix "$TITLE_PREFIX" \
    '.[] | select(.title | startswith($prefix)) | select(.title | contains(":") | not) | "\(.number) \(.title)"' 2>/dev/null) || true
  if [[ -n "$no_colon" ]]; then
    echo "$PROG: skipping issues without ':' in title:" >&2
    while IFS=' ' read -r num title; do
      printf "  #%s  %s\n" "$num" "$title" >&2
    done <<< "$no_colon"
  fi

  ISSUE_ENTRIES=$(printf '%s' "$issues_json" | jq -r --arg prefix "$TITLE_PREFIX" \
    --arg ts "$ts" --arg te "$te" \
    'map(select(.title | startswith($prefix))) |
     map(select(.title | contains(":"))) |
     map({number, prefix: (.title | split(":")[0]), title}) |
     map(select(.prefix >= $ts and .prefix <= $te)) |
     sort_by(.prefix) |
     .[] | "\(.number) \(.title)"' 2>/dev/null) || true

  ISSUE_LIST=$(printf '%s' "$ISSUE_ENTRIES" | awk '{print $1}' | tr '\n' ' ') || true
  ISSUE_LIST="${ISSUE_LIST%% }"

  if [[ -z "${ISSUE_LIST// }" ]]; then
    die "no issues matching prefix '$TITLE_PREFIX' in range $ts..$te with label '$READY_LABEL'"
  fi

  local count
  count=$(printf '%s' "$ISSUE_LIST" | wc -w)
  echo "$PROG: walking ${count// /} issues matching $ts..$te on $TRACKER ($ISSUES_URL)"
}

confirm_start() {
  if [[ ! -t 0 ]]; then
    return 0
  fi

  echo ""
  echo "The following issues will be processed:"
  while IFS=' ' read -r num title; do
    printf "  #%s  %s\n" "$num" "$title"
  done <<< "$ISSUE_ENTRIES"
  echo ""

  local ans
  read -r -p "Continue? [Y/n] " ans
  case "$ans" in
    [nN]|[nN][oO]) return 1 ;;
    *)             return 0 ;;
  esac
}

if [[ $TITLE_MODE -eq 0 ]]; then
  if [[ -z "$END" ]]; then
    END=$(highest_ready)
    [[ -n "$END" ]] || die "no tickets with label '$READY_LABEL' found in tracker"
  fi
  if [[ "$END" -lt "$START" ]]; then
    die "resolved END ($END) is below START ($START); nothing to do"
  fi
  ISSUE_LIST=$(seq "$START" "$END")
else
  resolve_title_range
  if [[ $SKIP_CONFIRM -eq 0 ]]; then
    confirm_start || exit 0
  fi
fi

# Build the give-up comment, including the tail of the final attempt's log.
build_giveup_comment() {
  local n=$1 attempts=$2
  local final_dir="$LOG_DIR/$n/attempt-$attempts"
  local fence='```'
  local msg="Agent loop gave up after $attempts attempts. See $LOG_DIR/$n/ for per-attempt logs."

  local tail_log=""
  if [[ -s "$final_dir/stderr.log" ]]; then
    tail_log=$(tail -n 20 "$final_dir/stderr.log")
  elif [[ -s "$final_dir/stdout.log" ]]; then
    tail_log=$(tail -n 20 "$final_dir/stdout.log")
  fi

  if [[ -n "$tail_log" ]]; then
    msg="${msg}

Last 20 lines from final attempt log:
${fence}
${tail_log}
${fence}"
  fi

  printf '%s' "$msg"
}

# Returns 0 if user confirms exit, 1 to continue. A second SIGINT during
# the prompt forces exit 130. On non-TTY stdin, defaults to continue.
confirm_exit() {
  if [[ ! -t 0 ]]; then
    return 1
  fi
  trap 'echo "" >&2; exit 130' INT
  local ans rc
  read -r -p "Exit loop? [y/N] " ans
  rc=$?
  trap 'INTERRUPTED=1' INT
  if [[ $rc -ne 0 ]]; then
    exit 130
  fi
  case "$ans" in
    [yY]|[yY][eE][sS]) return 0 ;;
    *)                 return 1 ;;
  esac
}

print_summary() {
  cat <<EOF

$PROG: summary
  attempted:  $COUNT_ATTEMPTED
  succeeded:  $COUNT_SUCCEEDED
  skipped:    $COUNT_SKIPPED
  blocked:    $COUNT_BLOCKED
  gave-up:    $COUNT_GAVE_UP
EOF
}

trap 'INTERRUPTED=1' INT

if [[ $TITLE_MODE -eq 0 ]]; then
  echo "$PROG: walking $START..$END on $TRACKER ($ISSUES_URL)"
fi
echo "$PROG: max-retries=$MAX_RETRIES timeout=${TIMEOUT}s ready-label=$READY_LABEL human-label=$HUMAN_LABEL"

for n in $ISSUE_LIST; do
  if [[ -n "$INTERRUPTED" ]]; then
    if confirm_exit; then
      echo "$PROG: interrupted at #$n; exiting"
      print_summary
      exit 0
    fi
    INTERRUPTED=
  fi

  status=$(inspect_ticket "$n")

  case "$status" in
    skip)
      echo "$PROG: #$n skip (not open or missing '$READY_LABEL')"
      COUNT_SKIPPED=$((COUNT_SKIPPED + 1))
      continue
      ;;
    blocked:*)
      echo "$PROG: #$n blocked by: ${status#blocked:}"
      COUNT_BLOCKED=$((COUNT_BLOCKED + 1))
      continue
      ;;
    ready)
      ;;
    *)
      echo "$PROG: #$n unknown inspect status: $status" >&2
      continue
      ;;
  esac

  url="${ISSUES_URL}/${n}"
  COUNT_ATTEMPTED=$((COUNT_ATTEMPTED + 1))
  succeeded=0
  retries=0

  while [[ $retries -le $MAX_RETRIES ]]; do
    attempt=$((retries + 1))
    attempt_dir="$LOG_DIR/$n/attempt-$attempt"
    mkdir -p "$attempt_dir"
    prompt="${url}
$(cat "$PROMPT_FILE")"
    printf '%s' "$prompt" > "$attempt_dir/prompt.txt"

    echo "$PROG: #$n attempt $attempt/$((MAX_RETRIES + 1))"
    set +e
    timeout "$TIMEOUT" opencode run "$prompt" \
      > "$attempt_dir/stdout.log" 2> "$attempt_dir/stderr.log"
    rc=$?
    set -e

    if [[ -n "$INTERRUPTED" ]]; then
      if confirm_exit; then
        echo "$PROG: interrupted at #$n attempt $attempt; exiting"
        print_summary
        exit 0
      fi
      INTERRUPTED=
    fi

    solved=no
    is_solved "$n" && solved=yes
    if [[ $REQUIRE_CHANGES -eq 1 ]]; then
      if has_changes; then
        changes=yes
      else
        changes=no
      fi
    else
      changes="n/a"
    fi

    if [[ $rc -eq 0 && $solved == yes && ( $REQUIRE_CHANGES -eq 0 || $changes == yes ) ]]; then
      echo "$PROG: #$n success on attempt $attempt (exit=0 solved=yes changes=$changes)"
      COUNT_SUCCEEDED=$((COUNT_SUCCEEDED + 1))
      succeeded=1
      break
    fi
    echo "$PROG: #$n attempt $attempt failed: exit=$rc solved=$solved changes=$changes"
    retries=$((retries + 1))
  done

  if [[ $succeeded -eq 0 ]]; then
    echo "$PROG: #$n giving up after $((MAX_RETRIES + 1)) attempts; marking $HUMAN_LABEL"
    post_comment "$n" "$(build_giveup_comment "$n" "$((MAX_RETRIES + 1))")" \
      || echo "$PROG: #$n warning: failed to post comment" >&2
    relabel_give_up "$n" \
      || echo "$PROG: #$n warning: failed to relabel" >&2
    COUNT_GAVE_UP=$((COUNT_GAVE_UP + 1))
  fi
done

print_summary
