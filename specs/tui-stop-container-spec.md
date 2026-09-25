# Spec: TUI "Stop Container" option in Settings

## Summary

Add a "Stop Container" option to the TUI Settings menu (`settings_menu()` in
`dist/scripts/start-tui.sh`). It lets the user gracefully stop a running
project container without leaving the TUI or resorting to manual
`podman stop` commands (today only the uninstall wizard prints those as
advice).

## Scope

- Settings menu gains the option "Stop Container", listed between
  "Config Restore" and "Uninstall".
- Podman-only (consistent with the rest of the codebase; no docker
  abstraction).
- Single-project stop only; no "stop all" action.

## Flow

1. **Settings menu** — `settings_menu()` shows "Stop Container" alongside the
   existing options and dispatches to `stop_container_menu()`.
2. **Nothing running** — If `get_running_containers()` returns no
   opencode-sandbox containers, show an info page
   (`show_page "No running containers" "Start a project first."`) and return
   to the Settings menu.
3. **Project selection** — List only projects whose container is *actually
   running*. The check is done live against `get_running_containers()` output
   matched against `opencode-sandbox-<container_id>`; the (possibly stale)
   `container_status` in `projects.json` is NOT used for the list. Menu label
   format matches `project_selection_wizard()`: `● <name> | <path>`, plus a
   "← Go Back" entry.
4. **Confirmation** — `show_menu "Stop <container-name>?" "Stop" "← Go Back"`.
   Declining returns to the Settings menu without touching the container.
   (No type-to-confirm: stopping is reversible.)
5. **Stop** — Run `podman stop "$container_name"` (default SIGTERM timeout).
6. **Verify** — Re-run `get_running_containers()` and confirm the container
   name is gone. Containers run via `podman run --rm`, so a stopped container
   is removed automatically; verification must check absence from `podman ps`.
7. **Success** — Update `projects.json` via
   `update_project_status "$project_path" "stopped"`, then show a success page
   (`show_page "Container stopped" "<container-name>"`) with
   `wait_for_enter`, and return to the Settings menu.
8. **Failure** — If `podman stop` fails or the container is still present
   after stopping (e.g. it vanished in a race, or stop errored), show an error
   page "Stop failed" with the container name, wait for Enter, and return to
   the Settings menu without changing the registry status.

## Function

```bash
stop_container_menu()
```

- Reads running containers, builds the running-project list, handles the
  empty case.
- On selection: confirms, stops, verifies, updates status, reports.

Settings case handler:

```bash
"Stop Container")
  stop_container_menu
  ;;
```

## Testing

Unit tests in `tests/test-start-tui.sh` with stubbed `podman` and
`show_menu`/`show_page`/`wait_for_enter`:

- no running containers → info page, no podman call
- running project listed and selectable
- declining confirmation leaves container running
- successful stop → `podman stop` called with correct name, registry status
  becomes "stopped"
- stop failure → error page, registry status unchanged

## Acceptance Criteria

- [ ] Settings menu shows "Stop Container"
- [ ] With nothing running, choosing it shows an info page and makes no podman stop call
- [ ] Only live-running projects are listed
- [ ] Confirmation is required; "← Go Back" aborts without side effects
- [ ] Successful stop verifies the container is gone and sets registry status to "stopped"
- [ ] Failed stop leaves registry status unchanged and returns to Settings
