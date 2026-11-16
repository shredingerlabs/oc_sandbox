# Dictation (Local STT) Specification

Speak into the prompt · per-workspace `oc-stt` pod member · `ctrl+alt+s` in the OpenCode TUI · dictation sidecar for web sessions.

Published 2026-11-13 as
[#164 — Spec: Dictation (local STT) — speak into the prompt via a per-workspace oc-stt member](https://github.com/shredingerlabs/oc_sandbox/issues/164)
from the grilling session of the same day (shared understanding confirmed by the requester).
Decision rationale: ADR-0036 (`docs/adr/0036-dictation-stt-member-and-mic-capture.md`).
Vocabulary: CONTEXT.md (dictation, STT member, dictation toggle, dictation keybind, dictation sidecar).
OpenCode facts verified against upstream v2.3.0 (2026-11-13). The body below mirrors the issue;
the issue is the live record. Mirror refreshed 2026-11-16 for the review-dialog delivery amendment.
Chord amended 2026-11-16 (requester-confirmed, agent session): dictation binds **`ctrl+alt+s`**
(verified free against the shipped opencode v2.2.6 stock defaults, and legacy-decodable:
`ctrl+shift+<letter>` chords collapse to the single swallowed control byte `0x13` on terminals
that do not report modifiers — SSH/tmux/legacy encoding, verified against v2.2.6 in a PTY rig;
`ctrl+shift+d`/`ctrl+alt+d` were taken and rejected) — the `variant.cycle` freeing is retired
and no stock keybind is touched. Stories 1–2's `ctrl+t` reads as `ctrl+alt+s` since this amendment.


When I work in a workspace I sometimes want to say a prompt instead of typing it — long descriptions, review comments, quick questions. There is no way to get spoken input into the OpenCode prompt: the sandbox container has no microphone, cloud speech services are out of the question for this project, and OpenCode ships no dictation. Everything must be typed.

## Solution

I toggle dictation from inside the OpenCode TUI with `ctrl+alt+s`: press once to start recording, press again to stop; my workspace's own local whisper server transcribes the audio, and the transcript opens in an editable review dialog — I fix what I need, press `ctrl+enter`, and the prompt is submitted to my session (`esc` discards; nothing is ever submitted without that explicit confirm; from the home route a session is created first, exactly like typing a prompt there). If the dialog route cannot deliver — plugin-API drift after an update, or a submission that fails after a retry — the transcript falls back to my clipboard with a paste toast. In a browser session, the workspace's STT member serves a small local page where I record with the browser microphone and copy the transcript into the web composer. Dictation is on by default for new workspaces, governed by a per-workspace toggle like the proxy toggle, and degrades gracefully (clear toast, audio persisted nowhere) when the machine has no microphone or the member is down.

## User Stories

1. As a workspace developer using the OpenCode TUI, I want to start a dictation recording with `ctrl+t`, so that I can speak a prompt instead of typing it.
2. As a TUI user, I want to stop the recording with the same key, so that transcription happens exactly when I finish speaking.
3. As a TUI user, I want the finished transcript to open in an editable review dialog, so that I can fix recognition errors before anything reaches my session.
4. As a TUI user, I want to submit the reviewed transcript with `ctrl+enter` and discard it with `esc`, so that sending is always my own explicit act.
5. As a TUI user, I want the dictation command discoverable in OpenCode's palette/slash UI under a clear name, so that I can find it without reading docs.
6. As a TUI user, I want the transcript to never be submitted without my explicit confirmation in the review dialog (and never auto-retried after a failure), so that I always review what I dictated before it is sent.
7. As a TUI user, I want a toast when I start a recording with no microphone available, so that dictation failure is a message, not a silence.
8. As a TUI user, I want a toast when the STT member is not reachable, so that I learn the cause instead of a generic error.
9. As a TUI user, I want recording to stop and transcribe automatically after ~5 minutes, so that runaway recordings don't eat memory.
10. As a privacy-conscious user, I want dictation audio persisted nowhere beyond the transcription request, so that my speech stays on my machine.
11. As a workspace developer in a browser (OpenCode web session), I want a local sidecar page where I can record with the browser microphone, so that I can dictate from the web UI too.
12. As a web user, I want the sidecar transcript displayed with a copy button, so that I can paste it into the web composer and review it.
13. As a web user, I want the sidecar reachable only from my own machine (loopback publish), so that no one else can reach my dictation endpoint.
14. As a web user on Safari, I want the sidecar to fall back to a supported recording codec, so that dictation works across browsers.
15. As a workspace creator, I want dictation preselected ON for new workspaces, so that I get the feature without configuring anything.
16. As a workspace creator in Standard display mode, I want hidden advanced rows to record their presets silently, so that creation stays simple while dictation is still on.
17. As a workspace creator in Advanced display mode, I want to see and change the dictation row, so that I can opt out at creation.
18. As an owner of a pre-feature workspace, I want dictation OFF until I enable it in settings, so that my next start doesn't silently grow RAM usage.
19. As a workspace owner, I want the dictation toggle in workspace settings (advanced-only, like the other option rows), so that it lives where every other per-workspace option lives.
20. As a workspace owner, I want toggle changes to apply on next container start (never live), so that container state stays predictable per the next-start apply contract.
21. As a workspace owner, I want the dictation member to start and stop with my workspace pod, so that stop/delete/restart treat it as one unit.
22. As an offline-run user, I want the STT member never started under `--offline`, so that offline runs stay fully offline.
23. As a WSL2 user, I want dictation to find my WSLg microphone automatically, so that dictation works on Windows without manual setup.
24. As a user on a headless machine, I want dictation to degrade to the sidecar path with a clear TUI toast, so that a missing desktop audio session breaks nothing.
25. As a user whose audio session died since boot, I want the first transcription attempt to surface a clear "no audio input" toast, so that I learn about broken capture immediately instead of getting an empty transcript.
26. As a workspace owner, I want to pin a transcription language in the workspace's STT settings file, so that short utterances transcribe reliably in my language.
27. As a maintainer, I want the oc-stt image listed in Build Images and Delete Images like the other infra images, so that image lifecycle stays uniform.
28. As a user rebuilding images after an update, I want the dictation plugin and settings re-seeded by the standard start/init flow, so that updates keep dictation working.
29. As a user, I want the transcription model baked into the image, so that the first dictation needs no download and works without any egress dependency.
30. As a user, I want dictation to work while SearXNG search is also enabled, so that both pod members coexist without port conflicts.
31. As a TUI user on the home route, I want dictation to create a session and open the review dialog there (like typing a prompt and pressing enter on the start screen), so that dictation works before I have opened a session.
32. As a TUI user whose transcript submission failed, I want the review dialog to stay open with my edited text and an error toast, so that a transient failure never loses a dictation.
33. As a TUI user whose dialog delivery is unavailable or terminally fails (for example after an opencode update), I want the transcript copied to my clipboard with a paste toast, so that dictation still lands through the fallback route.

## Implementation Decisions

- **STT member**: whisper.cpp built-in server as a per-workspace pod member `oc-stt-<container-id>` (image `oc-stt`, infra image outside the edition stage registry, like `oc-searxng`/`oc-proxy`), small multilingual ggml model baked into the image; an in-pod constant port that does not collide with searxng's 8080 (both members may coexist); one loopback publish (`127.0.0.1:<scan>:<STT_PORT>`) while the toggle is on — the first pod member with a host publish (web/CBM precedent). The member serves the dictation sidecar page on the same port.
- **Toggle**: `use_stt` boolean in sandbox_config.json next to `use_proxy`; carried to start.sh as a `--use_stt` flag; preselected ON at creation (both display modes record the preset; the row itself stays advanced-only — no presentation-matrix exception); absent reads OFF for pre-feature workspaces; joins the restart staleness fingerprint (ADR 0033) and applies on next start only; no management screen in v1 (member lifecycle is automatic with the pod).
- **Offline exclusion**: no member and no publish under `--offline` (SearXNG exclusion pattern, ADR 0026); validated beside the existing exclusion checks.
- **Mic capture**: start.sh detects the host PulseAudio socket (cascade: XDG-runtime pulse socket → WSLg `/mnt/wslg/PulseServer` — a start.sh variable so tests can stub it → WSLg runtime-dir fallback), bind-mounts the first existing socket into the workspace container, and sets `PULSE_SERVER=unix:<path>`; capture runs parecord into 16 kHz mono s16 WAV (whisper-ready, no conversion) — `parecord` comes from `pulseaudio-utils`, baked into the base stage of the image (common core, all editions; first live test showed the plugin's record spawn failing with "Executable not found in $PATH" without it). No socket → no mount: the member still runs (sidecar works), the TUI path degrades. The first transcription attempt doubles as the per-boot capture health check (WSLg capture is fragile by design). On SELinux distros the socket mount wants `label=disable` (Ubuntu/AppArmor unaffected).
- **TUI plugin**: a TUI-only OpenCode plugin (no server part, no npm pin) seeded into the config dir by the workspace init flow and auto-discovered by the TUI (runs unsandboxed in-process). It registers a keymap command on `ctrl+alt+s` ("Talk"; verified free against the shipped stock keybind defaults and legacy-decodable — no stock keybind is touched, no cli.json seeding; the earlier ctrl+t and ctrl+shift+s chords were retired with the requester-confirmed chord switches), toggles recording (parecord child; ~5-min auto-stop), and POSTs the WAV to the STT member via the `STT_URL` env constant (like `SEARXNG_URL`).
- **TUI delivery — review-dialog submit (primary)**: the transcript opens an editable review dialog through the plugin's dialog API (large, centered, ~half the terminal height, plain textarea, cursor at the end). Submitting with `ctrl+enter` sends the edited text through the plugin client's `session.prompt` to the target session; `esc` discards; empty text closes the dialog silently; a send failure shows an error toast and keeps the dialog open for retry (never auto-retries). Session targeting: an open session route is the target; the home route creates a session and navigates to it first (exactly like typing a prompt and pressing enter on the start screen); any other route discards with a warning toast (no composer there). Nothing is inserted into the composer — upstream has no prompt-write API — the explicit dialog submit replaces insertion.
- **TUI delivery — clipboard fallback (second option, failure-only)**: engaged only when the dialog/prompt path is unavailable or terminally fails after a retry (for example plugin-API drift after an image rebuild): the transcript is copied via the renderer's sanctioned OSC52 clipboard API and a toast says to paste into the prompt with the normal paste key. This is the originally specified path, demoted to fallback; the probe of triggering the TUI paste command programmatically applies to the fallback only.
- **Prior art**: the review-dialog submit route is the working pattern of the requester's voice-de reference plugin (host-side TUI dictation, verified on opencode 2.2.5: editable review dialog + SDK prompt submit on explicit `ctrl+enter`, home-route session bootstrap, dialog retained on send failure). Adopted as primary; verify it against then-current opencode at implementation.
- **Dictation sidecar**: static HTML/JS served by the member: `getUserMedia` + MediaRecorder → decode to 16 kHz mono WAV in-page (AudioContext resample; read back the actual sample rate for the WAV header; webm/opus with mp4/AAC fallback for Safari) → POST to the member → transcript with a copy button. Paste into the web composer stays manual (upstream loads built-in GUI plugins only in web tabs).
- **Per-workspace settings file**: `stt.json` seeded beside the workspace config (language: auto default), bind-mounted into the STT member (the ADR-0034 allowlist pattern); the member applies language server-side so both clients stay dumb. No model/language UI rows in v1.
- **Go wiring**: the config module gains the field (Create default ON, absent OFF); the args builder and its validations carry the flag; the first-run setup flow's inline args stay in parity; the restart fingerprint gains the field; wizard/settings rows follow the existing advanced-only row pattern with the on-preset preselect.
- **Image lifecycle**: the oc-stt image joins the infra-image registry (Build Images row, Delete Images use-marking) like `oc-searxng`.
- **Version posture**: the opencode install stays floating latest (ADR 0024); the breakage risk is documented in ADR-0036 and dictation is re-verified against the then-current opencode after image rebuilds. The dialog-submit route adds plugin-API surface (dialog + `session.prompt`) on top of keybind behavior — re-verification covers both routes, and the clipboard fallback exists precisely for this drift.

## Testing Decisions

Good tests here assert external behavior only: what argv a stubbed podman receives, what config files result, what HTTP endpoints a stubbed member sees, what the UI renders — never internal call graphs.

- **Bash script seam (existing; prior art `tests/test-start.sh`)**: run start.sh against a stubbed podman/ss and assert on the logged argv — the member run vector inside the pod block (`--pod`, `--name oc-stt-…`, the publish arg, no port conflict with searxng), `STT_URL` on the workspace container, the `--offline` exclusion, socket-mount/env presence and absence across the detection cascade (XDG path, stubbed WSLg path, no socket), and init-flow seeding of the settings file + plugin dir (prior art `tests/test-init-workspace.sh`).
- **Go smoke seam (existing; prior art `internal/smoke/checks_webui_proxy.go`, `checks_wssettings.go`, `checks_create.go`, discovery checks)**: fake-runner assertions for the config semantics (Create default ON, absent OFF), the args builder/validations carrying `--use_stt`, the restart fingerprint flipping stale when the toggle changes, WebUI wizard preset recording + settings POST + matrix visibility (advanced-only row; Standard hides it and silently records the preset), and oc-stt appearing in the image rows.
- **JS unit seam (the one new seam; node:test, dependency-free ESM)**: a dictation controller module with injected ports (record spawn, transcribe POST, dialog, prompt-send, session-create, clipboard, toast) owning the toggle state machine, the auto-stop cap, the first-attempt health check, the error mapping, and the delivery flow: the dialog opens with the transcript; submit invokes the prompt-send port with the edited text; `esc` sends nothing; empty text closes silently; a send failure retains the dialog and toasts without resending; the home route runs the session-create port before the dialog; a terminal dialog failure exercises the clipboard port + paste toast; the clipboard port is never touched when the dialog route succeeds. Plus the sidecar's pure WAV encoder (blob → 16 kHz mono PCM16). The plugin runs inside the TUI in-container and the sidecar in the browser — neither is CI-testable end-to-end, so both collapse to one pure boundary each.
- **Accepted as not machine-tested** (manual verification, mapped in ADR-0036): real TUI dialog rendering and keys, real submission through the plugin client, OSC52 emission through real terminals (fallback route), real PulseAudio/WSLg capture, whisper transcription quality, browser MediaRecorder behavior.

## Out of Scope

Streaming/partial preview · push-to-talk · a dictation management screen (manual member start/stop) · model/language UI rows (settings-file hand-edit only) · pinning the opencode version · upstream contributions (prompt-write API, web GUI plugin loading in browser tabs) · cloud STT · non-OpenCode clients (plain console sessions without opencode) · a user-facing delivery-route setting (the dialog is primary, the clipboard is failure-triggered only) · single-gesture paste adoption for the primary path (the paste-trigger probe is fallback-only).

## Further Notes

- Upstream v2.3.0 facts this design relies on (verified): server plugins are subscribe-only (no event publishing, no prompt-write API; the V1 `/tui/*` routes are gone); the TUI plugin part runs unsandboxed with renderer OSC52, keymap commands with `bind`, and a built-in `prompt.paste` keybind; the web client runs built-in GUI plugins only; `!` shell output becomes a timeline message and command-template shell interpolation submits directly — both unusable for composer insertion; the composer `!` shell runs outside the permission flow. Because no composer-insert API exists, the primary TUI route does not insert at all: it submits through the plugin client's `session.prompt` after explicit review (ADR-0036 amended 2026-11-16; the `session.prompt` route is unverified against v2.3.0 — probe at implementation).
- The implementer's hard constraints: the review-before-send invariant (the transcript is never submitted without the user's explicit confirm — dialog submit or manual send after paste) and the pod-as-unit ops model (ADR 0029).
- Open implementation probes (decide at build time): dialog + `session.prompt` reliability against then-current opencode (including home-route session creation); single-gesture paste trigger (fallback only); the default key of the TUI paste command (fallback toast wording); the exact in-pod STT port constant; sidecar silence-trimming; recording cap as constant or settings key.

