# Confirmation Popups for Destructive and Heavy Actions

A shared centered modal confirm (ADR-0021) is shown before triggered
actions run — Create Project (button), Restore Config (button), Build
(Container view button and edition-card double-click/Enter), Uninstall
(button click, after the typed `"uninstall"` confirm), and Export Config
(button, only when the destination file already exists — `Overwrite`
replaces it without a destination backup). Quit does not get a
popup: the existing guarded-exit footer prompt stays, and `ctrl+c` keeps the
SIGINT path. The start-time "Build now / Build later / Go back" prompt
remains its own three-option decision.

## Considered Options

- **Shared modal confirm (chosen)**: one component in `internal/shell`,
  consistent mechanics (focus starts on Cancel, ←/→/Tab switch buttons,
  Enter activates focus, esc/q cancels, click outside cancels; idle-only:
  pops are suppressed while an op is queued/running) and consistent copy
  (one-line title + one context line per action).
- **No confirmation for reversible ops**: Restore makes a safety backup and
  quit is a clean no-op — but Create (runs scripts, registers a project),
  Build (long, disk-heavy), and the Uninstall case make a uniform confirm
  cheaper to reason about than per-action exceptions.
- **Replace the typed uninstall confirm with the popup**: rejected — a
  popup is a weaker confirmation than typed text; the popup stacks after
  it (type → click button → popup → run), with a red Confirm and the
  currently toggled options echoed, matching the red UNINSTALL prompt of
  the bash TUI.

## Consequences

- **Create Project shows a content summary** (name, path, edition, modes,
  start option, VCS) with Confirm/Back instead of running scripts
  immediately; the credential-replacement confirms inside the flow keep
  firing afterwards.
- **Restore popup context** names the selected backup file and its target
  file; Restore still makes a safety backup first (ADR 0009 unchanged).
- **Build popup context** names the edition; launch paths stay unchanged
  (one op at a time, log streaming).
- **Quit keeps bash parity**: ask only while background work runs, clean
  exit 0 otherwise; `ctrl+c` exits 130 without asking.

## Update — Remove project from list (added later)

The fifth application: **Edit Project settings → Remove project from
list** (the removal pane button, glossary "remove project from list") gets
the shared popup with a red `Remove` confirm and default-focus `Cancel`.
The context is thicker than the one-line rule — three lines (what the
removal touches, what stays, the running-container warning) were judged
worth the read, following the Uninstall echo precedent. The card's
`remove` button stays above the gate: it opens the pane only.
