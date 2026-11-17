# B-list parity checklist — spec #147 §14 (ticket #163 gate)

Sign-off record: every box was checked on the real machine against the
`dev_WebUI` checkout at tip `c63be2e` — pre-deletion, with `--full-tui` still
present as the parity escape hatch. The flip-then-delete completion (#162 →
#163) landed afterwards; the `--full-tui` build line below is therefore
historical.

Build & run from the repo checkout (/home/dev/project, branch `dev_WebUI`, tip `c63be2e`):

```
cd /home/dev/project
go build -o .tmp/opencode/oc-sandbox ./cmd/oc-sandbox
.tmp/opencode/oc-sandbox            # default launch: minimal launcher, webserver at launch, browser auto-opens
.tmp/opencode/oc-sandbox --full-tui # full BubbleTea TUI (hidden flag, parity escape hatch)
```

Migration note: first start of this binary on a real pre-rename install migrates it (backups/ first, `version: 2.0`).
Test against a copy of `~/.config/oc-sandbox`, not the live one, if you care about your data.

Record each box PASS/FAIL (+ a line of detail). Everything failing → report back; fixes batch into one implementer.

## Boxes

1. [x] **Signal look on all screens (dark + bright)** — cool slate/navy palette, left accent bar + tinted edition tile, count pills in sidebar tree; theme toggle in-page.

2. [x] **Display-mode segmented control** — header of every screen; switching Standard↔Advanced re-renders immediately, no reload.

3. [x] **Cards (Open Workspace grid)** — edition tile hue per edition, HIL chip whenever on (any edition), relative last-used date.

4. [x] **Creation form** — catalogs feed the two selects; inline "New…" works from an empty catalog; token input masked; image-build row when image missing.

5. [x] **Pair-link handoff** — Web access opens a fresh one-time link in a NEW tab within the click; plain-URL fallback appears when pairing fails.

6. [x] **Host-terminal opens per platform** — console/OpenCode variants open a real terminal window; on Linux check the probe (or `OC_SANDBOX_TERMINAL` override); headless/SSH → exact copyable command shown, session pending.

7. [x] **Browser auto-open / Open WebUI recovery** — tab opens at launch; Open WebUI button re-opens after closing the tab or a failed auto-open.

8. [x] **Confirm popup set** — create summary, restore config, build, delete-container/delete-image danger, update, remove-from-list, replace-credentials, `adopt` (edition mismatch switch + web re-adopt), typed `uninstall`.

9. [x] **Log-pane streaming** — during image build, create chain, update run, uninstall.

10. [x] **Update phases end-to-end** — banner on `available`; running = globally exclusive; `applied` clears banner + re-reads VERSION + "Update installed — restart oc-sandbox to apply" note + toast.

11. [x] **Server-stopped screen** — via quit (SSE push) AND via hard-kill crash path (client reconnect exhaustion) — same screen both ways.

12. [x] **Quit containers prompt** — "Stop running containers before quitting? [y/N]", default leave running, esc cancels, guarded exit (press Quit again while busy).

13. [x] **Second launch** — while instance runs: pointer message, exit 0 (`GET /healthz` probe); foreign app on 8090 → scans upward.

14. [x] **First-run setup** — window auto-opens at skills stage; status line + chip; Copy manual command; Cancel setup (partial progress preserved); headless path; pending session blocks other workspace actions with a reason.

15. [x] **Legacy-folder import + convert** — `project/`-layout folder → informational prompt → "Convert to workspace layout" → imports normally; export/restore round trip (named backup, overwrite confirm, restore behind confirm).
