# WebUI-first face: launcher default, hidden `--full-tui` parity gate (flip-then-delete)

The WebUI-first spec (#147 §14) replaced the full BubbleTea TUI as the primary
face with the browser WebUI served by the same binary — embedded via `go:embed`,
bound to `127.0.0.1` behind a per-launch tokenized URL. The terminal face became
the minimal launcher (#151): tokenized URL line, **Open WebUI** button, **Quit**
with the guarded exit and the containers prompt. One release carries rename +
migration + WebUI + minimal TUI (the #98→#99 flip-then-delete precedent, checks
on ADR-0020).

**Decided shape**: the full BubbleTea TUI stayed reachable behind the **hidden
launch flag `--full-tui`** during the transitional window — same binary, no menu
entry, absent from `--help` (documented only for the transitional release) — so
the flip stayed reversible by inspection while the WebUI reached parity.
Parity was verified on two lists: the **A-list**, scripted via `--smoke` against
a fixture install and the real scripts (external outcomes only: registry/config
shapes, script argument vectors, HTTP/SSE grammar), and the **B-list**, manual
boxes on the real machine for the visual and platform judgement the smoke
harness cannot assert (recorded in `docs/verification/b-list-checklist.md`).
Every B-list box passing **authorized the flip** (#162): the launcher became the
default face and `--full-tui` the parity escape hatch. One release window later,
the gate **closed** (#163): the flag was removed — an unknown flag now aborts
with usage (exit 2, the story-28 abort code, before any home/config access) —
and the full-TUI code was deleted outright: the composed shell and its four
center views (`internal/app`'s Compose, `internal/shell`, `internal/containerview`,
`internal/settingsview`, `internal/taskmgr`, `internal/protoapp` and the
view half of the mixed packages), leaving the webserver's own screens and the
launcher as the only faces. The webserver invokes the surviving script-invoking
logic exactly as the TUI invoked it (ADR-0007); `StopContainers` moved into the
launcher package (its only remaining caller). The first-start migration
mechanics that ride the same release are ADR-0030/0032; the desktop shortcut
launches the same binary, which now auto-opens the browser (ADR-0015 untouched).

**Consequences**: the launcher package stays self-contained (no shell/theme
imports — it was written flip-ready); `go build ./...` covers only the WebUI +
launcher tree; the smoke A-list rides on (`--smoke` remains the pre-check
harness, A18 now asserts the gate is closed), and the B-list checklist is the
archival sign-off record for this release.

Decided 2026-11-09 via [Grilling: Minimal TUI launcher behavior](https://github.com/shredingerlabs/oc_sandbox/issues/138); closed 2026-11-17 via the B-list sign-off (#163).
