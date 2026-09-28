# ADR-0017: Project import of existing sandbox roots

Users may have a folder that *already is* (or formerly was) an opencode-sandbox
project root — e.g. after moving to a new machine or after losing the global
registry. They can re-register it via Settings → Import existing project, using
a TUI folder picker that starts at the configured `DEFAULT_PROJECT_PATH`
(falls back to `$HOME` if configured path does not exist on disk).

The flow handles two outcomes per folder:

1. **Matching root → register only.** The strict structure check passes:
   `sandbox_config.json` in `.opencode_config/` plus all expected directories
   (`project/`, `.opencode_config/`, `.opencode_data/`, `.ssh_local/`,
   `.git_local/`,    `.cbm_cache/`) plus a `project/.git` worktree. A summary screen (name
   prefilled from the folder basename, path, edition/modes/start option from
   the config) confirms
   registration; afterwards only `projects.json` changes. Nothing is scaffolded,
   cloned, rebuilt, or started — no wizard questions are asked. Projects with
   `setup_complete: false` are imported as-is; the existing setup-recovery path
   completes them on next start.

2. **Non-matching folder → refer, don't migrate.** The flow shows an
   informational prompt (structure requirement + pointer to the New Project
   workflow with git import) and offers an action row that jumps straight into
   the New Project wizard. **No** copy/move/clone of the folder into a new root
   is automated: relocating or duplicating user code behind a confirmation-free
   prompt was deemed too destructive/risky, and the New Project wizard already
   has a supported source path (`Clone existing repo via URL`, ADR-0016).

Considered and rejected:

- **Loose criteria** (only the config file, or only the directories) — rejected:
  structure-only checks false-positive on lookalike folders and yield
  half-broken projects; the strict check is cheap and unambiguous.
- **Broken-JSON as mere "not matching"** — rejected: the prompt must state that
  the config is *broken* (parse failure), not merely absent, so the user can
  repair the actual cause.
- **Automated fallback (scaffold + move/copy the picked folder into
  `<root>/project/`, with `project_source=imported`)** — rejected in favor of
  the referrer prompt; registry/config field definitions stay untouched.

Edge rules:

- Already-registered path or duplicate project name → info page, back to the
  picker; no silent rename, no overwrite.
- Structurally matching root with unparsable `sandbox_config.json` is treated
  as case 2 with the dedicated broken-config message.
- v1 does not detect git worktrees/detached `gitdir:` pointers under `project/`;
  a `project/.git` existence check only.

Consequences:
- No new `project_source` value, no `repo_url` semantics change (CONTEXT.md
  stays as-is apart from the `project import` glossary term).
- Import is idempotent-safe: repeating it on the same root hits the
  already-registered guard and changes nothing.
- The folder picker must work in both TUI modes (gum and bash-select fallback,
  ADR-0002): iterative directory navigation with an explicit "Select this
  folder" action row.
