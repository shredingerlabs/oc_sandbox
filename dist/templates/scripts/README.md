# afkLoop

`scripts/afkLoop.sh` is a stateless bash driver that walks a queue of
`ready-for-agent`-labelled tickets on GitHub or GitLab, spawning a fresh
`opencode` session per ticket. An attempt is counted as a success only when
the session exits 0 **and** the agent has created the sentinel file
`.opencode-loop/<N>/done`. On repeated failure the driver relabels the
ticket `ready-for-human` and moves on.

Tickets are selected by either a numeric range (`--start N --end N`) or a
title-prefix range (`--title-start T-K02 --title-end T-K05b`). The two
modes are mutually exclusive.

## Prerequisites

- `bash` 5+
- `jq`
- `timeout` (from GNU coreutils; available on Linux by default)
- `opencode`
- A tracker CLI, authenticated:
  - GitHub: `gh`
  - GitLab: `glab`

When `--require-changes` is used the CWD must be a git repository (the
driver reads `git status --porcelain`).

## Setup from scratch

### 1. Create the labels

```sh
# GitHub
gh label create ready-for-agent  --color 0E8A16 --description "Fully specified, ready for an AFK agent"
gh label create ready-for-human  --color B60205 --description "Requires human implementation"

# GitLab
glab label create ready-for-agent --color "#0E8A16" --description "Fully specified, ready for an AFK agent"
glab label create ready-for-human --color "#B60205" --description "Requires human implementation"
```

If your tracker already uses different label names, pass them via
`--ready-label` and `--human-label`.

### 2. Add a `LoopPrompt.md`

The driver refuses to start without a prompt file. The canonical template:

```md
Implement the ticket (use implement skill) from the given url.
When the ticket is fully solved (all acceptance criteria met, tests green, issue closed),
create the file .opencode-loop/<N>/done. Do NOT create it otherwise.
Commit changes if ticket is fully solved.
```

`./scripts/LoopPrompt.md` is the default; pass `--prompt-file` to use
another. The two non-negotiable lines are: implement the ticket, and only
create the sentinel when the work is actually done.

### 3. (Optional) Add a test fixture

Tickets like T-K01..T-K04 in this repo work against a tiny `tests/afkLoop/`
project: one `main.sh` plus one `test.sh` of plain-bash assertions. Use the
same shape to bootstrap tickets in a new repo — a small, runnable target
is easier for the agent to verify than a sprawling change.

See `tests/afkLoop/README.md` for a working example.

## Usage — numeric mode

```sh
./scripts/afkLoop.sh \
  --issues-url https://github.com/owner/repo/issues \
  --start 42
```

`--end` is optional; omit it to walk every `ready-for-agent` ticket from
`--start` upward. With `--require-changes` the driver also requires
`git status --porcelain` to be non-empty before counting a success.

```sh
./scripts/afkLoop.sh \
  --issues-url https://github.com/owner/repo/issues \
  --start 42 --require-changes --max-retries 5 --timeout 3600
```

## Usage — title mode

```sh
./scripts/afkLoop.sh \
  --issues-url https://github.com/owner/repo/issues \
  --title-start T-K02 --title-end T-K05b
```

In title mode the driver queries every `ready-for-agent` issue, keeps
those whose title starts with the prefix guard (default `T-K`), strips the
title at the first `:`, sorts the survivors lexicographically, and walks
the matching range. On a TTY the resolved list is printed and you are
asked `Continue? [Y/n]`; pass `--yes` to skip the prompt.

Override the guard when your tickets use a different family:

```sh
./scripts/afkLoop.sh \
  --issues-url https://github.com/owner/repo/issues \
  --title-start BUG-100 --title-end BUG-120 --title-prefix BUG
```

## All flags

Required:

- `--issues-url URL` — Issues URL without the number, e.g.
  `https://github.com/owner/repo/issues` or
  `https://gitlab.com/owner/repo/-/issues`.
- `--start N` — First ticket number to try (numeric mode).

Options:

- `--end N` — Last ticket number. If omitted in numeric mode, the script
  queries the tracker once at startup for the highest-numbered ticket
  carrying `--ready-label` and uses that as the end.
- `--max-retries N` — Max retries per ticket (default `3`; one initial
  attempt + `N` retries).
- `--timeout SECS` — Per-attempt timeout (default `1800`).
- `--ready-label NAME` — Label marking a ticket as agent-ready
  (default `ready-for-agent`).
- `--human-label NAME` — Label applied when the agent gives up
  (default `ready-for-human`).
- `--prompt-file PATH` — File holding the predefined prompt
  (default `LoopPrompt.md`).
