# Dictation (local STT) — verification matrix

Spec: [#164](https://github.com/shredingerlabs/oc_sandbox/issues/164) · ADR: [0036](../adr/0036-dictation-stt-member-and-mic-capture.md)

Most of dictation's external behavior lives inside the OpenCode TUI, a real
browser, and a real PulseAudio/WSLg audio session — none of which a CI
environment has. ADR-0036 therefore maps these items to a **manual pass**;
this file is the checklist. Items marked **pending** need a machine with
audio hardware (and, for the TUI/plugin items, a workspace with dictation
enabled and the `oc-stt` image built).

Machine-verifiable parts (test seams, per the spec's testing decisions) are
already wired into the repo suites and listed at the bottom.

## Manual pass (pending — needs real hardware)

| #     | Item                            | How to verify                                                                                                                                                                                                                           | Result  |
| ----- | ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| 1[x]  | Image builds                    | `build-container.sh oc-stt` on a real host: whisper.cpp v1.9.5 compiles, `ggml-small.bin` passes its sha256 check, image ≈ ≤1.5 GB                                                                                                      | pending |
| 2     | Transcription round-trip        | Run a workspace with dictation on; `curl -F file=@16k-mono.wav -F response_format=json http://127.0.0.1:8091/inference` returns `{"text": …}`                                                                                           | pending |
| 3     | Sidecar page                    | Open `http://127.0.0.1:8091/` in a browser; the dictation page is served by the member (same port)                                                                                                                                      | pending |
| 4 [X] | Plugin auto-loads               | Start a dictation workspace; the TUI shows the seeded plugin (palette: "Dictate: start/stop recording", `/dictate` slash). Probes whether the shipped opencode resolves `@opencode/plugin/tui` without an npm install in the config dir | pending |
| 5     | ctrl+alt+s toggle + recording | `ctrl+alt+s` starts recording (🔴 REC footer), second press stops and transcribes; parecord flags work in the real container (16 kHz mono s16 WAV); `ctrl+shift+s` must NOT be re-adopted — on modifier-less terminals (SSH/tmux) it collapses to swallowed `0x13` and silently no-ops (verified against v2.2.6, ADR-0036 D1) | pending |
| 6     | ~5-min cap                      | Leave a recording running; it auto-stops and transcribes at ~5 minutes                                                                                                                                                                  | pending |
| 7     | Review dialog + submit          | Dialog opens (large, centered, cursor at end); edit the text; `ctrl+enter` submits it to the session (the `session.prompt` probe, D7); `esc` discards; empty text closes silently                                                       | pending |
| 8     | Home-route bootstrap            | From the home route, dictation creates a session and lands the dialog in it (like typing a prompt on the start screen)                                                                                                                  | pending |
| 9     | Send-failure retention          | Force a send failure (e.g. stop the server mid-submit): error toast, dialog stays open with the edited text, second submit retries; no auto-retry                                                                                       | pending |
| 10    | Clipboard fallback              | Simulate dialog-path failure (API drift): transcript copied via OSC52 + paste toast; probe whether the OSC52 write is honored by the real terminal; fallback-only paste-trigger probe                                                   | pending |
| 11    | Capture health check            | Start recording with no microphone / dead audio session → "no audio input" toast (not an empty transcript)                                                                                                                              | pending |
| 12    | Member unreachable              | Stop the `oc-stt` member, then dictate → "STT member unreachable" toast                                                                                                                                                                 | pending |
| 13    | Native Linux capture            | Desktop Linux with PulseAudio: socket found under `$XDG_RUNTIME_DIR/pulse`, `parecord` captures, transcription works                                                                                                                    | pending |
| 14    | WSLg capture                    | WSL2: `/mnt/wslg/PulseServer` auto-detected, no manual setup; include the WSLg privacy-toggle failure mode (mic blocked → "no audio input")                                                                                             | pending |
| 15    | Headless degradation            | Host without any audio socket: no mount, member still runs, sidecar works, TUI path toasts                                                                                                                                              | pending |
| 16    | Offline exclusion               | `--offline --use_stt`: no member, no publish, start succeeds (silent skip + note)                                                                                                                                                       | pending |
| 17    | Safari sidecar                  | Sidecar in Safari: mp4/AAC fallback path records and transcribes                                                                                                                                                                        | pending |
| 18    | Chrome/Firefox sidecar          | webm/opus recording, transcript + copy button, paste into the web composer                                                                                                                                                              | pending |
| 19    | SearXNG coexistence             | Workspace with searxng + dictation: both members run, no port conflict (8080 vs 8090)                                                                                                                                                   | pending |
| 20    | Image rows                      | Build Images lists `oc-stt` (existence marker); Delete Images lists it with in-use marking and refuses while a workspace uses it                                                                                                        | pending |
| 21    | Language pinning                | Set `stt.json` `{"language": "de"}`, restart → member args carry `--language de`, transcription honors it; the hand-edited file survives re-seeding                                                                                     | pending |
| 22    | Toggle surfaces                 | WebUI wizard row (default-checked, advanced-only; Standard hides + records ON); ws-settings row POSTs both ways (next-start apply, restart card goes stale); TUI settings row toggles                                                   | pending |
| 23    | Recordings persist nowhere      | During/after dictation: no WAV files left in the workspace container (`/tmp` clean), no server-side storage                                                                                                                             | pending |
| 24    | Card mic button                 | Workspace → Open: a running dictation-enabled workspace shows the mic left of the settings gear; the click opens the sidecar in a new tab (live port); stopped or dictation-off cards show none                                          | pending |

## Machine-verified (already in the repo suites)

| Item                                                                                                                                                                                     | Suite                                            | State              |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ | ------------------ |
| build-container `oc-stt` target argv (direct + edition after-run + refusal)                                                                                                              | `bash tests/test-build-container.sh`             | pass (12 sections) |
| start.sh member run vector, pod-create publish, STT_URL, offline skip, socket cascade (all branches), seeding idempotence (stt.json, plugins, cli.json), language pin (mount + STT_LANG) | `bash tests/test-start.sh`                       | pass               |
| Config semantics (Create ON, absent OFF), `--use_stt` argv carriers, restart-fingerprint flip, wizard preset + settings POST + matrix visibility, image rows                             | `go test ./...` (smoke + unit)                   | pass               |
| Card mic button render (running && use_stt, left of the gear) + stt-link live-port resolution and not-running refusal                                                                | `go test ./...` (webserver)                      | pass               |
| Dictation controller state machine, cap, health check, error mapping, delivery flow (dialog open/submit/discard/retain/fallback, home-route session-create)                              | `node --test tests/dictation/dictation.test.mjs` | pass (26)          |
| Sidecar pure seam: WAV encoder (header from the actual sample rate, mono downmix), codec pick (webm/opus, mp4/AAC, none)                                                                 | `node --test tests/dictation/sidecar.test.mjs`   | pass (9)           |

## Dist completeness

Everything the feature needs ships in `dist/` (the release carries `dist/`
wholesale — `scripts/create-release.sh` publishes the tree as-is):

- `dist/stt/Dockerfile`, `dist/stt/entrypoint.sh`, `dist/stt/public/{index.html,app.js}` — the oc-stt image + sidecar page
- `dist/templates/opencode/plugins/dictation/{index.ts,tui.tsx,dictation.mjs}` — the TUI plugin (seeded by start.sh)
- `dist/scripts/start.sh` — `--use_stt` engine, socket cascade, seeding, language pin
- `dist/scripts/build-container.sh` — the `oc-stt` infra target

Layout documented in `dist/README.md`. Verified 2026-11-16 (all files present
on the integration branch).