- `--require-changes` — Also require `git status --porcelain` to be
  non-empty before an attempt is counted as a success.
- `--title-start PREFIX` — First title prefix (e.g. `T-K02`). Activates
  title mode; mutually exclusive with `--start` / `--end`.
- `--title-end PREFIX` — Last title prefix (e.g. `T-K05b`). Required in
  title mode.
- `--title-prefix STR` — Title prefix guard; only issues whose title
  starts with this string are considered (default `T-K`).
- `--yes`, `-y` — Skip the confirmation prompt in title mode.
- `-h`, `--help` — Show the full help text.

## The success contract

An attempt counts as a success only when **all** of these hold:

1. The `opencode run` session exited with code 0.
2. The agent has created `.opencode-loop/<N>/done` (the sentinel file).
3. `--require-changes` was passed **and** `git status --porcelain` is
   non-empty.

See `scripts/afkLoop.sh:227` for the sentinel check, and
`scripts/afkLoop.sh:456` for the gate. Exit 0 alone is not enough: the
sentinel is the only signal that the agent believes the acceptance
criteria are met.

## Per-ticket outcomes

| Outcome    | When                                                                  | Driver action                                                                  |
| ---------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `skip`     | Ticket is not open, or is missing the `--ready-label`.                | Move on, no attempt logged.                                                    |
| `blocked`  | Body has a `Blocked by: #N` line and at least one blocker is open.    | Move on, counted separately.                                                   |
| succeeded  | All three success gates pass on some attempt ≤ `--max-retries + 1`.   | Move on, counted.                                                              |
| gave-up    | Every attempt failed the success contract.                            | Relabel `ready-for-human`, post a comment with the tail of the last attempt.   |

The driver prints a summary at the end:

```
afkLoop.sh: summary
  attempted:  ...
  succeeded:  ...
  skipped:    ...
  blocked:    ...
  gave-up:    ...
```

## Logs

Per-attempt artefacts land in `.opencode-loop/<N>/attempt-<k>/`:

```
.opencode-loop/
└── 42/
    ├── done                       # sentinel: presence == solved
    ├── attempt-1/
    │   ├── prompt.txt
    │   ├── stdout.log
    │   └── stderr.log
    └── attempt-2/
        ├── prompt.txt
        ├── stdout.log
        └── stderr.log
```

`.opencode-loop/` is created at runtime and should be gitignored (this
repo's `.gitignore` already covers it). When the driver gives up on a
ticket, the last 20 lines of the final attempt's `stderr.log` (or
`stdout.log` if `stderr` is empty) are attached to the give-up comment
posted to the tracker.

## Interrupting

- One Ctrl-C: the driver prompts `Exit loop? [y/N]`. `y` exits cleanly
  with a summary; anything else continues the walk.
- A second Ctrl-C during the prompt: force-exit with code 130.
- Non-TTY stdin (e.g. running under CI): the prompt is skipped and the
  loop continues; send `SIGTERM` to stop.

## GitHub vs GitLab

The driver picks a backend from the host of `--issues-url`:

```sh
# GitHub
./scripts/afkLoop.sh \
  --issues-url https://github.com/owner/repo/issues \
  --start 42

# GitLab
./scripts/afkLoop.sh \
  --issues-url https://gitlab.com/owner/repo/-/issues \
  --start 42
```

All flags are identical. Internally the driver uses `gh issue` /
`gh issue edit` on GitHub and `glab issue` / `glab issue update` on
GitLab, and translates project-wide issue numbers to GitLab IIDs where
needed.

## Troubleshooting

- **`missing required command: gh` / `glab`** — install the tracker CLI
  and run `gh auth status` / `glab auth status`.
- **`no tickets with label 'ready-for-agent' found`** — the ready label
  name on the tracker doesn't match the default. Pass `--ready-label
  <name>`, or create the label.
- **Every attempt reports `solved=no`** — `LoopPrompt.md` is missing
  the line that instructs the agent to create `.opencode-loop/<N>/done`
  only when the work is fully done. Update the prompt file, not the
  driver.
- **Title mode finds nothing** — either the prefix guard is wrong
  (override with `--title-prefix`), or the candidate titles don't have a
  `:` after the prefix, or no ready-labelled tickets fall in the
  range.
- **Loop stops mid-walk with no error** — you sent Ctrl-C and answered
  `y`. Re-run; tickets already at sentinel state are skipped because
  their done file already exists.

## See also

- `docs/adr/0001-title-based-issue-selection.md` — why title mode exists
  and how it differs from numeric mode.
- `CONTEXT.md` — glossary: Ticket, Title prefix, Prefix guard, Ready
  label, Blocked by.
- `tests/afkLoop/README.md` — a working ticket fixture.
